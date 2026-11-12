package control

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

var ErrDefaultTargetProtocol = errors.New("default enrollment target invalid")

// PrepareDefaultTarget uses only the original native attempt and session.
func PrepareDefaultTarget(p state.Profile) ([]byte, error) {
	if p.Attempt == nil || p.Attempt.BrowserApproval || p.Attempt.Phase != enrollment.ChoosingContext || !validProof(p.Secrets.Access) || p.Secrets.SessionExpiresAt.IsZero() {
		return nil, ErrDefaultTargetProtocol
	}
	_, binding, err := NativeBinding(p)
	if err != nil {
		return nil, err
	}
	expires := p.Attempt.Deadline
	if p.Secrets.SessionExpiresAt.Before(expires) {
		expires = p.Secrets.SessionExpiresAt
	}
	r := coordinator.EnsureDefaultEnrollmentTargetRequest{SchemaVersion: 1, RequestID: p.Attempt.RequestID, AttemptBindingSHA256: hex.EncodeToString(binding[:]), SessionTokenBinding: coordinator.RegistrationSessionTokenBinding(p.Secrets.Access), CreatedAt: p.Attempt.CreatedAt, ExpiresAt: expires}
	if coordinator.SetDefaultTargetIdentityProof(&r, ed25519.PrivateKey(p.Keys.IdentityPrivate)) != nil {
		return nil, ErrDefaultTargetProtocol
	}
	return json.Marshal(r)
}

type DefaultTarget struct{ registration *Registration }

func NewDefaultTarget(client *http.Client, announcements Announcements, config RegistrationConfig) (*DefaultTarget, error) {
	r, err := NewRegistration(client, announcements, config)
	if err != nil {
		return nil, err
	}
	return &DefaultTarget{registration: r}, nil
}

func (d *DefaultTarget) Resolve(ctx context.Context, p state.Profile, original []byte) (coordinator.DefaultEnrollmentTarget, error) {
	var zero coordinator.DefaultEnrollmentTarget
	expected, err := PrepareDefaultTarget(p)
	if err != nil || !bytes.Equal(expected, original) || len(original) > min(coordinator.DefaultTargetMaxBytes, d.registration.config.MaxRequestBytes) || p.DefaultTarget == nil || p.DefaultTarget.Request != string(original) || !p.RemoteInFlight {
		return zero, ErrDefaultTargetProtocol
	}
	r, err := coordinator.DecodeEnsureDefaultEnrollmentTargetRequest(original, coordinator.DefaultTargetMaxBytes)
	if err != nil {
		return zero, ErrDefaultTargetProtocol
	}
	now := d.registration.config.Now()
	until := r.ExpiresAt
	if p.Attempt.Uncertain && p.Attempt.RecoveryUntil.Before(until) {
		until = p.Attempt.RecoveryUntil
	}
	remaining := until.Sub(now)
	if remaining <= 0 {
		return zero, context.DeadlineExceeded
	}
	if remaining > 30*time.Second {
		remaining = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, remaining)
	defer cancel()
	a, err := d.registration.announcements.Fetch(ctx, p.ControlOrigin)
	if err != nil || !a.MatchesProfile(p, d.registration.config.Now()) {
		return zero, ErrBootstrap
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.ControlOrigin+coordinator.DefaultTargetPath, bytes.NewReader(original))
	if err != nil {
		return zero, ErrDefaultTargetProtocol
	}
	req.Header.Set("Authorization", "Bearer "+p.Secrets.Access)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	response, err := d.registration.http.Do(req)
	if err != nil {
		return zero, registrationCallError(ctx)
	}
	defer func() { _ = response.Body.Close() }()
	media, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || response.StatusCode != http.StatusOK {
		return zero, ErrRegistrationUnavailable
	}
	maxBytes := min(coordinator.DefaultTargetMaxBytes, d.registration.config.MaxResponseBytes)
	raw, err := io.ReadAll(io.LimitReader(response.Body, int64(maxBytes)+1))
	if err != nil {
		return zero, registrationCallError(ctx)
	}
	if len(raw) > maxBytes {
		return zero, ErrDefaultTargetProtocol
	}
	result, err := coordinator.DecodeDefaultEnrollmentTarget(raw, maxBytes)
	if err != nil || result.Match(r, d.registration.config.Now()) != nil {
		return zero, ErrDefaultTargetProtocol
	}
	if ctx.Err() != nil {
		return zero, ctx.Err()
	}
	if !a.MatchesProfile(p, d.registration.config.Now()) {
		return zero, ErrBootstrap
	}
	return result, nil
}
