package tests

import (
	"testing"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/testclient"
	"google.golang.org/protobuf/proto"
)

// HC-018: disconnect intent must survive process death while the public offline
// notification is awaiting its response, before the CLI operation completes.
func TestControlPlaneInterruptedDisconnect(t *testing.T) {
	s, n, id := nativeControlScenario(t)
	initial := n.AwaitNativeStatus(func(v *ipc.Status) bool { return v.NodeId == id && v.ActiveProfileId != "" })
	const requestID = "00000000-0000-4000-8000-000000000001"
	args := testclient.NativeMutationArguments(requestID, initial)
	entered, release := s.HoldNextOfflineResponse(id)
	defer release()
	done := make(chan error, 1)
	go func() {
		_, err := n.ServiceCommand("disconnect", args...)
		done <- err
	}()
	select {
	case <-entered:
	case <-done:
		t.Fatal("disconnect completed before the offline response boundary")
	case <-time.After(10 * time.Second):
		t.Fatal("disconnect did not reach the public offline request boundary")
	}
	select {
	case <-done:
		t.Fatal("disconnect completed while its response was held")
	default:
	}
	accepted := &ipc.GetOperationResponse{}
	if err := n.NativeService("operation", accepted, "--request-id", requestID); err != nil {
		t.Fatal(err)
	}
	operation := accepted.Operation
	if operation == nil || operation.Id == "" || operation.ProfileId != initial.ActiveProfileId || operation.Kind != ipc.OperationKind_OPERATION_KIND_DISCONNECT || operation.State != ipc.OperationState_OPERATION_STATE_RUNNING {
		t.Fatal("held disconnect did not expose its running durable operation")
	}
	n.Crash()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("interrupted disconnect incorrectly reported CLI success")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("disconnect CLI did not end after agent termination")
	}
	release()
	n.Start()
	completed := n.AwaitNativeOperation(operation.Id)
	if completed.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || completed.Kind != operation.Kind || completed.ProfileId != operation.ProfileId {
		t.Fatal("interrupted disconnect did not resume its original operation")
	}
	replay := &ipc.DisconnectResponse{}
	if err := n.NativeService("disconnect", replay, args...); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(replay.Operation, completed) {
		t.Fatal("disconnect replay changed the durable outcome")
	}
	n.AwaitNativeStatus(func(v *ipc.Status) bool {
		return v.NodeId == id && v.GetStoredState().GetNodeCredentialPresent() && v.GetStoredState().GetCachedMapValid() && v.UserDisconnected && v.GetIntent().GetDesiredState() == ipc.DesiredState_DESIRED_STATE_DISCONNECTED
	})
	runNativeControlMutation(t, n, "connect", "00000000-0000-4000-8000-000000000002")
	n.AwaitNativeStatus(func(v *ipc.Status) bool {
		return v.NodeId == id && v.GetStoredState().GetNodeCredentialPresent() && v.GetStoredState().GetCachedMapValid() && !v.UserDisconnected && v.GetIntent().GetDesiredState() == ipc.DesiredState_DESIRED_STATE_CONNECTED
	})
	created := 0
	for _, event := range s.Events() {
		if event.Kind == "registered" {
			created++
		}
	}
	if created != 1 {
		t.Fatal("interrupted disconnect recovery created another registration")
	}
}
