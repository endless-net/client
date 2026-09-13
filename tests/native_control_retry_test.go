package tests

import (
	"errors"
	"testing"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/testclient"
	"google.golang.org/protobuf/proto"
)

// Only explicit Connect/Disconnect admission rejection can refresh CAS. Keep
// the caller's request ID and semantic payload unchanged; never retry an
// accepted operation, uncertain transport outcome, logout, or changed intent.
func retryNativeControlAdmission(command string, initial *ipc.Status, read func() (*ipc.Status, error), submit func(*ipc.Status) error) error {
	current := initial
	for attempt := 0; ; attempt++ {
		err := submit(current)
		if err == nil || attempt == 2 || (command != "connect" && command != "disconnect") || !testclient.IsNativeStaleState(err) {
			return err
		}
		next, readErr := read()
		if readErr != nil {
			return readErr
		}
		if current == nil || next == nil || current.NodeId == "" || current.NodeId != next.NodeId ||
			current.ActiveProfileId == "" || current.ActiveProfileId != next.ActiveProfileId ||
			current.GetMetadata().GetInstanceId() == "" || current.GetMetadata().GetInstanceId() != next.GetMetadata().GetInstanceId() ||
			next.GetMetadata().GetRevision() <= current.GetMetadata().GetRevision() ||
			current.UserDisconnected != next.UserDisconnected || !proto.Equal(current.Intent, next.Intent) || !proto.Equal(current.Network, next.Network) {
			return err
		}
		current = next
	}
}

func TestNativeControlAdmissionRetryBoundaries(t *testing.T) {
	stale := testclient.NativeServiceCommandError("connect", []byte(rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE).Error()))
	if !testclient.IsNativeStaleState(stale) {
		t.Fatal("fixture is not a canonical stale admission rejection")
	}
	for _, mode := range []string{"success", "revision", "exhausted", "uncertain", "logout", "read-error", "nil", "node", "profile", "instance", "same-revision", "regression", "intent", "disconnected", "network"} {
		t.Run(mode, func(t *testing.T) {
			initial := &ipc.Status{NodeId: "node", ActiveProfileId: "profile", Metadata: &ipc.SnapshotMetadata{InstanceId: "instance", Revision: 10}}
			command := "connect"
			if mode == "logout" {
				command = "logout"
			}
			calls, reads := 0, 0
			uncertain := errors.New("unclassified transport outcome")
			err := retryNativeControlAdmission(command, initial, func() (*ipc.Status, error) {
				reads++
				next := proto.Clone(initial).(*ipc.Status)
				next.Metadata.Revision += uint64(reads)
				switch mode {
				case "read-error":
					return nil, uncertain
				case "nil":
					return nil, nil
				case "node":
					next.NodeId = "other"
				case "profile":
					next.ActiveProfileId = "other"
				case "instance":
					next.Metadata.InstanceId = "other"
				case "same-revision":
					next.Metadata.Revision = 10
				case "regression":
					next.Metadata.Revision = 9
				case "intent":
					next.Intent = &ipc.ConnectionIntent{DesiredState: ipc.DesiredState_DESIRED_STATE_CONNECTED}
				case "disconnected":
					next.UserDisconnected = true
				case "network":
					next.Network = &ipc.Network{}
				}
				return next, nil
			}, func(status *ipc.Status) error {
				calls++
				if status.GetMetadata().GetRevision() != 9+uint64(calls) {
					t.Fatal("submission lost refreshed CAS")
				}
				if mode == "success" || mode == "revision" && calls == 2 {
					return nil
				}
				if mode == "uncertain" {
					return uncertain
				}
				return stale
			})
			wantCalls, wantReads := 1, 1
			if mode == "success" || mode == "uncertain" || mode == "logout" {
				wantReads = 0
			}
			if mode == "revision" {
				wantCalls = 2
			}
			if mode == "exhausted" {
				wantCalls, wantReads = 3, 2
			}
			if calls != wantCalls || reads != wantReads || (err == nil) != (mode == "success" || mode == "revision") {
				t.Fatalf("unexpected admission result: calls=%d reads=%d success=%t", calls, reads, err == nil)
			}
		})
	}
}
