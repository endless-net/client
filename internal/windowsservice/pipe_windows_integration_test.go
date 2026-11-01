//go:build windows && integration

package windowsservice

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/localipc"
	"go.uber.org/fx"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"google.golang.org/grpc"
)

type pipeService struct {
	clientv0.UnimplementedClientServiceServer
	config    localipc.PipeConfig
	callerSID string
}

func (s *pipeService) GetRuntimeInfo(ctx context.Context, _ *clientv0.GetRuntimeInfoRequest) (*clientv0.GetRuntimeInfoResponse, error) {
	caller, err := localipc.Caller(ctx)
	if err != nil {
		return nil, err
	}
	if caller.SID != s.callerSID {
		return nil, errors.New("wrong OS caller")
	}
	return &clientv0.GetRuntimeInfoResponse{Runtime: &clientv0.RuntimeInfo{InstanceId: s.config.InstanceID, Protocol: "endlessnet-client-ipc", IpcVersion: 0, ContractSha256: hex.EncodeToString(s.config.ContractDigest[:])}}, nil
}

// The SCM channel is in-process; the transport, gRPC handshake and OS SID checks
// use a real temporary named pipe. No installed service or private state is used.
func TestWindowsFxSCMNamedPipeStopAndListenerFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("separate real Windows IPC lifecycle fixture")
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sid := user.User.Sid.String()
	for _, failListener := range []bool{false, true} {
		t.Run(map[bool]string{false: "SCM-stop", true: "mandatory-listener-failure"}[failListener], func(t *testing.T) {
			var id [16]byte
			if _, err := rand.Read(id[:]); err != nil {
				t.Fatal(err)
			}
			pipe := localipc.PipeConfig{InstanceID: fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:]), ServerSID: sid, ContractDigest: [32]byte{7}, HandshakeTimeout: time.Second}
			cfg := Config{StartTimeout: 3 * time.Second, StopTimeout: time.Second, StatusTimeout: 100 * time.Millisecond, CheckpointInterval: 50 * time.Millisecond}
			var listener net.Listener
			var server *grpc.Server
			var stopping atomic.Bool
			served := make(chan error, 1)
			app := application(cfg, fx.Invoke(func(lc fx.Lifecycle, shutdown fx.Shutdowner) {
				lc.Append(fx.Hook{OnStart: func(ctx context.Context) error {
					var err error
					listener, err = localipc.Listen(pipe, []string{sid})
					if err != nil {
						return err
					}
					server, err = localipc.NewServer(pipe, func(context.Context) (string, error) { return "", nil }, &pipeService{config: pipe, callerSID: sid}, 65536, 16, localipc.Observation{Unary: func(ctx context.Context, request any, _ *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
						return next(ctx, request)
					}, Stream: func(server any, stream grpc.ServerStream, _ *grpc.StreamServerInfo, next grpc.StreamHandler) error {
						return next(server, stream)
					}})
					if err != nil {
						return errors.Join(err, listener.Close())
					}
					go func() {
						err := server.Serve(listener)
						served <- err
						if !stopping.Load() {
							_ = shutdown.Shutdown(fx.ExitCode(1))
						}
					}()
					// Bootstrap readiness through the actual OS-authenticated SDK.
					connection, err := localipc.Connect(ctx, pipe)
					if err != nil {
						stopping.Store(true)
						server.Stop()
						return err
					}
					if err := connection.Close(); err != nil {
						stopping.Store(true)
						server.Stop()
						return err
					}
					return nil
				}, OnStop: func(ctx context.Context) error {
					stopping.Store(true)
					drained := make(chan struct{})
					go func() { server.GracefulStop(); close(drained) }()
					select {
					case <-drained:
					case <-ctx.Done():
						server.Stop()
						<-drained
						return ctx.Err()
					}
					select {
					case err := <-served:
						if err != nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, grpc.ErrServerStopped) && !failListener {
							return err
						}
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				}})
			}))
			h, err := New(app, cfg)
			if err != nil {
				t.Fatal(err)
			}
			f := harness{commands: make(chan svc.ChangeRequest, 8), status: make(chan svc.Status, 64), done: make(chan outcome, 1)}
			go func() { specific, code := h.Execute(nil, f.commands, f.status); f.done <- outcome{specific, code} }()
			waitState(t, f, svc.Running)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			connection, err := localipc.Connect(ctx, pipe)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := connection.Close(); err != nil {
					t.Error(err)
				}
			}()
			if _, err := clientv0.NewClientServiceClient(connection).GetRuntimeInfo(ctx, &clientv0.GetRuntimeInfoRequest{}); err != nil {
				t.Fatal("real IPC unavailable after Fx readiness", err)
			}
			if failListener {
				if err := listener.Close(); err != nil {
					t.Fatal(err)
				}
				waitExit(t, f, exitDependency)
			} else {
				f.commands <- svc.ChangeRequest{Cmd: svc.Stop}
				waitExit(t, f, 0)
			}
			if !stopping.Load() {
				t.Fatal("Fx stop did not close required IPC")
			}
			retry, stop := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer stop()
			if reopened, err := localipc.Dial(retry, pipe); err == nil {
				_ = reopened.Close()
				t.Fatal("stopped pipe remained available")
			}
			existing, finish := context.WithTimeout(context.Background(), time.Second)
			defer finish()
			if _, err := clientv0.NewClientServiceClient(connection).GetRuntimeInfo(existing, &clientv0.GetRuntimeInfoRequest{}); err == nil {
				t.Fatal("existing IPC connection survived service shutdown")
			}
		})
	}
}
