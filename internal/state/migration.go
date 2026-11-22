package state

import (
	"context"
	"time"

	"github.com/endless-net/client/internal/enrollment"
	identityapi "github.com/endless-net/identity/api/v1"
	"google.golang.org/protobuf/proto"
)

// Upgrade directly preserves v1–v8 data in v9 before startup Load.
// It neither initializes missing state nor repairs unknown/corrupt state. The
// protected backend uses the same installation/DPAPI binding and atomic Replace;
// a lost Replace ACK remains uncertain and must be resolved by a fresh read.
func (s *Store) Upgrade(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	return s.backend.Exclusive(ctx, func(slot Slot) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		value, err := s.decode(slot)
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if value.Version == CurrentVersion {
			return value.Validate(s.installation)
		}
		if value.Version < 1 || value.Version >= CurrentVersion || value.Revision == ^uint64(0) {
			return ErrCorrupt
		}
		if value.Version < 4 && (len(value.TransportHints) != 0 || !value.HintObservedAt.IsZero()) {
			return ErrCorrupt
		}
		if value.Version < 5 && len(value.BootstrapCheckpoints) != 0 {
			return ErrCorrupt
		}
		for id, profile := range value.Profiles {
			if value.Version < 6 && profile.DefaultTarget != nil {
				return ErrCorrupt
			}
			if value.Version < 7 && (profile.ActiveSession != nil || profile.ActiveNodeCredential != nil || profile.SessionRefresh != nil || profile.NodeRenewal != nil) {
				return ErrCorrupt
			}
			if value.Version < 8 && (profile.SessionLogin != nil || len(profile.SessionLoginHistory) != 0 || len(profile.SessionRefreshHistory) != 0 || profile.ActiveSession != nil && profile.ActiveSession.Native != nil) {
				return ErrCorrupt
			}
			if value.Version == 1 && profile.Connection != nil || value.Version < 3 && len(profile.RemoteRevocations) != 0 {
				return ErrCorrupt
			}
			if err := migrateConsentURLs(&profile); err != nil {
				return err
			}
			value.Profiles[id] = profile
		}
		// Preserve v2 connections, v3 revocations, v4 transport hints and all keys.
		// Migration never invents a success hint or a remote command.
		value.Version = CurrentVersion
		if value.Validate(s.installation) != nil {
			return ErrCorrupt
		}
		value.Revision++
		return s.save(ctx, slot, value)
	})
}

func migrateConsentURLs(profile *Profile) error {
	profile.Login.VerificationURL = migrateConsentURL(profile.ControlOrigin, profile.Secrets.TransactionID, profile.Login.VerificationURL)
	login := profile.SessionLogin
	if login == nil || len(login.Transaction) == 0 {
		return nil
	}
	var tx identityapi.NativeLoginTransaction
	if err := proto.Unmarshal(login.Transaction, &tx); err != nil {
		return ErrCorrupt
	}
	action := tx.GetBrowserAction()
	if action == nil {
		return nil
	}
	next := migrateConsentURL(profile.ControlOrigin, tx.TransactionId, action.VerificationUrl)
	if next == action.VerificationUrl {
		return nil
	}
	action.VerificationUrl = next
	raw, err := proto.Marshal(&tx)
	if err != nil {
		return ErrCorrupt
	}
	login.Transaction = raw
	return nil
}

func migrateConsentURL(controlOrigin, transactionID, previous string) string {
	origin := enrollment.NativeConsentOrigin(controlOrigin)
	if validID(transactionID) && (previous == origin+"/api/v1/auth/native-login/"+transactionID || previous == origin+"/auth/native-login/"+transactionID) {
		return enrollment.NativeConsentURL(controlOrigin, transactionID)
	}
	return previous
}
