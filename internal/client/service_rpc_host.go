package client

import (
	"context"
	"errors"
	"net"

	"github.com/endless-net/client/clientipc/local"
)

// Serve owns listener and durable workers. A failed worker closes admission
// and cancels its sibling; return waits for all accepted work to stop/checkpoint.
// Callers must supply a local.Listen listener, never a TCP listener.
func (s *ClientRPCService) Serve(ctx context.Context, listener net.Listener, driver ClientRPCProfileDriver, enrollment ClientRPCEnrollmentProvider) error {
	if listener == nil {
		return errors.New("client RPC listener is required")
	}
	defer func() { _ = listener.Close() }()
	if s.bundleStore == nil {
		store, err := openClientRPCBundleStore(s.mutations.store.path+".bundles", s.mutations.now)
		if err != nil {
			return err
		}
		s.bundleStore = store
	}
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	profileDone, err := s.StartProfileWorker(workerCtx, driver)
	if err != nil {
		return err
	}
	enrollmentDone, err := s.StartEnrollmentWorker(workerCtx, enrollment)
	if err != nil {
		cancel()
		<-profileDone
		return err
	}
	var trustDone <-chan error
	if cfg := s.mutations.store.Read(); s.ServerIdentityProvider != nil || s.TrustRecoveryProvider != nil || (cfg.RPCState != nil && cfg.RPCState.Trust != nil) {
		trustDone, err = s.StartTrustWorker(workerCtx, driver)
		if err != nil {
			cancel()
			<-profileDone
			<-enrollmentDone
			return err
		}
	}
	bundleDone, err := s.startBundleWorker(workerCtx)
	if err != nil {
		cancel()
		<-profileDone
		<-enrollmentDone
		if trustDone != nil {
			<-trustDone
		}
		return err
	}
	var sessionDone <-chan error
	if cfg := s.mutations.store.Read(); s.SessionRenewalProvider.Renew != nil || s.SessionRenewalProvider.Poll != nil || (cfg.RPCState != nil && cfg.RPCState.SessionRenewal != nil) {
		sessionDone, err = s.StartSessionWorker(workerCtx)
		if err != nil {
			cancel()
			<-profileDone
			<-enrollmentDone
			if trustDone != nil {
				<-trustDone
			}
			<-bundleDone
			return err
		}
	}
	stopReadCapabilities := s.startReadCapabilities(workerCtx)
	defer stopReadCapabilities()
	sessionClockDone := s.mutations.startSessionClock(workerCtx)
	server := local.NewServer(s.Handler())
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Serve(listener) }()
	select {
	case <-ctx.Done():
		err = ctx.Err()
	case err = <-profileDone:
		profileDone = nil
	case err = <-enrollmentDone:
		enrollmentDone = nil
	case err = <-trustDone:
		trustDone = nil
	case err = <-bundleDone:
		bundleDone = nil
	case err = <-sessionClockDone:
		sessionClockDone = nil
	case err = <-sessionDone:
		sessionDone = nil
	case err = <-serverDone:
		serverDone = nil
	}
	cancel()
	_ = server.Close()
	if profileDone != nil {
		<-profileDone
	}
	if enrollmentDone != nil {
		<-enrollmentDone
	}
	if trustDone != nil {
		<-trustDone
	}
	if bundleDone != nil {
		<-bundleDone
	}
	if sessionClockDone != nil {
		<-sessionClockDone
	}
	if sessionDone != nil {
		<-sessionDone
	}
	if serverDone != nil {
		<-serverDone
	}
	if err == nil {
		return errors.New("client RPC host stopped unexpectedly")
	}
	return err
}
