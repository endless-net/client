package tests

import (
	"fmt"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/testclient"
	"github.com/endless-net/client/internal/testcontrol"
)

// IT-28: native profile switching isolates registered network state and
// restores that profile's identity and connection intent after restart.
func TestControlPlaneNativeProfileContextSwitch(t *testing.T) {
	requireControlScenario(t)
	s := testcontrol.NewTLS(t)
	network, joinToken, err := s.AddNetwork("profile-context", "100.92.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	n := testclient.New(t, s)
	n.TrustControlTLS(s)
	n.Enroll(s, network.Name, joinToken)
	n.Start()
	runNativeControlMutation(t, n, "connect", "6b170000-0000-4000-8000-000000000001")
	defer n.Stop()
	source := n.AwaitNativeStatus(func(v *ipc.Status) bool {
		return v.NodeId != "" && v.ActiveProfileId != "" && v.GetNetwork().GetId() == network.ID &&
			v.GetStoredState().GetNodeCredentialPresent() && v.GetStoredState().GetCachedMapValid() &&
			v.ConnectionPhase == ipc.ConnectionPhase_CONNECTION_PHASE_CONNECTED
	})
	sourceNode, sourceProfile := source.NodeId, source.ActiveProfileId

	created := &ipc.CreateProfileResponse{}
	createArgs := []string{"--request-id", "6b170000-0000-4000-8000-000000000002",
		"--expected-instance-id", source.GetMetadata().GetInstanceId(),
		"--expected-revision", fmt.Sprint(source.GetMetadata().GetRevision()),
		"--display-name", "empty-context", "--control-origin", s.URL()}
	if err := n.NativeService("create-profile", created, createArgs...); err != nil {
		t.Fatal("create empty profile failed", err)
	}
	if created.Operation == nil || created.Operation.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || created.Operation.ProfileId == "" {
		t.Fatal("create profile did not complete")
	}
	emptyProfile := created.Operation.ProfileId

	selectProfile := func(id, target string, requestID string) *ipc.Operation {
		t.Helper()
		before := n.AwaitNativeStatus(func(v *ipc.Status) bool { return v.ActiveProfileId == id })
		response := &ipc.SelectProfileResponse{}
		args := append(testclient.NativeMutationArguments(requestID, before), "--profile-id", target)
		if err := n.NativeService("select-profile", response, args...); err != nil {
			t.Fatal("select profile failed", err)
		}
		if response.Operation == nil || response.Operation.Id == "" {
			t.Fatal("profile selection returned no operation")
		}
		return n.AwaitNativeOperation(response.Operation.Id)
	}
	selected := selectProfile(sourceProfile, emptyProfile, "6b170000-0000-4000-8000-000000000003")
	if selected.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || selected.GetSelection().GetSelectedId() != emptyProfile {
		t.Fatalf("empty profile selection failed: state=%d failure=%d", selected.State, selected.GetFailure().GetCode())
	}
	empty := n.AwaitNativeStatus(func(v *ipc.Status) bool {
		return v.ActiveProfileId == emptyProfile && v.NodeId == "" && v.GetNetwork().GetId() == "" &&
			!v.GetStoredState().GetNodeCredentialPresent() && !v.GetStoredState().GetCachedMapValid()
	})
	if empty.GetIntent().GetDesiredState() == ipc.DesiredState_DESIRED_STATE_CONNECTED {
		t.Fatal("empty profile inherited the source connected intent")
	}
	n.Stop()
	n.Start()
	empty = n.AwaitNativeStatus(func(v *ipc.Status) bool {
		return v.ActiveProfileId == emptyProfile && v.NodeId == "" && v.GetNetwork().GetId() == "" &&
			!v.GetStoredState().GetNodeCredentialPresent() && !v.GetStoredState().GetCachedMapValid()
	})
	if empty.GetIntent().GetDesiredState() != ipc.DesiredState_DESIRED_STATE_DISCONNECTED {
		t.Fatal("restart restored source state into the empty profile")
	}

	selected = selectProfile(emptyProfile, sourceProfile, "6b170000-0000-4000-8000-000000000004")
	if selected.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || selected.GetSelection().GetSelectedId() != sourceProfile {
		t.Fatalf("source profile restoration failed: state=%d failure=%d", selected.State, selected.GetFailure().GetCode())
	}
	restored := n.AwaitNativeStatus(func(v *ipc.Status) bool {
		return v.ActiveProfileId == sourceProfile && v.NodeId == sourceNode && v.GetNetwork().GetId() == network.ID &&
			v.GetStoredState().GetNodeCredentialPresent() && v.GetStoredState().GetCachedMapValid() &&
			v.GetIntent().GetDesiredState() == ipc.DesiredState_DESIRED_STATE_CONNECTED &&
			v.ConnectionPhase == ipc.ConnectionPhase_CONNECTION_PHASE_CONNECTED
	})
	if restored.NodeId != sourceNode {
		t.Fatal("profile restoration changed the enrolled node identity")
	}
	registrations := 0
	for _, event := range s.Events() {
		if event.Kind == "registered" {
			registrations++
		}
	}
	if registrations != 1 {
		t.Fatal("profile switching created another node registration")
	}
}
