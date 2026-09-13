package client

import (
	"testing"

	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestRPCOwnershipClaimRequiresFreshEventSubscription(t *testing.T) {
	m := newRPCStoreTest(t)
	peer := local.Peer{Identity: "uid:1000"}
	sub, err := m.subscribe(peer, &ipc.BuildIdentity{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.unsubscribe(sub)
	first, err := sub.next(t.Context())
	if err != nil || first.Sequence != 1 || first.GetSnapshot().GetRuntime().GetCallerAccess() != ipc.Access_ACCESS_OBSERVER {
		t.Fatal("missing opening observer snapshot")
	}
	request := rpcCreateRequest(t, m)
	request.ControlOrigin = "https://control.test"
	op, err := m.createProfileAs(peer, request)
	if err != nil {
		t.Fatal(err)
	}
	event, err := sub.next(t.Context())
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	if event != nil {
		t.Fatal("role change emitted data on the old stream")
	}
	fresh, err := m.subscribe(peer, &ipc.BuildIdentity{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.unsubscribe(fresh)
	event, err = fresh.next(t.Context())
	if err != nil || event.Sequence != 1 || event.GetSnapshot().GetRuntime().GetCallerAccess() != ipc.Access_ACCESS_OWNER || event.Metadata.Revision != op.Metadata.Revision {
		t.Fatal("fresh subscription did not capture committed owner access")
	}
}
