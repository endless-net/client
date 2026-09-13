package main

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync"

	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
)

// startAgentRPC opens only the native v0 transport. There is no old IPC fallback.
// A host failure cancels the agent loop; stop joins workers before engine close.
func startAgentRPC(ctx context.Context, fail context.CancelCauseFunc, opts agentIPCOptions) (func() error, *client.ClientRPCMutations, error) {
	pipe, socket := strings.TrimSpace(opts.Pipe), strings.TrimSpace(opts.UnixSocket)
	if pipe == "" && socket == "" {
		return func() error { return nil }, nil, nil
	}
	if fail == nil || opts.ConfigStore == nil || opts.OperationMu == nil || opts.WireGuard == nil {
		return nil, nil, errors.New("native agent RPC requires runtime context, store, operation lock and engine")
	}
	if (pipe != "" && socket != "") || (runtime.GOOS == "windows" && socket != "") || (runtime.GOOS != "windows" && pipe != "") {
		return nil, nil, errors.New("native agent RPC requires exactly one platform-local endpoint")
	}
	endpoint := socket
	if pipe != "" {
		endpoint = pipe
	}
	mutations, err := client.NewClientRPCMutations(opts.ConfigStore)
	if err != nil {
		return nil, nil, err
	}
	listener, err := local.Listen(endpoint)
	if err != nil {
		return nil, nil, err
	}
	service := client.NewClientRPCService(mutations, &ipc.BuildIdentity{Version: version, Commit: commit, BuildDate: buildDate})
	service.ServerIdentityProvider = agentRPCServerIdentity
	service.TrustRecoveryProvider = agentRPCTrustRecovery
	service.NetworksProvider = agentRPCNetworks
	service.DiagnosticsProvider = agentRPCDiagnostics(opts)
	service.PeersProvider = agentRPCPeers(opts)
	hostCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	var hostErr error
	go func() {
		defer close(done)
		err := service.Serve(hostCtx, listener, agentRPCProfileDriver(opts), agentRPCEnroll)
		if hostCtx.Err() == nil {
			hostErr = err
			fail(err)
		}
	}()
	var stopOnce sync.Once
	return func() error {
		stopOnce.Do(cancel)
		<-done
		return hostErr
	}, mutations, nil
}
