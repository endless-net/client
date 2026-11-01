package control

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/state"
	sessionv1 "github.com/endless-net/identity/api/session/v1"
	identityapi "github.com/endless-net/identity/api/v1"
	"github.com/endless-net/identity/api/v1/identityapiconnect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const loginProfile = "972ad9d8-5193-40c6-b932-9bf0c0af97d8"
const loginAttempt = "993659c4-2182-4fa0-9d3e-0165b98bc661"

type memory struct {
	mu   sync.Mutex
	data []byte
	lose bool
}

func (m *memory) Exclusive(ctx context.Context, fn func(state.Slot) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return fn(m)
}
func (m *memory) Read(limit int64) ([]byte, error) {
	if m.data == nil {
		return nil, state.ErrMissing
	}
	if int64(len(m.data)) > limit {
		return nil, state.ErrCorrupt
	}
	return append([]byte(nil), m.data...), nil
}
func (m *memory) Replace(data []byte) error {
	m.data = append([]byte(nil), data...)
	if m.lose {
		m.lose = false
		return state.ErrUncertain
	}
	return nil
}

type testCodec struct{ cipher.AEAD }

func (c testCodec) Seal(data []byte) ([]byte, error) {
	nonce := make([]byte, c.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return c.AEAD.Seal(nonce, nonce, data, nil), nil
}
func (c testCodec) Open(data []byte) ([]byte, error) {
	if len(data) < c.NonceSize() {
		return nil, state.ErrCorrupt
	}
	return c.AEAD.Open(nil, data[:c.NonceSize()], data[c.NonceSize():], nil)
}

type staticAnnouncement struct{ value Announcement }

func (s staticAnnouncement) Fetch(context.Context, string) (Announcement, error) { return s.value, nil }

type nativeFixtureServer struct {
	started                                       chan struct{}
	t                                             *testing.T
	store                                         *state.Store
	memory                                        *memory
	now                                           *time.Time
	tx                                            *identityapi.NativeLoginTransaction
	starts, gets, created                         int
	lostStart, authorized, badBinding, loseResult bool
}

func (s *nativeFixtureServer) inFlight(ctx context.Context) {
	s.t.Helper()
	current, err := s.store.Load(ctx)
	if err != nil || !current.Profiles[loginProfile].RemoteInFlight || current.Profiles[loginProfile].InFlightAt.IsZero() {
		s.t.Fatal("RPC before durable send marker", err)
	}
}
func (s *nativeFixtureServer) result() *identityapi.NativeLoginTransaction {
	t := proto.Clone(s.tx).(*identityapi.NativeLoginTransaction)
	if s.badBinding {
		t.AttemptBindingSha256 = bytes.Repeat([]byte{9}, 32)
	}
	if s.authorized {
		t.State = identityapi.NativeLoginState_NATIVE_LOGIN_STATE_AUTHORIZED
		t.PollAfterSeconds = 0
		t.Outcome = &identityapi.NativeLoginTransaction_Result{Result: &identityapi.NativeLoginResult{Session: &sessionv1.UserSession{SessionId: "native-session", UserId: "native-user", State: sessionv1.UserSessionState_USER_SESSION_STATE_ACTIVE, RenewalSupported: true, ExpiresAt: timestamppb.New(s.now.Add(time.Hour))}, AccessBearer: strings.Repeat("a", 43), RenewalAuthorization: &sessionv1.SessionRenewalAuthorization{Bearer: strings.Repeat("n", 43), ExpiresAt: timestamppb.New(s.now.Add(24 * time.Hour))}}}
	}
	return t
}
func (s *nativeFixtureServer) StartNativeLogin(ctx context.Context, q *connect.Request[identityapi.StartNativeLoginRequest]) (*connect.Response[identityapi.StartNativeLoginResponse], error) {
	s.inFlight(ctx)
	s.starts++
	if s.started != nil {
		select {
		case s.started <- struct{}{}:
		default:
		}
	}
	if q.Header().Get("Authorization") != "Bearer "+strings.Repeat("r", 43) {
		s.t.Fatal("wrong Start credential")
	}
	if s.tx == nil {
		s.created++
		s.tx = &identityapi.NativeLoginTransaction{TransactionId: "ab8851d7-45e8-43bc-9b9a-a67ef3dd40fe", RequestId: q.Msg.RequestId, AttemptBindingSha256: append([]byte(nil), q.Msg.AttemptBindingSha256...), State: identityapi.NativeLoginState_NATIVE_LOGIN_STATE_PENDING, CreatedAt: timestamppb.New(*s.now), ExpiresAt: timestamppb.New(s.now.Add(600 * time.Second)), ReplayExpiresAt: timestamppb.New(s.now.Add(1320 * time.Second)), PollAfterSeconds: 2}
		s.tx.Outcome = &identityapi.NativeLoginTransaction_BrowserAction{BrowserAction: &identityapi.NativeLoginBrowserAction{VerificationUrl: "https://control.example/api/v1/auth/native-login/" + s.tx.TransactionId, ExpiresAt: s.tx.ExpiresAt}}
	}
	if s.tx.RequestId != q.Msg.RequestId || !bytes.Equal(s.tx.AttemptBindingSha256, q.Msg.AttemptBindingSha256) {
		s.t.Fatal("retry changed request binding")
	}
	if s.lostStart {
		s.lostStart = false
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("secret-backend-text"))
	}
	return connect.NewResponse(&identityapi.StartNativeLoginResponse{Transaction: s.result(), PollAuthorization: strings.Repeat("p", 43)}), nil
}
func (s *nativeFixtureServer) GetNativeLogin(ctx context.Context, q *connect.Request[identityapi.GetNativeLoginRequest]) (*connect.Response[identityapi.GetNativeLoginResponse], error) {
	s.inFlight(ctx)
	s.gets++
	if q.Header().Get("Authorization") != "Bearer "+strings.Repeat("p", 43) || q.Msg.TransactionId != s.tx.TransactionId {
		s.t.Fatal("wrong poll credential/binding")
	}
	if s.loseResult {
		s.memory.lose = true
		s.loseResult = false
	}
	return connect.NewResponse(&identityapi.GetNativeLoginResponse{Transaction: s.result()}), nil
}
func (s *nativeFixtureServer) CancelNativeLogin(context.Context, *connect.Request[identityapi.CancelNativeLoginRequest]) (*connect.Response[identityapi.CancelNativeLoginResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("not part of waiting fixture"))
}

func loginFixture(t *testing.T) (*LoginExecutor, *nativeFixtureServer, *time.Time) {
	t.Helper()
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	m := &memory{}
	block, err := aes.NewCipher(bytes.Repeat([]byte{4}, 32))
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	store, err := state.New(m, testCodec{aead}, "e0802a1d-0c1c-43cc-86c5-cbe0ec3f25e8", 1<<20, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err = store.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	announcement, err := DecodeAnnouncement("https://control.example", raw, now)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := state.GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	hash, err := keys.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := enrollment.New(enrollment.Binding{AttemptID: loginAttempt, RequestID: "dfb91920-980f-40b7-bb10-a42f5cfccbdd", ProfileID: loginProfile, Owner: "S-1-5-21-1", ControlOrigin: announcement.Origin, DeviceKeyHash: hash}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Update(ctx, 1, func(s *state.State) error {
		s.OwnerSID = attempt.Owner
		s.ActiveProfileID = loginProfile
		s.Profiles[loginProfile] = state.Profile{ID: loginProfile, Hostname: "device", ControlOrigin: attempt.ControlOrigin, Keys: keys, Attempt: &attempt, Secrets: state.RemoteSecrets{RequestProof: strings.Repeat("r", 43)}, Trust: state.ServerTrust{KeyID: announcement.KeyID, AnnouncementID: announcement.ID, Bundle: announcement.Bundle}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	server := &nativeFixtureServer{t: t, store: store, memory: m, now: &now}
	mux := http.NewServeMux()
	path, handler := identityapiconnect.NewNativeLoginServiceHandler(server)
	mux.Handle(path, handler)
	client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		v := w.Result()
		v.Request = r
		return v, nil
	})}
	native, err := NewNative(client, staticAnnouncement{announcement}, 1<<20, "/api/v1/auth/native-login/", func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	executor, err := NewLoginExecutor(store, native, LoginConfig{MaxPending: 1, MaxCommitRetries: 3, RetryInterval: time.Second, WriteBudget: time.Second, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	return executor, server, &now
}

func TestNativeLostStartPollHintAndAuthorizedCommit(t *testing.T) {
	e, server, clock := loginFixture(t)
	ctx := context.Background()
	server.lostStart = true
	due, err := e.Reconcile(ctx, loginProfile, loginAttempt)
	if err != nil {
		t.Fatal(err)
	}
	s, err := e.store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	original := s.Profiles[loginProfile]
	if !original.Attempt.Uncertain || !original.Attempt.RecoveryUntil.Equal(clock.Add(120*time.Second)) || original.Secrets.TransactionID != "" {
		t.Fatal("lost Start not preserved as uncertain")
	}
	*clock = due
	due, err = e.Reconcile(ctx, loginProfile, loginAttempt)
	if err != nil {
		t.Fatal(err)
	}
	if server.starts != 2 || server.created != 1 {
		t.Fatal("lost Start created another login")
	}
	if _, err = e.Reconcile(ctx, loginProfile, loginAttempt); err != nil || server.gets != 0 {
		t.Fatal("poll hint ignored", err)
	}
	*clock = due
	server.authorized = true
	server.loseResult = true
	if _, err = e.Reconcile(ctx, loginProfile, loginAttempt); !errors.Is(err, state.ErrUncertain) {
		t.Fatal("lost grant persistence ACK reported success", err)
	}
	s, err = e.store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p := s.Profiles[loginProfile]
	if p.Attempt.Phase != enrollment.ChoosingContext || p.Attempt.NodeID != "" || p.Secrets.Access == "" || p.Secrets.Renewal == "" || p.Secrets.SessionID != "native-session" || !bytes.Equal(p.Keys.IdentityPrivate, original.Keys.IdentityPrivate) {
		t.Fatal("authorized result not durably bound to original identity")
	}
	if _, err = e.Reconcile(ctx, loginProfile, loginAttempt); err != nil || server.gets != 1 || server.created != 1 {
		t.Fatal("restart replay created another grant", err)
	}
}

func TestNativeCrashRecoveryHasOriginalFiniteBudget(t *testing.T) {
	e, server, clock := loginFixture(t)
	ctx := context.Background()
	sent := *clock
	if err := e.store.Update(ctx, 2, func(s *state.State) error {
		p := s.Profiles[loginProfile]
		p.RemoteInFlight = true
		p.InFlightAt = sent
		s.Profiles[loginProfile] = p
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(121 * time.Second)
	due, err := e.Reconcile(ctx, loginProfile, loginAttempt)
	if err != nil || !due.IsZero() {
		t.Fatal(err)
	}
	s, err := e.store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	a := s.Profiles[loginProfile].Attempt
	if a.Phase != enrollment.Expired || !a.Uncertain || !a.RecoveryUntil.Equal(sent.Add(120*time.Second)) || server.starts != 0 {
		t.Fatal("restart extended recovery or claimed remote rollback")
	}
}

func TestNativeCrashAfterObservedExpiryRemainsUncertain(t *testing.T) {
	e, server, clock := loginFixture(t)
	created := *clock
	*clock = created.Add(601 * time.Second)
	if err := e.store.Update(context.Background(), 2, func(s *state.State) error {
		p := s.Profiles[loginProfile]
		expired, err := p.Attempt.Observe(created.Add(600 * time.Second))
		if err != nil {
			return err
		}
		p.Attempt = &expired
		p.RemoteInFlight = true
		p.InFlightAt = created.Add(590 * time.Second)
		s.Profiles[loginProfile] = p
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Reconcile(context.Background(), loginProfile, loginAttempt); err != nil {
		t.Fatal(err)
	}
	s, err := e.store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	a := s.Profiles[loginProfile].Attempt
	if a.Phase != enrollment.Expired || !a.Uncertain || !a.RecoveryUntil.Equal(created.Add(710*time.Second)) || server.starts != 0 {
		t.Fatal("local expiry was mistaken for proven remote rollback")
	}
}

func TestNativeMalformedBindingStopsWithoutNewLogin(t *testing.T) {
	e, server, _ := loginFixture(t)
	server.badBinding = true
	ctx := context.Background()
	if _, err := e.Reconcile(ctx, loginProfile, loginAttempt); err != nil {
		t.Fatal(err)
	}
	s, err := e.store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p := s.Profiles[loginProfile]
	if p.Login.BlockedReason != "client.login.invalid_response" || p.Secrets.Access != "" || !p.Attempt.Uncertain {
		t.Fatal("malformed response granted authority")
	}
	if _, err = e.Reconcile(ctx, loginProfile, loginAttempt); err != nil || server.starts != 1 {
		t.Fatal("bad response triggered new login", err)
	}
	w, err := NewLoginWorker(e, workerTracer(), workerMeter(), workerLogger())
	if err != nil {
		t.Fatal(err)
	}
	if err = w.Wake(ctx, loginProfile, loginAttempt); err != nil {
		t.Fatal(err)
	}
	if err = w.Wake(ctx, loginProfile, loginAttempt); err != nil {
		t.Fatal("duplicate wake not deduplicated", err)
	}
	otherID := "c4154ca2-5e02-4ce5-b968-f9c599b0440f"
	otherAttempt := "65b76ef6-7179-4db1-9b31-ef6f4b33087c"
	s, err = e.store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.store.Update(ctx, s.Revision, func(s *state.State) error {
		p := s.Profiles[loginProfile]
		a := *p.Attempt
		a.ProfileID = otherID
		a.AttemptID = otherAttempt
		a.RequestID = otherAttempt
		p.ID = otherID
		p.Attempt = &a
		p.RemoteInFlight = false
		p.InFlightAt = time.Time{}
		p.Login.BlockedReason = ""
		s.Profiles[otherID] = p
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err = w.Wake(ctx, otherID, otherAttempt); !errors.Is(err, ErrLoginQueueFull) {
		t.Fatal("queue overflow silently dropped")
	}
}

func TestNativeBindingKnownVector(t *testing.T) {
	e, _, _ := loginFixture(t)
	s, err := e.store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	p := s.Profiles[loginProfile]
	p.Keys.IdentityPrivate = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{1}, 32))
	fingerprint, err := p.Keys.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	p.Attempt.DeviceKeyHash = fingerprint
	public, binding, err := NativeBinding(p)
	if err != nil || public != "enp_iojj3XQJ8ZX9UtstPLpdcspnCb8dlBIb83SIAbQPb1w" || hex.EncodeToString(binding[:]) != "2091d28cc49274a1991ed67ff84a712e1bdd52ff5b64d5d3d10494279afe4420" {
		t.Fatal("published ordered length-prefix binding changed", err)
	}
}

func TestLoginWorkerStartupAndBoundedStop(t *testing.T) {
	e, server, _ := loginFixture(t)
	server.started = make(chan struct{}, 1)
	w, err := NewLoginWorker(e, workerTracer(), workerMeter(), workerLogger())
	if err != nil {
		t.Fatal(err)
	}
	if server.starts != 0 {
		t.Fatal("constructor started background work")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	select {
	case <-server.started:
	case <-time.After(2 * time.Second):
		t.Fatal("startup did not resume persisted attempt")
	}
	cancel()
	select {
	case err = <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker ignored bounded shutdown")
	}
}
