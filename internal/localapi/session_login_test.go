package localapi

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/json"
	"testing"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/localipc"
	"github.com/endless-net/client/internal/state"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const sessionRequestID = "3d2e7c6c-fefd-4a60-b2d1-561bc839b053"

type sessionRunner struct {
	store *state.Store
	calls int
}

func (r *sessionRunner) Wake(ctx context.Context, profile, attempt string) error {
	s, err := r.store.Load(ctx)
	if err != nil {
		return err
	}
	l := s.Profiles[profile].SessionLogin
	if l == nil || l.Binding.AttemptID != attempt || l.RequestProof == "" {
		return state.ErrCorrupt
	}
	r.calls++
	return nil
}

func sessionRequest() *clientv0.StartSessionLoginRequest {
	return &clientv0.StartSessionLoginRequest{RequestId: sessionRequestID, Mutation: connectionMutation(3)}
}

func TestSessionLoginLostACKExactRetryAcrossRestartAndPureQueries(t *testing.T) {
	c, backend, _, caller := revocationFixture(t)
	runner := &sessionRunner{store: c.store}
	c.config.SessionLogins = runner
	c.config.MaxSessionLogins = 1
	backend.lostACK = true
	if _, err := c.startSessionLogin(t.Context(), caller, sessionRequest()); status.Code(err) != codes.Unavailable {
		t.Fatal("lost ACK was success", err)
	}
	backend.lostACK = false
	if runner.calls != 0 {
		t.Fatal("uncertain save dispatched work")
	}
	before := append([]byte(nil), backend.data...)
	query := &clientv0.GetSessionLoginRequest{Selector: &clientv0.GetSessionLoginRequest_RequestId{RequestId: sessionRequestID}}
	read, err := c.getSessionLogin(t.Context(), caller, query)
	if err != nil || read.Attempt.RequestId != sessionRequestID || read.Attempt.Phase != clientv0.SessionLoginPhase_SESSION_LOGIN_PHASE_PREPARING || read.Attempt.BrowserAction != nil {
		t.Fatal("cannot recover durable original request", err)
	}
	config := c.config
	config.RuntimeID = "61c69d16-57f6-4722-9df6-6d2aa7191ad1"
	restarted, err := New(c.store, c.runner, config)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := restarted.startSessionLogin(t.Context(), caller, sessionRequest())
	if err != nil || replay.Attempt.AttemptId != read.Attempt.AttemptId || !replay.Attempt.Deadline.AsTime().Equal(read.Attempt.Deadline.AsTime()) {
		t.Fatal("exact retry required stale instance/CAS or started another login", err)
	}
	if _, err := restarted.getSessionLogin(t.Context(), caller, &clientv0.GetSessionLoginRequest{Selector: &clientv0.GetSessionLoginRequest_Current{Current: true}}); err != nil {
		t.Fatal(err)
	}
	session, err := restarted.getSession(t.Context(), caller, &clientv0.GetSessionRequest{})
	if err != nil || session.LoginAttempt == nil || session.LoginAttempt.RequestId != sessionRequestID {
		t.Fatal("GetSession lost current attempt", err)
	}
	if !bytes.Equal(before, backend.data) || runner.calls != 0 {
		t.Fatal("query/exact replay changed state or woke remote work")
	}
	changed := sessionRequest()
	changed.RequestId = "e49a7fae-0fe3-4206-9fcf-b52d6ad57ffc"
	changed.Mutation = &clientv0.MutationContext{ExpectedInstanceId: config.RuntimeID, ExpectedRevision: read.Attempt.Metadata.Revision}
	if _, err := restarted.startSessionLogin(t.Context(), caller, changed); status.Code(err) != codes.FailedPrecondition {
		t.Fatal("active attempt replaced", err)
	}
}

func TestSessionLoginOwnerAndInvalidInputsDoNotClaimOrMutate(t *testing.T) {
	c, backend, _, caller := revocationFixture(t)
	runner := &sessionRunner{store: c.store}
	c.config.SessionLogins = runner
	c.config.MaxSessionLogins = 1
	before := append([]byte(nil), backend.data...)
	foreign := localipc.Principal{SID: "S-1-5-21-99", Administrator: true}
	if _, err := c.startSessionLogin(t.Context(), foreign, sessionRequest()); status.Code(err) != codes.PermissionDenied {
		t.Fatal("foreign admin started owner login", err)
	}
	if _, err := c.getSessionLogin(t.Context(), foreign, &clientv0.GetSessionLoginRequest{Selector: &clientv0.GetSessionLoginRequest_Current{Current: true}}); status.Code(err) != codes.PermissionDenied {
		t.Fatal("foreign admin read owner login", err)
	}
	for _, input := range []*clientv0.GetSessionLoginRequest{nil, {}, {Selector: &clientv0.GetSessionLoginRequest_Current{Current: false}}} {
		if _, err := c.getSessionLogin(t.Context(), caller, input); status.Code(err) != codes.InvalidArgument {
			t.Fatal("invalid query accepted", err)
		}
	}
	if _, err := c.getSessionLogin(t.Context(), caller, &clientv0.GetSessionLoginRequest{Selector: &clientv0.GetSessionLoginRequest_Current{Current: true}}); status.Code(err) != codes.NotFound {
		t.Fatal("missing attempt invented", err)
	}
	if !bytes.Equal(before, backend.data) || runner.calls != 0 {
		t.Fatal("rejected input wrote state/dispatched")
	}
	unclaimed, b, r, _ := fixture(t)
	unclaimed.config.SessionLogins = runner
	unclaimed.config.MaxSessionLogins = 1
	unclaimedBefore := append([]byte(nil), b.data...)
	if _, err := unclaimed.startSessionLogin(t.Context(), caller, sessionRequest()); status.Code(err) != codes.PermissionDenied {
		t.Fatal("Start claimed fresh installation", err)
	}
	if !bytes.Equal(unclaimedBefore, b.data) || r.calls != 0 {
		t.Fatal("Start changed fresh installation")
	}
}

func TestSessionLoginCapacityFailurePrecedesDispatchAndPreservesBytes(t *testing.T) {
	c, backend, _, caller := revocationFixture(t)
	value, err := c.store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	block, err := aes.NewCipher(bytes.Repeat([]byte{3}, 32))
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	c.store, err = state.New(backend, testCodec{gcm}, installationID, int64(len(raw)+1), 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	runner := &sessionRunner{store: c.store}
	c.config.SessionLogins = runner
	c.config.MaxSessionLogins = 1
	before := append([]byte(nil), backend.data...)
	if _, err := c.startSessionLogin(t.Context(), caller, sessionRequest()); status.Code(err) != codes.ResourceExhausted {
		t.Fatal("capacity admitted or misclassified", err)
	}
	if runner.calls != 0 || !bytes.Equal(before, backend.data) {
		t.Fatal("capacity failure changed bytes/dispatched")
	}
}
