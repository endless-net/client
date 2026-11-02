package state

import (
	"time"

	"github.com/endless-net/client/internal/enrollment"
	contract "github.com/endless-net/coordinator/api/enrollment"
)

// RemoteRevocation is a protected command journal, never a grant or IPC model.
// Its size is bounded by Store's existing plaintext/ciphertext limits. Entries
// are retained so a later command cannot erase an unresolved earlier effect.
type RemoteRevocation struct {
	CallerSID, UserID, ControlOrigin   string
	Hostname, AssignedIP, TargetStatus string
	CreatedAt                          time.Time
	Request                            contract.RevokeNodeRequest
	Receipt                            *contract.NodeRevocationReceipt `json:",omitempty"`
}

func validateRevocations(owner string, p Profile) error {
	for command, r := range p.RemoteRevocations {
		if !validID(command) || r.Request.CommandID != command || r.Request.Validate() != nil || r.CallerSID != owner || r.UserID == "" || r.ControlOrigin != p.ControlOrigin || r.CreatedAt.IsZero() || p.Attempt == nil || p.Attempt.Phase != enrollment.Registered || r.Request.AccountID != p.Attempt.AccountID || r.Request.NetworkID != p.Attempt.NetworkID {
			return ErrCorrupt
		}
		if r.Receipt != nil && r.Receipt.Match(r.Request) != nil {
			return ErrCorrupt
		}
	}
	return nil
}

func sameRevocationBinding(a, b RemoteRevocation) bool {
	return a.CallerSID == b.CallerSID && a.UserID == b.UserID && a.ControlOrigin == b.ControlOrigin && a.Hostname == b.Hostname && a.AssignedIP == b.AssignedIP && a.TargetStatus == b.TargetStatus && a.CreatedAt.Equal(b.CreatedAt) && a.Request == b.Request
}

func revocationsUpdate(before, after Profile) error {
	for command, old := range before.RemoteRevocations {
		next, ok := after.RemoteRevocations[command]
		if !ok || !sameRevocationBinding(old, next) {
			return ErrCorrupt
		}
		if old.Receipt != nil && (next.Receipt == nil || old.Receipt.Revision != next.Receipt.Revision || old.Receipt.Status != next.Receipt.Status || !old.Receipt.AcceptedAt.Equal(next.Receipt.AcceptedAt) || !old.Receipt.EnforceBy.Equal(next.Receipt.EnforceBy)) {
			return ErrCorrupt
		}
	}
	return nil
}
