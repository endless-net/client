package main

import (
	"errors"
	"sync"
	"testing"

	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
)

func TestRPCProfileDriverFailsClosed(t *testing.T) {
	lock := &sync.Mutex{}
	driver := agentRPCProfileDriver(agentIPCOptions{OperationMu: lock})
	if driver.Lock != lock {
		t.Fatal("driver does not share agent lock")
	}
	_, err := driver.Stop(t.Context())
	if rpc.FailureFromError(err).GetCode() != ipc.ErrorCode_ERROR_CODE_UNAVAILABLE {
		t.Fatal(err)
	}
	if err := driver.Start(t.Context(), client.Config{}); rpc.FailureFromError(err).GetCode() != ipc.ErrorCode_ERROR_CODE_UNAVAILABLE {
		t.Fatal(err)
	}
	wg := &testAgentWireGuard{}
	driver = agentRPCProfileDriver(agentIPCOptions{OperationMu: lock, WireGuard: wg})
	if err := driver.Start(t.Context(), client.Config{}); rpc.FailureFromError(err).GetCode() != ipc.ErrorCode_ERROR_CODE_NEEDS_ENROLLMENT || wg.configureCalls != 0 {
		t.Fatal("unverified map reached Configure", err)
	}
	continuity, err := driver.Stop(t.Context())
	if err != nil || continuity != ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN || wg.downCalls != 1 {
		t.Fatal("unknown inspection claimed continuity", err)
	}
	wg.down = func() (client.WireGuardApplyResult, error) {
		return client.WireGuardApplyResult{}, errors.New("private driver diagnostic")
	}
	_, err = driver.Stop(t.Context())
	if rpc.FailureFromError(err).GetCode() != ipc.ErrorCode_ERROR_CODE_APPLY_FAILED {
		t.Fatal(err)
	}
}
