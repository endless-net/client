package clientapp

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"net/netip"
	"os"
	"runtime"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/control"
	"github.com/endless-net/client/internal/dataplane"
	"github.com/endless-net/client/internal/localapi"
	"github.com/endless-net/client/internal/localipc"
	"github.com/endless-net/client/internal/state"
	"github.com/google/uuid"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
)

const (
	linuxStateDirectory  = "/var/lib/endlessnet-client"
	linuxControlOrigin   = "https://api.endlessnet.ru"
	linuxApprovalBase    = "https://admin.endlessnet.ru"
	linuxCallTimeout     = 30 * time.Second
	linuxPollInterval    = 100 * time.Millisecond
	linuxWriteBudget     = time.Second
	linuxStartTimeout    = 3 * time.Second
	linuxStopTimeout     = 10 * time.Second
	linuxMessageBytes    = 65536
	linuxCredentialBytes = 16384
	linuxMaxConnections  = 8
	linuxMaxPeers        = 16
	linuxMaxGrants       = 32
	linuxListenPort      = 51820
)

func InitializeLinuxState(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, linuxCallTimeout)
	defer cancel()
	store, err := state.OpenLinux(ctx, linuxStateDirectory, true, linuxMessageBytes, 2*linuxMessageBytes)
	if err != nil {
		return err
	}
	if err = store.Initialize(ctx); !errors.Is(err, state.ErrStale) {
		return err
	}
	// Installer retries and future upgrades preserve the current identity.
	return store.Upgrade(ctx)
}

func ValidateLinuxState(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, linuxCallTimeout)
	defer cancel()
	store, err := state.OpenLinux(ctx, linuxStateDirectory, false, linuxMessageBytes, 2*linuxMessageBytes)
	if err != nil {
		return err
	}
	view, err := store.Load(ctx)
	clearConnectionKeys(view)
	return err
}

func RunLinux(build *clientv0.BuildIdentity, digest [32]byte, output io.Writer) error {
	if build == nil || build.Platform != clientv0.Platform_PLATFORM_LINUX || build.Architecture != runtime.GOARCH ||
		build.Version == "" || build.Commit == "" || output == nil || digest == [32]byte{} || os.Geteuid() != 0 {
		return ErrLifecycle
	}
	lock, err := state.LockLinuxRuntime(linuxStateDirectory)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	application, err := newLinuxApp(build, digest, output, linuxStateDirectory, localipc.LinuxRuntimeDirectory)
	if err != nil {
		return err
	}
	start, cancel := context.WithTimeout(context.Background(), linuxStartTimeout)
	err = application.Start(start)
	cancel()
	if err != nil {
		return err
	}
	signal := <-application.Wait()
	stop, cancel := context.WithTimeout(context.Background(), linuxStopTimeout)
	defer cancel()
	err = application.Stop(stop)
	if signal.ExitCode != 0 {
		return errors.Join(err, ErrWorkers)
	}
	return err
}

func newLinuxApp(build *clientv0.BuildIdentity, digest [32]byte, output io.Writer, stateDir, runDir string) (*fx.App, error) {
	instance, err := uuid.NewRandom()
	if err != nil {
		return nil, err
	}
	clock, err := connectivity.NewLinuxClock(instance.String())
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), linuxStartTimeout)
	defer cancel()
	store, err := state.OpenLinux(ctx, stateDir, false, linuxMessageBytes, 2*linuxMessageBytes)
	if err != nil {
		return nil, err
	}
	if err := store.Upgrade(ctx); err != nil {
		return nil, err
	}
	view, err := store.Load(ctx)
	clearConnectionKeys(view)
	if err != nil || (view.OwnerSID != "" && view.OwnerSID != "uid:0") {
		return nil, state.ErrCorrupt
	}
	installation, err := uuid.Parse(view.InstallationID)
	if err != nil {
		return nil, state.ErrCorrupt
	}
	ledger := state.NewLinuxNetworkLedger(stateDir)
	if err := dataplane.CleanLinuxNetwork(ctx, ledger); err != nil {
		return nil, err
	}
	var telemetry *Telemetry
	app := fx.New(fx.NopLogger, fx.StartTimeout(linuxStartTimeout), fx.StopTimeout(linuxStopTimeout),
		fx.Provide(func(lc fx.Lifecycle) (*Telemetry, error) {
			t, err := NewTelemetry(lc, TelemetryConfig{LogLevel: "info", LogFormat: "json", LogQueueSize: 16, MaxLogBytes: 2048, SampleRatio: 1, Export: "disabled"}, output)
			telemetry = t
			return t, err
		}),
		fx.WithLogger(func(t *Telemetry) fxevent.Logger { return t.FXLogger() }),
		fx.Invoke(func(lc fx.Lifecycle, shutdown fx.Shutdowner, t *Telemetry) error {
			return composeLinux(lc, shutdown, t, store, ledger, clock, instance, installation, runDir, build, digest)
		}))
	if err := app.Err(); err != nil {
		if telemetry != nil {
			ctx, cancel := context.WithTimeout(context.Background(), linuxStopTimeout)
			defer cancel()
			_ = telemetry.stop(ctx)
		}
		return nil, err
	}
	return app, nil
}

func composeLinux(lc fx.Lifecycle, shutdown fx.Shutdowner, t *Telemetry, store *state.Store, ledger state.LinuxNetworkLedger,
	clock connectivity.Clock, instance, installation uuid.UUID, runDir string, build *clientv0.BuildIdentity, digest [32]byte) error {
	signals := t.Signals()
	client, err := signals.newControlHTTPClient(linuxCallTimeout, linuxMaxConnections)
	if err != nil {
		return err
	}
	lc.Append(fx.Hook{OnStop: func(context.Context) error { client.CloseIdleConnections(); return nil }})
	bootstrap, err := control.NewVerifiedBootstrap(client, linuxMessageBytes, time.Now, control.EmbeddedBootstrapRoots(), store)
	if err != nil {
		return err
	}
	registration := control.RegistrationConfig{MaxRequestBytes: linuxMessageBytes, MaxResponseBytes: linuxMessageBytes, MaxCredentialBytes: linuxCredentialBytes, Now: time.Now}
	executor := control.RegistrationExecutorConfig{MaxCommitRetries: 3, RetryInterval: time.Second, WriteBudget: linuxWriteBudget, Now: time.Now}
	credentials, err := control.NewCredentials(store, client, bootstrap, registration, executor)
	if err != nil {
		return err
	}
	enrollment, sessionLogin, err := t.enrollmentWorkers(store, client, bootstrap,
		control.LoginConfig{MaxPending: 4, MaxCommitRetries: 3, RetryInterval: time.Second, WriteBudget: linuxWriteBudget, Now: time.Now}, registration, linuxApprovalBase)
	if err != nil {
		return err
	}
	transport, err := connectivity.NewTransport(client, connectivity.HTTPConfig{
		CallTimeout: linuxCallTimeout, StreamDuration: time.Minute, ReadIdleTimeout: 15 * time.Second,
		MaxRequestBytes: linuxMessageBytes, MaxResponseBytes: linuxMessageBytes, MaxRecords: 128, MaxCalls: 2, MaxStreams: 1,
	})
	if err != nil {
		return err
	}
	lc.Append(fx.Hook{OnStop: func(context.Context) error { transport.CloseIdleConnections(); return nil }})
	connections, err := linuxConnections(store, ledger, clock, instance.String(), bootstrap, credentials, transport, signals)
	if err != nil {
		return err
	}
	tcpConfig := control.TargetConfig{MaxCalls: 16, MaxAccounts: 100, MaxTargets: 500, MaxResponseBytes: linuxMessageBytes, UpstreamPageSize: 100, Now: time.Now}
	targets, err := control.NewTargets(client, bootstrap, tcpConfig)
	if err != nil {
		return err
	}
	dashboard, err := control.NewDashboard(client, bootstrap, tcpConfig)
	if err != nil {
		return err
	}
	revocations, err := control.NewRevocations(client, bootstrap, control.RevocationConfig{MaxBytes: linuxMessageBytes, MaxTargets: 500, Now: time.Now})
	if err != nil {
		return err
	}
	var pageKey [32]byte
	if _, err := rand.Read(pageKey[:]); err != nil {
		return err
	}
	service, err := localapi.New(store, enrollment, localapi.Config{
		RecentLogs: t.RecentLogs, LogPageKey: pageKey, LogResponseBytes: linuxMessageBytes,
		Revocations: revocations, Connections: connections, SessionLogins: sessionLogin, MaxSessionLogins: 4,
		ConnectionClock: clock, ConnectionObservations: connections, WatchInterval: linuxPollInterval,
		Targets: targets, Dashboard: dashboard, TargetPageKey: pageKey, Announcements: bootstrap,
		RuntimeID: instance.String(), DefaultProfileID: uuid.NewSHA1(installation, []byte("default-profile")).String(),
		DefaultControlOrigin: linuxControlOrigin, DefaultProfileDisplayName: "EndlessNet",
		ContractDigest: digest, Build: build, MaxProfiles: 4, MaxDisplayNameBytes: 128, MaxHostnameBytes: 128, Now: time.Now,
	})
	if err != nil {
		return err
	}
	ipc, err := newLinuxIPC(lc, signals, localipc.LinuxSocketConfig{
		Directory: runDir, InstanceID: instance.String(), ContractDigest: digest, HandshakeTimeout: time.Second,
	}, store.Owner, service, linuxMessageBytes, linuxMaxConnections, 16, time.Second)
	if err != nil {
		return err
	}
	_, err = NewWorkers(lc, shutdown, signals, ipc.Run, enrollment.Run, connections.Run, sessionLogin.Run)
	return err
}

func linuxConnections(store *state.Store, ledger state.LinuxNetworkLedger, clock connectivity.Clock, instance string, bootstrap *control.Bootstrap,
	credentials *control.Credentials, transport *connectivity.Transport, signals *Signals) (*ConnectionWorker, error) {
	resources, err := dataplane.NewTransportResources(linuxMaxPeers)
	if err != nil {
		return nil, err
	}
	native := dataplane.LinuxSessionConfig{
		MTU: 1420, MaxPeers: linuxMaxPeers, MaxGrants: linuxMaxGrants, MaxProtected: 32, ListenPort: linuxListenPort,
		ProbeInterval: time.Second, RetryInterval: time.Second,
		Relay: dataplane.RelayConfig{DialTimeout: 3 * time.Second, IOTimeout: 5 * time.Second, HeartbeatInterval: time.Second, Now: func() time.Time {
			sample, err := clock.Sample()
			if err != nil {
				return time.Time{}
			}
			return sample.Wall
		}},
	}
	factory := connectionFactory{
		store: store, transport: transport, stream: transport.StreamSnapshot,
		cleanup:  func(ctx context.Context, _ string) error { return dataplane.CleanLinuxNetwork(ctx, ledger) },
		endpoint: linuxUnderlayEndpoint,
		config: connectionConfig{
			Verifier:    connectivity.VerifierConfig{Announcements: bootstrap, Clock: clock, Now: time.Now, CallTimeout: linuxCallTimeout, MaxBytes: linuxMessageBytes, MaxPeers: linuxMaxPeers, MaxRules: linuxMaxGrants, MaxCredentialBytes: linuxCredentialBytes},
			EndpointTTL: 120 * time.Second, RenewalInterval: 30 * time.Second, RetryInterval: time.Second,
			ProbeInterval: time.Second, WriteBudget: linuxWriteBudget, MaxQueuedMaps: 8,
			ApplyTimeout: 20 * time.Second, ListenPort: linuxListenPort,
			PrepareCredentials: credentials.Ensure, RefreshCredentials: credentials.Refresh,
			ReportFailure: func(generation uint64, stage string, err error) {
				if err != nil && !errors.Is(err, context.Canceled) {
					failure := nativeConnectionFailure(err, connectivity.FailureApply)
					code := failure.Error()
					switch failure {
					case connectivity.FailureControl:
						code = "control_unavailable"
					case connectivity.FailureTrust:
						code = "trust_required"
					}
					signals.logger.Warn("client.connection.native_failure", "component", "connection", "generation", generation,
						"operation", stage, "error_code", code)
				}
			},
		},
		makeNative: func(ctx context.Context, profile string, generation uint64) (nativeConnectionSession, error) {
			return dataplane.NewLinuxSession(ctx, store, ledger, clock, profile, generation, native, resources)
		},
	}
	return NewConnectionWorker(store, factory.newOwner, ConnectionWorkerConfig{
		Clock: clock, RuntimeID: instance, ReadTimeout: linuxWriteBudget, PollInterval: linuxPollInterval,
		CleanupTimeout: 5 * time.Second, RefreshSession: credentials.RefreshSession, SessionRetryInterval: time.Second, Logger: signals.logger,
	})
}

func linuxUnderlayEndpoint() (netip.AddrPort, error) {
	// UDP connect only asks the kernel to select a source address. No packet is
	// sent to the documentation address.
	socket, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 9})
	if err != nil {
		return netip.AddrPort{}, connectivity.FailurePath
	}
	defer func() { _ = socket.Close() }()
	local, ok := socket.LocalAddr().(*net.UDPAddr)
	if !ok {
		return netip.AddrPort{}, connectivity.FailurePath
	}
	address, ok := netip.AddrFromSlice(local.IP)
	address = address.Unmap()
	if !ok || !address.Is4() || !address.IsGlobalUnicast() {
		return netip.AddrPort{}, connectivity.FailurePath
	}
	return netip.AddrPortFrom(address, linuxListenPort), nil
}
