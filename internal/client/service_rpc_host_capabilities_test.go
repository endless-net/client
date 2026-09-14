package client

import (
	"context"
	"errors"
	"net"
	"sort"
	"sync"
	"testing"

	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
)

type rpcHostCapabilityListener struct {
	rpcHostTestListener
	observe func()
}

func (l *rpcHostCapabilityListener) Accept() (net.Conn, error) {
	l.observe()
	return l.rpcHostTestListener.Accept()
}

func TestRPCHostCapabilitiesRequireConfiguredProviders(t *testing.T) {
	for _, scenario := range []struct {
		name                     string
		peers, diagnostics, logs bool
	}{
		{"no providers", false, false, false},
		{"peers only", true, false, false},
		{"diagnostics without logs", false, true, false},
		{"logs without diagnostics", false, false, true},
		{"complete diagnostics", false, true, true},
		{"all read providers", true, true, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			m := newRPCStoreTest(t)
			s := NewClientRPCService(m, nil)
			if !scenario.logs {
				s.RecentLogsProvider = nil
			}
			if scenario.peers {
				s.PeersProvider = func(context.Context) (ClientRPCPeerObservation, error) {
					t.Error("readiness performed peer discovery")
					return ClientRPCPeerObservation{}, nil
				}
			}
			if scenario.diagnostics {
				s.DiagnosticsProvider = func(context.Context) (ClientRPCDiagnosticsObservation, error) {
					t.Error("readiness performed diagnostics")
					return ClientRPCDiagnosticsObservation{}, nil
				}
			}
			want := []ipc.Capability{ipc.Capability_CAPABILITY_ENROLLMENT, ipc.Capability_CAPABILITY_CONNECTION, ipc.Capability_CAPABILITY_LOCAL_FORGET, ipc.Capability_CAPABILITY_PROFILES, ipc.Capability_CAPABILITY_PREFERENCES, ipc.Capability_CAPABILITY_RESOURCES, ipc.Capability_CAPABILITY_SUPPORT_INFO}
			if scenario.peers {
				want = append(want, ipc.Capability_CAPABILITY_PEERS)
			}
			if scenario.diagnostics && scenario.logs {
				want = append(want, ipc.Capability_CAPABILITY_DIAGNOSTICS)
			}
			sort.Slice(want, func(i, j int) bool { return want[i] < want[j] })
			failure := errors.New("synthetic listener failure")
			observations := 0
			listener := &rpcHostCapabilityListener{rpcHostTestListener: rpcHostTestListener{err: failure}, observe: func() {
				observations++
				snapshot, err := m.snapshotAs(local.Peer{Identity: "observer"}, s.build)
				if err != nil {
					t.Error(err)
					return
				}
				got := snapshot.Runtime.Capabilities
				if len(got) != len(want) {
					t.Errorf("capabilities = %d, want %d", len(got), len(want))
					return
				}
				for i, capability := range got {
					if capability.Capability != want[i] || capability.Restriction.Availability != ipc.Availability_AVAILABILITY_AVAILABLE || capability.Platform != s.build.Platform {
						t.Error("host advertised unavailable provider family")
					}
				}
			}}
			driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
				return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_NOT_APPLICABLE, nil
			}, Start: func(context.Context, Config) error { return nil }}
			provider := func(context.Context, Config, ClientRPCEnrollmentInput, func(Config) error) (*ipc.UserAction, error) {
				return nil, nil
			}
			if err := s.Serve(t.Context(), listener, driver, provider); !errors.Is(err, failure) {
				t.Fatal("host did not propagate listener shutdown", err)
			}
			if observations != 1 {
				t.Fatal("host readiness was not observed during listener startup")
			}
			snapshot, err := m.snapshotAs(local.Peer{Identity: "observer"}, s.build)
			if err != nil || len(snapshot.Runtime.Capabilities) != 0 {
				t.Fatal("stopped host retained provider readiness", err)
			}
		})
	}
}

func TestRPCDiagnosticsWithoutBundleStorageNotAdvertised(t *testing.T) {
	m := newRPCStoreTest(t)
	s := NewClientRPCService(m, nil)
	s.DiagnosticsProvider = func(context.Context) (ClientRPCDiagnosticsObservation, error) {
		return ClientRPCDiagnosticsObservation{}, nil
	}
	_, err := s.startBundleWorker(t.Context())
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	snapshot, err := m.snapshotAs(local.Peer{Identity: "observer"}, s.build)
	if err != nil || len(snapshot.Runtime.Capabilities) != 0 {
		t.Fatal("missing bundle storage advertised complete diagnostics", err)
	}
}
