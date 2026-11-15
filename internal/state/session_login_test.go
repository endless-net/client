package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	sessionv1 "github.com/endless-net/identity/api/session/v1"
	identityapi "github.com/endless-net/identity/api/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const loginRequest = "7f88d8fb-76df-4f8d-9f7f-3c44b56c70c2"
const loginAttempt = "3d8bdf1c-b741-438e-b908-471e4e2a60da"
const loginTransaction = "fcb3ad13-e348-4c64-9f1c-f5c19b35ffb9"

func sessionLoginFixture(t *testing.T) (*Store, *memoryBackend, time.Time) {
	t.Helper()
	s, b, now := registeredStateFixture(t)
	value, err := s.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	p := value.Profiles[profileID]
	p.Secrets.Access = strings.Repeat("a", 43)
	p.Secrets.Renewal = strings.Repeat("r", 43)
	p.Secrets.UserID, p.Secrets.SessionID = "original-user", "initial-session"
	p.Secrets.SessionExpiresAt, p.Secrets.RenewalExpiresAt = now.Add(-time.Minute), now.Add(-time.Minute)
	p.SessionRefresh = &SessionRefresh{RequestID: "10aed345-9726-4256-b230-e6d17754bbf8", ExpectedSessionID: p.Secrets.SessionID, UserID: p.Secrets.UserID, Renewal: p.Secrets.Renewal, CreatedAt: now.Add(-20 * time.Minute), Deadline: now.Add(-10 * time.Minute), SentAt: now.Add(-19 * time.Minute)}
	value.Profiles[profileID] = p
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	b.data, err = s.protector.Seal(raw) // Test-only existing v7 registration fixture.
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	return s, b, now
}

func changeLoginProfile(t *testing.T, store *Store, change func(*Profile)) error {
	t.Helper()
	s, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return store.Update(t.Context(), s.Revision, func(next *State) error {
		p := next.Profiles[profileID]
		change(&p)
		next.Profiles[profileID] = p
		return nil
	})
}

func preparedLogin(t *testing.T, store *Store, now time.Time) {
	t.Helper()
	if err := changeLoginProfile(t, store, func(p *Profile) {
		var err error
		p.SessionLogin, err = NewSessionLogin(*p, loginRequest, loginAttempt, strings.Repeat("q", 43), now)
		if err != nil {
			t.Fatal(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := changeLoginProfile(t, store, func(p *Profile) {
		p.SessionLogin.SentAt, p.SessionLogin.InFlightAt = now, now
	}); err != nil {
		t.Fatal(err)
	}
}

func nativeLoginSuccess(t *testing.T, p Profile, now time.Time, user string) []byte {
	t.Helper()
	shadow, err := p.SessionLoginShadow()
	if err != nil {
		t.Fatal(err)
	}
	_, binding, err := NativeLoginBinding(shadow)
	if err != nil {
		t.Fatal(err)
	}
	tx := &identityapi.NativeLoginTransaction{
		TransactionId: loginTransaction, RequestId: loginRequest, AttemptBindingSha256: binding[:],
		State:     identityapi.NativeLoginState_NATIVE_LOGIN_STATE_AUTHORIZED,
		CreatedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(10 * time.Minute)), ReplayExpiresAt: timestamppb.New(now.Add(22 * time.Minute)),
		Outcome: &identityapi.NativeLoginTransaction_Result{Result: &identityapi.NativeLoginResult{
			Session:      &sessionv1.UserSession{SessionId: "fresh-session", UserId: user, State: sessionv1.UserSessionState_USER_SESSION_STATE_ACTIVE, ExpiresAt: timestamppb.New(now.Add(time.Hour)), RenewalSupported: true},
			AccessBearer: strings.Repeat("n", 43), RenewalAuthorization: &sessionv1.SessionRenewalAuthorization{Bearer: strings.Repeat("s", 43), ExpiresAt: timestamppb.New(now.Add(24 * time.Hour))},
		}},
	}
	raw, err := proto.Marshal(tx)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func adoptLogin(t *testing.T, p *Profile, now time.Time, user string) {
	t.Helper()
	previous, err := p.CurrentSession()
	if err != nil {
		t.Fatal(err)
	}
	p.SessionLogin.Phase = SessionLoginSucceeded
	p.SessionLogin.ObservedAt = now.Add(time.Second)
	p.SessionLogin.InFlightAt = time.Time{}
	p.SessionLogin.Transaction = nativeLoginSuccess(t, *p, now, user)
	p.SessionLogin.PollProof = strings.Repeat("p", 43)
	p.ActiveSession = &ActiveSession{RequestID: loginRequest, PreviousSessionID: previous.SessionID, PreviousRenewalSHA256: CredentialDigest(previous.Renewal), Native: &NativeSession{Binding: p.SessionLogin.Binding, CreatedAt: p.SessionLogin.CreatedAt, AdoptedAt: p.SessionLogin.ObservedAt, Transaction: append([]byte(nil), p.SessionLogin.Transaction...)}}
	if p.SessionRefresh != nil {
		p.SessionRefreshHistory = map[string]*SessionRefresh{p.SessionRefresh.RequestID: p.SessionRefresh}
		p.SessionRefresh = nil
	}
}

func TestV7SessionLoginUpgradePreservesUnknownRenewalAndIdentity(t *testing.T) {
	store, backend, _ := sessionLoginFixture(t)
	before, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	old := before
	old.Version = 7
	raw, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	backend.data, err = store.protector.Seal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Upgrade(t.Context()); err != nil {
		t.Fatal(err)
	}
	after, err := store.Load(t.Context())
	if err != nil || after.Version != 8 || after.Revision != before.Revision+1 || !reflect.DeepEqual(before.Profiles, after.Profiles) {
		t.Fatal("v7 data changed or current authority invented", err)
	}
	if err := store.Upgrade(t.Context()); err != nil {
		t.Fatal(err)
	}
	again, err := store.Load(t.Context())
	if err != nil || again.Revision != after.Revision {
		t.Fatal("upgrade repeated", err)
	}
}

func TestFreshSessionAdoptionPreservesEnrollmentAndUnknownRefresh(t *testing.T) {
	store, _, now := sessionLoginFixture(t)
	before, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	original := before.Profiles[profileID]
	preparedLogin(t, store, now)
	if err := changeLoginProfile(t, store, func(p *Profile) { adoptLogin(t, p, now, "original-user") }); err != nil {
		t.Fatal(err)
	}
	after, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	p := after.Profiles[profileID]
	current, err := p.CurrentSession()
	if err != nil || current.UserID != "original-user" || current.SessionID != "fresh-session" || current.Access != strings.Repeat("n", 43) {
		t.Fatal("fresh session not current", err)
	}
	if !reflect.DeepEqual(original.Secrets, p.Secrets) || !reflect.DeepEqual(original.Attempt, p.Attempt) || !bytes.Equal(original.Keys.IdentityPrivate, p.Keys.IdentityPrivate) || !bytes.Equal(original.Keys.WireGuardPrivate, p.Keys.WireGuardPrivate) || !reflect.DeepEqual(original.NodeRenewal, p.NodeRenewal) {
		t.Fatal("login reset registration/keys/Node operation")
	}
	if p.SessionRefresh != nil || !reflect.DeepEqual(original.SessionRefresh, p.SessionRefreshHistory[original.SessionRefresh.RequestID]) {
		t.Fatal("unknown refresh lost or changed")
	}
	if err := changeLoginProfile(t, store, func(p *Profile) {
		p.SessionRefreshHistory[original.SessionRefresh.RequestID].Deadline = now.Add(time.Hour)
	}); !errors.Is(err, ErrCorrupt) {
		t.Fatal("archived renewal deadline was editable", err)
	}
}

func TestSessionLoginRejectsForeignUserAndUnsentAdoption(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		store, _, now := sessionLoginFixture(t)
		preparedLogin(t, store, now)
		if !foreign {
			if err := changeLoginProfile(t, store, func(p *Profile) { p.SessionLogin.InFlightAt = time.Time{} }); err != nil {
				t.Fatal(err)
			}
		}
		user := "original-user"
		if foreign {
			user = "foreign-user"
		}
		if err := changeLoginProfile(t, store, func(p *Profile) { adoptLogin(t, p, now, user) }); !errors.Is(err, ErrCorrupt) {
			t.Fatal("unauthorized session adopted", err)
		}
	}
}

func TestSessionLoginFrozenBindingAndLostWriteACK(t *testing.T) {
	store, backend, now := sessionLoginFixture(t)
	preparedLogin(t, store, now)
	for _, change := range []func(*Profile){
		func(p *Profile) { p.SessionLogin.RequestProof = "changed-proof" },
		func(p *Profile) { p.SessionLogin.Binding.RequestID = journalID },
		func(p *Profile) {
			p.SessionLogin.CreatedAt = now.Add(time.Minute)
			p.SessionLogin.Deadline = now.Add(11 * time.Minute)
		},
	} {
		if err := changeLoginProfile(t, store, change); !errors.Is(err, ErrCorrupt) {
			t.Fatal("frozen login input changed", err)
		}
	}
	backend.failAfter = true
	err := changeLoginProfile(t, store, func(p *Profile) { adoptLogin(t, p, now, "original-user") })
	if !errors.Is(err, ErrUncertain) {
		t.Fatal("lost ACK reported success", err)
	}
	backend.failAfter = false
	reloaded, err := store.Load(t.Context())
	if err != nil || reloaded.Profiles[profileID].SessionLogin.Phase != SessionLoginSucceeded {
		t.Fatal("durable outcome lost", err)
	}
}

func TestSessionLoginRetainsSafeReceiptAndRejectsLateOldAdoption(t *testing.T) {
	store, _, now := sessionLoginFixture(t)
	preparedLogin(t, store, now)
	if err := changeLoginProfile(t, store, func(p *Profile) { adoptLogin(t, p, now, "original-user") }); err != nil {
		t.Fatal(err)
	}
	s, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	old := s.Profiles[profileID].SessionLogin
	if err := changeLoginProfile(t, store, func(p *Profile) {
		p.SessionLoginHistory = map[string]SessionLoginReceipt{old.Binding.RequestID: old.Receipt()}
		var err error
		p.SessionLogin, err = NewSessionLogin(*p, "4b97f459-7bb1-4ff9-a251-d9e81a1a0ca9", "e4084949-f942-43b0-9146-9100f545ce1c", strings.Repeat("v", 43), now.Add(11*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	s, err = store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(s.Profiles[profileID].SessionLoginHistory[loginRequest])
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{strings.Repeat("q", 43), strings.Repeat("p", 43), strings.Repeat("n", 43), strings.Repeat("s", 43)} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatal("receipt retained grant/proof")
		}
	}
	if err := changeLoginProfile(t, store, func(p *Profile) { p.SessionLogin = old; adoptLogin(t, p, now, "original-user") }); !errors.Is(err, ErrCorrupt) {
		t.Fatal("late response replaced current attempt", err)
	}
	if err := changeLoginProfile(t, store, func(p *Profile) { delete(p.SessionLoginHistory, loginRequest) }); !errors.Is(err, ErrCorrupt) {
		t.Fatal("replay receipt was evicted", err)
	}
}
