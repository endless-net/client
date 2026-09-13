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
		// IDs are exact. Names and case-folded names do not act as aliases.
		for _, ref := range []string{initial.Network.Name, strings.ToUpper(initial.Network.Name), foreign.ID, foreign.Name, "absent-network"} {
			status := retained(before)
			requestID := nextID()
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			err := retryNativeControlAdmission("select-network", status, readStatus, func(current *ipc.Status) error {
				_, err := consumer.SelectNetwork(ctx, connect.NewRequest(&ipc.SelectNetworkRequest{Profile: &ipc.ProfileRef{ProfileId: current.ActiveProfileId},
					Mutation: &ipc.MutationContext{RequestId: requestID, ExpectedInstanceId: current.GetMetadata().GetInstanceId(), ExpectedRevision: current.GetMetadata().GetRevision()}, NetworkId: ref}))
				return err
			})
			if rpc.FailureFromError(err).GetCode() != ipc.ErrorCode_ERROR_CODE_UNSUPPORTED {
				cancel()
				t.Fatal("unsupported selection did not fail closed")
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
