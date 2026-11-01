// Package enrollment owns the local enrollment state machine, independently of
// IPC, key storage, and server transport. A transition is a persistence candidate:
// the caller must commit it atomically before executing its next network action.
package enrollment

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	LoginTTL    = 600 * time.Second
	ApprovalTTL = 600 * time.Second
	RecoveryTTL = 120 * time.Second
	AttemptTTL  = 1320 * time.Second
)

var (
	ErrInvalid    = errors.New("invalid enrollment state")
	ErrBinding    = errors.New("enrollment binding changed")
	ErrTransition = errors.New("invalid enrollment transition")
	uuidPattern   = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

type Phase string

const (
	AwaitingLogin   Phase = "awaiting_login"
	ChoosingContext Phase = "choosing_context"
	Registering     Phase = "registering"
	PendingApproval Phase = "pending_approval"
	Completing      Phase = "completing"
	Registered      Phase = "registered"
	Denied          Phase = "denied"
	Cancelled       Phase = "cancelled"
	Expired         Phase = "expired"
)

// Binding identifies one already persisted device identity and attempt. The
// private keys and remote grants live only in the protected state adapter.
// Owner is an authenticated OS principal, never a user-provided display name.
type Binding struct {
	AttemptID     string
	RequestID     string
	ProfileID     string
	Owner         string
	ControlOrigin string
	DeviceKeyHash [32]byte
}

// Attempt is internal persisted state, not a copy of a producer's wire DTO.
// Uncertain means the remote outcome is unknown even if local waiting ended.
type Attempt struct {
	Binding
	// Explicit device-only approval class. Absence preserves native state data.
	BrowserApproval bool `json:",omitempty"`
	Phase           Phase
	Uncertain       bool
	Revision        uint64
	CreatedAt       time.Time
	ObservedAt      time.Time
	LoginUntil      time.Time
	ApprovalUntil   time.Time
	Deadline        time.Time
	RecoveryUntil   time.Time
	AccountID       string
	NetworkID       string
	NodeID          string
	CredentialEnds  time.Time
}

func New(binding Binding, now time.Time) (Attempt, error) {
	a := Attempt{Binding: binding, Phase: AwaitingLogin, Revision: 1,
		CreatedAt: now, ObservedAt: now, LoginUntil: now.Add(LoginTTL), Deadline: now.Add(AttemptTTL)}
	if err := a.Validate(); err != nil {
		return Attempt{}, err
	}
	return a, nil
}

// Validate is required when restoring state. A corrupt store must not silently
// create another attempt/key or move any absolute deadline into the future.
func (a Attempt) Validate() error {
	for _, id := range []string{a.AttemptID, a.RequestID} {
		if !uuidPattern.MatchString(id) || id == "00000000-0000-0000-0000-000000000000" {
			return ErrInvalid
		}
	}
	origin, err := url.Parse(a.ControlOrigin)
	if err != nil || origin.Scheme != "https" || origin.Host == "" || origin.User != nil ||
		origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" || origin.ForceQuery ||
		origin.Host != strings.ToLower(origin.Host) || origin.String() != a.ControlOrigin {
		return ErrInvalid
	}
	for _, value := range []string{a.AttemptID, a.RequestID, a.ProfileID, a.Owner} {
		if value == "" || strings.TrimSpace(value) != value {
			return ErrInvalid
		}
	}
	if a.DeviceKeyHash == [32]byte{} || a.Revision == 0 || a.CreatedAt.IsZero() ||
		a.ObservedAt.Before(a.CreatedAt) || !a.LoginUntil.Equal(a.CreatedAt.Add(LoginTTL)) ||
		!a.Deadline.Equal(a.CreatedAt.Add(AttemptTTL)) ||
		(!a.RecoveryUntil.IsZero() && (a.RecoveryUntil.After(a.Deadline) || !a.RecoveryUntil.After(a.CreatedAt))) ||
		(!a.ApprovalUntil.IsZero() && (a.ApprovalUntil.After(a.Deadline) || !a.ApprovalUntil.After(a.CreatedAt))) {
		return ErrInvalid
	}
	if a.Uncertain && a.RecoveryUntil.IsZero() {
		return ErrInvalid
	}
	if (a.AccountID == "") != (a.NetworkID == "") {
		return ErrInvalid
	}
	if a.BrowserApproval && (a.ApprovalUntil.IsZero() || a.Phase == AwaitingLogin || a.Phase == ChoosingContext || a.Phase == Registering) {
		return ErrInvalid
	}
	switch a.Phase {
	case AwaitingLogin, ChoosingContext:
		if a.AccountID != "" || !a.ApprovalUntil.IsZero() {
			return ErrInvalid
		}
	case Registering:
		if a.AccountID == "" || !a.ApprovalUntil.IsZero() {
			return ErrInvalid
		}
	case PendingApproval, Completing:
		if a.AccountID == "" && (!a.BrowserApproval || a.Phase == Completing) || a.ApprovalUntil.IsZero() {
			return ErrInvalid
		}
	case Registered:
		if a.AccountID == "" || a.NodeID == "" || a.CredentialEnds.IsZero() || a.Uncertain {
			return ErrInvalid
		}
	case Denied, Cancelled, Expired:
	default:
		return ErrInvalid
	}
	if a.Phase != Registered && (a.NodeID != "" || !a.CredentialEnds.IsZero()) {
		return ErrInvalid
	}
	return nil
}

// Resume checks exact immutable inputs, including OS ownership and the key
// fingerprint. It never creates a new request ID or extends the attempt.
func (a Attempt) Resume(binding Binding, now time.Time) (Attempt, error) {
	if a.Binding != binding {
		return Attempt{}, ErrBinding
	}
	return a.Observe(now)
}

func (a Attempt) Observe(now time.Time) (Attempt, error) {
	if err := a.Validate(); err != nil {
		return Attempt{}, err
	}
	if now.Before(a.ObservedAt) {
		return Attempt{}, ErrInvalid
	}
	if a.terminal() {
		return a, nil
	}
	deadline := a.Deadline
	if a.Uncertain {
		deadline = earlier(deadline, a.RecoveryUntil)
	} else if a.Phase == AwaitingLogin {
		deadline = earlier(deadline, a.LoginUntil)
	} else if !a.ApprovalUntil.IsZero() {
		deadline = earlier(deadline, a.ApprovalUntil)
	}
	if !now.Before(deadline) {
		a.Phase = Expired
		a.Revision++
	}
	if !a.ObservedAt.Equal(now) {
		a.ObservedAt = now
		a.Revision++
	}
	return a, nil
}

// MarkUncertain starts one finite recovery window. More timeouts, restarts, or
// observations cannot reset it. Local expiry does not prove remote rollback.
func (a Attempt) MarkUncertain(now time.Time) (Attempt, error) {
	if err := a.Validate(); err != nil {
		return Attempt{}, err
	}
	if now.Before(a.ObservedAt) {
		return Attempt{}, ErrInvalid
	}
	if a.terminal() {
		return a, nil
	}
	if a.RecoveryUntil.IsZero() {
		a.RecoveryUntil = earlier(now.Add(RecoveryTTL), a.Deadline)
	}
	a.Uncertain = true
	a.Revision++
	return a.Observe(now)
}

// RecoverLostCall uses the persisted send time when a crash prevented observing
// the first uncertain result. Restart must not grant another recovery window.
func (a Attempt) RecoverLostCall(sentAt, now time.Time) (Attempt, error) {
	if a.Validate() != nil || sentAt.Before(a.CreatedAt) || sentAt.After(now) || now.Before(a.ObservedAt) {
		return Attempt{}, ErrInvalid
	}
	if a.Phase == Registered || a.Phase == Denied {
		return a, nil
	}
	if a.RecoveryUntil.IsZero() {
		a.RecoveryUntil = earlier(sentAt.Add(RecoveryTTL), a.Deadline)
	}
	a.Uncertain = true
	a.Revision++
	return a.Observe(now)
}

func (a Attempt) LoginSucceeded(now time.Time) (Attempt, error) {
	a, err := a.Observe(now)
	if err != nil {
		return Attempt{}, err
	}
	if a.Phase != AwaitingLogin {
		return Attempt{}, ErrTransition
	}
	a.Phase, a.Uncertain = ChoosingContext, false
	a.Revision++
	return a, nil
}

// Select requires an already authorized target from the control adapter. The
// same authorization applies to automatic single-target selection and UI input.
func (a Attempt) Select(accountID, networkID string, now time.Time) (Attempt, error) {
	a, err := a.Observe(now)
	if err != nil {
		return Attempt{}, err
	}
	if accountID == "" || networkID == "" {
		return Attempt{}, ErrInvalid
	}
	if a.AccountID != "" && (a.AccountID != accountID || a.NetworkID != networkID) {
		return Attempt{}, ErrBinding
	}
	if a.Phase == Registering && a.AccountID == accountID && a.NetworkID == networkID {
		return a, nil
	}
	if a.Phase != ChoosingContext {
		return Attempt{}, ErrTransition
	}
	a.AccountID, a.NetworkID, a.Phase = accountID, networkID, Registering
	a.Revision++
	return a, nil
}

// AwaitApproval uses the original authoritative approval deadline; a response
// or replay must not grant another 600 seconds. The transport validates source
// and response binding before this method is called.
func (a Attempt) AwaitApproval(deadline, now time.Time) (Attempt, error) {
	a, err := a.Observe(now)
	if err != nil {
		return Attempt{}, err
	}
	if deadline.After(now.Add(ApprovalTTL)) || deadline.After(a.Deadline) || !deadline.After(now) {
		return Attempt{}, ErrInvalid
	}
	if !a.ApprovalUntil.IsZero() && !a.ApprovalUntil.Equal(deadline) {
		return Attempt{}, ErrBinding
	}
	if a.Phase != Registering && a.Phase != PendingApproval {
		return Attempt{}, ErrTransition
	}
	a.Phase, a.ApprovalUntil, a.Uncertain = PendingApproval, deadline, false
	a.Revision++
	return a, nil
}

// Complete is an authoritative committed result, not mere approval. Before
// calling, the adapter must verify source, response binding, Node credential
// signature/purpose, and that admission actually committed. No tunnel is started.
func (a Attempt) Complete(nodeID string, credentialEnds, now time.Time) (Attempt, error) {
	a, err := a.Observe(now)
	if err != nil {
		return Attempt{}, err
	}
	if a.Phase == Registered {
		if a.NodeID == nodeID && a.CredentialEnds.Equal(credentialEnds) {
			return a, nil
		}
		return Attempt{}, ErrBinding
	}
	if a.Phase != Registering && a.Phase != PendingApproval && a.Phase != Completing {
		return Attempt{}, ErrTransition
	}
	if a.AccountID == "" || a.NetworkID == "" || nodeID == "" || !credentialEnds.After(now) {
		return Attempt{}, ErrInvalid
	}
	a.Phase, a.NodeID, a.CredentialEnds, a.Uncertain = Registered, nodeID, credentialEnds, false
	a.Revision++
	return a, nil
}

// ResolveCommittedRegistration accepts a verified original committed result
// during the same finite recovery window, including a concurrent local cancel.
// It does not authorize dispatch after cancel or extend an elapsed deadline.
// Only the adapter that verified the exact original request may call it.
func (a Attempt) ResolveCommittedRegistration(nodeID string, credentialEnds, now time.Time) (Attempt, error) {
	if err := a.Validate(); err != nil {
		return Attempt{}, err
	}
	if a.Phase == Cancelled && a.Uncertain && !now.Before(a.ObservedAt) && now.Before(a.Deadline) && now.Before(a.RecoveryUntil) {
		a.Phase = Registering
		if !a.ApprovalUntil.IsZero() {
			a.Phase = Completing
		}
		a.Revision++
	}
	return a.Complete(nodeID, credentialEnds, now)
}

func (a Attempt) Cancel(now time.Time) (Attempt, error) {
	a, err := a.Observe(now)
	if err != nil || a.terminal() {
		return a, err
	}
	a.Phase = Cancelled // Uncertain remains true when a remote mutation may exist.
	a.Revision++
	return a, nil
}

// Approved does not mean registered; only a later committed result can do that.
func (a Attempt) Approved(now time.Time) (Attempt, error) {
	a, err := a.Observe(now)
	if err != nil {
		return Attempt{}, err
	}
	if a.Phase != PendingApproval {
		return Attempt{}, ErrTransition
	}
	a.Phase, a.Uncertain = Completing, false
	a.Revision++
	return a, nil
}

// Reject requires an authoritative denial, not a transport timeout.
func (a Attempt) Reject(now time.Time) (Attempt, error) {
	a, err := a.Observe(now)
	if err != nil || a.terminal() {
		return a, err
	}
	a.Phase, a.Uncertain = Denied, false
	a.Revision++
	return a, nil
}

func (a Attempt) terminal() bool {
	return a.Phase == Registered || a.Phase == Denied || a.Phase == Cancelled || a.Phase == Expired
}

func earlier(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
