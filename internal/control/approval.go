package control

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"
	"unicode/utf8"

	approvalmodel "github.com/endless-net/client/internal/approval"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

var ErrApprovalProtocol = approvalmodel.ErrProtocol
var ErrApprovalNotDue = errors.New("approval poll not due")

type ApprovalConfig struct {
	RegistrationConfig
	// ApprovalBase must match the explicitly configured Coordinator browser base.
	// No URL received from IPC, an error body or a redirect establishes this trust.
	ApprovalBase string
}

type Approval struct {
	registration *Registration
	base         string
}

// ApprovalSnapshot contains daemon-only poll authority. Even an enrolled status
// is an observation: it does not contain a verified Node credential or grant.
type ApprovalSnapshot = approvalmodel.Snapshot

func NewApproval(client *http.Client, announcements Announcements, config ApprovalConfig) (*Approval, error) {
	if !approvalmodel.ValidBase(config.ApprovalBase) {
		return nil, ErrApprovalProtocol
	}
	r, err := NewRegistration(client, announcements, config.RegistrationConfig)
	if err != nil {
		return nil, err
	}
	return &Approval{registration: r, base: config.ApprovalBase}, nil
}

// Create sends once from the exact private replay input. The caller must save a
// validated result atomically before polling; this adapter never changes state.
func (a *Approval) Create(ctx context.Context, p state.Profile, original []byte, previous *ApprovalSnapshot) (ApprovalSnapshot, error) {
	q, err := a.input(p, original)
	if err != nil {
		return ApprovalSnapshot{}, err
	}
	if previous != nil && a.validatePrevious(q, p, *previous) != nil {
		return ApprovalSnapshot{}, ErrApprovalProtocol
	}
	raw, err := a.call(ctx, p, previous, http.MethodPost, "/nodes/enrollment-requests", "", original)
	if err != nil {
		return ApprovalSnapshot{}, err
	}
	v, err := coordinator.DecodeCreateNodeEnrollmentRequestResponse(raw, a.registration.config.MaxResponseBytes)
	now := a.registration.config.Now()
	if err != nil || !approvalToken(v.PollToken) || a.validate(q, p, v.Request, v.PollAfterSeconds, now, true) != nil {
		return ApprovalSnapshot{}, ErrApprovalProtocol
	}
	if previous != nil && (now.Before(previous.ObservedAt) || previous.PollToken != v.PollToken || !approvalSuccessor(previous.Request, v.Request)) {
		return ApprovalSnapshot{}, ErrApprovalProtocol
	}
	return ApprovalSnapshot{Request: v.Request, PollToken: v.PollToken, PollAfterSeconds: v.PollAfterSeconds, ObservedAt: now}, nil
}

// Poll uses only the saved poll bearer and ID. It never retries, creates a new
// request, follows an approval URL or treats approved as registered.
func (a *Approval) Poll(ctx context.Context, p state.Profile, original []byte, previous ApprovalSnapshot) (ApprovalSnapshot, error) {
	q, err := a.input(p, original)
	if err != nil || a.validatePrevious(q, p, previous) != nil {
		return ApprovalSnapshot{}, ErrApprovalProtocol
	}
	if previous.Request.Status == "pending" && a.registration.config.Now().Before(previous.ObservedAt.Add(time.Duration(previous.PollAfterSeconds)*time.Second)) {
		return ApprovalSnapshot{}, ErrApprovalNotDue
	}
	raw, err := a.call(ctx, p, &previous, http.MethodGet, "/nodes/enrollment-requests/"+previous.Request.ID, previous.PollToken, nil)
	if err != nil {
		return ApprovalSnapshot{}, err
	}
	v, err := coordinator.DecodeNodeEnrollmentRequestStatusResponse(raw, a.registration.config.MaxResponseBytes)
	now := a.registration.config.Now()
	if err != nil || now.Before(previous.ObservedAt) || a.validate(q, p, v.Request, v.PollAfterSeconds, now, false) != nil || !approvalSuccessor(previous.Request, v.Request) {
		return ApprovalSnapshot{}, ErrApprovalProtocol
	}
	return ApprovalSnapshot{Request: v.Request, PollToken: previous.PollToken, PollAfterSeconds: v.PollAfterSeconds, ObservedAt: now}, nil
}

func (a *Approval) validatePrevious(q coordinator.CreateNodeEnrollmentRequest, p state.Profile, previous ApprovalSnapshot) error {
	if !approvalToken(previous.PollToken) || previous.ObservedAt.IsZero() || previous.ObservedAt.After(a.registration.config.Now()) || previous.ObservedAt.Before(previous.Request.CreatedAt) {
		return ErrApprovalProtocol
	}
	return a.validate(q, p, previous.Request, previous.PollAfterSeconds, previous.ObservedAt, previous.PollAfterSeconds > 0)
}

func (a *Approval) input(p state.Profile, original []byte) (coordinator.CreateNodeEnrollmentRequest, error) {
	r := a.registration
	q, err := coordinator.DecodeCreateNodeEnrollmentRequest(original, r.config.MaxRequestBytes)
	if err != nil || !p.RemoteInFlight || p.InFlightAt.IsZero() || !bytes.Equal(original, []byte(p.Secrets.RegistrationRequest)) {
		return coordinator.CreateNodeEnrollmentRequest{}, ErrApprovalProtocol
	}
	expected, err := PrepareApproval(p, ApprovalTarget{AccountID: q.AccountID, NetworkID: q.NetworkID, NetworkName: q.NetworkName})
	if err != nil || !bytes.Equal(original, expected) || p.InFlightAt.Before(p.Attempt.CreatedAt) || p.InFlightAt.After(r.config.Now()) || r.config.Now().Before(p.Attempt.ObservedAt) {
		return coordinator.CreateNodeEnrollmentRequest{}, ErrApprovalProtocol
	}
	return q, nil
}

func (a *Approval) budget(p state.Profile, previous *ApprovalSnapshot) time.Duration {
	now := a.registration.config.Now()
	deadline := p.Attempt.Deadline
	if p.Attempt.Uncertain {
		if p.Attempt.RecoveryUntil.Before(deadline) {
			deadline = p.Attempt.RecoveryUntil
		}
	} else {
		for _, end := range []time.Time{p.Attempt.ApprovalUntil, approvalExpiry(previous)} {
			if !end.IsZero() && end.Before(deadline) {
				deadline = end
			}
		}
	}
	return min(30*time.Second, deadline.Sub(now))
}

func approvalExpiry(s *ApprovalSnapshot) time.Time {
	if s == nil {
		return time.Time{}
	}
	return s.Request.ExpiresAt
}

func (a *Approval) call(ctx context.Context, p state.Profile, previous *ApprovalSnapshot, method, path, token string, body []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	remaining := a.budget(p, previous)
	if remaining <= 0 {
		return nil, context.DeadlineExceeded
	}
	ctx, cancel := context.WithTimeout(ctx, remaining)
	defer cancel()
	r := a.registration
	current, err := r.announcements.Fetch(ctx, p.ControlOrigin)
	if err != nil || current.Origin != p.ControlOrigin || current.Validate(r.config.Now()) != nil || current.KeyID != p.Trust.KeyID || current.ID != p.Trust.AnnouncementID || !bytes.Equal(current.Bundle, p.Trust.Bundle) {
		return nil, ErrBootstrap
	}
	req, err := http.NewRequestWithContext(ctx, method, p.ControlOrigin+path, bytes.NewReader(body))
	if err != nil {
		return nil, ErrApprovalProtocol
	}
	req.Header.Set("Accept", "application/json")
	if method == http.MethodPost && len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if a.budget(p, previous) <= 0 {
		return nil, context.DeadlineExceeded
	}
	response, err := r.http.Do(req)
	if err != nil {
		return nil, registrationCallError(ctx)
	}
	defer func() { _ = response.Body.Close() }()
	media, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	created := method == http.MethodPost && path == "/nodes/enrollment-requests" && response.StatusCode == http.StatusCreated
	if err != nil || media != "application/json" || response.StatusCode != http.StatusOK && !created {
		return nil, ErrRegistrationUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, int64(r.config.MaxResponseBytes)+1))
	if err != nil || len(raw) > r.config.MaxResponseBytes {
		return nil, registrationCallError(ctx)
	}
	if !utf8.Valid(raw) {
		return nil, ErrApprovalProtocol
	}
	if ctx.Err() != nil {
		return nil, registrationCallError(ctx)
	}
	if a.budget(p, previous) <= 0 {
		return nil, context.DeadlineExceeded
	}
	if current.Validate(r.config.Now()) != nil {
		return nil, ErrBootstrap
	}
	return raw, nil
}

func approvalToken(token string) bool { return approvalmodel.ValidToken(token) }
func (a *Approval) validate(q coordinator.CreateNodeEnrollmentRequest, p state.Profile, v coordinator.NodeEnrollmentRequest, hint int, now time.Time, create bool) error {
	return approvalmodel.ValidateView(q, v, a.base, p.Attempt.CreatedAt, hint, now, create)
}
func approvalSuccessor(old, next coordinator.NodeEnrollmentRequest) bool {
	return approvalmodel.Successor(old, next)
}
