//go:build windows

package clientapp

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/control"
	"github.com/endless-net/client/internal/localapi"
	"github.com/endless-net/client/internal/localipc"
	"github.com/endless-net/client/internal/windowsservice"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"google.golang.org/protobuf/proto"
)

// WindowsRuntime owns application composition, never installation/provisioning.
// Release metadata and the exact published contract digest come from packaging.
type WindowsRuntime struct {
	App        *fx.App
	config     Config
	pipe       localipc.PipeConfig
	telemetry  *Telemetry
	state      *ProtectedState
	bootstrap  *control.Bootstrap
	http       *http.Client
	ipc        *IPC
	workers    *Workers
	controller *localapi.Controller
	connection *ConnectionWorker
}

func NewWindows(c Config, build *clientv0.BuildIdentity, digest [32]byte, output io.Writer) (*WindowsRuntime, error) {
	if c.Validate() != nil || build == nil || output == nil || digest == [32]byte{} || build.Version == "" || build.Commit == "" || build.Platform != clientv0.Platform_PLATFORM_WINDOWS || build.Architecture != runtime.GOARCH {
		return nil, ErrConfig
	}
	c.AllowedSIDs = append([]string(nil), c.AllowedSIDs...)
	build = proto.Clone(build).(*clientv0.BuildIdentity)
	var id [16]byte
	var pageKey [32]byte
	if _, err := rand.Read(id[:]); err != nil {
		return nil, ErrConfig
	}
	if _, err := rand.Read(pageKey[:]); err != nil {
		return nil, ErrConfig
	}
	id[6] = id[6]&15 | 64
	id[8] = id[8]&63 | 128
	instance := fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
	clock, err := connectivity.NewWindowsClock(instance)
	if err != nil {
		return nil, ErrConfig
	}
	r := &WindowsRuntime{config: c, pipe: localipc.PipeConfig{InstanceID: instance, ServerSID: c.ServiceSID, ContractDigest: digest, HandshakeTimeout: c.HandshakeTimeout}}
	r.App = fx.New(fx.NopLogger, fx.StartTimeout(c.StartTimeout), fx.StopTimeout(c.StopTimeout),
		fx.Provide(func(lc fx.Lifecycle) (*Telemetry, error) {
			t, err := NewTelemetry(lc, c.TelemetryConfig, output)
			if err == nil {
				r.telemetry = t
			}
			return t, err
		}),
		fx.WithLogger(func(t *Telemetry) fxevent.Logger { return t.FXLogger() }),
		fx.Invoke(func(lc fx.Lifecycle, shutdown fx.Shutdowner, t *Telemetry) error {
			var err error
			r.state, err = NewProtectedState(lc, t.Signals(), c)
			if err != nil {
				return err
			}
			r.http, err = t.Signals().NewHTTPClient(c)
			if err != nil {
				return err
			}
			lc.Append(t.Signals().hook("control_http", func(ctx context.Context) error { return ctx.Err() }, func(context.Context) error { r.http.CloseIdleConnections(); return nil }))
			bootstrap, err := control.NewVerifiedBootstrap(r.http, int64(c.MaxBootstrapBytes), time.Now, control.EmbeddedBootstrapRoots(), r.state.Store())
			if err != nil {
				return err
			}
			r.bootstrap = bootstrap
			transport, err := connectivity.NewTransport(r.http, c.connectionHTTP())
			if err != nil {
				return err
			}
			lc.Append(t.Signals().hook("control_http", func(ctx context.Context) error { return ctx.Err() }, func(context.Context) error { transport.CloseIdleConnections(); return nil }))
			factory, err := NewWindowsConnectionFactory(r.state.Store(), transport, c.connectionDriver(clock, bootstrap))
			if err != nil {
				return err
			}
			r.connection, err = NewConnectionWorker(r.state.Store(), factory, ConnectionWorkerConfig{Clock: clock, RuntimeID: instance, ReadTimeout: c.WriteBudget, PollInterval: c.PollInterval, CleanupTimeout: c.CleanupTimeout})
			if err != nil {
				return err
			}
			native, err := control.NewNative(r.http, bootstrap, c.MaxResponseBytes, c.NativeConsentPrefix, time.Now)
			if err != nil {
				return err
			}
			login, err := control.NewLoginExecutor(r.state.Store(), native, control.LoginConfig{MaxPending: c.MaxPending, MaxCommitRetries: c.MaxCommitRetries, RetryInterval: c.RetryInterval, WriteBudget: c.WriteBudget, Now: time.Now})
			if err != nil {
				return err
			}
			registration, err := control.NewRegistration(r.http, bootstrap, control.RegistrationConfig{MaxRequestBytes: c.MaxRequestBytes, MaxResponseBytes: c.MaxResponseBytes, MaxCredentialBytes: c.MaxCredentialBytes, Now: time.Now})
			if err != nil {
				return err
			}
			register, err := control.NewRegistrationExecutor(r.state.Store(), registration, control.RegistrationExecutorConfig{MaxCommitRetries: c.MaxCommitRetries, RetryInterval: c.RetryInterval, WriteBudget: c.WriteBudget, Now: time.Now})
			if err != nil {
				return err
			}
			approval, err := control.NewApproval(r.http, bootstrap, control.ApprovalConfig{RegistrationConfig: control.RegistrationConfig{MaxRequestBytes: c.MaxRequestBytes, MaxResponseBytes: c.MaxResponseBytes, MaxCredentialBytes: c.MaxCredentialBytes, Now: time.Now}, ApprovalBase: c.ApprovalBase})
			if err != nil {
				return err
			}
			approve, err := control.NewApprovalExecutor(r.state.Store(), approval, control.ApprovalExecutorConfig{MaxCommitRetries: c.MaxCommitRetries, RetryInterval: c.RetryInterval, WriteBudget: c.WriteBudget, Now: time.Now})
			if err != nil {
				return err
			}
			defaultTarget, err := control.NewDefaultTarget(r.http, bootstrap, control.RegistrationConfig{MaxRequestBytes: c.MaxRequestBytes, MaxResponseBytes: c.MaxResponseBytes, MaxCredentialBytes: c.MaxCredentialBytes, Now: time.Now})
			if err != nil {
				return err
			}
			resolve, err := control.NewDefaultTargetExecutor(r.state.Store(), defaultTarget, control.RegistrationExecutorConfig{MaxCommitRetries: c.MaxCommitRetries, RetryInterval: c.RetryInterval, WriteBudget: c.WriteBudget, Now: time.Now})
			if err != nil {
				return err
			}
			worker, err := t.NewDefaultEnrollmentWorker(login, register, approve, resolve)
			if err != nil {
				return err
			}
			targets, err := control.NewTargets(r.http, bootstrap, control.TargetConfig{MaxCalls: c.MaxQueryCalls, MaxAccounts: c.MaxAccounts, MaxTargets: c.MaxTargets, MaxResponseBytes: c.MaxResponseBytes, UpstreamPageSize: c.UpstreamPageSize, Now: time.Now})
			if err != nil {
				return err
			}
			revocations, err := control.NewRevocations(r.http, bootstrap, control.RevocationConfig{MaxBytes: min(c.MaxRequestBytes, c.MaxResponseBytes), MaxTargets: c.MaxTargets, Now: time.Now})
			if err != nil {
				return err
			}
			r.controller, err = localapi.New(r.state.Store(), worker, localapi.Config{Revocations: revocations, Connections: r.connection, ConnectionClock: clock, ConnectionObservations: r.connection, WatchInterval: c.PollInterval, Targets: targets, TargetPageKey: pageKey, Announcements: bootstrap, RuntimeID: instance, DefaultProfileID: c.DefaultProfileID, DefaultControlOrigin: c.ControlOrigin, DefaultProfileDisplayName: c.ProfileDisplayName, ContractDigest: digest, Build: build, MaxProfiles: c.MaxProfiles, MaxDisplayNameBytes: c.MaxDisplayNameBytes, MaxHostnameBytes: c.MaxHostnameBytes, Now: time.Now})
			if err != nil {
				return err
			}
			r.ipc, err = NewIPC(lc, t.Signals(), r.pipe, c.AllowedSIDs, r.state.Store().Owner, r.controller, c.MaxMessageBytes, c.MaxConnections, c.MaxConcurrentRPCs, c.DrainTimeout)
			if err != nil {
				return err
			}
			r.workers, err = NewWorkers(lc, shutdown, t.Signals(), r.ipc.Run, worker.Run, func(ctx context.Context) error {
				return r.connection.RunWindows(ctx, c.MaxInventoryRows, c.MaxInterfaces)
			})
			if err == nil && c.RuntimeInstanceFile != "" {
				lc.Append(t.Signals().hook("ipc", func(ctx context.Context) error {
					return publishRuntimeInstance(ctx, c.RuntimeInstanceFile, instance, c.StateLockRetry)
				}, func(context.Context) error {
					return removeRuntimeInstance(c.RuntimeInstanceFile, instance)
				}))
			}
			return err
		}))
	if r.App.Err() != nil {
		ctx, cancel := context.WithTimeout(context.Background(), c.StopTimeout)
		defer cancel()
		if r.telemetry != nil {
			_ = r.telemetry.stop(ctx)
		}
		return nil, ErrLifecycle
	}
	return r, nil
}

// RunSCM is an explicit service-host entry. It neither registers nor starts an
// installed service and has no console fallback.
func (r *WindowsRuntime) RunSCM() error {
	return windowsservice.Run(r.App, windowsservice.Config{StartTimeout: r.config.StartTimeout, StopTimeout: r.config.StopTimeout, StatusTimeout: r.config.StatusTimeout, CheckpointInterval: r.config.CheckpointInterval})
}
