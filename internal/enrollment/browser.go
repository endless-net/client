package enrollment

import "time"

// BeginBrowserApproval is an explicit local choice, never a response to 403.
// The caller must reject any prior frozen native request or in-flight effect
// and atomically persist this transition with the original device-only input.
// Requested context is a hint, not a current Account/Network permission.
func (a Attempt) BeginBrowserApproval(account, network string, now time.Time) (Attempt, error) {
	a, err := a.Observe(now)
	if err != nil {
		return Attempt{}, err
	}
	if a.BrowserApproval || a.Uncertain || a.Phase != ChoosingContext && a.Phase != Registering {
		return Attempt{}, ErrTransition
	}
	if a.AccountID != "" && (a.AccountID != account || a.NetworkID != network) {
		return Attempt{}, ErrBinding
	}
	if account != "" && network != "" {
		a.AccountID, a.NetworkID = account, network
	}
	a.BrowserApproval = true
	a.Phase = PendingApproval
	a.ApprovalUntil = earlier(now.Add(ApprovalTTL), a.Deadline)
	a.Revision++
	if a.Validate() != nil {
		return Attempt{}, ErrInvalid
	}
	return a, nil
}

// BindBrowserContext accepts only an adapter-verified original owner receipt.
// It does not verify the receipt or grant registration. Cancelled unknown calls
// may retain their original resolved context inside the fixed recovery window.
func (a Attempt) BindBrowserContext(account, network string, now time.Time) (Attempt, error) {
	if a.Validate() != nil || now.Before(a.ObservedAt) || !a.BrowserApproval || account == "" || network == "" {
		return Attempt{}, ErrInvalid
	}
	if a.AccountID != "" && (a.AccountID != account || a.NetworkID != network) {
		return Attempt{}, ErrBinding
	}
	if a.Phase == Cancelled && a.Uncertain && now.Before(a.Deadline) && now.Before(a.RecoveryUntil) {
		a.AccountID, a.NetworkID = account, network
		a.Revision++
		return a, nil
	}
	a, err := a.Observe(now)
	if err != nil {
		return Attempt{}, err
	}
	if a.Phase != PendingApproval && a.Phase != Completing {
		return Attempt{}, ErrTransition
	}
	a.AccountID, a.NetworkID = account, network
	a.Revision++
	return a, nil
}
