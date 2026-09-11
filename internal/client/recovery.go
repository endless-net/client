package client

import (
	"errors"
	"strings"
	"time"
)

type RecoveryPhase string

const (
	RecoveryPhaseRecovering    RecoveryPhase = "recovering"
	RecoveryPhaseBlocked       RecoveryPhase = "recovery_blocked"
	RecoveryPhasePolicyBlocked RecoveryPhase = "policy_blocked"
	RecoveryPhaseNeedsLogin    RecoveryPhase = "needs_login"
)

// EnrollmentRecovery is durable, non-secret progress for one signing-identity
// recovery. IdempotencyID is saved before the first renewal request and reused
// until a validated response is committed or explicit local cleanup finishes.
type EnrollmentRecovery struct {
	OperationID            string        `json:"operation_id"`
	IdempotencyID          string        `json:"idempotency_id"`
	Phase                  RecoveryPhase `json:"phase"`
	ConfirmedControlOrigin string        `json:"confirmed_control_origin"`
	ConfirmedKeyID         string        `json:"confirmed_key_id"`
	ErrorCode              string        `json:"error_code,omitempty"`
	RequestID              string        `json:"request_id,omitempty"`
	Retryable              bool          `json:"retryable,omitempty"`
	UpdatedAt              string        `json:"updated_at"`
}

func NewEnrollmentRecovery(operationID, idempotencyID, controlOrigin, keyID string, now time.Time) (EnrollmentRecovery, error) {
	value := EnrollmentRecovery{
		OperationID:            strings.TrimSpace(operationID),
		IdempotencyID:          strings.TrimSpace(idempotencyID),
		Phase:                  RecoveryPhaseRecovering,
		ConfirmedControlOrigin: strings.TrimSpace(controlOrigin),
		ConfirmedKeyID:         strings.TrimSpace(keyID),
		UpdatedAt:              now.UTC().Format(time.RFC3339Nano),
	}
	if value.OperationID == "" || value.IdempotencyID == "" || value.ConfirmedControlOrigin == "" || value.ConfirmedKeyID == "" {
		return EnrollmentRecovery{}, errors.New("recovery operation, idempotency, control origin, and key ID are required")
	}
	return value, nil
}

func (r EnrollmentRecovery) WithFailure(phase RecoveryPhase, code, requestID string, retryable bool, now time.Time) EnrollmentRecovery {
	r.Phase = phase
	r.ErrorCode = strings.TrimSpace(code)
	r.RequestID = strings.TrimSpace(requestID)
	r.Retryable = retryable
	r.UpdatedAt = now.UTC().Format(time.RFC3339Nano)
	return r
}

func (r EnrollmentRecovery) Validate() error {
	if strings.TrimSpace(r.OperationID) == "" || strings.TrimSpace(r.IdempotencyID) == "" ||
		strings.TrimSpace(r.ConfirmedControlOrigin) == "" || strings.TrimSpace(r.ConfirmedKeyID) == "" {
		return errors.New("recovery operation is incomplete")
	}
	switch r.Phase {
	case RecoveryPhaseRecovering, RecoveryPhaseBlocked, RecoveryPhasePolicyBlocked, RecoveryPhaseNeedsLogin:
	default:
		return errors.New("recovery phase is invalid")
	}
	if _, err := time.Parse(time.RFC3339Nano, r.UpdatedAt); err != nil {
		return errors.New("recovery updated_at is invalid")
	}
	return nil
}

// ApplyTerminalRecoveryCleanup removes only state bound to the authoritative
// node record. Device keys, local ownership, server trust, user session, and
// connection intent remain available for a normal re-enrollment flow.
func ApplyTerminalRecoveryCleanup(cfg *Config) error {
	if cfg == nil {
		return errors.New("client config is required")
	}
	clearNodeBoundState(cfg)
	return nil
}

// ApplyLocalLogoutCleanup implements the explicit local-logout retention
// matrix. It additionally clears the user session and atomically records an
// intentional disconnected state.
func ApplyLocalLogoutCleanup(cfg *Config, now time.Time) error {
	if cfg == nil {
		return errors.New("client config is required")
	}
	clearNodeBoundState(cfg)
	cfg.Token = ""
	cfg.ActiveAccountID = ""
	cfg.ConnectionIntent = &ConnectionIntent{
		DesiredState: ConnectionIntentDesiredDisconnected,
		Reason:       "local_logout",
		UpdatedAt:    now.UTC().Format(time.RFC3339),
	}
	return nil
}

func clearNodeBoundState(cfg *Config) {
	cfg.NodeID = ""
	cfg.NetworkID = ""
	cfg.NodeCredential = ""
	cfg.NodeApprovalState = ""
	cfg.EnrollmentRequestID = ""
	cfg.EnrollmentPollToken = ""
	cfg.ApprovalURL = ""
	cfg.EnrollmentRequest = nil
	cfg.PendingDirectRegistration = nil
	cfg.MapRevision = 0
	cfg.MapGlobalRevision = 0
	cfg.MapHash = ""
	cfg.CachedMap = nil
	cfg.CachedMapSavedAt = nil
	cfg.EnrollmentRecovery = nil
}
