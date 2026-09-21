package main

import (
	"context"
	"errors"
	"net"
	"runtime"
	"strings"
	"sync"

	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
)

// The native exit runtime belongs to the agent, including when local IPC is
// disabled. One supervisor joins it and the optional v0 host before engine close.
func startAgentRPC(ctx context.Context, fail context.CancelCauseFunc, opts agentIPCOptions) (func() error, *client.ClientRPCMutations, error) {
	pipe, socket := strings.TrimSpace(opts.Pipe), strings.TrimSpace(opts.UnixSocket)
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
	if engine, ok := opts.WireGuard.(*client.WireGuardEngine); ok {
		if opts.ExitRuntime != nil {
			if !opts.ExitRuntime.BoundTo(engine, opts.OperationMu, opts.ConfigStore) {
				return nil, nil, errors.New("native exit runtime differs from agent scope")
			}
		} else {
			opts.ExitRuntime, err = client.NewNativeExitRuntime(engine, opts.OperationMu, opts.ConfigStore)
			if err != nil && !errors.Is(err, client.ErrNativeExitUnsupported) {
				return nil, nil, err
			}
		}
	} else if opts.ExitRuntime != nil {
		return nil, nil, errors.New("native exit runtime requires its concrete engine")
	}
	var listener net.Listener
	if endpoint != "" {
		listener, err = local.Listen(endpoint)
		if err != nil {
			return nil, nil, err
		}
	}
	service := client.NewClientRPCService(mutations, &ipc.BuildIdentity{Version: version, Commit: commit, BuildDate: buildDate})
	failures := make(chan error, 1)
	var failureOnce sync.Once
	reportFailure := func(err error) {
		failureOnce.Do(func() {
			failures <- err
			fail(err)
		})
	}
	service.RuntimeFailure = reportFailure
	service.ServerIdentityProvider = agentRPCServerIdentity
	service.TrustRecoveryProvider = agentRPCTrustRecovery
	service.NetworksProvider = agentRPCNetworks
	service.NetworkSelectionProviders = client.ClientRPCNetworkSelectionProviders{Networks: agentRPCNetworks, Register: agentRPCRegisterNetworkTarget, Cleanup: agentRPCCleanupNetworkTarget}
	service.SessionProvider = agentRPCSession
	service.SessionRenewalProvider = agentRPCSessionRenewal()
	service.DiagnosticsProvider = agentRPCDiagnostics(opts)
	service.PeersProvider = agentRPCPeers(opts)
	service.ResourceEnforcementProvider = opts.WireGuard.TryResourceEnforcement
	hostCtx, cancel := context.WithCancel(ctx)
	var exitDone <-chan error
	if opts.ExitRuntime != nil {
		exitDone, err = service.StartNativeExitWorker(hostCtx, opts.ExitRuntime, opts.OperationMu)
		if err != nil {
			cancel()
			if listener != nil {
				_ = listener.Close()
			}
			return nil, nil, err
		}
	}
	var rpcDone <-chan error
	if listener != nil {
		result := make(chan error, 1)
		rpcDone = result
		go func() { result <- service.Serve(hostCtx, listener, agentRPCProfileDriver(opts), agentRPCEnroll) }()
	}
	done := make(chan struct{})
	var hostErr error
	go func() {
		defer close(done)
		hostErr = superviseAgentRPCRuntime(hostCtx, cancel, reportFailure, exitDone, rpcDone)
		select {
		case reported := <-failures:
			hostErr = reported
		default:
		}
	}()
	var stopOnce sync.Once
	return func() error {
		stopOnce.Do(cancel)
		<-done
		return hostErr
	}, mutations, nil
}

func superviseAgentRPCRuntime(ctx context.Context, cancel context.CancelFunc, fail context.CancelCauseFunc, exitDone, rpcDone <-chan error) error {
	var result error
	select {
	case <-ctx.Done():
	case result = <-exitDone:
		exitDone = nil
		if result == nil {
			result = errors.New("native exit worker stopped unexpectedly")
		}
	case result = <-rpcDone:
		rpcDone = nil
		if result == nil {
			result = errors.New("native RPC host stopped unexpectedly")
		}
	}
	if ctx.Err() == nil && result != nil {
		// Cancel the agent too, so lifecycle releases a suspended effect lock
		// before the supervisor waits for native workers to checkpoint and stop.
		fail(result)
	} else {
		result = nil
	}
	cancel()
	if exitDone != nil {
		<-exitDone
	}
	if rpcDone != nil {
		<-rpcDone
	}
	return result
}
