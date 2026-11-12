package state

import (
	"reflect"
	"time"

	"github.com/endless-net/client/internal/enrollment"

	coordinator "github.com/endless-net/coordinator/api/enrollment"
	sessionv1 "github.com/endless-net/identity/api/session/v1"
	"google.golang.org/protobuf/proto"
)

type SessionRefresh struct {
	RequestID, ExpectedSessionID, UserID, Renewal string
	CreatedAt, Deadline, SentAt                   time.Time
	OperationID, PollAuthorization                string
	ReplayUntil                                   time.Time
	Operation                                     []byte
}

type NodeRenewal struct {
	Request, Response           string
	CreatedAt, Deadline, SentAt time.Time
}

func (*SessionRefresh) String() string   { return "[private session refresh]" }
func (*SessionRefresh) GoString() string { return "[private session refresh]" }
func (*NodeRenewal) String() string      { return "[private node renewal]" }
func (*NodeRenewal) GoString() string    { return "[private node renewal]" }

func credentialTimes(created, deadline, sent time.Time) bool {
	return !created.IsZero() && deadline.After(created) && !deadline.After(created.Add(coordinator.NodeRenewalExecutionTTL)) && (sent.IsZero() || !sent.Before(created) && sent.Before(deadline))
}

func validateCredentialProgress(p Profile) error {
	if p.SessionRefresh != nil {
		r := p.SessionRefresh
		if p.Attempt == nil || p.Secrets.NodeCredential == "" || !credentialTimes(r.CreatedAt, r.Deadline, r.SentAt) || r.UserID != p.Secrets.UserID || r.Renewal == "" || sessionv1.ValidateSessionRenewalRequest(&sessionv1.RenewSessionRequest{RequestId: r.RequestID, ExpectedSessionId: r.ExpectedSessionID}) != nil {
			return ErrCorrupt
		}
		if len(r.Operation) != 0 {
			if r.SentAt.IsZero() {
				return ErrCorrupt
			}
			var op sessionv1.SessionRenewal
			if len(r.Operation) > 65536 || proto.Unmarshal(r.Operation, &op) != nil || sessionv1.ValidateSessionRenewal(&op, &sessionv1.RenewSessionRequest{RequestId: r.RequestID, ExpectedSessionId: r.ExpectedSessionID}, []string{enrollment.NativeConsentOrigin(p.ControlOrigin)}) != nil || op.OperationId != r.OperationID || !op.ReplayExpiresAt.AsTime().Equal(r.ReplayUntil) || r.PollAuthorization == "" {
				return ErrCorrupt
			}
			if result := op.GetResult(); result != nil && result.Session.UserId != r.UserID {
				return ErrCorrupt
			}
		} else if r.OperationID != "" || r.PollAuthorization != "" || !r.ReplayUntil.IsZero() {
			return ErrCorrupt
		}
	}
	if p.NodeRenewal != nil {
		r := p.NodeRenewal
		if p.Attempt == nil || p.Secrets.NodeCredential == "" || !credentialTimes(r.CreatedAt, r.Deadline, r.SentAt) {
			return ErrCorrupt
		}
		_, err := profileRenewalRequest(p, r.Request)
		if err != nil {
			return ErrCorrupt
		}
		// Reuse the same complete binding check before and after admission.
		if r.Response != "" {
			if r.SentAt.IsZero() {
				return ErrCorrupt
			}
			copy := p
			copy.ActiveNodeCredential = &ActiveNodeCredential{Request: r.Request, Receipt: r.Response}
			if validateActiveCredentials(copy) != nil {
				return ErrCorrupt
			}
		}
	}
	return nil
}

func credentialProgressUpdate(before, after Profile) error {
	if before.SessionRefresh != nil {
		old, next := before.SessionRefresh, after.SessionRefresh
		if next == nil {
			return ErrCorrupt
		}
		if next.RequestID != old.RequestID {
			if before.ActiveSession == nil || before.ActiveSession.RequestID != old.RequestID {
				return ErrCorrupt
			}
		} else {
			if old.ExpectedSessionID != next.ExpectedSessionID || old.UserID != next.UserID || old.Renewal != next.Renewal || !old.CreatedAt.Equal(next.CreatedAt) || !old.Deadline.Equal(next.Deadline) || !old.SentAt.IsZero() && !old.SentAt.Equal(next.SentAt) || old.OperationID != "" && (old.OperationID != next.OperationID || old.PollAuthorization != next.PollAuthorization || !old.ReplayUntil.Equal(next.ReplayUntil)) {
				return ErrCorrupt
			}
			if before.ActiveSession != nil && before.ActiveSession.RequestID == old.RequestID && !reflect.DeepEqual(old, next) {
				return ErrCorrupt
			}
			if len(old.Operation) != 0 {
				var op sessionv1.SessionRenewal
				if proto.Unmarshal(old.Operation, &op) != nil {
					return ErrCorrupt
				}
				if op.State != sessionv1.SessionRenewalState_SESSION_RENEWAL_STATE_WAITING_FOR_USER && !reflect.DeepEqual(old, next) {
					return ErrCorrupt
				}
			}
		}
	}
	if after.SessionRefresh != nil && (before.SessionRefresh == nil || before.SessionRefresh.RequestID != after.SessionRefresh.RequestID) {
		current, err := before.CurrentSession()
		if err != nil {
			return err
		}
		r := after.SessionRefresh
		if r.ExpectedSessionID != current.SessionID || r.Renewal != current.Renewal || r.UserID != current.UserID || !r.SentAt.IsZero() || len(r.Operation) != 0 {
			return ErrCorrupt
		}
	}
	if before.NodeRenewal != nil {
		old, next := before.NodeRenewal, after.NodeRenewal
		if next == nil {
			return ErrCorrupt
		}
		if old.Request != next.Request {
			if before.ActiveNodeCredential == nil || before.ActiveNodeCredential.Request != old.Request || old.Response == "" {
				return ErrCorrupt
			}
		} else if !old.CreatedAt.Equal(next.CreatedAt) || !old.Deadline.Equal(next.Deadline) || !old.SentAt.IsZero() && !old.SentAt.Equal(next.SentAt) || old.Response != "" && old.Response != next.Response {
			return ErrCorrupt
		}
	}
	if after.NodeRenewal != nil && (before.NodeRenewal == nil || before.NodeRenewal.Request != after.NodeRenewal.Request) {
		current, _, err := before.CurrentNodeCredential()
		if err != nil {
			return err
		}
		request, err := coordinator.DecodeRegisterNodeRequest([]byte(after.NodeRenewal.Request), 65536)
		if err != nil || request.NodeCredential != current || !after.NodeRenewal.SentAt.IsZero() || after.NodeRenewal.Response != "" {
			return ErrCorrupt
		}
	}
	return nil
}
