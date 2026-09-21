package client

import (
	"testing"

	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
)

// Exit cleanup needs both registered identity dimensions. Keep the generic
// connection fixture's narrower setup unchanged for unrelated domain tests.
func rpcExitFixture(t *testing.T) (*ClientRPCMutations, local.Peer, *ipc.ProfileRef) {
	t.Helper()
	m, owner, profile := rpcConnectFixture(t)
	if err := m.store.Update(func(cfg *Config) error { cfg.NetworkID = "registered-network"; return nil }); err != nil {
		t.Fatal(err)
	}
	return m, owner, profile
}
