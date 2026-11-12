// Package localapi implements Client-owned RPCs over the authenticated IPC
// boundary. Durable local preparation is separate from remote authorization.
package localapi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/control"
	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/localipc"
	"github.com/endless-net/client/internal/state"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Wake schedules reconciliation of an already persisted attempt. The runner
// must load the original binding, check trust before login, and persist each
// remote request before sending. A full queue returns an error, never a drop.
type Runner interface {
	Wake(context.Context, string, string) error
}

type Config struct {
	Revocations                                       RevocationSource
	Connections                                       ConnectionRunner
	ConnectionClock                                   connectivity.Clock
	ConnectionObservations                            connectivity.ObservationSource
	WatchInterval                                     time.Duration
	Targets                                           TargetSource
	TargetPageKey                                     [32]byte
	Announcements                                     control.Announcements
	RuntimeID, DefaultProfileID, DefaultControlOrigin string
	DefaultProfileDisplayName                         string
	ContractDigest                                    [32]byte
	Build                                             *clientv0.BuildIdentity
	MaxProfiles                                       int
	MaxDisplayNameBytes, MaxHostnameBytes             int
	Now                                               func() time.Time
}

type Controller struct {
	clientv0.UnimplementedClientServiceServer
	store  *state.Store
	runner Runner
	config Config
}

var canonicalID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var errReplay = errors.New("exact local replay")
var errBindingConflict = errors.New("enrollment input binding conflict")
var errRunnerUnavailable = errors.New("enrollment runner unavailable")

func validID(id string) bool {
	return canonicalID.MatchString(id) && id != "00000000-0000-0000-0000-000000000000"
}
func validOrigin(origin string) bool {
	u, err := url.Parse(origin)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.Host == strings.ToLower(u.Host) && u.User == nil && u.Path == "" && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && u.String() == origin
}

func clearProfileKeys(s state.State) {
	for _, profile := range s.Profiles {
		clear(profile.Keys.IdentityPrivate)
		clear(profile.Keys.WireGuardPrivate)
	}
}

func New(store *state.Store, runner Runner, config Config) (*Controller, error) {
	if config.WatchInterval < time.Millisecond || config.WatchInterval > 10*time.Second {
		return nil, state.ErrCorrupt
	}
	if config.Connections != nil && config.ConnectionClock == nil {
		return nil, state.ErrCorrupt
	}
	if (config.Targets == nil) != (config.TargetPageKey == [32]byte{}) {
		return nil, state.ErrCorrupt
	}
	if store == nil || runner == nil || config.Announcements == nil || !validID(config.RuntimeID) || !validID(config.DefaultProfileID) || !validOrigin(config.DefaultControlOrigin) || config.ContractDigest == [32]byte{} || config.Build == nil || config.MaxProfiles <= 0 || config.MaxDisplayNameBytes <= 0 || config.MaxHostnameBytes <= 0 || strings.TrimSpace(config.DefaultProfileDisplayName) == "" || len(config.DefaultProfileDisplayName) > config.MaxDisplayNameBytes || strings.IndexFunc(config.DefaultProfileDisplayName, unicode.IsControl) >= 0 {
		return nil, state.ErrCorrupt
	}
	config.Build = proto.Clone(config.Build).(*clientv0.BuildIdentity)
	if config.Now == nil {
		config.Now = time.Now
	}
	return &Controller{store: store, runner: runner, config: config}, nil
}

func rpcError(err error) error {
	if failure := connectionDetails(err); failure != nil {
		code := codes.Unavailable
		switch failure.Code {
		case clientv0.ErrorCode_ERROR_CODE_PERMISSION_REQUIRED:
			code = codes.PermissionDenied
		case clientv0.ErrorCode_ERROR_CODE_POLICY_BLOCKED:
			code = codes.FailedPrecondition
		case clientv0.ErrorCode_ERROR_CODE_RESOURCE_CONFLICT:
			code = codes.FailedPrecondition
		}
		s, detailErr := status.New(code, failure.ReasonKey).WithDetails(failure)
		if detailErr == nil {
			return s.Err()
		}
		return status.Error(code, failure.ReasonKey)
	}
	code, typed, reason := codes.Unavailable, clientv0.ErrorCode_ERROR_CODE_UNAVAILABLE, "client.state.unavailable"
	// Even a wrapped backend/runner status may contain credentials. Preserve
	// known classifications only; all messages/details are built here.
	switch status.Code(err) {
	case codes.PermissionDenied:
		code, typed, reason = codes.PermissionDenied, clientv0.ErrorCode_ERROR_CODE_OWNER_REQUIRED, "client.owner.required"
	case codes.Unauthenticated:
		code, typed, reason = codes.Unauthenticated, clientv0.ErrorCode_ERROR_CODE_UNAUTHENTICATED, "client.caller.required"
	case codes.NotFound:
		code, typed, reason = codes.NotFound, clientv0.ErrorCode_ERROR_CODE_NOT_FOUND, "client.enrollment.not_found"
	case codes.ResourceExhausted:
		code, typed, reason = codes.ResourceExhausted, clientv0.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED, "client.profile.limit"
	}
	switch {
	case errors.Is(err, errBindingConflict):
		code, typed, reason = codes.FailedPrecondition, clientv0.ErrorCode_ERROR_CODE_RESOURCE_CONFLICT, "client.enrollment.binding_conflict"
	case errors.Is(err, errRunnerUnavailable):
		reason = "client.enrollment.queue_unavailable"
	case errors.Is(err, state.ErrStale):
		code, typed, reason = codes.Aborted, clientv0.ErrorCode_ERROR_CODE_STALE_STATE, "client.state.stale"
	case errors.Is(err, state.ErrUncertain):
		reason = "client.state.write_uncertain"
	case errors.Is(err, context.Canceled):
		code, typed, reason = codes.Canceled, clientv0.ErrorCode_ERROR_CODE_CANCELLED, "client.operation.cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		code, typed, reason = codes.DeadlineExceeded, clientv0.ErrorCode_ERROR_CODE_DEADLINE_EXCEEDED, "client.operation.deadline"
	}
	s, detailErr := status.New(code, reason).WithDetails(&clientv0.Failure{Code: typed, ReasonKey: reason, Retryable: false, ActionOwner: clientv0.ActionOwner_ACTION_OWNER_SUPPORT})
	if detailErr != nil {
		return status.Error(code, reason)
	}
	return s.Err()
}

func owner(s *state.State, p localipc.Principal, claim bool) error {
	if p.SID == "" {
		return status.Error(codes.Unauthenticated, "OS caller required")
	}
	if s.OwnerSID == p.SID {
		return nil
	}
	if s.OwnerSID == "" && claim {
		s.OwnerSID = p.SID
		return nil
	}
	return status.Error(codes.PermissionDenied, "installation owner required")
}

func (c *Controller) mutation(m *clientv0.MutationContext) error {
	if m == nil || m.ExpectedRevision == 0 {
		return status.Error(codes.InvalidArgument, "mutation preconditions required")
	}
	if m.ExpectedInstanceId != c.config.RuntimeID {
		return rpcError(state.ErrStale)
	}
	return nil
}

func randomID() (string, error) {
	var v [16]byte
	if _, err := rand.Read(v[:]); err != nil {
		return "", err
	}
	v[6] = v[6]&15 | 64
	v[8] = v[8]&63 | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", v[:4], v[4:6], v[6:8], v[8:10], v[10:]), nil
}
func requestProof() (string, error) {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value[:]), nil
}
func profileID(ref *clientv0.ProfileRef, active string) string {
	if ref == nil || ref.ProfileId == "" {
		return active
	}
	return ref.ProfileId
}

func (c *Controller) GetRuntimeInfo(ctx context.Context, _ *clientv0.GetRuntimeInfoRequest) (*clientv0.GetRuntimeInfoResponse, error) {
	p, err := localipc.Caller(ctx)
	if err != nil {
		return nil, err
	}
	return c.runtimeInfo(ctx, p)
}

func (c *Controller) runtimeInfo(ctx context.Context, p localipc.Principal) (*clientv0.GetRuntimeInfoResponse, error) {
	if p.SID == "" {
		return nil, status.Error(codes.Unauthenticated, "local OS caller required")
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	s, err := c.store.Load(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	defer clearProfileKeys(s)
	access := clientv0.Access_ACCESS_OBSERVER
	if s.OwnerSID == p.SID {
		access = clientv0.Access_ACCESS_OWNER
	}
	if p.Administrator {
		access = clientv0.Access_ACCESS_ADMINISTRATOR
	}
	capabilities := enrollmentCapabilities(s, p, c.config.Build.Platform)
	if c.config.Connections != nil && c.config.ConnectionObservations != nil {
		restriction := &clientv0.Restriction{Availability: clientv0.Availability_AVAILABILITY_PERMISSION_REQUIRED, ReasonKey: "client.owner.required", ActionOwner: clientv0.ActionOwner_ACTION_OWNER_USER}
		if s.OwnerSID != "" && s.OwnerSID == p.SID {
			restriction = &clientv0.Restriction{Availability: clientv0.Availability_AVAILABILITY_AVAILABLE}
		}
		capabilities = append(capabilities, &clientv0.CapabilityStatus{Capability: clientv0.Capability_CAPABILITY_CONNECTION, Restriction: restriction, Platform: c.config.Build.Platform})
	}
	return &clientv0.GetRuntimeInfoResponse{Runtime: &clientv0.RuntimeInfo{Build: proto.Clone(c.config.Build).(*clientv0.BuildIdentity), InstanceId: c.config.RuntimeID, CallerAccess: access, Capabilities: capabilities, ContractSha256: hex.EncodeToString(c.config.ContractDigest[:]), Protocol: "endlessnet-client-ipc", IpcVersion: 0}}, nil
}

func (c *Controller) CreateProfile(ctx context.Context, req *clientv0.CreateProfileRequest) (*clientv0.CreateProfileResponse, error) {
	p, err := localipc.Caller(ctx)
	if err != nil {
		return nil, err
	}
	return c.createProfile(ctx, p, req)
}
func (c *Controller) createProfile(ctx context.Context, p localipc.Principal, req *clientv0.CreateProfileRequest) (*clientv0.CreateProfileResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "profile required")
	}
	if err := c.mutation(req.Mutation); err != nil {
		return nil, err
	}
	if !validOrigin(req.ControlOrigin) || strings.TrimSpace(req.DisplayName) == "" || len(req.DisplayName) > c.config.MaxDisplayNameBytes || strings.IndexFunc(req.DisplayName, unicode.IsControl) >= 0 {
		return nil, status.Error(codes.InvalidArgument, "invalid profile")
	}
	id, err := randomID()
	if err != nil {
		return nil, rpcError(err)
	}
	err = c.store.Update(ctx, req.Mutation.ExpectedRevision, func(s *state.State) error {
		if err := owner(s, p, true); err != nil {
			return err
		}
		if len(s.Profiles) >= c.config.MaxProfiles {
			return status.Error(codes.ResourceExhausted, "profile limit reached")
		}
		if s.Profiles == nil {
			s.Profiles = map[string]state.Profile{}
		}
		s.Profiles[id] = state.Profile{ID: id, DisplayName: req.DisplayName, ControlOrigin: req.ControlOrigin}
		if s.ActiveProfileID == "" {
			s.ActiveProfileID = id
		}
		return nil
	})
	if err != nil {
		return nil, rpcError(err)
	}
	return &clientv0.CreateProfileResponse{Profile: &clientv0.ProfileResult{ProfileId: id}}, nil
}

func (c *Controller) Enroll(ctx context.Context, req *clientv0.EnrollRequest) (*clientv0.EnrollResponse, error) {
	p, err := localipc.Caller(ctx)
	if err != nil {
		return nil, err
	}
	return c.begin(ctx, p, req)
}
func (c *Controller) begin(ctx context.Context, p localipc.Principal, req *clientv0.EnrollRequest) (*clientv0.EnrollResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "enrollment required")
	}
	if err := c.mutation(req.Mutation); err != nil {
		return nil, err
	}
	if !validID(req.RequestId) || req.Hostname == "" || len(req.Hostname) > c.config.MaxHostnameBytes || strings.TrimSpace(req.Hostname) != req.Hostname || strings.IndexFunc(req.Hostname, unicode.IsControl) >= 0 || req.Mode < clientv0.EnrollmentMode_ENROLLMENT_MODE_WORKSTATION || req.Mode > clientv0.EnrollmentMode_ENROLLMENT_MODE_INTERACTIVE {
		return nil, status.Error(codes.InvalidArgument, "invalid enrollment input")
	}
	var result *clientv0.EnrollmentAttempt
	err := c.store.Update(ctx, req.Mutation.ExpectedRevision, func(s *state.State) error {
		if err := owner(s, p, true); err != nil {
			return err
		}
		id := profileID(req.Profile, s.ActiveProfileID)
		if id == "" {
			id = c.config.DefaultProfileID
		}
		profile, ok := s.Profiles[id]
		if !ok {
			if len(s.Profiles) != 0 || id != c.config.DefaultProfileID {
				return status.Error(codes.NotFound, "profile not found")
			}
			profile = state.Profile{ID: id, ControlOrigin: c.config.DefaultControlOrigin, DisplayName: c.config.DefaultProfileDisplayName}
			s.ActiveProfileID = id
		}
		for otherID, other := range s.Profiles {
			if otherID != id && other.Attempt != nil && other.Attempt.RequestID == req.RequestId {
				return errBindingConflict
			}
		}
		if profile.Attempt != nil {
			if profile.Attempt.RequestID != req.RequestId || profile.Mode != int32(req.Mode) || profile.Hostname != req.Hostname {
				return errBindingConflict
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
		}
		keys, err := state.GenerateKeys()
		if err != nil {
			return err
		}
		fingerprint, err := keys.Fingerprint()
		if err != nil {
			return err
		}
		idAttempt, err := randomID()
		if err != nil {
			return err
		}
		proof, err := requestProof()
		if err != nil {
			return err
		}
		a, err := enrollment.New(enrollment.Binding{AttemptID: idAttempt, RequestID: req.RequestId, ProfileID: id, Owner: p.SID, ControlOrigin: profile.ControlOrigin, DeviceKeyHash: fingerprint}, c.config.Now())
		if err != nil {
			return err
		}
		profile.Keys = keys
		profile.Attempt = &a
		profile.Mode = int32(req.Mode)
		profile.Hostname = req.Hostname
		profile.Secrets.RequestProof = proof
		if s.Profiles == nil {
			s.Profiles = map[string]state.Profile{}
		}
		s.Profiles[id] = profile
		result = c.project(s, profile, a)
		result.Metadata.Revision++
		return nil
	})
	if err != nil && !errors.Is(err, errReplay) {
		return nil, rpcError(err)
	}
	// Never wake a worker before persistence succeeds or after a failed/ambiguous
	// write. Duplicate wakes reconcile the original UUID/key; they are not Enroll.
	if err = c.dispatch(ctx, result); err != nil {
		return nil, err
	}
	return &clientv0.EnrollResponse{Attempt: result}, nil
}

func pending(p clientv0.EnrollmentPhase) bool {
	return p >= clientv0.EnrollmentPhase_ENROLLMENT_PHASE_AWAITING_LOGIN && p <= clientv0.EnrollmentPhase_ENROLLMENT_PHASE_COMPLETING
}

func (c *Controller) project(s *state.State, p state.Profile, a enrollment.Attempt) *clientv0.EnrollmentAttempt {
	phases := map[enrollment.Phase]clientv0.EnrollmentPhase{enrollment.AwaitingLogin: clientv0.EnrollmentPhase_ENROLLMENT_PHASE_AWAITING_LOGIN, enrollment.ChoosingContext: clientv0.EnrollmentPhase_ENROLLMENT_PHASE_CHOOSING_CONTEXT, enrollment.Registering: clientv0.EnrollmentPhase_ENROLLMENT_PHASE_REGISTERING, enrollment.PendingApproval: clientv0.EnrollmentPhase_ENROLLMENT_PHASE_PENDING_APPROVAL, enrollment.Completing: clientv0.EnrollmentPhase_ENROLLMENT_PHASE_COMPLETING, enrollment.Registered: clientv0.EnrollmentPhase_ENROLLMENT_PHASE_REGISTERED, enrollment.Denied: clientv0.EnrollmentPhase_ENROLLMENT_PHASE_DENIED, enrollment.Cancelled: clientv0.EnrollmentPhase_ENROLLMENT_PHASE_CANCELLED, enrollment.Expired: clientv0.EnrollmentPhase_ENROLLMENT_PHASE_EXPIRED}
	r := &clientv0.EnrollmentAttempt{Mode: clientv0.EnrollmentMode(p.Mode), Hostname: p.Hostname, AttemptId: a.AttemptID, RequestId: a.RequestID, ProfileId: p.ID, Phase: phases[a.Phase], Certainty: clientv0.EnrollmentCertainty_ENROLLMENT_CERTAINTY_KNOWN, CreatedAt: timestamppb.New(a.CreatedAt), Deadline: timestamppb.New(a.Deadline), LoginExpiresAt: timestamppb.New(a.LoginUntil), Metadata: &clientv0.SnapshotMetadata{InstanceId: c.config.RuntimeID, Revision: s.Revision, GeneratedAt: timestamppb.New(c.config.Now())}}
	if !a.ApprovalUntil.IsZero() {
		r.ApprovalExpiresAt = timestamppb.New(a.ApprovalUntil)
	}
	if !a.RecoveryUntil.IsZero() {
		r.RecoveryDeadline = timestamppb.New(a.RecoveryUntil)
	}
	if a.AccountID != "" {
		r.AccountId = proto.String(a.AccountID)
		r.NetworkId = proto.String(a.NetworkID)
	}
	if a.NodeID != "" {
		r.NodeId = proto.String(a.NodeID)
	}
	if pending(r.Phase) || a.Uncertain {
		r.PollAfterSeconds = 1
	}
	if a.Phase == enrollment.AwaitingLogin && !a.Uncertain && p.Login.VerificationURL != "" && c.config.Now().Before(p.Login.ExpiresAt) {
		expires := p.Login.ExpiresAt
		if a.LoginUntil.Before(expires) {
			expires = a.LoginUntil
		}
		r.BrowserAction = &clientv0.EnrollmentBrowserAction{VerificationUrl: p.Login.VerificationURL, ExpiresAt: timestamppb.New(expires)}
	}
	if p.Login.BlockedReason != "" {
		r.Restriction = &clientv0.Restriction{Availability: clientv0.Availability_AVAILABILITY_TEMPORARILY_UNAVAILABLE, ReasonKey: p.Login.BlockedReason, ActionOwner: clientv0.ActionOwner_ACTION_OWNER_SUPPORT}
	}
	if p.Registration.BlockedReason != "" {
		r.Restriction = &clientv0.Restriction{Availability: clientv0.Availability_AVAILABILITY_TEMPORARILY_UNAVAILABLE, ReasonKey: p.Registration.BlockedReason, ActionOwner: clientv0.ActionOwner_ACTION_OWNER_SUPPORT}
	}
	if p.DefaultTarget != nil && p.DefaultTarget.BlockedReason != "" {
		r.Restriction = &clientv0.Restriction{Availability: clientv0.Availability_AVAILABILITY_TEMPORARILY_UNAVAILABLE, ReasonKey: p.DefaultTarget.BlockedReason, ActionOwner: clientv0.ActionOwner_ACTION_OWNER_SUPPORT}
	}
	switch a.Phase {
	case enrollment.Denied:
		r.Restriction = &clientv0.Restriction{Availability: clientv0.Availability_AVAILABILITY_POLICY_BLOCKED, ReasonKey: "client.enrollment.denied", ActionOwner: clientv0.ActionOwner_ACTION_OWNER_ACCESS_ADMINISTRATOR}
	case enrollment.Cancelled:
		r.Restriction = &clientv0.Restriction{Availability: clientv0.Availability_AVAILABILITY_TEMPORARILY_UNAVAILABLE, ReasonKey: "client.enrollment.cancelled", ActionOwner: clientv0.ActionOwner_ACTION_OWNER_USER}
	case enrollment.Expired:
		r.Restriction = &clientv0.Restriction{Availability: clientv0.Availability_AVAILABILITY_TEMPORARILY_UNAVAILABLE, ReasonKey: "client.enrollment.expired", ActionOwner: clientv0.ActionOwner_ACTION_OWNER_USER}
	}
	if a.Uncertain {
		r.Certainty = clientv0.EnrollmentCertainty_ENROLLMENT_CERTAINTY_UNCERTAIN
		r.Restriction = &clientv0.Restriction{Availability: clientv0.Availability_AVAILABILITY_TEMPORARILY_UNAVAILABLE, ReasonKey: "client.enrollment.outcome_unknown", ActionOwner: clientv0.ActionOwner_ACTION_OWNER_SUPPORT}
	}
	return r
}
