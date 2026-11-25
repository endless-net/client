package control

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"connectrpc.com/connect"

	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	wire "github.com/endless-net/coordinator/api/publicquery/v1"
	"github.com/endless-net/coordinator/api/publicquery/v1/publicqueryv1connect"
	"google.golang.org/protobuf/types/known/timestamppb"
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
	if err != nil {
		return zero, enrollmentBootstrapError{err}
	}
	if !a.MatchesProfile(p, d.registration.config.Now()) {
		return zero, ErrBootstrap
	}
	req := bearer(&wire.EnsureDefaultEnrollmentTargetRequest{
		Version: uint32(r.SchemaVersion), RequestId: r.RequestID, AttemptBindingSha256: r.AttemptBindingSHA256,
		IdentityPublicKey: r.IdentityPublicKey, IdentitySignature: r.IdentitySignature, SessionTokenBinding: r.SessionTokenBinding,
		CreatedAt: timestamppb.New(r.CreatedAt), ExpiresAt: timestamppb.New(r.ExpiresAt),
	}, p.Secrets.Access)
	client := publicqueryv1connect.NewEnrollmentServiceClient(d.registration.http, p.ControlOrigin,
		connect.WithSendMaxBytes(min(coordinator.DefaultTargetMaxBytes, d.registration.config.MaxRequestBytes)),
		connect.WithReadMaxBytes(min(coordinator.DefaultTargetMaxBytes, d.registration.config.MaxResponseBytes)))
	response, err := client.EnsureDefaultEnrollmentTarget(ctx, req)
	if err != nil {
		return zero, registrationCallError(ctx, err)
	}
	m := response.Msg
	if !knownRPC(m.ProtoReflect()) || m.ResolvedAt.CheckValid() != nil || m.ExpiresAt.CheckValid() != nil {
		return zero, ErrDefaultTargetProtocol
	}
	result := coordinator.DefaultEnrollmentTarget{
		SchemaVersion: 1, RequestID: m.RequestId, RequestSHA256: m.RequestSha256, AccountID: m.AccountId,
		NetworkID: m.NetworkId, Revision: m.Revision, ResolvedAt: m.ResolvedAt.AsTime(), ExpiresAt: m.ExpiresAt.AsTime(),
	}
	if result.Match(r, d.registration.config.Now()) != nil {
		return zero, ErrDefaultTargetProtocol
	}
	if ctx.Err() != nil {
		return zero, ctx.Err()
	}
	if !a.MatchesProfile(p, d.registration.config.Now()) {
		return zero, errEnrollmentTrustAfterCall
	}
	return result, nil
}
