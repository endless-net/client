package localapi

import (
	"context"
	"errors"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/localipc"
	"github.com/endless-net/client/internal/state"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (c *Controller) GetEnrollmentAttempt(ctx context.Context, req *clientv0.GetEnrollmentAttemptRequest) (*clientv0.GetEnrollmentAttemptResponse, error) {
	p, err := localipc.Caller(ctx)
	if err != nil {
		return nil, err
	}
	return c.get(ctx, p, req)
}
func (c *Controller) get(ctx context.Context, p localipc.Principal, req *clientv0.GetEnrollmentAttemptRequest) (*clientv0.GetEnrollmentAttemptResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if req == nil || ((req.GetAttemptId() == "") == (req.GetRequestId() == "")) || (req.GetAttemptId() != "" && !validID(req.GetAttemptId())) || (req.GetRequestId() != "" && !validID(req.GetRequestId())) {
		return nil, status.Error(codes.InvalidArgument, "one enrollment selector required")
	}
	// This is a read of the original attempt. A worker may commit between Load
	// and the local observation CAS; reread and reauthorize without repeating a
	// user mutation or creating another remote operation. The outer budget stays
	// fixed, and persistent contention remains a visible typed stale result.
	for range 3 {
		result, err := c.getObserved(ctx, p, req)
		if !errors.Is(err, state.ErrStale) {
			return result, err
		}
		if err := ctx.Err(); err != nil {
			return nil, rpcError(err)
		}
	}
	return nil, rpcError(state.ErrStale)
}

func (c *Controller) getObserved(ctx context.Context, p localipc.Principal, req *clientv0.GetEnrollmentAttemptRequest) (*clientv0.GetEnrollmentAttemptResponse, error) {
	s, err := c.store.Load(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	if err = owner(&s, p, false); err != nil {
		return nil, err
	}
	id := profileID(req.Profile, s.ActiveProfileID)
	profile, ok := s.Profiles[id]
	if !ok || profile.Attempt == nil || (req.GetAttemptId() != "" && req.GetAttemptId() != profile.Attempt.AttemptID) || (req.GetRequestId() != "" && req.GetRequestId() != profile.Attempt.RequestID) {
		return nil, status.Error(codes.NotFound, "enrollment not found")
	}
	a, err := profile.Attempt.Observe(c.config.Now())
	if profile.RemoteInFlight && !a.Uncertain && a.Phase == enrollment.Expired {
		if profile.Attempt.BrowserApproval {
			a, err = profile.Attempt.RecoverLostCall(profile.InFlightAt, c.config.Now())
		} else {
			a, err = a.RecoverLostCall(profile.InFlightAt, c.config.Now())
		}
	}
	if err != nil {
		return nil, rpcError(err)
	}
	result := c.project(&s, profile, a)
	if a != *profile.Attempt {
		err = c.store.Update(ctx, s.Revision, func(current *state.State) error {
			if err := owner(current, p, false); err != nil {
				return err
			}
			profile.Attempt = &a
			current.Profiles[id] = profile
			result = c.project(current, profile, a)
			result.Metadata.Revision++
			return nil
		})
		if errors.Is(err, state.ErrStale) {
			return nil, state.ErrStale
		}
		if err != nil {
			return nil, rpcError(err)
		}
	}
	if err = c.dispatch(ctx, result); err != nil {
		return nil, err
	}
	return &clientv0.GetEnrollmentAttemptResponse{Attempt: result}, nil
}

func (c *Controller) CancelEnrollmentAttempt(ctx context.Context, req *clientv0.CancelEnrollmentAttemptRequest) (*clientv0.CancelEnrollmentAttemptResponse, error) {
	p, err := localipc.Caller(ctx)
	if err != nil {
		return nil, err
	}
	return c.cancelAttempt(ctx, p, req)
}
func (c *Controller) cancelAttempt(ctx context.Context, p localipc.Principal, req *clientv0.CancelEnrollmentAttemptRequest) (*clientv0.CancelEnrollmentAttemptResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if req == nil || !validID(req.AttemptId) {
		return nil, status.Error(codes.InvalidArgument, "attempt required")
	}
	if err := c.mutation(req.Mutation); err != nil {
		return nil, err
	}
	var result *clientv0.EnrollmentAttempt
	err := c.store.Update(ctx, req.Mutation.ExpectedRevision, func(s *state.State) error {
		if err := owner(s, p, false); err != nil {
			return err
		}
		id := profileID(req.Profile, s.ActiveProfileID)
		profile, ok := s.Profiles[id]
		if !ok || profile.Attempt == nil || profile.Attempt.AttemptID != req.AttemptId {
			return status.Error(codes.NotFound, "enrollment not found")
		}
		a := *profile.Attempt
		var err error
		if profile.RemoteInFlight {
			if a.BrowserApproval {
				a, err = a.RecoverLostCall(profile.InFlightAt, c.config.Now())
			} else {
				a, err = a.MarkUncertain(c.config.Now())
			}
			if err != nil {
				return err
			}
		}
		a, err = a.Cancel(c.config.Now())
		if err != nil {
			return err
		}
		result = c.project(s, profile, a)
		if a == *profile.Attempt {
			return errReplay
		}
		profile.Attempt = &a
		s.Profiles[id] = profile
		result.Metadata.Revision++
		return nil
	})
	if err != nil && !errors.Is(err, errReplay) {
		return nil, rpcError(err)
	}
	return &clientv0.CancelEnrollmentAttemptResponse{Attempt: result}, nil
}
