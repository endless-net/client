package localapi

import (
	"context"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/localipc"
	"github.com/endless-net/client/internal/state"
	identityapi "github.com/endless-net/identity/api/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (c *Controller) StartSessionLogin(ctx context.Context, request *clientv0.StartSessionLoginRequest) (*clientv0.StartSessionLoginResponse, error) {
	caller, err := localipc.Caller(ctx)
	if err != nil {
		return nil, err
	}
	return c.startSessionLogin(ctx, caller, request)
}

func (c *Controller) startSessionLogin(parent context.Context, caller localipc.Principal, request *clientv0.StartSessionLoginRequest) (*clientv0.StartSessionLoginResponse, error) {
	if clientv0.ValidateStartSessionLoginRequest(request) != nil {
		return nil, userFailure(codes.InvalidArgument, clientv0.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "client.session.invalid_input")
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	s, err := c.store.Load(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	defer clearProfileKeys(s)
	if err = owner(&s, caller, false); err != nil {
		return nil, rpcError(err)
	}
	id := profileID(request.Profile, s.ActiveProfileID)
	p, ok := s.Profiles[id]
	if !ok {
		return nil, userFailure(codes.NotFound, clientv0.ErrorCode_ERROR_CODE_NOT_FOUND, "client.session.profile_not_found")
	}
	// Retained exact replay precedes instance/revision CAS and never wakes work.
	for otherID, profile := range s.Profiles {
		var receipt *state.SessionLoginReceipt
		if profile.SessionLogin != nil && profile.SessionLogin.Binding.RequestID == request.RequestId {
			r := profile.SessionLogin.Receipt()
			receipt = &r
		}
		if r, found := profile.SessionLoginHistory[request.RequestId]; found {
			receipt = &r
		}
		if receipt != nil {
			if otherID != id {
				return nil, userFailure(codes.FailedPrecondition, clientv0.ErrorCode_ERROR_CODE_RESOURCE_CONFLICT, "client.session.binding_conflict")
			}
			if profile.SessionLogin != nil && profile.SessionLogin.Binding.RequestID == request.RequestId {
				return &clientv0.StartSessionLoginResponse{Attempt: c.projectCurrentSessionLogin(s, profile)}, nil
			}
			return &clientv0.StartSessionLoginResponse{Attempt: c.projectSessionLoginReceipt(s, *receipt)}, nil
		}
	}
	if p.Attempt == nil || p.Attempt.Phase != enrollment.Registered || p.Secrets.UserID == "" {
		return nil, userFailure(codes.FailedPrecondition, clientv0.ErrorCode_ERROR_CODE_NEEDS_ENROLLMENT, "client.session.registration_required")
	}
	if c.config.SessionLogins == nil {
		return nil, userFailure(codes.Unimplemented, clientv0.ErrorCode_ERROR_CODE_UNSUPPORTED, "client.session.login_unavailable")
	}
	if err = c.mutation(request.Mutation); err != nil {
		return nil, err
	}
	var result *clientv0.SessionLoginAttempt
	err = c.store.Update(ctx, request.Mutation.ExpectedRevision, func(value *state.State) error {
		if err := owner(value, caller, false); err != nil {
			return err
		}
		profile, exists := value.Profiles[id]
		if !exists || profile.Attempt == nil || profile.Attempt.Phase != enrollment.Registered {
			return userFailure(codes.FailedPrecondition, clientv0.ErrorCode_ERROR_CODE_NEEDS_ENROLLMENT, "client.session.registration_required")
		}
		now := c.config.Now().UTC()
		pending := 0
		for _, other := range value.Profiles {
			if other.SessionLogin != nil && !other.SessionLogin.Terminal() && now.Before(other.SessionLogin.Deadline) {
				pending++
			}
		}
		if profile.SessionLogin != nil {
			old := profile.SessionLogin
			if now.Before(old.Deadline) || !old.Terminal() && old.Phase != state.SessionLoginUncertain {
				return userFailure(codes.FailedPrecondition, clientv0.ErrorCode_ERROR_CODE_BUSY, "client.session.login_active")
			}
			if profile.SessionLoginHistory == nil {
				profile.SessionLoginHistory = map[string]state.SessionLoginReceipt{}
			}
			profile.SessionLoginHistory[old.Binding.RequestID] = old.Receipt()
		}
		if pending >= c.config.MaxSessionLogins {
			return userFailure(codes.ResourceExhausted, clientv0.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED, "client.session.queue_full")
		}
		attempt, err := randomID()
		if err != nil {
			return err
		}
		proof, err := requestProof()
		if err != nil {
			return err
		}
		profile.SessionLogin, err = state.NewSessionLogin(profile, request.RequestId, attempt, proof, now)
		if err != nil {
			return err
		}
		value.Profiles[id] = profile
		if err = c.store.CheckSessionLoginCapacity(*value, id); err != nil {
			return err
		}
		result = c.projectCurrentSessionLogin(*value, profile)
		result.Metadata.Revision++
		return nil
	})
	if err != nil {
		if status.Code(err) != codes.Unknown {
			return nil, err
		}
		return nil, rpcError(err)
	}
	if err = c.config.SessionLogins.Wake(ctx, id, result.AttemptId); err != nil {
		return nil, userFailure(codes.Unavailable, clientv0.ErrorCode_ERROR_CODE_UNAVAILABLE, "client.session.queue_unavailable")
	}
	return &clientv0.StartSessionLoginResponse{Attempt: result}, nil
}

func (c *Controller) GetSessionLogin(ctx context.Context, request *clientv0.GetSessionLoginRequest) (*clientv0.GetSessionLoginResponse, error) {
	caller, err := localipc.Caller(ctx)
	if err != nil {
		return nil, err
	}
	return c.getSessionLogin(ctx, caller, request)
}

func (c *Controller) getSessionLogin(parent context.Context, caller localipc.Principal, request *clientv0.GetSessionLoginRequest) (*clientv0.GetSessionLoginResponse, error) {
	if clientv0.ValidateGetSessionLoginRequest(request) != nil {
		return nil, userFailure(codes.InvalidArgument, clientv0.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "client.session.invalid_input")
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	s, err := c.store.Load(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	defer clearProfileKeys(s)
	if err = owner(&s, caller, false); err != nil {
		return nil, rpcError(err)
	}
	p, ok := s.Profiles[profileID(request.Profile, s.ActiveProfileID)]
	if !ok {
		return nil, userFailure(codes.NotFound, clientv0.ErrorCode_ERROR_CODE_NOT_FOUND, "client.session.profile_not_found")
	}
	l := p.SessionLogin
	if l != nil && (request.GetCurrent() || request.GetAttemptId() == l.Binding.AttemptID || request.GetRequestId() == l.Binding.RequestID) {
		return &clientv0.GetSessionLoginResponse{Attempt: c.projectCurrentSessionLogin(s, p)}, nil
	}
	if !request.GetCurrent() {
		for _, receipt := range p.SessionLoginHistory {
			if request.GetRequestId() == receipt.Binding.RequestID || request.GetAttemptId() == receipt.Binding.AttemptID {
				return &clientv0.GetSessionLoginResponse{Attempt: c.projectSessionLoginReceipt(s, receipt)}, nil
			}
		}
	}
	return nil, userFailure(codes.NotFound, clientv0.ErrorCode_ERROR_CODE_NOT_FOUND, "client.session.login_not_found")
}

func (c *Controller) projectSessionLoginReceipt(s state.State, r state.SessionLoginReceipt) *clientv0.SessionLoginAttempt {
	phases := map[state.SessionLoginPhase]clientv0.SessionLoginPhase{state.SessionLoginPreparing: clientv0.SessionLoginPhase_SESSION_LOGIN_PHASE_PREPARING, state.SessionLoginPending: clientv0.SessionLoginPhase_SESSION_LOGIN_PHASE_AWAITING_CONFIRMATION, state.SessionLoginSucceeded: clientv0.SessionLoginPhase_SESSION_LOGIN_PHASE_SUCCEEDED, state.SessionLoginDenied: clientv0.SessionLoginPhase_SESSION_LOGIN_PHASE_DENIED, state.SessionLoginExpired: clientv0.SessionLoginPhase_SESSION_LOGIN_PHASE_EXPIRED, state.SessionLoginFailed: clientv0.SessionLoginPhase_SESSION_LOGIN_PHASE_FAILED, state.SessionLoginUncertain: clientv0.SessionLoginPhase_SESSION_LOGIN_PHASE_UNCERTAIN}
	runtime := &clientv0.SessionLoginAttempt{AttemptId: r.Binding.AttemptID, RequestId: r.Binding.RequestID, ProfileId: r.Binding.ProfileID, Phase: phases[r.Phase], CreatedAt: timestamppb.New(r.CreatedAt), Deadline: timestamppb.New(r.Deadline), Metadata: &clientv0.SnapshotMetadata{InstanceId: c.config.RuntimeID, Revision: s.Revision, GeneratedAt: timestamppb.New(c.config.Now())}}
	if r.Reason != "" {
		runtime.Restriction = &clientv0.Restriction{Availability: clientv0.Availability_AVAILABILITY_TEMPORARILY_UNAVAILABLE, ReasonKey: r.Reason, ActionOwner: clientv0.ActionOwner_ACTION_OWNER_USER}
	}
	if r.Phase == state.SessionLoginSucceeded {
		runtime.Session = &clientv0.Session{State: clientv0.SessionState_SESSION_STATE_ACTIVE, ExpiresAt: timestamppb.New(r.SessionExpiresAt)}
		if !c.config.Now().Before(r.SessionExpiresAt) {
			runtime.Session.State = clientv0.SessionState_SESSION_STATE_EXPIRED
		}
	}
	return runtime
}

func (c *Controller) projectCurrentSessionLogin(s state.State, p state.Profile) *clientv0.SessionLoginAttempt {
	l := p.SessionLogin
	if l == nil {
		return nil
	}
	r := c.projectSessionLoginReceipt(s, l.Receipt())
	now := c.config.Now()
	if !l.Terminal() && now.Before(l.EffectiveDeadline()) && (l.Phase != state.SessionLoginUncertain || l.RecoveryUntil.IsZero() || now.Before(l.RecoveryUntil)) {
		seconds := int64((l.NextPollAt.Sub(now) + time.Second - 1) / time.Second)
		r.PollAfterSeconds = uint32(max(1, min(30, seconds)))
	}
	if l.Phase == state.SessionLoginPending && l.Reason == "" && now.Before(l.EffectiveDeadline()) {
		var tx identityapi.NativeLoginTransaction
		if proto.Unmarshal(l.Transaction, &tx) == nil && tx.GetBrowserAction() != nil {
			r.BrowserAction = &clientv0.SessionLoginBrowserAction{VerificationUrl: tx.GetBrowserAction().VerificationUrl, ExpiresAt: tx.GetBrowserAction().ExpiresAt}
		}
	}
	return r
}
