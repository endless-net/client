package control

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	wire "github.com/endless-net/coordinator/api/publicquery/v1"
	sessionv1 "github.com/endless-net/identity/api/session/v1"
	"github.com/endless-net/identity/api/session/v1/sessionv1connect"
	signing "github.com/endless-net/signing/api/contract"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
)

var ErrCredentialsLogin = errors.New("current user login required")
var ErrAccountNodeDenied = errors.New("account node renewal denied")
var ErrNodeRenewalUnresolved = errors.New("node credential renewal outcome unresolved")
var ErrNodeRenewalPending = errors.New("node credential renewal can still be replayed")
var ErrCredentialsProtocol = errors.New("credential renewal response invalid")
var ErrCredentialsUnavailable = errors.New("credential renewal outcome unavailable")

type Credentials struct {
	mu     sync.Mutex
	store  *state.Store
	remote *Registration
	config RegistrationExecutorConfig
}

func NewCredentials(store *state.Store, client *http.Client, announcements Announcements, remoteConfig RegistrationConfig, c RegistrationExecutorConfig) (*Credentials, error) {
	if store == nil || c.Now == nil || c.MaxCommitRetries < 1 || c.WriteBudget < time.Millisecond || c.WriteBudget > 30*time.Second {
		return nil, ErrCredentialsProtocol
	}
	r, err := NewRegistration(client, announcements, remoteConfig)
	if err != nil {
		return nil, err
	}
	return &Credentials{store: store, remote: r, config: c}, nil
}

func (c *Credentials) load(ctx context.Context, id string, generation uint64) (state.State, state.Profile, error) {
	s, err := c.store.Load(ctx)
	if err != nil {
		return s, state.Profile{}, err
	}
	p, ok := s.Profiles[id]
	if !ok || s.ActiveProfileID != id || p.Attempt == nil || p.Attempt.Phase != enrollment.Registered || p.Attempt.Owner != s.OwnerSID || p.Connection == nil || p.Connection.Generation != generation || p.Connection.Intent != state.IntentConnected {
		return s, p, state.ErrStale
	}
	return s, p, nil
}

func (c *Credentials) save(ctx context.Context, id string, generation uint64, change func(*state.Profile) error) error {
	write, stop := context.WithTimeout(context.WithoutCancel(ctx), c.config.WriteBudget)
	defer stop()
	for range c.config.MaxCommitRetries {
		s, _, err := c.load(write, id, generation)
		if err != nil {
			return err
		}
		err = c.store.Update(write, s.Revision, func(value *state.State) error {
			p := value.Profiles[id]
			if err := change(&p); err != nil {
				return err
			}
			value.Profiles[id] = p
			return nil
		})
		if !errors.Is(err, state.ErrStale) {
			return err
		}
	}
	return state.ErrStale
}

func (c *Credentials) trusted(ctx context.Context, p state.Profile) (Announcement, error) {
	a, err := c.remote.announcements.Fetch(ctx, p.ControlOrigin)
	if errors.Is(err, ErrBootstrapUnavailable) {
		return Announcement{}, ErrCredentialsUnavailable
	}
	if err != nil {
		return Announcement{}, err
	}
	if !a.MatchesProfile(p, c.config.Now()) {
		return Announcement{}, ErrBootstrap
	}
	return a, nil
}

// Ensure prepares an existing registered device before native effects. Remote
// calls are single dispatches; admitted retry/restart resumes frozen IDs.
// Successful session/credential adoption never mutates initial login or receipt.
func (c *Credentials) Ensure(ctx context.Context, id string, generation uint64) error {
	return c.Refresh(ctx, id, generation, false, false)
}

// RefreshSession rotates only the user session while a connected profile has no
// live tunnel. Leave a still-replayable Node command bound to its original
// bearer. Once its deadline passes, Connect can recover it with the new session.
func (c *Credentials) RefreshSession(ctx context.Context, id string, generation uint64) error {
	if !c.mu.TryLock() {
		return state.ErrStale
	}
	defer c.mu.Unlock()
	_, p, err := c.load(ctx, id, generation)
	if err != nil {
		return err
	}
	if p.JoinReceipt != nil {
		return nil
	}
	if pendingNodeRenewal(p) && c.config.Now().Before(p.NodeRenewal.Deadline) {
		return ErrNodeRenewalPending
	}
	return c.ensureSession(ctx, id, generation, true)
}

func pendingNodeRenewal(p state.Profile) bool {
	return p.NodeRenewal != nil && (p.ActiveNodeCredential == nil || p.ActiveNodeCredential.Request != p.NodeRenewal.Request)
}

// Refresh also admits proactive rotation chosen by the connection's scheduler.
// Retries retain the frozen request and deadline. Recovery creates a separately bound command.
func (c *Credentials) Refresh(ctx context.Context, id string, generation uint64, session, node bool) error {
	if !c.mu.TryLock() {
		return state.ErrStale
	}
	defer c.mu.Unlock()
	_, p, err := c.load(ctx, id, generation)
	if err != nil {
		return err
	}
	_, ends, err := p.CurrentNodeCredential()
	if err != nil {
		return err
	}
	if p.JoinReceipt != nil {
		if node || pendingNodeRenewal(p) || !ends.After(c.config.Now()) {
			return c.ensureNode(ctx, id, generation)
		}
		return nil
	}
	if pendingNodeRenewal(p) {
		now := c.config.Now()
		if !now.Before(p.NodeRenewal.Deadline) {
			if p.NodeRenewal.SentAt.IsZero() {
				return ErrNodeRenewalUnresolved
			}
			current, err := p.CurrentSession()
			if err != nil {
				return err
			}
			if err = c.ensureSession(ctx, id, generation, session || !current.ExpiresAt.After(now.Add(coordinator.NodeRenewalExecutionTTL))); err != nil {
				return err
			}
			session = false
		}
		if err = c.ensureNode(ctx, id, generation); err != nil {
			return err
		}
		node = false
		_, p, err = c.load(ctx, id, generation)
		if err != nil {
			return err
		}
		_, ends, err = p.CurrentNodeCredential()
		if err != nil {
			return err
		}
	}
	node = node || !ends.After(c.config.Now())
	current, err := p.CurrentSession()
	if err != nil {
		return err
	}
	// Reserve enough session lifetime for a newly frozen Node operation, rather
	// than expiring its bearer in the middle of a lost-response recovery.
	session = session || node && !current.ExpiresAt.After(c.config.Now().Add(coordinator.NodeRenewalExecutionTTL))
	if err = c.ensureSession(ctx, id, generation, session); err != nil {
		return err
	}
	if node {
		return c.ensureNode(ctx, id, generation)
	}
	return nil
}

func (c *Credentials) ensureSession(ctx context.Context, id string, generation uint64, renew bool) error {
	_, p, err := c.load(ctx, id, generation)
	if err != nil {
		return err
	}
	current, err := p.CurrentSession()
	if err != nil {
		return err
	}
	pending := p.SessionRefresh != nil && (p.ActiveSession == nil || p.ActiveSession.RequestID != p.SessionRefresh.RequestID)
	if current.ExpiresAt.After(c.config.Now()) && !pending && !renew {
		return nil
	}
	if !pending {
		now := c.config.Now().UTC()
		if !validProof(current.Renewal) || current.SessionID == "" || current.UserID == "" || !current.RenewalExpiresAt.After(now) {
			return ErrCredentialsLogin
		}
		deadline := now.Add(coordinator.NodeRenewalExecutionTTL)
		if current.RenewalExpiresAt.Before(deadline) {
			deadline = current.RenewalExpiresAt
		}
		proposed := state.SessionRefresh{RequestID: uuid.NewString(), ExpectedSessionID: current.SessionID, UserID: current.UserID, Renewal: current.Renewal, CreatedAt: now, Deadline: deadline}
		if err = c.save(ctx, id, generation, func(p *state.Profile) error {
			latest, err := p.CurrentSession()
			if err != nil || latest != current {
				return state.ErrStale
			}
			p.SessionRefresh = &proposed
			return nil
		}); err != nil {
			return err
		}
	}
	_, p, err = c.load(ctx, id, generation)
	if err != nil {
		return err
	}
	r := *p.SessionRefresh
	if !c.config.Now().Before(r.Deadline) || !r.ReplayUntil.IsZero() && !c.config.Now().Before(r.ReplayUntil) {
		return ErrCredentialsLogin
	}
	call, stop := context.WithTimeout(ctx, min(r.Deadline.Sub(c.config.Now()), 30*time.Second))
	defer stop()
	if _, err = c.trusted(call, p); err != nil {
		return err
	}
	if r.SentAt.IsZero() {
		sent := c.config.Now()
		if err = c.save(ctx, id, generation, func(p *state.Profile) error {
			if p.SessionRefresh == nil || p.SessionRefresh.RequestID != r.RequestID {
				return state.ErrStale
			}
			p.SessionRefresh.SentAt = sent
			return nil
		}); err != nil {
			return err
		}
		r.SentAt = sent
	}
	client := sessionv1connect.NewSessionServiceClient(c.remote.http, p.ControlOrigin, connect.WithReadMaxBytes(c.remote.config.MaxResponseBytes))
	var operation *sessionv1.SessionRenewal
	poll := r.PollAuthorization
	request := &sessionv1.RenewSessionRequest{RequestId: r.RequestID, ExpectedSessionId: r.ExpectedSessionID}
	if r.OperationID == "" {
		req := bearer(request, r.Renewal)
		response, callErr := client.RenewSession(call, req)
		if callErr != nil {
			return credentialCallError(call, callErr)
		}
		if response == nil || response.Msg == nil || len(response.Msg.ProtoReflect().GetUnknown()) != 0 {
			return ErrCredentialsProtocol
		}
		operation, poll = response.Msg.Operation, response.Msg.PollAuthorization
	} else {
		req := bearer(&sessionv1.GetSessionRenewalRequest{OperationId: r.OperationID}, poll)
		response, callErr := client.GetSessionRenewal(call, req)
		if callErr != nil {
			return credentialCallError(call, callErr)
		}
		if response == nil || response.Msg == nil || len(response.Msg.ProtoReflect().GetUnknown()) != 0 {
			return ErrCredentialsProtocol
		}
		operation = response.Msg.Operation
	}
	now := c.config.Now()
	if sessionv1.ValidateSessionRenewal(operation, request, []string{enrollment.NativeConsentOrigin(p.ControlOrigin)}) != nil || len(operation.ProtoReflect().GetUnknown()) != 0 || !validProof(poll) || !now.Before(operation.ReplayExpiresAt.AsTime()) || r.OperationID != "" && operation.OperationId != r.OperationID || !r.ReplayUntil.IsZero() && !r.ReplayUntil.Equal(operation.ReplayExpiresAt.AsTime()) {
		return ErrCredentialsProtocol
	}
	if result := operation.GetResult(); result != nil {
		if result.Session.UserId != r.UserID || result.Session.ExpiresAt == nil || !now.Before(result.Session.ExpiresAt.AsTime()) || result.RenewalAuthorization == nil || !now.Before(result.RenewalAuthorization.ExpiresAt.AsTime()) || result.AccessBearer == r.Renewal || result.RenewalAuthorization.Bearer == r.Renewal {
			return ErrCredentialsProtocol
		}
	}
	encoded, err := proto.Marshal(operation)
	if err != nil {
		return ErrCredentialsProtocol
	}
	if err = c.save(ctx, id, generation, func(p *state.Profile) error {
		if p.SessionRefresh == nil || p.SessionRefresh.RequestID != r.RequestID || p.SessionRefresh.Renewal != r.Renewal || !p.SessionRefresh.Deadline.Equal(r.Deadline) {
			return state.ErrStale
		}
		latest, err := p.CurrentSession()
		if err != nil || latest.SessionID != r.ExpectedSessionID || latest.Renewal != r.Renewal {
			return state.ErrStale
		}
		next := *p.SessionRefresh
		next.OperationID, next.PollAuthorization, next.ReplayUntil, next.Operation = operation.OperationId, poll, operation.ReplayExpiresAt.AsTime(), encoded
		p.SessionRefresh = &next
		if operation.State == sessionv1.SessionRenewalState_SESSION_RENEWAL_STATE_SUCCEEDED {
			p.ActiveSession = &state.ActiveSession{RequestID: r.RequestID, PreviousSessionID: r.ExpectedSessionID, PreviousRenewalSHA256: state.CredentialDigest(r.Renewal), Operation: encoded}
		}
		return nil
	}); err != nil {
		return err
	}
	if operation.State != sessionv1.SessionRenewalState_SESSION_RENEWAL_STATE_SUCCEEDED {
		return ErrCredentialsLogin
	}
	return nil
}

func credentialCallError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if connect.CodeOf(err) == connect.CodeUnauthenticated || connect.CodeOf(err) == connect.CodePermissionDenied {
		return ErrCredentialsLogin
	}
	switch connect.CodeOf(err) {
	case connect.CodeInvalidArgument, connect.CodeDataLoss, connect.CodeUnimplemented, connect.CodeFailedPrecondition:
		return ErrCredentialsProtocol
	}
	return ErrCredentialsUnavailable
}

func (c *Credentials) ensureNode(ctx context.Context, id string, generation uint64) error {
	_, p, err := c.load(ctx, id, generation)
	if err != nil {
		return err
	}
	var current state.SessionCredentials
	if p.JoinReceipt == nil {
		current, err = p.CurrentSession()
		if err != nil {
			return err
		}
		if !current.ExpiresAt.After(c.config.Now()) {
			return ErrCredentialsLogin
		}
	}
	pending := pendingNodeRenewal(p)
	now := c.config.Now().UTC()
	recovery := pending && !now.Before(p.NodeRenewal.Deadline)
	if !pending || recovery {
		operationID := uuid.NewString()
		if recovery {
			if p.NodeRenewal.SentAt.IsZero() {
				return ErrNodeRenewalUnresolved
			}
			source, err := coordinator.DecodeRegisterNodeRequest([]byte(p.NodeRenewal.Request), c.remote.config.MaxRequestBytes)
			if err != nil || source.IdempotencyID == "" {
				return ErrCredentialsProtocol
			}
			operationID = coordinator.NodeRenewalRecoveryID(source.IdempotencyID)
		}
		previous, _, err := p.CurrentNodeCredential()
		if err != nil {
			return err
		}
		initial, err := p.RegistrationIdentity(c.remote.config.MaxRequestBytes)
		if err != nil {
			return ErrCredentialsProtocol
		}
		request := coordinator.RegisterNodeRequest{SchemaVersion: 2, IdempotencyID: operationID, AccountID: p.Attempt.AccountID, NetworkID: p.Attempt.NetworkID, Hostname: initial.Hostname, PublicKey: initial.PublicKey, DeviceFingerprint: initial.DeviceFingerprint, NodeCredential: previous, RegistrationBinding: initial.Binding, SessionTokenBinding: coordinator.RegistrationSessionTokenBinding(current.Access)}
		if coordinator.SetRegisterNodeIdentityProof(&request, ed25519.PrivateKey(p.Keys.IdentityPrivate)) != nil {
			return ErrCredentialsProtocol
		}
		raw, err := json.Marshal(request)
		if err != nil || len(raw) > c.remote.config.MaxRequestBytes {
			return ErrCredentialsProtocol
		}
		deadline := now.Add(coordinator.NodeRenewalExecutionTTL)
		if !current.ExpiresAt.IsZero() && current.ExpiresAt.Before(deadline) {
			deadline = current.ExpiresAt
		}
		proposed := state.NodeRenewal{Request: string(raw), CreatedAt: now, Deadline: deadline}
		if err = c.save(ctx, id, generation, func(p *state.Profile) error {
			latest, _, err := p.CurrentNodeCredential()
			if err != nil || latest != previous {
				return state.ErrStale
			}
			p.NodeRenewal = &proposed
			return nil
		}); err != nil {
			return err
		}
	}
	_, p, err = c.load(ctx, id, generation)
	if err != nil {
		return err
	}
	r := *p.NodeRenewal
	if !c.config.Now().Before(r.Deadline) {
		return ErrNodeRenewalUnresolved
	}
	request, decodeErr := coordinator.DecodeRegisterNodeRequest([]byte(r.Request), c.remote.config.MaxRequestBytes)
	if decodeErr != nil {
		return ErrCredentialsProtocol
	}
	if p.JoinReceipt == nil {
		current, err = p.CurrentSession()
		if err != nil {
			return err
		}
		if r.SentAt.IsZero() && request.SessionTokenBinding != coordinator.RegistrationSessionTokenBinding(current.Access) {
			return ErrNodeRenewalUnresolved
		}
	}
	// Coordinator authorizes a known frozen command against the current
	// same-user session, without changing the signed inputs.

	call, stop := context.WithTimeout(ctx, min(r.Deadline.Sub(c.config.Now()), 30*time.Second))
	defer stop()
	if _, err = c.trusted(call, p); err != nil {
		return err
	}
	if r.SentAt.IsZero() {
		sent := c.config.Now()
		if err = c.save(ctx, id, generation, func(p *state.Profile) error {
			if p.NodeRenewal == nil || p.NodeRenewal.Request != r.Request {
				return state.ErrStale
			}
			p.NodeRenewal.SentAt = sent
			return nil
		}); err != nil {
			return err
		}
		r.SentAt = sent
	}
	requestRPC := connect.NewRequest(&wire.RegisterNodeRequest{
		Proof: registrationProof(request),
		Authorization: &wire.RegisterNodeRequest_Renewal{Renewal: &wire.NodeCredentialRenewal{
			NodeCredential: request.NodeCredential, RegistrationBinding: request.RegistrationBinding,
			SessionTokenBinding: request.SessionTokenBinding,
		}},
	})
	if p.JoinReceipt == nil {
		requestRPC.Header().Set("Authorization", "Bearer "+current.Access)
	}
	response, err := c.remote.rpc(p.ControlOrigin).RegisterNode(call, requestRPC)
	if err != nil {
		if p.JoinReceipt != nil && (connect.CodeOf(err) == connect.CodePermissionDenied || connect.CodeOf(err) == connect.CodeUnauthenticated) {
			return ErrAccountNodeDenied
		}
		if connect.CodeOf(err) == connect.CodeAborted {
			return ErrNodeRenewalUnresolved
		}
		return credentialCallError(call, err)
	}
	m := response.Msg.GetRenewal()
	if !knownRPC(response.Msg.ProtoReflect()) || m == nil || m.IssuedAt.CheckValid() != nil || m.ExpiresAt.CheckValid() != nil {
		return ErrCredentialsProtocol
	}
	result := coordinator.NodeCredentialRenewalResponse{
		SchemaVersion: 1, IdempotencyID: m.IdempotencyId, RenewalBinding: m.RenewalBinding,
		RegistrationBinding: m.RegistrationBinding, PreviousCredentialSHA256: m.PreviousCredentialSha256,
		NetworkID: m.NetworkId, NodeID: m.NodeId, NodeCredential: m.NodeCredential, IssuedAt: m.IssuedAt.AsTime(), ExpiresAt: m.ExpiresAt.AsTime(),
	}
	// Keep the same receipt and deadline if the server clock is ahead.
	// Wait for local time before checking trust or accepting the credential.
	if delay := result.IssuedAt.Sub(c.config.Now()); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-call.Done():
			return call.Err()
		case <-timer.C:
		}
	}
	if err := call.Err(); err != nil {
		return err
	}
	announcement, err := c.trusted(call, p)
	if err != nil {
		return err
	}
	var trust signing.ServerKeyResponse
	if json.Unmarshal(announcement.Bundle, &trust) != nil {
		return ErrBootstrap
	}
	claims, err := coordinator.DecodeNodeCredential(result.NodeCredential, c.remote.config.MaxCredentialBytes)
	if err != nil {
		return ErrCredentialsProtocol
	}
	key, err := signing.ResolveBootstrapKey(trust.NodeCredentialTrustBundle, claims.KeyID, c.config.Now())
	if err != nil || key.NotBefore != nil && result.IssuedAt.Before(*key.NotBefore) || key.NotAfter != nil && result.ExpiresAt.After(*key.NotAfter) {
		return ErrBootstrap
	}
	public, err := base64.RawURLEncoding.Strict().DecodeString(key.PublicKey)
	if err != nil {
		return ErrBootstrap
	}
	verify := coordinator.VerifyNodeCredentialRenewalResult
	if p.JoinReceipt != nil {
		verify = coordinator.VerifyAccountNodeRenewalResult
	}
	if _, err = verify(request, result, p.Attempt.NetworkID, p.Attempt.NodeID, request.RegistrationBinding, c.remote.config.MaxCredentialBytes, ed25519.PublicKey(public), c.config.Now()); err != nil {
		return ErrCredentialsProtocol
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return ErrCredentialsProtocol
	}
	return c.save(ctx, id, generation, func(p *state.Profile) error {
		if p.NodeRenewal == nil || p.NodeRenewal.Request != r.Request || !p.NodeRenewal.Deadline.Equal(r.Deadline) {
			return state.ErrStale
		}
		previous, _, err := p.CurrentNodeCredential()
		if err != nil || previous != request.NodeCredential {
			return state.ErrStale
		}
		next := *p.NodeRenewal
		next.Response = string(raw)
		p.NodeRenewal = &next
		p.ActiveNodeCredential = &state.ActiveNodeCredential{Request: r.Request, Receipt: string(raw)}
		return nil
	})
}
