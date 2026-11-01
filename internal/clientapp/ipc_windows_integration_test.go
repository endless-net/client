//go:build windows && integration

package clientapp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/localipc"
	"go.uber.org/fx"
	"golang.org/x/sys/windows"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type bootstrapService struct {
	clientv0.UnimplementedClientServiceServer
	config           localipc.PipeConfig
	wrong            bool
	entered, release chan struct{}
	calls            atomic.Int32
	blockAt          int32
}

func (s *bootstrapService) GetRuntimeInfo(ctx context.Context, _ *clientv0.GetRuntimeInfoRequest) (*clientv0.GetRuntimeInfoResponse, error) {
	p, err := localipc.Caller(ctx)
	if err != nil || p.SID != s.config.ServerSID {
		return nil, ErrIPC
	}
	blockAt := s.blockAt
	if blockAt == 0 {
		blockAt = 2
	}
	if s.calls.Add(1) == blockAt && s.entered != nil {
		close(s.entered)
		<-s.release // Intentionally ignore cancellation to test forced stop.
	}
	digest := s.config.ContractDigest
	if s.wrong {
		digest[0] ^= 1
	}
	return &clientv0.GetRuntimeInfoResponse{Runtime: &clientv0.RuntimeInfo{InstanceId: s.config.InstanceID, Protocol: "endlessnet-client-ipc", IpcVersion: 0, ContractSha256: hex.EncodeToString(digest[:])}}, nil
}

func TestIPCConcurrentRPCsAreBoundedOnOneConnection(t *testing.T) {
	if testing.Short() {
		t.Skip("separate Windows HTTP2 concurrency fixture")
	}
	config := pipeConfig(t)
	service := &bootstrapService{config: config, entered: make(chan struct{}), release: make(chan struct{}), blockAt: 3}
	var once sync.Once
	release := func() { once.Do(func() { close(service.release) }) }
	defer release()
	h := &hooks{}
	_, err := NewIPC(h, testSignals(t), config, []string{config.ServerSID}, func(context.Context) (string, error) { return "", nil }, service, 65536, 4, 1, 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.hook.OnStart(budget(t)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		release()
		if err := h.hook.OnStop(budget(t)); err != nil {
			t.Error(err)
		}
	})
	connection, err := localipc.Connect(budget(t), config)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := connection.Close(); err != nil {
			t.Error(err)
		}
	}()
	client := clientv0.NewClientServiceClient(connection)
	first := make(chan error, 1)
	go func() { _, err := client.GetRuntimeInfo(budget(t), &clientv0.GetRuntimeInfoRequest{}); first <- err }()
	select {
	case <-service.entered:
	case <-budget(t).Done():
		t.Fatal("first request did not enter")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := client.GetRuntimeInfo(ctx, &clientv0.GetRuntimeInfoRequest{}); status.Code(err) != codes.DeadlineExceeded {
		t.Fatal("excess request not bounded by deadline", err)
	}
	if service.calls.Load() != 3 {
		t.Fatal("concurrent request reached handler beyond explicit stream limit")
	}
	release()
	select {
	case err := <-first:
		if err != nil {
			t.Fatal(err)
		}
	case <-budget(t).Done():
		t.Fatal("first request did not finish")
	}
	if _, err := client.GetRuntimeInfo(budget(t), &clientv0.GetRuntimeInfoRequest{}); err != nil {
		t.Fatal("stream slot not released", err)
	}
}

func pipeConfig(t *testing.T) localipc.PipeConfig {
	t.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		t.Fatal(err)
	}
	return localipc.PipeConfig{InstanceID: fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:]), ServerSID: user.User.Sid.String(), ContractDigest: [32]byte{7}, HandshakeTimeout: time.Second}
}

func TestIPCApplicationLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("separate Windows named-pipe application fixture")
	}
	for _, mode := range []string{"clean", "listener_failure", "enrollment_failure", "wrong_bootstrap"} {
		t.Run(mode, func(t *testing.T) {
			config := pipeConfig(t)
			service := &bootstrapService{config: config, wrong: mode == "wrong_bootstrap"}
			var endpoint *IPC
			var workers *Workers
			release := make(chan struct{})
			app := fx.New(fx.NopLogger, fx.StartTimeout(time.Second), fx.StopTimeout(time.Second), fx.Invoke(func(lc fx.Lifecycle, shutdown fx.Shutdowner) error {
				var err error
				endpoint, err = NewIPC(lc, testSignals(t), config, []string{config.ServerSID}, func(context.Context) (string, error) { return "", nil }, service, 65536, 8, 16, 200*time.Millisecond)
				if err != nil {
					return err
				}
				run := func(ctx context.Context) error {
					select {
					case <-release:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				workers, err = NewWorkers(lc, shutdown, testSignals(t), endpoint.Run, run)
				return err
			}))
			if err := app.Err(); err != nil {
				t.Fatal(err)
			}
			err := app.Start(budget(t))
			if mode == "wrong_bootstrap" {
				if err == nil || workers.Healthy() {
					t.Fatal("wrong descriptor admitted startup")
				}
				assertPipeClosed(t, config)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !workers.Healthy() {
				t.Fatal("workers not started")
			}
			connection, err := localipc.Connect(budget(t), config)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := connection.Close(); err != nil {
					t.Error(err)
				}
			}()
			if mode != "clean" {
				shutdown := app.Wait()
				if mode == "listener_failure" {
					if err := endpoint.listener.Close(); err != nil {
						t.Fatal(err)
					}
				} else {
					close(release)
				}
				select {
				case signal := <-shutdown:
					if signal.ExitCode != 1 {
						t.Fatal(signal)
					}
				case <-budget(t).Done():
					t.Fatal("no mandatory failure shutdown")
				}
				if workers.Healthy() {
					t.Fatal("failure kept health")
				}
			}
			err = app.Stop(budget(t))
			if mode == "clean" && err != nil {
				t.Fatal(err)
			}
			if mode != "clean" && !errors.Is(err, ErrWorkers) {
				t.Fatal(err)
			}
			assertPipeClosed(t, config)
			if _, err := clientv0.NewClientServiceClient(connection).GetRuntimeInfo(budget(t), &clientv0.GetRuntimeInfoRequest{}); err == nil {
				t.Fatal("existing connection survived stop")
			}
		})
	}
}

func assertPipeClosed(t *testing.T, config localipc.PipeConfig) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if connection, err := localipc.Dial(ctx, config); err == nil {
		_ = connection.Close()
		t.Fatal("pipe still admits connections")
	}
}

func TestIPCForcedStopDoesNotClaimCleanDrain(t *testing.T) {
	if testing.Short() {
		t.Skip("separate Windows named-pipe drain fixture")
	}
	config := pipeConfig(t)
	service := &bootstrapService{config: config, entered: make(chan struct{}), release: make(chan struct{})}
	h := &hooks{}
	endpoint, err := NewIPC(h, testSignals(t), config, []string{config.ServerSID}, func(context.Context) (string, error) { return "", nil }, service, 65536, 8, 16, 30*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.hook.OnStart(budget(t)); err != nil {
		t.Fatal(err)
	}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(service.release) }) }
	defer release()
	// Dial via SDK credentials, then use the already bootstrapped channel's
	// next call as an in-flight RPC. Connect itself invokes the blocking call.
	called := make(chan error, 1)
	go func() {
		c, err := localipc.Connect(budget(t), config)
		if c != nil {
			_ = c.Close()
		}
		called <- err
	}()
	select {
	case <-service.entered:
	case <-budget(t).Done():
		t.Fatal("RPC did not enter")
	}
	if err := h.hook.OnStop(budget(t)); !errors.Is(err, ErrIPC) {
		t.Fatal("forced stop reported clean", err)
	}
	select {
	case err := <-called:
		if err == nil {
			t.Fatal("blocked RPC survived forced stop")
		}
	case <-budget(t).Done():
		t.Fatal("client not cancelled")
	}
	assertPipeClosed(t, config)
	// A repeated stop must retain the timeout failure after cleanup completes.
	if err := h.hook.OnStop(budget(t)); !errors.Is(err, ErrIPC) {
		t.Fatal(err)
	}
	release()
	select {
	case <-endpoint.closed:
	case <-budget(t).Done():
		t.Fatal("cleanup did not finish after handler released")
	}
}

func TestIPCConnectionLimitPreservesAuthenticatedBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("separate Windows named-pipe connection fixture")
	}
	config := pipeConfig(t)
	h := &hooks{}
	endpoint, err := NewIPC(h, testSignals(t), config, []string{config.ServerSID}, func(context.Context) (string, error) { return "", nil }, &bootstrapService{config: config}, 65536, 1, 16, 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.hook.OnStart(budget(t)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := h.hook.OnStop(budget(t)); err != nil {
			t.Error(err)
		}
	})
	first, err := localipc.Connect(budget(t), config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if extra, err := localipc.Connect(ctx, config); err == nil {
		_ = extra.Close()
		t.Fatal("connection limit exceeded")
	}
	endpoint.listener.mu.Lock()
	count := len(endpoint.listener.connections)
	endpoint.listener.mu.Unlock()
	if count != 1 {
		t.Fatalf("tracked connections: %d", count)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := localipc.Connect(budget(t), config)
	if err != nil {
		t.Fatal("connection slot not released", err)
	}
	if err := next.Close(); err != nil {
		t.Fatal(err)
	}
}
