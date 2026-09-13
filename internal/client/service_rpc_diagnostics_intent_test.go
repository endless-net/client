package client

import (
	"context"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestRPCDiagnosticsPreservesDisconnectedIntentDuringBusyInspection(t *testing.T) {
	m, peer, profile := rpcConnectFixture(t)
	if err := m.store.Update(func(cfg *Config) error {
		cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredDisconnected, Reason: "user_disconnect"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s := NewClientRPCService(m, nil)
	s.DiagnosticsProvider = func(context.Context) (ClientRPCDiagnosticsObservation, error) {
		return ClientRPCDiagnosticsObservation{TunnelBusy: true}, nil
	}
	response, err := s.diagnosticsAs(t.Context(), peer, &ipc.GetDiagnosticsRequest{Profile: profile})
	if err != nil {
		t.Fatal(err)
	}
	diagnostics := response.Diagnostics
	if diagnostics.Status.GetIntent().GetDesiredState() != ipc.DesiredState_DESIRED_STATE_DISCONNECTED || !diagnostics.Status.UserDisconnected || diagnostics.Status.ActiveProfileId != profile.ProfileId {
		t.Fatal("busy inspection changed disconnected intent", diagnostics.Status)
	}
	if diagnostics.Status.ConnectionPhase != ipc.ConnectionPhase_CONNECTION_PHASE_UNSPECIFIED {
		t.Fatal("missing runtime observation was fabricated")
	}
	if diagnostics.Tunnel.Ok || diagnostics.Tunnel.GetFailure().GetCode() != ipc.ErrorCode_ERROR_CODE_BUSY || !diagnostics.Truncated {
		t.Fatal("busy inspection reported healthy diagnostics")
	}
}
