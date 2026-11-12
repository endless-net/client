package localapi

import (
	"bytes"
	"context"
	"errors"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/control"
	"github.com/endless-net/client/internal/localipc"
	"github.com/endless-net/client/internal/state"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var errConfirmation = errors.New("server identity confirmation mismatch")
var errTrustRequired = errors.New("server identity confirmation required")
var errTrustChanged = errors.New("server identity announcement changed")
var errAdmissionUnavailable = errors.New("admission executor unavailable")

func (c *Controller) announcement(ctx context.Context, origin string) (control.Announcement, error) {
	a, err := c.config.Announcements.Fetch(ctx, origin)
	if err != nil || a.Origin != origin || a.Validate(c.config.Now()) != nil {
		return control.Announcement{}, control.ErrBootstrap
	}
	return a, nil
}

func (c *Controller) wake(ctx context.Context, profile, attempt string) error {
	s, err := c.store.Load(ctx)
	if err != nil {
		return err
	}
	p, ok := s.Profiles[profile]
	if !ok || p.Attempt == nil || p.Attempt.AttemptID != attempt {
		return state.ErrStale
	}
	a, err := c.announcement(ctx, p.ControlOrigin)
	if err != nil {
		return err
	}
	if a.VerifiedAt(c.config.Now()) {
		current, loadErr := c.store.Load(ctx)
		if loadErr != nil {
			return loadErr
		}
		err = c.store.Update(ctx, current.Revision, func(current *state.State) error {
			latest, exists := current.Profiles[profile]
			if !exists || latest.Attempt == nil || latest.Attempt.Binding != p.Attempt.Binding || current.BootstrapCheckpoints[p.ControlOrigin] != a.Checkpoint() || !a.VerifiedAt(c.config.Now()) {
				return state.ErrStale
			}
			latest.Trust = state.ServerTrust{KeyID: a.KeyID, AnnouncementID: a.ID, Bundle: a.Bundle}
			current.Profiles[profile] = latest
			return nil
		})
		if err != nil {
			return err
		}
	} else {
		if p.Trust.KeyID == "" {
			return errTrustRequired
		}
		if a.KeyID != p.Trust.KeyID || a.ID != p.Trust.AnnouncementID || !bytes.Equal(a.Bundle, p.Trust.Bundle) {
			return errTrustChanged
		}
	}
	err = c.runner.Wake(ctx, profile, attempt)
	if errors.Is(err, control.ErrLoginPhaseUnsupported) {
		return errAdmissionUnavailable
	}
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errRunnerUnavailable
}

func (c *Controller) dispatch(ctx context.Context, result *clientv0.EnrollmentAttempt) error {
	if !pending(result.Phase) {
		return nil
	}
	err := c.wake(ctx, result.ProfileId, result.AttemptId)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, errTrustRequired), errors.Is(err, errTrustChanged):
		result.BrowserAction = nil
		reason := "client.server_identity.confirmation_required"
		if errors.Is(err, errTrustChanged) {
			reason = "client.server_identity.changed"
		}
		result.Restriction = &clientv0.Restriction{Availability: clientv0.Availability_AVAILABILITY_PERMISSION_REQUIRED, ReasonKey: reason, ActionOwner: clientv0.ActionOwner_ACTION_OWNER_DEVICE_ADMINISTRATOR}
		return nil
	case errors.Is(err, control.ErrBootstrap):
		result.BrowserAction = nil
		result.Restriction = &clientv0.Restriction{Availability: clientv0.Availability_AVAILABILITY_TEMPORARILY_UNAVAILABLE, ReasonKey: "client.server_identity.unavailable", ActionOwner: clientv0.ActionOwner_ACTION_OWNER_SUPPORT}
		return nil
	case errors.Is(err, errAdmissionUnavailable):
		result.Restriction = &clientv0.Restriction{Availability: clientv0.Availability_AVAILABILITY_TEMPORARILY_UNAVAILABLE, ReasonKey: "client.enrollment.registration_unavailable", ActionOwner: clientv0.ActionOwner_ACTION_OWNER_SUPPORT}
		return nil
	default:
		return rpcError(err)
	}
}

func (c *Controller) GetServerIdentity(ctx context.Context, req *clientv0.GetServerIdentityRequest) (*clientv0.GetServerIdentityResponse, error) {
	p, err := localipc.Caller(ctx)
	if err != nil {
		return nil, err
	}
	return c.getIdentity(ctx, p, req)
}
func (c *Controller) getIdentity(ctx context.Context, p localipc.Principal, req *clientv0.GetServerIdentityRequest) (*clientv0.GetServerIdentityResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "profile required")
	}
	s, err := c.store.Load(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	if err = owner(&s, p, false); err != nil {
		return nil, err
	}
	profile, ok := s.Profiles[profileID(req.Profile, s.ActiveProfileID)]
	if !ok {
		return nil, status.Error(codes.NotFound, "profile not found")
	}
	a, err := c.announcement(ctx, profile.ControlOrigin)
	if err != nil {
		return nil, rpcError(err)
	}
	return &clientv0.GetServerIdentityResponse{Identity: &clientv0.ServerIdentity{ProfileId: profile.ID, ControlOrigin: profile.ControlOrigin, TrustedKeyId: profile.Trust.KeyID, AnnouncedKeyId: a.KeyID, Changed: a.ID != profile.Trust.AnnouncementID || a.KeyID != profile.Trust.KeyID, AnnouncementId: a.ID}, Metadata: &clientv0.SnapshotMetadata{InstanceId: c.config.RuntimeID, Revision: s.Revision, GeneratedAt: timestamppb.New(c.config.Now())}}, nil
}

func (c *Controller) TrustServerIdentity(ctx context.Context, req *clientv0.TrustServerIdentityRequest) (*clientv0.TrustServerIdentityResponse, error) {
	p, err := localipc.Caller(ctx)
	if err != nil {
		return nil, err
	}
	return c.trust(ctx, p, req)
}
func (c *Controller) trust(ctx context.Context, caller localipc.Principal, req *clientv0.TrustServerIdentityRequest) (*clientv0.TrustServerIdentityResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if caller.SID == "" || !caller.Administrator {
		return nil, status.Error(codes.PermissionDenied, "OS administrator required")
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "confirmation required")
	}
	if err := c.mutation(req.Mutation); err != nil {
		return nil, err
	}
	s, err := c.store.Load(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	id := profileID(req.Profile, s.ActiveProfileID)
	p, ok := s.Profiles[id]
	if !ok {
		return nil, status.Error(codes.NotFound, "profile not found")
	}
	if req.ConfirmedControlOrigin != p.ControlOrigin {
		return nil, confirmationError()
	}
	// Fetch again for the action; a previous UI observation is not current authority.
	a, err := c.announcement(ctx, p.ControlOrigin)
	if err != nil {
		return nil, rpcError(err)
	}
	if req.ConfirmedKeyId != a.KeyID || req.ConfirmedAnnouncementId != a.ID {
		return nil, confirmationError()
	}
	changed := false
	err = c.store.Update(ctx, req.Mutation.ExpectedRevision, func(current *state.State) error {
		p, ok := current.Profiles[id]
		if !ok || p.ControlOrigin != a.Origin || a.Validate(c.config.Now()) != nil || a.VerifiedAt(c.config.Now()) && current.BootstrapCheckpoints[a.Origin] != a.Checkpoint() {
			return errConfirmation
		}
		if p.Trust.KeyID == a.KeyID && p.Trust.AnnouncementID == a.ID && bytes.Equal(p.Trust.Bundle, a.Bundle) {
			return errReplay
		}
		p.Trust = state.ServerTrust{KeyID: a.KeyID, AnnouncementID: a.ID, Bundle: append([]byte(nil), a.Bundle...)}
		current.Profiles[id] = p
		changed = true
		return nil
	})
	if errors.Is(err, errConfirmation) {
		return nil, confirmationError()
	}
	if err != nil && !errors.Is(err, errReplay) {
		return nil, rpcError(err)
	}
	// Approval persists even if scheduling later fails; retries must read current
	// metadata rather than creating another trust identity or enrollment.
	if p.Attempt != nil {
		result := c.project(&s, p, *p.Attempt)
		if err = c.dispatch(ctx, result); err != nil {
			return nil, err
		}
	}
	return &clientv0.TrustServerIdentityResponse{Change: &clientv0.ChangeResult{Changed: changed}}, nil
}

func confirmationError() error {
	s, err := status.New(codes.FailedPrecondition, "client.server_identity.confirmation_mismatch").WithDetails(&clientv0.Failure{Code: clientv0.ErrorCode_ERROR_CODE_IDENTITY_CONFIRMATION_MISMATCH, ReasonKey: "client.server_identity.confirmation_mismatch", ActionOwner: clientv0.ActionOwner_ACTION_OWNER_DEVICE_ADMINISTRATOR})
	if err != nil {
		return status.Error(codes.FailedPrecondition, "client.server_identity.confirmation_mismatch")
	}
	return s.Err()
}
