package localapi

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/control"
	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/localipc"
	"github.com/endless-net/client/internal/state"
	signingcontract "github.com/endless-net/signing/api/contract"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const runtimeID = "c5d74361-ed6e-4693-8ee2-96d4335379c3"
const defaultID = "0b6e11e3-3daa-4846-92ed-e07b6d8ce566"
const requestID = "1bf2f0fa-10d6-4bd7-8a2c-969596194c40"
const installationID = "d65c0fd2-1a9d-4217-bb07-cd5cd51022bc"

type memory struct {
	mu      sync.Mutex
	data    []byte
	lostACK bool
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
	if m.lostACK {
		return state.ErrUncertain
	}
	return nil
}

type testCodec struct{ cipher.AEAD }

func (c testCodec) Seal(data []byte) ([]byte, error) {
	n := make([]byte, c.NonceSize())
	if _, err := rand.Read(n); err != nil {
		return nil, err
	}
	return c.AEAD.Seal(n, n, data, nil), nil
}
func (c testCodec) Open(data []byte) ([]byte, error) {
	if len(data) < c.NonceSize() {
		return nil, state.ErrCorrupt
	}
	return c.AEAD.Open(nil, data[:c.NonceSize()], data[c.NonceSize():], nil)
}

type testRunner struct {
	store *state.Store
	mu    sync.Mutex
	calls int
	err   error
}

func (r *testRunner) Wake(ctx context.Context, profile, attempt string) error {
	s, err := r.store.Load(ctx)
	if err != nil {
		return err
	}
	p := s.Profiles[profile]
	if p.Attempt == nil || p.Attempt.AttemptID != attempt || len(p.Keys.IdentityPrivate) == 0 || p.Secrets.RequestProof == "" {
		return errors.New("worker invoked before durable preparation")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	return r.err
}
func config(now *time.Time) Config {
	return Config{Announcements: &testAnnouncements{now: now}, RuntimeID: runtimeID, DefaultProfileID: defaultID, DefaultControlOrigin: "https://control.example", ContractDigest: [32]byte{7}, Build: &clientv0.BuildIdentity{Version: "fixture", Commit: "fixture", Architecture: "amd64"}, MaxProfiles: 2, MaxDisplayNameBytes: 128, MaxHostnameBytes: 253, DefaultProfileDisplayName: "fixture-profile", Now: func() time.Time { return *now }}
}
func fixture(t *testing.T) (*Controller, *memory, *testRunner, *time.Time) {
	t.Helper()
	m := &memory{}
	block, err := aes.NewCipher(bytes.Repeat([]byte{3}, 32))
	if err != nil {
		t.Fatal(err)
	}
	a, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	s, err := state.New(m, testCodec{a}, installationID, 1<<20, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)
	r := &testRunner{store: s}
	c, err := New(s, r, config(&now))
	if err != nil {
		t.Fatal(err)
	}
	return c, m, r, &now
}
func request(revision uint64) *clientv0.EnrollRequest {
	return &clientv0.EnrollRequest{Mutation: &clientv0.MutationContext{ExpectedInstanceId: runtimeID, ExpectedRevision: revision}, RequestId: requestID, Mode: clientv0.EnrollmentMode_ENROLLMENT_MODE_WORKSTATION, Hostname: "device"}
}
func getRequest() *clientv0.GetEnrollmentAttemptRequest {
	return &clientv0.GetEnrollmentAttemptRequest{Selector: &clientv0.GetEnrollmentAttemptRequest_RequestId{RequestId: requestID}}
}

func TestDurableClaimExactReplayAndRuntimeRestart(t *testing.T) {
	c, _, runner, now := fixture(t)
	ctx := context.Background()
	caller := localipc.Principal{SID: "S-1-5-21-11"}
	first, err := c.begin(ctx, caller, request(1))
	if err != nil {
		t.Fatal(err)
	}
	if first.Attempt.Phase != clientv0.EnrollmentPhase_ENROLLMENT_PHASE_AWAITING_LOGIN || first.Attempt.NodeId != nil || first.Attempt.BrowserAction != nil {
		t.Fatal("local preparation claimed remote success")
	}
	s, err := c.store.Load(ctx)
	if err != nil || s.OwnerSID != caller.SID || s.Revision != 2 {
		t.Fatal("claim not committed", err)
	}
	key := append([]byte(nil), s.Profiles[defaultID].Keys.IdentityPrivate...)
	if runner.calls != 0 || first.Attempt.Restriction == nil {
		t.Fatal("unconfirmed trust dispatched login")
	}
	if _, err = c.trust(ctx, localipc.Principal{SID: "S-1-5-21-99", Administrator: true}, confirmation(t, c)); err != nil {
		t.Fatal(err)
	}
	replay, err := c.begin(ctx, caller, request(3))
	expected := proto.Clone(first).(*clientv0.EnrollResponse)
	expected.Attempt.Metadata.Revision = 3
	expected.Attempt.Restriction = nil
	if err != nil || !proto.Equal(expected, replay) {
		t.Fatalf("exact replay changed result: %v", err)
	}
	if runner.calls != 2 {
		t.Fatal("durable work not scheduled/reconciled")
	}
	if _, err = c.begin(ctx, caller, request(1)); status.Code(err) != codes.Aborted {
		t.Fatal("stale mutation accepted")
	}
	changed := request(3)
	changed.Hostname = "another-device"
	if _, err = c.begin(ctx, caller, changed); status.Code(err) != codes.FailedPrecondition {
		t.Fatal("changed input silently replaced key")
	}
	if _, err = c.get(ctx, localipc.Principal{SID: "S-1-5-21-12", Administrator: true}, getRequest()); status.Code(err) != codes.PermissionDenied {
		t.Fatal("foreign administrator read owner attempt")
	}
	observer, err := c.snapshot(ctx, localipc.Principal{SID: "S-1-5-21-12", Administrator: true})
	if err != nil || observer.Status.ActiveProfileId != "" || observer.Status.Hostname != "" || observer.Status.StoredState != nil {
		t.Fatal("observer snapshot leaked owner context", err)
	}
	*now = now.Add(time.Minute)
	restartedConfig := config(now)
	restartedConfig.RuntimeID = "8fb48f6f-3057-43e8-a2fa-28ecda4c624d"
	restarted, err := New(c.store, runner, restartedConfig)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = restarted.begin(ctx, caller, request(2)); status.Code(err) != codes.Aborted {
		t.Fatal("old runtime mutation accepted after restart")
	}
	got, err := restarted.get(ctx, caller, getRequest())
	if err != nil || got.Attempt.AttemptId != first.Attempt.AttemptId || !got.Attempt.Deadline.AsTime().Equal(first.Attempt.Deadline.AsTime()) || got.Attempt.Metadata.InstanceId != restartedConfig.RuntimeID {
		t.Fatalf("restart read recovery: %v", err)
	}
	s, err = c.store.Load(ctx)
	if err != nil || !bytes.Equal(s.Profiles[defaultID].Keys.IdentityPrivate, key) {
		t.Fatal("restart rekeyed", err)
	}
	public, err := protojson.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{s.Profiles[defaultID].Secrets.RequestProof, "private", "access_bearer", "renewal"} {
		if strings.Contains(string(public), secret) {
			t.Fatal("public projection contains secret material")
		}
	}
}

func TestWriteAckLossAndQueueFailureRecoverOriginalAttempt(t *testing.T) {
	c, m, runner, _ := fixture(t)
	ctx := context.Background()
	caller := localipc.Principal{SID: "S-1-5-21-11"}
	m.lostACK = true
	if _, err := c.begin(ctx, caller, request(1)); status.Code(err) != codes.Unavailable || runner.calls != 0 {
		t.Fatal("worker ran after uncertain persistence")
	}
	m.lostACK = false
	runner.err = status.Error(codes.Unavailable, "secret-remote-token")
	if _, err := c.trust(ctx, localipc.Principal{SID: "S-1-5-21-99", Administrator: true}, confirmation(t, c)); status.Code(err) != codes.Unavailable || strings.Contains(err.Error(), "secret-remote-token") {
		t.Fatal("queue failure leaked backend detail")
	}
	before, err := c.store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	runner.err = nil
	got, err := c.get(ctx, caller, getRequest())
	if err != nil || got.Attempt.AttemptId != before.Profiles[defaultID].Attempt.AttemptID {
		t.Fatal("lost local response created another attempt", err)
	}
	if _, err = c.begin(ctx, caller, request(3)); err != nil {
		t.Fatal("exact retry after recovery failed", err)
	}
}

func TestConcurrentClaimAndCancelInflight(t *testing.T) {
	c, _, _, now := fixture(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.begin(ctx, localipc.Principal{SID: fmtSID(i)}, request(1))
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if status.Code(err) != codes.Aborted {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatal("multiple initial owners")
	}
	s, err := c.store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	owner := localipc.Principal{SID: s.OwnerSID}
	if err = c.store.Update(ctx, s.Revision, func(s *state.State) error {
		p := s.Profiles[defaultID]
		a, e := p.Attempt.LoginSucceeded(*now)
		if e != nil {
			return e
		}
		a, e = a.Select("account", "network", *now)
		if e != nil {
			return e
		}
		p.Attempt = &a
		p.RemoteInFlight = true
		p.InFlightAt = *now
		s.Profiles[defaultID] = p
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s, err = c.store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	response, err := c.cancelAttempt(ctx, owner, &clientv0.CancelEnrollmentAttemptRequest{Mutation: &clientv0.MutationContext{ExpectedInstanceId: runtimeID, ExpectedRevision: s.Revision}, AttemptId: s.Profiles[defaultID].Attempt.AttemptID})
	if err != nil || response.Attempt.Phase != clientv0.EnrollmentPhase_ENROLLMENT_PHASE_CANCELLED || response.Attempt.Certainty != clientv0.EnrollmentCertainty_ENROLLMENT_CERTAINTY_UNCERTAIN || response.Attempt.RecoveryDeadline == nil || response.Attempt.PollAfterSeconds != 1 {
		t.Fatal("inflight cancellation implied remote rollback", err)
	}
	*now = now.Add(time.Minute)
	got, err := c.get(ctx, owner, getRequest())
	if err != nil || !got.Attempt.RecoveryDeadline.AsTime().Equal(response.Attempt.RecoveryDeadline.AsTime()) {
		t.Fatal("recovery deadline extended", err)
	}
}
func fmtSID(i int) string { return "S-1-5-21-" + string(rune('1'+i)) }

func TestCreateProfileClaimsWithMutationAndPreservesContext(t *testing.T) {
	c, _, _, _ := fixture(t)
	ctx := context.Background()
	caller := localipc.Principal{SID: "S-1-5-21-11"}
	bad := &clientv0.CreateProfileRequest{Mutation: &clientv0.MutationContext{ExpectedInstanceId: runtimeID, ExpectedRevision: 1}, ControlOrigin: "https://invalid.example/path", DisplayName: "fixture-profile"}
	if _, err := c.createProfile(ctx, caller, bad); status.Code(err) != codes.InvalidArgument {
		t.Fatal("non-origin profile accepted")
	}
	s, err := c.store.Load(ctx)
	if err != nil || s.OwnerSID != "" || s.Revision != 1 {
		t.Fatal("invalid input claimed installation", err)
	}
	bad.ControlOrigin = "https://chosen.example"
	created, err := c.createProfile(ctx, caller, bad)
	if err != nil {
		t.Fatal(err)
	}
	s, err = c.store.Load(ctx)
	if err != nil || s.ActiveProfileID != created.Profile.ProfileId || s.Profiles[s.ActiveProfileID].Attempt != nil || len(s.Profiles[s.ActiveProfileID].Keys.IdentityPrivate) != 0 {
		t.Fatal("CreateProfile performed enrollment", err)
	}
	foreign := localipc.Principal{SID: "S-1-5-21-12", Administrator: true}
	bad.Mutation.ExpectedRevision = 2
	if _, err = c.createProfile(ctx, foreign, bad); status.Code(err) != codes.PermissionDenied {
		t.Fatal("foreign administrator replaced initial owner")
	}
	started, err := c.begin(ctx, caller, request(2))
	if err != nil || started.Attempt.ProfileId != created.Profile.ProfileId {
		t.Fatal("active profile enrollment failed", err)
	}
	s, err = c.store.Load(ctx)
	if err != nil || s.Profiles[s.ActiveProfileID].Attempt.ControlOrigin != "https://chosen.example" {
		t.Fatal("configured default replaced selected profile origin", err)
	}
}

func TestRPCRequiresOSCallerAndDeadlineDoesNotRestart(t *testing.T) {
	c, _, _, now := fixture(t)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("caller-sid", "S-1-5-21-11", "administrator", "true"))
	if _, err := c.Enroll(ctx, request(1)); status.Code(err) != codes.Unauthenticated {
		t.Fatal("metadata forged OS caller")
	}
	caller := localipc.Principal{SID: "S-1-5-21-11"}
	first, err := c.begin(ctx, caller, request(1))
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(enrollment.LoginTTL)
	got, err := c.get(ctx, caller, getRequest())
	if err != nil || got.Attempt.Phase != clientv0.EnrollmentPhase_ENROLLMENT_PHASE_EXPIRED || got.Attempt.PollAfterSeconds != 0 || !got.Attempt.Deadline.AsTime().Equal(first.Attempt.Deadline.AsTime()) {
		t.Fatal("read extended login/deadline", err)
	}
}

// Synthetic public material, decoded/validated with the published producer SDK.
type testAnnouncements struct {
	now     *time.Time
	variant byte
	err     error
}

func (a *testAnnouncements) Fetch(_ context.Context, origin string) (control.Announcement, error) {
	if a.err != nil {
		return control.Announcement{}, a.err
	}
	bundle := func(value byte) signingcontract.BootstrapTrustBundle {
		raw := bytes.Repeat([]byte{value}, 32)
		digest := sha256.Sum256(raw)
		key := signingcontract.BootstrapTrustKey{KeyID: "ed25519:" + base64.RawURLEncoding.EncodeToString(digest[:]), Algorithm: "ed25519", PublicKey: base64.RawURLEncoding.EncodeToString(raw)}
		return signingcontract.BootstrapTrustBundle{Version: 1, ActiveKeyID: key.KeyID, Keys: []signingcontract.BootstrapTrustKey{key}}
	}
	raw, err := json.Marshal(signingcontract.ServerKeyResponse{TrustBundle: bundle(1), NodeCredentialTrustBundle: bundle(2 + a.variant), RelayTrustBundle: bundle(9)})
	if err != nil {
		return control.Announcement{}, err
	}
	return control.DecodeAnnouncement(origin, raw, *a.now)
}
func confirmation(t *testing.T, c *Controller) *clientv0.TrustServerIdentityRequest {
	t.Helper()
	s, err := c.store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	p := s.Profiles[s.ActiveProfileID]
	a, err := c.announcement(context.Background(), p.ControlOrigin)
	if err != nil {
		t.Fatal(err)
	}
	return &clientv0.TrustServerIdentityRequest{Mutation: &clientv0.MutationContext{ExpectedInstanceId: c.config.RuntimeID, ExpectedRevision: s.Revision}, Profile: &clientv0.ProfileRef{ProfileId: p.ID}, ConfirmedControlOrigin: a.Origin, ConfirmedKeyId: a.KeyID, ConfirmedAnnouncementId: a.ID}
}
