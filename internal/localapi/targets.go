package localapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/control"
	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/localipc"
	"github.com/endless-net/client/internal/state"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type TargetSource interface {
	Fetch(context.Context, state.Profile) ([]*clientv0.EnrollmentTarget, error)
}

var errNoTargets = errors.New("no authorized enrollment targets")

func targetError(err error) error {
	code, typed, reason, actor := codes.Unavailable, clientv0.ErrorCode_ERROR_CODE_UNAVAILABLE, "client.enrollment.targets_unavailable", clientv0.ActionOwner_ACTION_OWNER_SUPPORT
	switch {
	case errors.Is(err, errNoTargets):
		code, typed, reason, actor = codes.PermissionDenied, clientv0.ErrorCode_ERROR_CODE_PERMISSION_REQUIRED, "client.enrollment.no_targets", clientv0.ActionOwner_ACTION_OWNER_ACCESS_ADMINISTRATOR
	case errors.Is(err, control.ErrQueryLimit):
		code, typed, reason = codes.ResourceExhausted, clientv0.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED, "client.enrollment.query_limit"
	case errors.Is(err, control.ErrTargetDenied):
		code, typed, reason, actor = codes.PermissionDenied, clientv0.ErrorCode_ERROR_CODE_PERMISSION_REQUIRED, "client.enrollment.target_denied", clientv0.ActionOwner_ACTION_OWNER_ACCESS_ADMINISTRATOR
	case errors.Is(err, control.ErrSessionDenied):
		code, typed, reason, actor = codes.Unauthenticated, clientv0.ErrorCode_ERROR_CODE_UNAUTHENTICATED, "client.session.login_required", clientv0.ActionOwner_ACTION_OWNER_USER
	case errors.Is(err, control.ErrBootstrap):
		code, typed, reason, actor = codes.FailedPrecondition, clientv0.ErrorCode_ERROR_CODE_SERVER_IDENTITY_CHANGED, "client.server_identity.unavailable", clientv0.ActionOwner_ACTION_OWNER_DEVICE_ADMINISTRATOR
	case errors.Is(err, state.ErrStale):
		code, typed, reason, actor = codes.Aborted, clientv0.ErrorCode_ERROR_CODE_STALE_STATE, "client.enrollment.targets_stale", clientv0.ActionOwner_ACTION_OWNER_USER
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return rpcError(err)
	}
	s, e := status.New(code, reason).WithDetails(&clientv0.Failure{Code: typed, ReasonKey: reason, ActionOwner: actor})
	if e != nil {
		return status.Error(code, reason)
	}
	return s.Err()
}

func (c *Controller) targetContext(ctx context.Context, caller localipc.Principal, ref *clientv0.ProfileRef, attempt string) (state.State, state.Profile, error) {
	s, err := c.store.Load(ctx)
	if err != nil {
		return s, state.Profile{}, rpcError(err)
	}
	if err = owner(&s, caller, false); err != nil {
		return s, state.Profile{}, err
	}
	p, ok := s.Profiles[profileID(ref, s.ActiveProfileID)]
	if !ok || p.Attempt == nil || p.Attempt.AttemptID != attempt {
		return s, p, status.Error(codes.NotFound, "enrollment not found")
	}
	if (p.Attempt.Phase != enrollment.ChoosingContext && p.Attempt.Phase != enrollment.Registering) || p.Attempt.Uncertain || !c.config.Now().Before(p.Attempt.Deadline) {
		return s, p, status.Error(codes.FailedPrecondition, "target selection not active")
	}
	if p.Secrets.Access == "" || p.Secrets.SessionID == "" || !c.config.Now().Before(p.Secrets.SessionExpiresAt) {
		return s, p, targetError(control.ErrSessionDenied)
	}
	if c.config.Targets == nil || c.config.TargetPageKey == [32]byte{} {
		return s, p, targetError(errors.New("target adapter not configured"))
	}
	return s, p, nil
}

func (c *Controller) ListEnrollmentTargets(ctx context.Context, req *clientv0.ListEnrollmentTargetsRequest) (*clientv0.ListEnrollmentTargetsResponse, error) {
	p, err := localipc.Caller(ctx)
	if err != nil {
		return nil, err
	}
	return c.listTargets(ctx, p, req)
}
func (c *Controller) listTargets(ctx context.Context, caller localipc.Principal, req *clientv0.ListEnrollmentTargetsRequest) (*clientv0.ListEnrollmentTargetsResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if req == nil || !validID(req.AttemptId) {
		return nil, status.Error(codes.InvalidArgument, "attempt required")
	}
	s, p, err := c.targetContext(ctx, caller, req.Profile, req.AttemptId)
	if err != nil {
		return nil, err
	}
	size := req.GetPage().GetPageSize()
	if size == 0 {
		size = 100
	}
	if size > 500 {
		return nil, status.Error(codes.InvalidArgument, "page size exceeds 500")
	}
	binding := c.pageBinding(s, p, caller.SID, size)
	offset := uint64(0)
	expiry := c.config.Now().Add(300 * time.Second)
	for _, limit := range []time.Time{p.Attempt.Deadline, p.Secrets.SessionExpiresAt} {
		if limit.Before(expiry) {
			expiry = limit
		}
	}
	var expected []byte
	if token := req.GetPage().GetPageToken(); token != "" {
		if len(token) != 151 {
			return nil, targetError(state.ErrStale)
		}
		data, e := base64.RawURLEncoding.Strict().DecodeString(token)
		if e != nil || len(data) != 113 || data[0] != 1 {
			return nil, targetError(state.ErrStale)
		}
		mac := hmac.New(sha256.New, c.config.TargetPageKey[:])
		_, _ = mac.Write(data[:81])
		if !hmac.Equal(mac.Sum(nil), data[81:]) || !hmac.Equal(binding[:], data[49:81]) {
			return nil, targetError(state.ErrStale)
		}
		expiry = time.Unix(int64(binary.BigEndian.Uint64(data[1:9])), 0)
		if !c.config.Now().Before(expiry) {
			return nil, targetError(state.ErrStale)
		}
		offset = binary.BigEndian.Uint64(data[9:17])
		expected = data[17:49]
	}
	// Every page refetches the bounded catalog and reauthorizes each producer page.
	targets, err := c.config.Targets.Fetch(ctx, p)
	if err != nil {
		return nil, targetError(err)
	}
	data, err := proto.MarshalOptions{Deterministic: true}.Marshal(&clientv0.ListEnrollmentTargetsResponse{Targets: targets})
	if len(targets) == 0 {
		return nil, targetError(errNoTargets)
	}
	if err != nil {
		return nil, targetError(control.ErrTargetProtocol)
	}
	digest := sha256.Sum256(data)
	if (expected != nil && !hmac.Equal(expected, digest[:])) || offset > uint64(len(targets)) {
		return nil, targetError(state.ErrStale)
	}
	current, err := c.store.Load(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	if current.Revision != s.Revision {
		return nil, targetError(state.ErrStale)
	}
	end := offset + uint64(size)
	if end > uint64(len(targets)) {
		end = uint64(len(targets))
	}
	metadata := &clientv0.SnapshotMetadata{InstanceId: c.config.RuntimeID, Revision: s.Revision, GeneratedAt: timestamppb.New(c.config.Now())}
	page := &clientv0.PageResponse{Metadata: metadata}
	if end < uint64(len(targets)) {
		token := make([]byte, 81)
		token[0] = 1
		binary.BigEndian.PutUint64(token[1:9], uint64(expiry.Unix()))
		binary.BigEndian.PutUint64(token[9:17], end)
		copy(token[17:49], digest[:])
		copy(token[49:81], binding[:])
		mac := hmac.New(sha256.New, c.config.TargetPageKey[:])
		_, _ = mac.Write(token)
		page.NextPageToken = base64.RawURLEncoding.EncodeToString(append(token, mac.Sum(nil)...))
	}
	return &clientv0.ListEnrollmentTargetsResponse{Targets: targets[int(offset):int(end)], Page: page, Metadata: metadata}, nil
}

func (c *Controller) pageBinding(s state.State, p state.Profile, sid string, size uint32) [32]byte {
	h := sha256.New()
	access := sha256.Sum256([]byte(p.Secrets.Access))
	for _, value := range []string{"endlessnet-target-page-003-v1", c.config.RuntimeID, sid, p.ID, p.Attempt.AttemptID, p.Attempt.RequestID, p.ControlOrigin, p.Secrets.SessionID, p.Trust.AnnouncementID, fmt.Sprint(s.Revision), fmt.Sprint(size), string(access[:])} {
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(value)))
		_, _ = h.Write(length[:])
		_, _ = h.Write([]byte(value))
	}
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func (c *Controller) SelectEnrollmentTarget(ctx context.Context, req *clientv0.SelectEnrollmentTargetRequest) (*clientv0.SelectEnrollmentTargetResponse, error) {
	p, err := localipc.Caller(ctx)
	if err != nil {
		return nil, err
	}
	return c.selectTarget(ctx, p, req)
}
func (c *Controller) selectTarget(ctx context.Context, caller localipc.Principal, req *clientv0.SelectEnrollmentTargetRequest) (*clientv0.SelectEnrollmentTargetResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if req == nil || !validID(req.AttemptId) || req.AccountId == "" || req.NetworkId == "" {
		return nil, status.Error(codes.InvalidArgument, "target required")
	}
	if err := c.mutation(req.Mutation); err != nil {
		return nil, err
	}
	s, p, err := c.targetContext(ctx, caller, req.Profile, req.AttemptId)
	if err != nil {
		return nil, err
	}
	if req.Mutation.ExpectedRevision != s.Revision {
		return nil, rpcError(state.ErrStale)
	}
	if p.Attempt.AccountID != "" && (p.Attempt.AccountID != req.AccountId || p.Attempt.NetworkID != req.NetworkId) {
		return nil, rpcError(errBindingConflict)
	}
	targets, err := c.config.Targets.Fetch(ctx, p)
	if err != nil {
		return nil, targetError(err)
	}
	found := false
	for _, target := range targets {
		if target.AccountId == req.AccountId && target.NetworkId == req.NetworkId && target.GetRestriction().GetAvailability() == clientv0.Availability_AVAILABILITY_AVAILABLE {
			found = true
			break
		}
	}
	if !found {
		return nil, targetError(control.ErrTargetDenied)
	}
	var result *clientv0.EnrollmentAttempt
	err = c.store.Update(ctx, s.Revision, func(s *state.State) error {
		if err := owner(s, caller, false); err != nil {
			return err
		}
		p := s.Profiles[p.ID]
		a, err := p.Attempt.Select(req.AccountId, req.NetworkId, c.config.Now())
		if err != nil {
			return err
		}
		if p.Secrets.RegistrationID == "" {
			p.Secrets.RegistrationID, err = randomID()
			if err != nil {
				return err
			}
		}
		p.Attempt = &a
		s.Profiles[p.ID] = p
		result = c.project(s, p, a)
		result.Metadata.Revision++
		return nil
	})
	if err != nil {
		return nil, rpcError(err)
	}
	if err = c.dispatch(ctx, result); err != nil {
		return nil, err
	}
	return &clientv0.SelectEnrollmentTargetResponse{Attempt: result}, nil
}
