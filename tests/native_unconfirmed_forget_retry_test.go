package tests

import (
	"errors"
	"testing"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestNativeUnconfirmedForgetAdmissionRetry(t *testing.T) {
	stale := rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	invalid := rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	for _, mode := range []string{"domain-rejection", "exhausted", "changed-intent", "changed-profile", "uncertain", "accepted"} {
		t.Run(mode, func(t *testing.T) {
			initial := &ipc.Status{NodeId: "node", ActiveProfileId: "profile", Metadata: &ipc.SnapshotMetadata{InstanceId: "instance", Revision: 10}}
			calls := 0
			uncertain := errors.New("transport outcome unknown")
			err := retryNativeControlAdmission("unconfirmed-local-forget", initial, func() (*ipc.Status, error) {
				next := proto.Clone(initial).(*ipc.Status)
				next.Metadata.Revision += uint64(calls)
				if mode == "changed-intent" {
					next.Intent = &ipc.ConnectionIntent{DesiredState: ipc.DesiredState_DESIRED_STATE_DISCONNECTED}
				}
				if mode == "changed-profile" {
					next.ActiveProfileId = "other"
				}
				return next, nil
			}, func(current *ipc.Status) error {
				calls++
				if current.Metadata.Revision != 9+uint64(calls) {
					t.Fatal("probe did not use refreshed revision")
				}
				if mode == "accepted" {
					return nil
				}
				if mode == "uncertain" {
					return uncertain
				}
				if mode == "domain-rejection" && calls == 2 {
					return invalid
				}
				return stale
			})
			wantCalls, wantErr := 1, error(stale)
			switch mode {
			case "domain-rejection":
				wantCalls, wantErr = 2, invalid
			case "exhausted":
				wantCalls = 3
			case "uncertain":
				wantErr = uncertain
			case "accepted":
				wantErr = nil
			}
			if calls != wantCalls || err != wantErr {
				t.Fatalf("probe retry: calls=%d want=%d, err=%v want=%v", calls, wantCalls, err, wantErr)
			}
		})
	}
}
