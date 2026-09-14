package client

import (
	"context"
	"testing"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestExitEventsAndContainmentReadShareCommittedRevision(t *testing.T) {
	m, owner, profile := rpcConnectFixture(t)
	sub, err := m.subscribe(owner, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.unsubscribe(sub)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if _, err := sub.next(ctx); err != nil {
		t.Fatal(err)
	}
	accepted, err := m.clearExitNodeAs(owner, &ipc.ClearExitNodeRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile})
	if err != nil {
		t.Fatal(err)
	}
	waitInvalidation := func(revision uint64) {
		t.Helper()
		for {
			event, err := sub.next(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if invalidated := event.GetInvalidated(); invalidated != nil {
				if invalidated.Domain != ipc.Domain_DOMAIN_EXIT_NODE || invalidated.ProfileId != profile.ProfileId || event.Metadata.Revision != revision {
					t.Fatal("exit event lost revision or profile", event)
				}
				return
			}
		}
	}
	waitInvalidation(accepted.Metadata.Revision)
	running, err := m.ReconcileOperation(accepted.Id, func(cfg *Config, op *ipc.Operation) error {
		op.State = ipc.OperationState_OPERATION_STATE_RUNNING
		markExitContainment(cfg, cfg.RPCState.ExitChange, ipc.ErrorCode_ERROR_CODE_STALE_STATE, m.now())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	waitInvalidation(running.Metadata.Revision)
	status, err := NewClientRPCService(m, nil).exitNodeAs(ctx, owner, &ipc.GetExitNodeRequest{Profile: profile})
	if err != nil {
		t.Fatal(err)
	}
	if status.Metadata.Revision != running.Metadata.Revision || status.ApplyState != ipc.ApplyState_APPLY_STATE_PENDING || status.Failure.Code != ipc.ErrorCode_ERROR_CODE_STALE_STATE || status.Failure.ReasonKey != "exit_context_changed" || status.Control.Mutation.ReasonKey != "exit_containment_pending" {
		t.Fatal("containment read lost durable cause", status)
	}
	if status.FailClosed || status.Ipv4.FailClosed || status.Ipv6.FailClosed || status.EffectiveExitNodeId != nil {
		t.Fatal("containment intent fabricated observed protection")
	}
	if status.Ipv4.Failure.Code != ipc.ErrorCode_ERROR_CODE_UNAVAILABLE || status.Ipv6.Failure.Code != ipc.ErrorCode_ERROR_CODE_UNAVAILABLE {
		t.Fatal("aggregate cause erased missing family observation")
	}
}
