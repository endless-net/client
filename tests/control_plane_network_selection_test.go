package tests

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/testclient"
	"github.com/endless-net/client/internal/testcontrol"
	"google.golang.org/protobuf/proto"
)

// HC-020/HC-023: current-ID reselection and account-catalog authorization.
// Cross-network enrollment, switching and rollback require separate acceptance.
func TestControlPlaneNetworkSelectionBoundary(t *testing.T) {
	s, n, id := nativeControlScenario(t)
	initial := n.AwaitNativeStatus(func(v *ipc.Status) bool {
		return v.NodeId == id && v.GetNetwork().GetId() != "" && v.GetStoredState().GetCachedMapValid()
	})
	foreign, _, err := s.AddNetwork("foreign-network", "198.18.99.0/24")
	if err != nil {
		t.Fatal(err)
	}
	endpoint := n.Socket
	if runtime.GOOS == "windows" {
		endpoint = n.Pipe
	}
	consumer, err := local.NewClient(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	serial := 100
	nextID := func() string { serial++; return fmt.Sprintf("00000000-0000-4000-8000-%012d", serial) }
	readStatus := func() (*ipc.Status, error) {
		response := &ipc.GetStatusResponse{}
		err := n.NativeService("status", response)
		return response.Status, err
	}
	retained := func(before *ipc.Status) *ipc.Status {
		t.Helper()
		return n.AwaitNativeStatus(func(v *ipc.Status) bool {
			return v.NodeId == id && v.GetNetwork().GetId() == initial.Network.Id && v.ActiveProfileId == initial.ActiveProfileId &&
				v.UserDisconnected == before.UserDisconnected && v.GetIntent().GetDesiredState() == before.GetIntent().GetDesiredState() &&
				v.GetStoredState().GetNodeCredentialPresent() && v.GetStoredState().GetCachedMapValid()
		})
	}
	// A join token is node-scoped, not a user session for browsing an account.
	catalogDenied := func(status *ipc.Status) {
		t.Helper()
		if status.GetStoredState().GetTokenPresent() {
			t.Fatal("join-token fixture unexpectedly has a user session")
		}
		// This fixture enrolls only with a join token, without selecting a user
		// account. Status may expose the signed map's account ID; that metadata
		// is not the profile's authenticated account context for ListNetworks.
		want := ipc.ErrorCode_ERROR_CODE_NEEDS_ENROLLMENT
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		response, err := consumer.ListNetworks(ctx, connect.NewRequest(&ipc.ListNetworksRequest{Profile: &ipc.ProfileRef{ProfileId: status.ActiveProfileId}}))
		if response != nil || rpc.FailureFromError(err).GetCode() != want {
			t.Fatalf("accountless node catalog: response_present=%t failure_code=%d, want=%d", response != nil, rpc.FailureFromError(err).GetCode(), want)
		}
	}
	for _, disconnected := range []bool{false, true} {
		if disconnected {
			runNativeControlMutation(t, n, "disconnect", nextID())
		}
		before := n.AwaitNativeStatus(func(v *ipc.Status) bool { return v.NodeId == id && v.UserDisconnected == disconnected })
		catalogDenied(before)
		requestID := nextID()
		var args []string
		selected := &ipc.SelectNetworkResponse{}
		if err := retryNativeControlAdmission("select-network", before, readStatus, func(current *ipc.Status) error {
			args = append(testclient.NativeMutationArguments(requestID, current), "--network-id", initial.Network.Id)
			return n.NativeService("select-network", selected, args...)
		}); err != nil {
			t.Fatal(err)
		}
		op := selected.Operation
		if op == nil || op.Id == "" || op.Kind != ipc.OperationKind_OPERATION_KIND_SELECT_NETWORK || op.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED ||
			op.ProfileId != before.ActiveProfileId || op.GetSelection().GetSelectedId() != initial.Network.Id || op.Continuity != ipc.ConnectionContinuity_CONNECTION_CONTINUITY_PRESERVED {
			t.Fatal("current-ID selection did not preserve the selected network and connection")
		}
		retained(before)
		// A node-only join token cannot authorize a different-network selection,
		// regardless of whether the supplied string is an ID or a network name.
		for _, ref := range []string{initial.Network.Name, strings.ToUpper(initial.Network.Name), foreign.ID, foreign.Name, "absent-network"} {
			status := retained(before)
			requestID := nextID()
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			err := retryNativeControlAdmission("select-network", status, readStatus, func(current *ipc.Status) error {
				_, err := consumer.SelectNetwork(ctx, connect.NewRequest(&ipc.SelectNetworkRequest{Profile: &ipc.ProfileRef{ProfileId: current.ActiveProfileId},
					Mutation: &ipc.MutationContext{RequestId: requestID, ExpectedInstanceId: current.GetMetadata().GetInstanceId(), ExpectedRevision: current.GetMetadata().GetRevision()}, NetworkId: ref}))
				return err
			})
			if rpc.FailureFromError(err).GetCode() != ipc.ErrorCode_ERROR_CODE_NEEDS_LOGIN {
				cancel()
				t.Fatal("accountless network selection did not require a user session")
			}
			_, err = consumer.GetOperation(ctx, connect.NewRequest(&ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_RequestId{RequestId: requestID}}))
			cancel()
			if rpc.FailureFromError(err).GetCode() != ipc.ErrorCode_ERROR_CODE_NOT_FOUND {
				t.Fatal("rejected selection left an accepted operation")
			}
			retained(before)
		}
		n.Stop()
		n.Start()
		status := retained(before)
		catalogDenied(status)
		replay := &ipc.SelectNetworkResponse{}
		if err := n.NativeService("select-network", replay, args...); err != nil {
			t.Fatal(err)
		}
		if !proto.Equal(replay.Operation, op) {
			t.Fatal("selection replay after process restart changed the durable outcome")
		}
		retained(before)
	}
	registrations := 0
	for _, event := range s.Events() {
		if event.Kind == "registered" {
			registrations++
		}
	}
	if registrations != 1 {
		t.Fatal("current selection or rejected switch created another registration")
	}
}

// IT-22/US-04: a user-authorized cross-network selection registers a distinct
// node, adopts only its signed map, and keeps that context after agent restart.
func TestControlPlaneNativeCrossNetworkSelection(t *testing.T) {
	requireControlScenario(t)
	s := testcontrol.NewTLS(t)
	source, sourceJoin, err := s.AddNetwork("selection-source", "100.90.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	target, _, err := s.AddNetwork("selection-target", "100.91.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	n := testclient.New(t, s)
	n.TrustControlTLS(s)
	n.Enroll(s, source.Name, sourceJoin)
	// Join-token enrollment clears any pre-existing user session when it adopts
	// the node credential. Authenticate afterward to exercise account-scoped
	// network discovery and selection as an actual signed-in user.
	n.MustRun("login", "--config", n.Config, "--server", s.URL(), "--token", s.SessionToken(), "--map-signing-trust-file", n.TrustFile)
	n.MustRun("billing", "accounts", "--config", n.Config, "--use", "test-account")
	n.Start()
	runNativeControlMutation(t, n, "connect", "6b160000-0000-4000-8000-000000000001")
	defer n.Stop()
	sourceStatus := n.AwaitNativeStatus(func(v *ipc.Status) bool {
		return v.NodeId != "" && v.GetNetwork().GetId() == source.ID && v.GetStoredState().GetCachedMapValid() &&
			v.ConnectionPhase == ipc.ConnectionPhase_CONNECTION_PHASE_CONNECTED
	})
	sourceNode, profileID := sourceStatus.NodeId, sourceStatus.ActiveProfileId
	endpoint := n.Socket
	if runtime.GOOS == "windows" {
		endpoint = n.Pipe
	}
	consumer, err := local.NewClient(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	catalog, err := consumer.ListNetworks(ctx, connect.NewRequest(&ipc.ListNetworksRequest{Profile: &ipc.ProfileRef{ProfileId: profileID}}))
	if err != nil || catalog == nil || catalog.Msg == nil {
		t.Fatal("authenticated network catalog was unavailable", err)
	}
	selectedTarget := false
	selectedSource := false
	for _, item := range catalog.Msg.Networks {
		if item.GetAccountId() != "test-account" {
			t.Fatal("network catalog crossed account authority")
		}
		selectedTarget = selectedTarget || item.GetId() == target.ID
		selectedSource = selectedSource || item.GetId() == source.ID
	}
	if !selectedSource || !selectedTarget || catalog.Msg.SelectedNetworkId != source.ID {
		t.Fatal("catalog did not expose the authorized source and target with the source selected")
	}
	requestID := "6b160000-0000-4000-8000-000000000002"
	status := sourceStatus
	var selected *ipc.SelectNetworkResponse
	err = retryNativeControlAdmission("select-network", status, func() (*ipc.Status, error) {
		response := &ipc.GetStatusResponse{}
		if err := n.NativeService("status", response); err != nil {
			return nil, err
		}
		return response.Status, nil
	}, func(current *ipc.Status) error {
		selected = &ipc.SelectNetworkResponse{}
		args := append(testclient.NativeMutationArguments(requestID, current), "--network-id", target.ID)
		return n.NativeService("select-network", selected, args...)
	})
	if err != nil {
		t.Fatal("cross-network selection was not accepted", err)
	}
	if selected.GetOperation() == nil || selected.GetOperation().GetId() == "" ||
		selected.GetOperation().GetKind() != ipc.OperationKind_OPERATION_KIND_SELECT_NETWORK || selected.GetOperation().GetProfileId() != profileID {
		t.Fatal("cross-network selection returned the wrong operation")
	}
	operation := n.AwaitNativeOperation(selected.Operation.Id)
	if operation.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || operation.GetSelection().GetSelectedId() != target.ID {
		failure := operation.GetFailure()
		t.Fatalf("cross-network selection did not complete: state=%s failure=%s reason=%q",
			operation.State, failure.GetCode(), failure.GetReasonKey())
	}
	targetStatus := n.AwaitNativeStatus(func(v *ipc.Status) bool {
		return v.NodeId != "" && v.NodeId != sourceNode && v.ActiveProfileId == profileID && v.GetNetwork().GetId() == target.ID &&
			v.GetStoredState().GetNodeCredentialPresent() && v.GetStoredState().GetCachedMapValid() &&
			v.Agent != nil && v.Agent.SnapshotState == ipc.AgentSnapshotState_AGENT_SNAPSHOT_STATE_CURRENT &&
			v.Agent.MapRevision == v.MapRevision && v.ConnectionPhase == ipc.ConnectionPhase_CONNECTION_PHASE_CONNECTED &&
			!v.UserDisconnected && v.GetIntent().GetDesiredState() == ipc.DesiredState_DESIRED_STATE_CONNECTED
	})
	targetNode := targetStatus.NodeId
	n.Stop()
	n.Start()
	restarted := n.AwaitNativeStatus(func(v *ipc.Status) bool {
		return v.NodeId == targetNode && v.ActiveProfileId == profileID && v.GetNetwork().GetId() == target.ID &&
			v.GetStoredState().GetCachedMapValid() && v.Agent != nil && v.Agent.SnapshotState == ipc.AgentSnapshotState_AGENT_SNAPSHOT_STATE_CURRENT &&
			v.Agent.MapRevision == v.MapRevision && v.ConnectionPhase == ipc.ConnectionPhase_CONNECTION_PHASE_CONNECTED &&
			!v.UserDisconnected && v.GetIntent().GetDesiredState() == ipc.DesiredState_DESIRED_STATE_CONNECTED
	})
	if restarted.GetNetwork().GetId() != target.ID || restarted.NodeId == sourceNode {
		t.Fatal("restart restored the source network or identity")
	}
	registered := map[string]bool{}
	for _, event := range s.Events() {
		if event.Kind == "registered" {
			registered[event.NodeID] = true
		}
	}
	if len(registered) != 2 || !registered[sourceNode] || !registered[targetNode] {
		t.Fatal("cross-network registration did not preserve distinct source and target identities")
	}
}
