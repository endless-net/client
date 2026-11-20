//go:build windows

package clientapp

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/control"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	sessionv1 "github.com/endless-net/identity/api/session/v1"
	"github.com/endless-net/identity/api/session/v1/sessionv1connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type onlineRenewalFixture struct {
	sessionv1connect.UnimplementedSessionServiceHandler
	t                       *testing.T
	f                       *connectionBackendFixture
	base                    time.Time
	offset                  atomic.Int64
	mu                      sync.Mutex
	head, access, grant     string
	lastCommand, lastWire   string
	nodes                   map[string][]byte
	sessions                map[string]*sessionv1.RenewSessionResponse
	nodeCalls, sessionCalls int
	mode                    string
	entered, release        chan struct{}
}

func (f *onlineRenewalFixture) now() time.Time { return f.base.Add(time.Duration(f.offset.Load())) }

func newOnlineRenewalFixture(t *testing.T, mode string) (*onlineRenewalFixture, *windowsConnection) {
	t.Helper()
	f := &onlineRenewalFixture{t: t, base: time.Now().UTC(), access: strings.Repeat("a", 43), grant: strings.Repeat("r", 43), nodes: map[string][]byte{}, sessions: map[string]*sessionv1.RenewSessionResponse{}, mode: mode, entered: make(chan struct{}, 1), release: make(chan struct{})}
	f.f = newConnectionBackendFixturePrepared(t, func(p *state.Profile) {
		p.Attempt.CredentialEnds = f.base.Add(30 * time.Minute)
		p.Hostname = "fixture-device"
		p.Secrets.Access, p.Secrets.Renewal, p.Secrets.UserID = f.access, f.grant, "fixture-user"
		p.Secrets.SessionID = uuid.NewString()
		p.Secrets.SessionExpiresAt, p.Secrets.RenewalExpiresAt = f.base.Add(time.Hour), f.base.Add(24*time.Hour)
		if mode == "session-only" {
			p.Secrets.SessionExpiresAt = f.base.Add(10 * time.Minute)
		}
		wg, err := ecdh.X25519().NewPrivateKey(p.Keys.WireGuardPrivate)
		if err != nil {
			t.Fatal(err)
		}
		fingerprint, err := p.Keys.Fingerprint()
		if err != nil {
			t.Fatal(err)
		}
		r := coordinator.RegisterNodeRequest{SchemaVersion: coordinator.SchemaVersion, IdempotencyID: p.Secrets.RegistrationID, AccountID: p.Attempt.AccountID, NetworkID: p.Attempt.NetworkID, Hostname: p.Hostname, PublicKey: base64.StdEncoding.EncodeToString(wg.PublicKey().Bytes()), DeviceFingerprint: hex.EncodeToString(fingerprint[:]), SessionTokenBinding: coordinator.RegistrationSessionTokenBinding(f.access)}
		if err := coordinator.SetRegisterNodeIdentityProof(&r, ed25519.PrivateKey(p.Keys.IdentityPrivate)); err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		p.Secrets.RegistrationRequest = string(raw)
	}, 30*time.Second)
	// Long-running renewal scenarios do not consume the endpoint-observation
	// channel used by the shorter backend tests. Keep it from stalling the RPC.
	f.f.patches = make(chan uint64, 4096)
	f.f.now = f.now
	f.f.factory.config.Verifier.Now = f.now
	f.f.factory.config.Verifier.Clock = endpointClockFunc(func() (connectivity.ClockSample, error) {
		at := f.now()
		return connectivity.ClockSample{Wall: at, Epoch: workerRuntime, Uptime: time.Duration(at.UnixNano()), UpperUptime: time.Duration(at.UnixNano())}, nil
	})
	initial, err := f.f.store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	f.head = initial.Profiles[workerProfile].Secrets.NodeCredential
	clearConnectionKeys(initial)
	f.f.checkRequest = func(r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Header.Get("X-EndlessNet-Node-Credential") != f.head {
			t.Error("map/endpoint used a retired credential")
		}
	}
	_, handler := sessionv1connect.NewSessionServiceHandler(f)
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/nodes/register" {
			return f.node(r)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		response := w.Result()
		response.Request = r
		return response, nil
	})}
	c, err := control.NewCredentials(f.f.store, client, f.f.factory.config.Verifier.Announcements, control.RegistrationConfig{MaxRequestBytes: 16384, MaxResponseBytes: 65536, MaxCredentialBytes: 8192, Now: f.now}, control.RegistrationExecutorConfig{MaxCommitRetries: 5, RetryInterval: 20 * time.Millisecond, WriteBudget: time.Second, Now: f.now})
	if err != nil {
		t.Fatal(err)
	}
	f.f.factory.config.PrepareCredentials = c.Ensure
	f.f.factory.config.RefreshCredentials = c.Refresh
	owner, err := f.f.factory.newOwner(context.Background(), workerProfile, 1)
	if err != nil {
		t.Fatal(err)
	}
	d := owner.(*windowsConnection)
	t.Cleanup(func() {
		if err := d.Disconnect(workerCallContext(t)); err != nil {
			t.Error(err)
		}
	})
	if err := d.Connect(backendOperation(t, f.f), false); err != nil {
		t.Fatal("initial connect", err)
	}
	return f, d
}

func (f *onlineRenewalFixture) RenewSession(_ context.Context, r *connect.Request[sessionv1.RenewSessionRequest]) (*connect.Response[sessionv1.RenewSessionResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessionCalls++
	if known := f.sessions[r.Msg.RequestId]; known != nil {
		return connect.NewResponse(known), nil
	}
	if r.Header().Get("Authorization") != "Bearer "+f.grant {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("fixture grant"))
	}
	f.access, f.grant = uuid.NewString()+"aaaaaaaa", uuid.NewString()+"rrrrrrrr"
	op := &sessionv1.SessionRenewal{OperationId: uuid.NewString(), RequestId: r.Msg.RequestId, PreviousSessionId: r.Msg.ExpectedSessionId, State: sessionv1.SessionRenewalState_SESSION_RENEWAL_STATE_SUCCEEDED, ReplayExpiresAt: timestamppb.New(f.now().Add(12 * time.Minute)), Outcome: &sessionv1.SessionRenewal_Result{Result: &sessionv1.SessionRenewalResult{Session: &sessionv1.UserSession{SessionId: uuid.NewString(), UserId: "fixture-user", State: sessionv1.UserSessionState_USER_SESSION_STATE_ACTIVE, ExpiresAt: timestamppb.New(f.now().Add(time.Hour)), RenewalSupported: true}, AccessBearer: f.access, RenewalAuthorization: &sessionv1.SessionRenewalAuthorization{Bearer: f.grant, ExpiresAt: timestamppb.New(f.now().Add(24 * time.Hour))}}}}
	response := &sessionv1.RenewSessionResponse{Operation: op, PollAuthorization: strings.Repeat("p", 43)}
	f.sessions[r.Msg.RequestId] = response
	if f.mode == "lost-session" && f.sessionCalls == 1 {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("fixture lost response"))
	}
	return connect.NewResponse(response), nil
}

func (f *onlineRenewalFixture) node(r *http.Request) (*http.Response, error) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	request, err := coordinator.DecodeRegisterNodeRequest(raw, 16384)
	if err != nil || coordinator.ValidateNodeCredentialRenewalRequest(request, 8192) != nil {
		return nil, errors.New("invalid fixture request")
	}
	f.mu.Lock()
	f.nodeCalls++
	call := f.nodeCalls
	if f.mode == "denied" {
		f.mu.Unlock()
		return &http.Response{StatusCode: 403, Header: make(http.Header), Body: http.NoBody, Request: r}, nil
	}
	response := f.nodes[request.IdempotencyID]
	if response == nil {
		recovery := f.mode == "expired-unknown" && f.lastCommand != "" &&
			request.IdempotencyID == coordinator.NodeRenewalRecoveryID(f.lastCommand) &&
			request.NodeCredential == f.lastWire
		if !recovery && request.NodeCredential != f.head || r.Header.Get("Authorization") != "Bearer "+f.access {
			f.mu.Unlock()
			return nil, errors.New("rotation used wrong head/session")
		}
		previous, err := coordinator.DecodeNodeCredential(f.head, 8192)
		if err != nil {
			f.mu.Unlock()
			return nil, err
		}
		if !recovery && !f.now().Before(previous.ExpiresAt) {
			f.t.Error("renewal started after expiry")
		}
		key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{62}, 32))
		expires := f.now().Add(30 * time.Minute)
		payload, err := coordinator.NodeCredentialSigningPayload(previous.KeyID, previous.NetworkID, previous.NodeID, previous.Scopes, expires)
		if err != nil {
			f.mu.Unlock()
			return nil, err
		}
		claims := coordinator.NodeCredentialClaims{Version: previous.Version, Algorithm: previous.Algorithm, KeyID: previous.KeyID, NetworkID: previous.NetworkID, NodeID: previous.NodeID, Scopes: previous.Scopes, ExpiresAt: expires, Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, payload))}
		if f.mode == "bad-signature" {
			claims.Signature = base64.RawURLEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))
		}
		encoded, err := json.Marshal(claims)
		if err != nil {
			f.mu.Unlock()
			return nil, err
		}
		credential := "enc_" + base64.RawURLEncoding.EncodeToString(encoded)
		result := coordinator.NodeCredentialRenewalResponse{SchemaVersion: 1, IdempotencyID: request.IdempotencyID, RenewalBinding: coordinator.RegistrationIdentityProofBinding(request), RegistrationBinding: request.RegistrationBinding, PreviousCredentialSHA256: coordinator.NodeCredentialSHA256(request.NodeCredential), NetworkID: previous.NetworkID, NodeID: previous.NodeID, NodeCredential: credential, IssuedAt: f.now(), ExpiresAt: expires}
		response, err = json.Marshal(result)
		if err != nil {
			f.mu.Unlock()
			return nil, err
		}
		f.nodes[request.IdempotencyID] = response
		f.lastCommand, f.lastWire = request.IdempotencyID, request.NodeCredential
		f.head = credential
	}
	f.mu.Unlock()
	if (f.mode == "lost-node" || f.mode == "expired-unknown") && call == 1 {
		return nil, errors.New("fixture lost response")
	}
	if f.mode == "blocked" && call == 1 {
		f.entered <- struct{}{}
		select {
		case <-r.Context().Done():
			return nil, r.Context().Err()
		case <-f.release:
		}
	}
	if f.mode == "late" && call == 1 {
		f.entered <- struct{}{}
		<-f.release // Deliberately misbehaving remote completion after cancellation.
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(response)), Request: r}, nil
}

func awaitCredentialCondition(t *testing.T, condition func() bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for !condition() {
		select {
		case <-ctx.Done():
			t.Fatal("credential transition did not finish")
		case <-tick.C:
		}
	}
}

func TestOnlineCredentialsSurviveSeveralOriginalTTLsWithoutNativeRestart(t *testing.T) {
	for _, mode := range []string{"valid", "lost-node", "lost-session"} {
		t.Run(mode, func(t *testing.T) {
			f, d := newOnlineRenewalFixture(t, mode)
			t.Cleanup(func() {
				if t.Failed() {
					f.mu.Lock()
					t.Logf("rpc=%d/%d owner=%T:%#v refresh=%d offline=%d", f.nodeCalls, f.sessionCalls, d.result(), d.result(), f.f.native.refreshes.Load(), f.f.native.offlines.Load())
					f.mu.Unlock()
				}
			})
			before, err := f.f.store.Load(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer clearConnectionKeys(before)
			// Let the worker anchor the original deadlines before advancing time.
			awaitCredentialCondition(t, func() bool { return f.f.native.probes.Load() > 0 })
			for cycle := 1; cycle <= 4; cycle++ {
				f.offset.Store(int64(time.Duration(cycle) * 25 * time.Minute))
				awaitCredentialCondition(t, func() bool {
					value, err := f.f.store.Load(t.Context())
					if err != nil {
						return false
					}
					defer clearConnectionKeys(value)
					_, ends, err := value.Profiles[workerProfile].CurrentNodeCredential()
					return err == nil && ends.Equal(f.now().Add(30*time.Minute)) && f.f.native.refreshes.Load() >= uint64(cycle)
				})
			}
			after, err := f.f.store.Load(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer clearConnectionKeys(after)
			a, b := before.Profiles[workerProfile], after.Profiles[workerProfile]
			if !reflect.DeepEqual(a.Attempt, b.Attempt) || !reflect.DeepEqual(a.Keys, b.Keys) || a.Secrets != b.Secrets || b.Connection.Generation != 1 {
				t.Fatal("rotation replaced original device/history/generation")
			}
			current, err := b.CurrentSession()
			if err != nil || !current.ExpiresAt.After(f.now()) || current.SessionID == a.Secrets.SessionID {
				t.Fatal("session did not rotate", err)
			}
			f.mu.Lock()
			nodes, sessions := len(f.nodes), len(f.sessions)
			f.mu.Unlock()
			if nodes != 4 || sessions != 2 || f.f.native.starts.Load() != 1 || f.f.native.stops.Load() != 0 || f.f.native.offlines.Load() != 0 || f.f.native.revoked.Load() {
				t.Fatal("connection interrupted or renewal duplicated", nodes, sessions)
			}
			select {
			case <-d.done:
				t.Fatal("connection owner stopped")
			default:
			}
		})
	}
}

func TestOnlineRenewalDoesNotBlockProbesAndDisconnectCancelsRPC(t *testing.T) {
	f, d := newOnlineRenewalFixture(t, "blocked")
	awaitCredentialCondition(t, func() bool { return f.f.native.probes.Load() > 0 })
	f.offset.Store(int64(25 * time.Minute))
	select {
	case <-f.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("renewal did not start")
	}
	probes := f.f.native.probes.Load()
	awaitCredentialCondition(t, func() bool { return f.f.native.probes.Load() > probes+2 })
	if f.f.native.stops.Load() != 0 || f.f.native.offlines.Load() != 0 {
		t.Fatal("blocked RPC stopped the tunnel")
	}
	if err := workerIntent(t, f.f.store, state.IntentDisconnected, f.now(), workerRuntime); err != nil {
		t.Fatal(err)
	}
	if err := d.Disconnect(workerCallContext(t)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-d.done:
	case <-time.After(time.Second):
		t.Fatal("renewal worker leaked")
	}
	value, err := f.f.store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer clearConnectionKeys(value)
	p := value.Profiles[workerProfile]
	if p.ActiveNodeCredential != nil || p.NodeRenewal == nil || p.NodeRenewal.SentAt.IsZero() {
		t.Fatal("cancel adopted a stale result or discarded its frozen command")
	}
}

func TestOnlineRenewalDoesNotRetryAuthoritativeDenial(t *testing.T) {
	for _, mode := range []string{"denied", "bad-signature"} {
		t.Run(mode, func(t *testing.T) {
			f, d := newOnlineRenewalFixture(t, mode)
			awaitCredentialCondition(t, func() bool { return f.f.native.probes.Load() > 0 })
			f.offset.Store(int64(25 * time.Minute))
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			want := connectivity.FailureLogin
			if mode == "bad-signature" {
				want = connectivity.FailureTrust
			}
			if err := d.Serve(ctx); !errors.Is(err, want) {
				t.Fatal("denial did not stop authoritatively", err)
			}
			f.mu.Lock()
			calls := f.nodeCalls
			f.mu.Unlock()
			if calls != 1 || !f.f.native.revoked.Load() {
				t.Fatal("denial retried or traffic not revoked", calls)
			}
		})
	}
}

func TestOnlineSessionRenewsIndependentlyOfNode(t *testing.T) {
	f, d := newOnlineRenewalFixture(t, "session-only")
	awaitCredentialCondition(t, func() bool { return f.f.native.probes.Load() > 0 })
	// Advance between endpoint RPCs: a seven-minute discontinuity during a
	// 200ms endpoint receipt is a clock-expiry test, not a renewal scenario.
	d.credentialMu.Lock()
	f.offset.Store(int64(7 * time.Minute))
	d.credentialMu.Unlock()
	awaitCredentialCondition(t, func() bool {
		value, err := f.f.store.Load(t.Context())
		if err != nil {
			return false
		}
		defer clearConnectionKeys(value)
		return value.Profiles[workerProfile].ActiveSession != nil && f.f.native.refreshes.Load() >= 1
	})
	f.mu.Lock()
	nodes, sessions := f.nodeCalls, f.sessionCalls
	f.mu.Unlock()
	if nodes != 0 || sessions != 1 || f.f.native.starts.Load() != 1 || f.f.native.stops.Load() != 0 || f.f.native.revoked.Load() {
		t.Fatal("session-only refresh unnecessarily changed node transport", nodes, sessions)
	}
}

func TestOnlineMetadataReadTimeoutDoesNotStopCredentialOrEndpointLoop(t *testing.T) {
	f, d := newOnlineRenewalFixture(t, "valid")
	backend := f.f.backend
	probes := f.f.native.probes.Load()
	backend.readTimeout.Store(true)
	defer backend.readTimeout.Store(false)
	awaitCredentialCondition(t, func() bool { return backend.timedOutReads.Load() >= 2 })
	backend.readTimeout.Store(false)
	awaitCredentialCondition(t, func() bool { return f.f.native.probes.Load() > probes+2 })
	select {
	case <-d.done:
		t.Fatal("local metadata timeout stopped an authorized native owner", d.result())
	default:
	}
	if f.f.native.starts.Load() != 1 || f.f.native.stops.Load() != 0 || f.f.native.revoked.Load() {
		t.Fatal("metadata retry changed native owner")
	}
}

func TestOnlineRenewalWaitsForAdmittedNativeMapApply(t *testing.T) {
	f, d := newOnlineRenewalFixture(t, "valid")
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	var once sync.Once
	defer once.Do(func() { close(release) })
	f.f.native.refreshHook = func(ctx context.Context) error {
		select {
		case <-entered:
			return nil
		default:
			close(entered)
		}
		select {
		case <-ctx.Done():
			finished <- ctx.Err()
			return ctx.Err()
		case <-release:
			finished <- nil
			return nil
		}
	}
	// A genuine reconnect supplies another signed map to the existing owner.
	f.f.mu.Lock()
	body := f.f.streamBody
	f.f.mu.Unlock()
	if err := body.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("next native map was not admitted")
	}
	f.offset.Store(int64(25 * time.Minute))
	// The renewal worker must wait for this admitted apply before parking its source.
	select {
	case err := <-finished:
		t.Fatal("credential handoff cancelled an admitted map apply", err)
	case <-time.After(100 * time.Millisecond):
	}
	once.Do(func() { close(release) })
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	awaitCredentialCondition(t, func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.nodeCalls > 0 && f.f.native.refreshes.Load() >= 2
	})
	select {
	case <-d.done:
		t.Fatal("handoff terminated native owner", d.result())
	default:
	}
}

func TestOnlineExpiredUnknownRenewalRecoversSameDevice(t *testing.T) {
	f, d := newOnlineRenewalFixture(t, "expired-unknown")
	awaitCredentialCondition(t, func() bool { return f.f.native.probes.Load() > 0 })
	f.offset.Store(int64(25 * time.Minute))
	awaitCredentialCondition(t, func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.nodeCalls == 1 })
	before, err := f.f.store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer clearConnectionKeys(before)
	original := before.Profiles[workerProfile]
	frozen, err := coordinator.DecodeRegisterNodeRequest([]byte(original.NodeRenewal.Request), 16384)
	if err != nil {
		t.Fatal(err)
	}
	f.offset.Store(int64(36 * time.Minute))
	awaitCredentialCondition(t, func() bool { return f.f.native.refreshes.Load() >= 1 })
	after, err := f.f.store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer clearConnectionKeys(after)
	current := after.Profiles[workerProfile]
	if current.ActiveNodeCredential == nil || current.Secrets != original.Secrets ||
		!reflect.DeepEqual(current.Attempt, original.Attempt) || !reflect.DeepEqual(current.Keys, original.Keys) {
		t.Fatal("recovery changed the device or failed to adopt the new credential")
	}
	recovered, err := coordinator.DecodeRegisterNodeRequest([]byte(current.ActiveNodeCredential.Request), 16384)
	if err != nil || recovered.IdempotencyID != coordinator.NodeRenewalRecoveryID(frozen.IdempotencyID) {
		t.Fatal("recovery lost its predecessor operation", err)
	}
	f.mu.Lock()
	calls := f.nodeCalls
	f.mu.Unlock()
	if calls != 2 {
		t.Fatal("recovery dispatched duplicate commands", calls)
	}
	select {
	case <-d.done:
		t.Fatal("recovery terminated the native owner", d.result())
	default:
	}
}

func TestOnlineLateResultCannotAdoptAfterDisconnectGeneration(t *testing.T) {
	f, d := newOnlineRenewalFixture(t, "late")
	t.Cleanup(func() {
		select {
		case <-f.release:
		default:
			close(f.release)
		}
	})
	awaitCredentialCondition(t, func() bool { return f.f.native.probes.Load() > 0 })
	f.offset.Store(int64(25 * time.Minute))
	select {
	case <-f.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("renewal did not start")
	}
	if err := workerIntent(t, f.f.store, state.IntentDisconnected, f.now(), workerRuntime); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- d.Disconnect(workerCallContext(t)) }()
	<-d.lifetime.Done()
	close(f.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("late RPC prevented shutdown")
	}
	value, err := f.f.store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer clearConnectionKeys(value)
	p := value.Profiles[workerProfile]
	if p.ActiveNodeCredential != nil || p.NodeRenewal == nil || p.Connection.Intent != state.IntentDisconnected {
		t.Fatal("stale completion restored authority")
	}
}
