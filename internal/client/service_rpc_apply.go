package client

import (
	"context"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
)

// The caller holds profileWorker and the shared driver lock. Registering the
// cancellation under the acceptance lock closes the race between checking intent
// and starting network work. Providers must honor ctx and return after their
// in-flight work stops; cancellation is not evidence of successful cleanup.
func (m *ClientRPCMutations) applyProfileConnection(ctx context.Context, driver ClientRPCProfileDriver, cfg Config) error {
	m.mu.Lock()
	current := m.store.Read()
	if current.ConnectionIntent == nil || current.ConnectionIntent.DesiredState != ConnectionIntentDesiredConnected {
		m.mu.Unlock()
		return rpc.Error(connect.CodeCanceled, ipc.ErrorCode_ERROR_CODE_CANCELLED)
	}
	applyCtx, cancel := context.WithCancel(ctx)
	m.cancelApply = cancel
	m.mu.Unlock()
	defer func() {
		cancel()
		m.mu.Lock()
		m.cancelApply = nil
		m.mu.Unlock()
	}()
	err := driver.Start(applyCtx, cfg)
	if err != nil && applyCtx.Err() != nil && ctx.Err() == nil {
		return rpc.Error(connect.CodeCanceled, ipc.ErrorCode_ERROR_CODE_CANCELLED)
	}
	return err
}
