package client

import (
	"context"
	"errors"
	"net"

	"github.com/endless-net/client/clientipc/local"
)

// Serve owns listener and both durable workers. A failed worker closes admission
// and cancels its sibling; return waits for all accepted work to stop/checkpoint.
// Callers must supply a local.Listen listener, never a TCP listener.
func (s *ClientRPCService) Serve(ctx context.Context, listener net.Listener, driver ClientRPCProfileDriver, enrollment ClientRPCEnrollmentProvider) error {
	if listener == nil {
		return errors.New("client RPC listener is required")
	}
	defer func() { _ = listener.Close() }()
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
	if serverDone != nil {
		<-serverDone
	}
	if err == nil {
		return errors.New("client RPC host stopped unexpectedly")
	}
	return err
}
