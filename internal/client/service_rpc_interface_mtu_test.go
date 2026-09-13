package client

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestRPCDiagnosticsPreservesOSInterfaceMTURange(t *testing.T) {
	for _, mtu := range []int64{0, 1500, 65535, 65536, 4294967295, -1, 4294967296} {
		if strconv.IntSize == 32 && mtu > 2147483647 {
			continue // The OS adapter's int cannot represent this value on 32-bit hosts.
		}
		t.Run(fmt.Sprint(mtu), func(t *testing.T) {
			m, peer, profile := rpcConnectFixture(t)
			s := NewClientRPCService(m, nil)
			s.DiagnosticsProvider = func(context.Context) (ClientRPCDiagnosticsObservation, error) {
				return ClientRPCDiagnosticsObservation{
					Tunnel:     WireGuardInspection{OK: true, MTU: 1420, ListenPort: 51820},
					Interfaces: []NetworkInterfaceStatus{{Name: "lo", Index: 1, MTU: int(mtu)}},
				}, nil
			}
			result, err := s.diagnosticsAs(t.Context(), peer, &ipc.GetDiagnosticsRequest{Profile: profile})
			if mtu < 0 || mtu > 4294967295 {
				assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED)
				if result != nil {
					t.Fatal("out-of-range OS MTU returned a wrapped/truncated value")
				}
				return
			}
			if err != nil {
				t.Fatal("representable OS interface MTU rejected", err)
			}
			if len(result.Diagnostics.Interfaces) != 1 || result.Diagnostics.Interfaces[0].Mtu != uint32(mtu) || result.Diagnostics.Tunnel.Mtu != 1420 {
				t.Fatal("OS MTU was truncated or confused with tunnel MTU")
			}
		})
	}
}
