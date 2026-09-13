package client

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestRPCDiagnosticsCollectsNativePartialSnapshot(t *testing.T) {
	m, peer, profile := rpcConnectFixture(t)
	s := NewClientRPCService(m, &ipc.BuildIdentity{Version: "test-build"})
	observation := ClientRPCDiagnosticsObservation{OSVersion: "test-os", Tunnel: WireGuardInspection{OK: true, Interface: "test0", MTU: 1420, ListenPort: 12345, Error: "synthetic-private-error"},
		Interfaces: []NetworkInterfaceStatus{{Name: "test0", Index: 1, MTU: 1420, Addresses: []string{"192.0.2.1"}, Error: "synthetic-private-interface-error"}}}
	calls := 0
	s.DiagnosticsProvider = func(context.Context) (ClientRPCDiagnosticsObservation, error) { calls++; return observation, nil }
	req := &ipc.GetDiagnosticsRequest{Profile: profile}
	_, err := s.diagnosticsAs(t.Context(), local.Peer{Identity: "observer"}, req)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED)
	if calls != 0 {
		t.Fatal("observer inspected local interfaces")
	}
	result, err := s.diagnosticsAs(t.Context(), peer, req)
	if err != nil {
		t.Fatal(err)
	}
	d := result.Diagnostics
	if !d.Truncated || len(d.Failures) == 0 || d.Tunnel.Ok || d.Tunnel.Failure == nil || d.Interfaces[0].Failure == nil || len(d.RecentLogs) != 1 {
		t.Fatal("missing partial/error/log semantics")
	}
	if d.Client.Version != "test-build" || d.OsName != runtime.GOOS || d.GoVersion != runtime.Version() || d.Metadata.InstanceId != m.instanceID || d.Metadata.Revision != d.Status.Metadata.Revision {
		t.Fatal("inconsistent native metadata")
	}
	wire, err := proto.Marshal(d)
	if err != nil || strings.Contains(string(wire), "synthetic-private") {
		t.Fatal("raw diagnostic error leaked", err)
	}
	observation.Interfaces[0].Addresses[0] = "changed"
	if d.Interfaces[0].Addresses[0] != "192.0.2.1" {
		t.Fatal("response aliases observation")
	}
}

func TestRPCDiagnosticsRejectsChangedContextAndInvalidProvider(t *testing.T) {
	for _, mode := range []string{"inactive", "nil-provider", "provider-error", "revision", "owner", "cancel", "oversized", "negative-mtu"} {
		t.Run(mode, func(t *testing.T) {
			m, peer, profile := rpcConnectFixture(t)
			s := NewClientRPCService(m, nil)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			s.DiagnosticsProvider = func(context.Context) (ClientRPCDiagnosticsObservation, error) {
				out := ClientRPCDiagnosticsObservation{}
				switch mode {
				case "inactive":
					t.Fatal("inactive profile inspected global engine")
				case "provider-error":
					return out, errors.New("private provider detail")
				case "revision", "owner":
					if err := m.store.Update(func(cfg *Config) error {
						if mode == "owner" {
							cfg.LocalOwnerID = "replacement"
						} else {
							cfg.RPCState.Revision++
						}
						return nil
					}); err != nil {
						t.Fatal(err)
					}
				case "cancel":
					cancel()
				case "oversized":
					out.Interfaces = make([]NetworkInterfaceStatus, 257)
				case "negative-mtu":
					out.Tunnel.MTU = -1
				}
				return out, nil
			}
			if mode == "nil-provider" {
				s.DiagnosticsProvider = nil
			}
			if mode == "inactive" {
				if err := m.store.Update(func(cfg *Config) error { cfg.RPCState.ActiveProfileID = ""; return nil }); err != nil {
					t.Fatal(err)
				}
			}
			result, err := s.diagnosticsAs(ctx, peer, &ipc.GetDiagnosticsRequest{Profile: profile})
			if result != nil {
				t.Fatal("invalid diagnostic snapshot returned")
			}
			code := ipc.ErrorCode_ERROR_CODE_UNAVAILABLE
			switch mode {
			case "inactive", "nil-provider":
				code = ipc.ErrorCode_ERROR_CODE_UNSUPPORTED
			case "revision":
				code = ipc.ErrorCode_ERROR_CODE_STALE_STATE
			case "owner":
				code = ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED
			case "oversized", "negative-mtu":
				code = ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED
			case "cancel":
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				return
			}
			assertRPCFailure(t, err, code)
		})
	}
}
