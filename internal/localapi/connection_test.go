package localapi

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/localipc"
	"github.com/endless-net/client/internal/state"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type connectionDispatch struct {
	generation uint64
	wait       bool
	intent     state.DesiredIntent
}

type connectionFixtureClock struct {
	epoch func() string
	now   func() time.Time
}

func (c connectionFixtureClock) Sample() (connectivity.ClockSample, error) {
	now := c.now()
	uptime := time.Duration(now.UnixNano())
	return connectivity.ClockSample{Wall: now, Uptime: uptime, UpperUptime: uptime, Epoch: c.epoch()}, nil
}
func setConnectionFixtureClock(c *Controller) {
	c.config.ConnectionClock = connectionFixtureClock{epoch: func() string { return c.config.RuntimeID }, now: c.config.Now}
}

type connectionRunner struct {
	store  *state.Store
	mu     sync.Mutex
	calls  []connectionDispatch
	failed uint64
	err    error
	block  bool
}

func (r *connectionRunner) Retryable(_ string, generation uint64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.failed != 0 && r.failed == generation
}

func (r *connectionRunner) Reconcile(ctx context.Context, id string, generation uint64, wait bool) error {
	s, err := r.store.Load(ctx)
	if err != nil {
		return err
	}
	c := s.Profiles[id].Connection
	if c == nil || c.Generation != generation || c.Request == nil {
		return errors.New("dispatched before durable command")
	}
	if _, ok := ctx.Deadline(); !ok {
		return errors.New("unbounded dispatch")
	}
	r.mu.Lock()
	r.calls = append(r.calls, connectionDispatch{generation, wait, c.Intent})
	err, block := r.err, r.block
	r.mu.Unlock()
	if block {
		<-ctx.Done()
		return ctx.Err()
	}
	return err
}

func connectionFixture(t *testing.T) (*Controller, *memory, *connectionRunner, *time.Time, localipc.Principal) {
	t.Helper()
	c, b, _, now := fixture(t)
	caller := localipc.Principal{SID: "S-1-5-21-11"}
	if _, err := c.begin(context.Background(), caller, request(1)); err != nil {
		t.Fatal(err)
	}
	if err := c.store.Update(context.Background(), 2, func(s *state.State) error {
		p := s.Profiles[defaultID]
		p.Attempt.Phase = enrollment.Registered
		p.Attempt.AccountID, p.Attempt.NetworkID, p.Attempt.NodeID = "account", "network", "node"
		p.Attempt.CredentialEnds = now.Add(time.Hour)
		p.Secrets.RegistrationID = requestID
		p.Secrets.RegistrationRequest = "fixture exact registration request"
		p.Secrets.NodeCredential = "enc_fixture"
		s.Profiles[defaultID] = p
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	r := &connectionRunner{store: c.store}
	c.config.Connections = r
	setConnectionFixtureClock(c)
	return c, b, r, now, caller
}

func connectionMutation(revision uint64) *clientv0.MutationContext {
	return &clientv0.MutationContext{ExpectedInstanceId: runtimeID, ExpectedRevision: revision}
}

func TestConnectionCommandsJoinRunningAttemptAndExplicitRetryFencesGeneration(t *testing.T) {
	c, _, r, now, caller := connectionFixture(t)
	ctx := context.Background()
	before, _ := c.store.Load(ctx)
	keys := before.Profiles[defaultID].Keys
	if _, err := c.connect(ctx, caller, &clientv0.ConnectRequest{Mutation: connectionMutation(3), Async: true}); err != nil {
		t.Fatal(err)
	}
	accepted, _ := c.store.Load(ctx)
	first := *accepted.Profiles[defaultID].Connection
	*now = now.Add(time.Minute)
	if _, err := c.connect(ctx, caller, &clientv0.ConnectRequest{Mutation: connectionMutation(4)}); err != nil {
		t.Fatal(err)
	}
	joined, _ := c.store.Load(ctx)
	if joined.Revision != 4 || joined.Profiles[defaultID].Connection.Deadline != first.Deadline || len(r.calls) != 2 || r.calls[0].wait || !r.calls[1].wait || r.calls[1].generation != 1 {
		t.Fatal("duplicate renewed operation or changed async semantics")
	}
	r.failed = 1
	if _, err := c.connect(ctx, caller, &clientv0.ConnectRequest{Mutation: connectionMutation(4), Async: true}); err != nil {
		t.Fatal(err)
	}
	retried, _ := c.store.Load(ctx)
	if retried.Profiles[defaultID].Connection.Generation != 2 || retried.Profiles[defaultID].Connection.Deadline != now.Add(30*time.Second) {
		t.Fatal("explicit retry did not fence old generation")
	}
	result, err := c.disconnect(ctx, caller, &clientv0.DisconnectRequest{Mutation: connectionMutation(5)})
	if err != nil || !result.Change.Changed {
		t.Fatal(err)
	}
	stopped, _ := c.store.Load(ctx)
	p := stopped.Profiles[defaultID]
	if p.Connection.Intent != state.IntentDisconnected || p.Connection.Generation != 3 || p.Connection.Deadline != now.Add(10*time.Second) || p.Attempt.Phase != enrollment.Registered || !bytes.Equal(keys.WireGuardPrivate, p.Keys.WireGuardPrivate) || !bytes.Equal(keys.IdentityPrivate, p.Keys.IdentityPrivate) {
		t.Fatal("disconnect changed identity/registration or lost durable fence")
	}
	result, err = c.disconnect(ctx, caller, &clientv0.DisconnectRequest{Mutation: connectionMutation(6)})
	if err != nil || result.Change.Changed {
		t.Fatal("disconnect replay was not idempotent", err)
	}
	if s, _ := c.store.Load(ctx); s.Revision != 6 {
		t.Fatal("disconnect replay rewrote state")
	}
}

func TestConnectionMutationRejectsStaleForeignAndUnsupportedBeforeDispatch(t *testing.T) {
	for _, name := range []string{"revision", "instance", "foreign_admin", "no_caller", "inactive", "unregistered", "unsupported", "invalid_profile"} {
		t.Run(name, func(t *testing.T) {
			c, _, r, _, caller := connectionFixture(t)
			req := &clientv0.ConnectRequest{Mutation: connectionMutation(3), Async: true}
			want := codes.Aborted
			switch name {
			case "revision":
				req.Mutation.ExpectedRevision = 2
			case "instance":
				req.Mutation.ExpectedInstanceId = requestID
			case "foreign_admin":
				caller = localipc.Principal{SID: "S-1-5-21-99", Administrator: true}
				want = codes.PermissionDenied
			case "no_caller":
				caller = localipc.Principal{}
				want = codes.Unauthenticated
			case "inactive":
				if err := c.store.Update(context.Background(), 3, func(s *state.State) error {
					s.Profiles[requestID] = state.Profile{ID: requestID, ControlOrigin: "https://another.example"}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				req.Mutation.ExpectedRevision = 4
				req.Profile = &clientv0.ProfileRef{ProfileId: requestID}
				want = codes.FailedPrecondition
			case "unregistered":
				c, _, _, _ = fixture(t)
				if _, err := c.begin(context.Background(), caller, request(1)); err != nil {
					t.Fatal(err)
				}
				c.config.Connections = r
				setConnectionFixtureClock(c)
				req.Mutation.ExpectedRevision = 2
				want = codes.FailedPrecondition
			case "unsupported":
				c.config.Connections = nil
				want = codes.Unimplemented
			case "invalid_profile":
				req.Profile = &clientv0.ProfileRef{ProfileId: "bad"}
				want = codes.InvalidArgument
			}
			before, _ := c.store.Load(context.Background())
			if result, err := c.connect(context.Background(), caller, req); result != nil || status.Code(err) != want {
				t.Fatal("wrong rejection", err)
			}
			after, _ := c.store.Load(context.Background())
			if len(r.calls) != 0 || after.Revision != before.Revision || after.Profiles[defaultID].Connection != nil {
				t.Fatal("failed command had effects")
			}
		})
	}
}

func TestConnectionLostCommitACKDoesNotDispatchOrPermitStaleReplay(t *testing.T) {
	c, b, r, _, caller := connectionFixture(t)
	b.lostACK = true
	request := &clientv0.ConnectRequest{Mutation: connectionMutation(3), Async: true}
	if _, err := c.connect(context.Background(), caller, request); status.Code(err) != codes.Unavailable {
		t.Fatal(err)
	}
	b.lostACK = false
	if _, err := c.connect(context.Background(), caller, request); status.Code(err) != codes.Aborted {
		t.Fatal("stale replay dispatched", err)
	}
	s, _ := c.store.Load(context.Background())
	if len(r.calls) != 0 || s.Profiles[defaultID].Connection.Generation != 1 || s.Revision != 4 {
		t.Fatal("uncertain commit redispatched/reset")
	}
}

func TestConnectionWaitCancellationPreservesAcceptedIntentAndSanitizesRunnerError(t *testing.T) {
	c, _, r, _, caller := connectionFixture(t)
	r.block = true
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := c.connect(ctx, caller, &clientv0.ConnectRequest{Mutation: connectionMutation(3)}); status.Code(err) != codes.DeadlineExceeded {
		t.Fatal(err)
	}
	s, _ := c.store.Load(context.Background())
	if s.Profiles[defaultID].Connection.Intent != state.IntentConnected || len(r.calls) != 1 {
		t.Fatal("caller timeout rolled back intent")
	}
	r.block = false
	r.err = status.Error(codes.Unavailable, "secret-runner-error")
	_, err := c.connect(context.Background(), caller, &clientv0.ConnectRequest{Mutation: connectionMutation(4), Async: true})
	if status.Code(err) != codes.Unavailable || bytes.Contains([]byte(err.Error()), []byte("secret-runner-error")) {
		t.Fatal("runner details leaked", err)
	}
}

func TestConnectionConcurrentCommandsConsumeRevisionOnce(t *testing.T) {
	c, _, r, _, caller := connectionFixture(t)
	start := make(chan struct{})
	results := make(chan error, 2)
	go func() {
		<-start
		_, err := c.connect(context.Background(), caller, &clientv0.ConnectRequest{Mutation: connectionMutation(3), Async: true})
		results <- err
	}()
	go func() {
		<-start
		_, err := c.disconnect(context.Background(), caller, &clientv0.DisconnectRequest{Mutation: connectionMutation(3)})
		results <- err
	}()
	close(start)
	first, second := <-results, <-results
	if first != nil && second != nil || first == nil && second == nil || first != nil && status.Code(first) != codes.Aborted || second != nil && status.Code(second) != codes.Aborted {
		t.Fatal("concurrent commands were both accepted or not CAS fenced", first, second)
	}
	s, err := c.store.Load(context.Background())
	if err != nil || s.Revision != 4 || s.Profiles[defaultID].Connection.Generation != 1 || len(r.calls) != 1 {
		t.Fatal("concurrent command had duplicate effects", err)
	}
}

func TestConnectionStatusProjectsIntentWithoutInventingAppliedState(t *testing.T) {
	c, _, _, _, caller := connectionFixture(t)
	if _, err := c.connect(context.Background(), caller, &clientv0.ConnectRequest{Mutation: connectionMutation(3), Async: true}); err != nil {
		t.Fatal(err)
	}
	s, _ := c.store.Load(context.Background())
	result, err := c.snapshot(context.Background(), caller)
	if err != nil || result.Status.Intent.DesiredState != clientv0.DesiredState_DESIRED_STATE_CONNECTED || !result.Status.Intent.UpdatedAt.AsTime().Equal(s.Profiles[defaultID].Connection.AcceptedAt) || result.Status.ConnectionPhase != clientv0.ConnectionPhase_CONNECTION_PHASE_UNSPECIFIED || result.Status.ServiceState != clientv0.ServiceState_SERVICE_STATE_RECOVERING {
		t.Fatal("intent claimed applied state or was lost", err)
	}
	observer, err := c.snapshot(context.Background(), localipc.Principal{SID: "S-1-5-21-99", Administrator: true})
	if err != nil || observer.Status.ActiveProfileId != "" || observer.Status.Intent.DesiredState == clientv0.DesiredState_DESIRED_STATE_CONNECTED {
		t.Fatal("foreign observer received owner intent", err)
	}
}

func TestConnectionRejectsForeignClockEpochBeforeDurableIntent(t *testing.T) {
	c, _, r, _, caller := connectionFixture(t)
	c.config.ConnectionClock = connectionFixtureClock{epoch: func() string { return requestID }, now: c.config.Now}
	if _, err := c.connect(context.Background(), caller, &clientv0.ConnectRequest{Mutation: connectionMutation(3), Async: true}); status.Code(err) != codes.Unavailable {
		t.Fatal(err)
	}
	s, err := c.store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.Revision != 3 || s.Profiles[defaultID].Connection != nil || len(r.calls) != 0 {
		t.Fatal("foreign clock receipt accepted")
	}
}
