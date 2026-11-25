//go:build windows

package clientapp

import (
	"context"
	"net/netip"
	"time"

	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/dataplane"
	"github.com/endless-net/client/internal/state"
)

type WindowsConnectionConfig struct {
	Verifier                                                                connectivity.VerifierConfig
	Native                                                                  dataplane.WindowsSessionConfig
	Endpoint                                                                netip.AddrPort
	EndpointTTL, RenewalInterval, RetryInterval, ProbeInterval, WriteBudget time.Duration
	MaxQueuedMaps                                                           int
	PrepareCredentials                                                      func(context.Context, string, uint64) error
	RefreshCredentials                                                      func(context.Context, string, uint64, bool, bool) error
}

// validateSettings performs no I/O and needs no runtime clock or trust provider.
func (c WindowsConnectionConfig) validateSettings() error {
	if c.Native.Validate() != nil || c.Verifier.MaxBytes <= 0 || c.Verifier.MaxPeers <= 0 || c.Verifier.MaxRules <= 0 || c.Verifier.MaxCredentialBytes <= 0 || c.Verifier.CallTimeout < time.Millisecond || c.Verifier.CallTimeout > 30*time.Second || !c.Endpoint.IsValid() || !c.Endpoint.Addr().Is4() || !c.Endpoint.Addr().IsGlobalUnicast() || c.Endpoint.Addr().IsLoopback() || c.Endpoint.Port() == 0 || c.EndpointTTL < time.Millisecond || c.EndpointTTL > 300*time.Second || c.RenewalInterval < time.Millisecond || c.RenewalInterval >= c.EndpointTTL || c.RetryInterval < time.Millisecond || c.RetryInterval > 30*time.Second || c.ProbeInterval < time.Millisecond || c.ProbeInterval > 30*time.Second || c.WriteBudget < time.Millisecond || c.WriteBudget > 10*time.Second || c.MaxQueuedMaps <= 0 {
		return ErrConnectionWorker
	}
	return nil
}

// NewWindowsConnectionFactory allocates no adapter or background worker. The
// returned factory allocates a per-generation owner only; Connect owns bounded
// cleanup/fresh authorization/native apply/announcement/path confirmation.
func NewWindowsConnectionFactory(store *state.Store, transport *connectivity.Transport, c WindowsConnectionConfig) (ConnectionFactory, error) {
	if store == nil || transport == nil || c.Verifier.Clock == nil || c.Verifier.Announcements == nil || c.Verifier.Now == nil || c.validateSettings() != nil {
		return nil, ErrConnectionWorker
	}
	cleanup, err := dataplane.NewWindowsCleanup(store, dataplane.WindowsCleanupConfig{MaxInventoryRows: c.Native.MaxInventoryRows, MaxInterfaces: c.Native.MaxInterfaces, MaxProtected: c.Native.MaxProtected, TransactionWait: c.Native.TransactionWait, PollInterval: c.Native.DADPollInterval, ReportFailure: c.Native.ReportFailure})
	if err != nil {
		return nil, err
	}
	resources, err := dataplane.NewTransportResources(c.Native.MaxPeers)
	if err != nil {
		return nil, err
	}
	f := &connectionFactory{store: store, transport: transport, config: c.shared(), stream: transport.StreamSnapshot}
	f.endpoint = func() (netip.AddrPort, error) {
		return underlayEndpoint(c.Endpoint, c.Native.MaxInventoryRows, c.Native.MaxInterfaces)
	}
	f.makeNative = func(ctx context.Context, profile string, generation uint64) (nativeConnectionSession, error) {
		return dataplane.NewWindowsSession(ctx, store, c.Verifier.Clock, profile, generation, c.Native, resources)
	}
	f.cleanup = func(ctx context.Context, profile string) error {
		value, err := store.Load(ctx)
		if err != nil {
			return err
		}
		defer clearConnectionKeys(value)
		p, ok := value.Profiles[profile]
		if !ok || p.Connection == nil {
			return state.ErrStale
		}
		j := p.Connection.Journal
		if j == nil {
			return nil
		}
		return cleanup.Run(ctx, profile, j.ID, j.Generation)
	}
	return f.newOwner, nil
}

func (c WindowsConnectionConfig) shared() connectionConfig {
	return connectionConfig{
		Verifier: c.Verifier, Endpoint: c.Endpoint, EndpointTTL: c.EndpointTTL,
		RenewalInterval: c.RenewalInterval, RetryInterval: c.RetryInterval,
		ProbeInterval: c.ProbeInterval, WriteBudget: c.WriteBudget, MaxQueuedMaps: c.MaxQueuedMaps,
		ApplyTimeout: c.Native.ApplyTimeout, ListenPort: c.Native.ListenPort,
		PrepareCredentials: c.PrepareCredentials, RefreshCredentials: c.RefreshCredentials,
		ReportFailure: c.Native.ReportFailure, ClassifyCleanup: windowsCleanupFailure,
	}
}
