package control

import (
	"testing"
	"time"

	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/state"
	identityapi "github.com/endless-net/identity/api/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestOfficialNativeBrowserOriginIsExactAndPersists(t *testing.T) {
	executor, _, now := loginFixture(t)
	s, err := executor.store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	p := s.Profiles[loginProfile]
	p.ControlOrigin, p.Attempt.ControlOrigin = enrollment.ControlOrigin, enrollment.ControlOrigin
	p.Trust = state.ServerTrust{}
	_, binding, err := NativeBinding(p)
	if err != nil {
		t.Fatal(err)
	}
	n := &Native{consentPrefix: "/api/v1/auth/native-login/", now: func() time.Time { return *now }}
	const transaction = "ab8851d7-45e8-43bc-9b9a-a67ef3dd40fe"
	tx := &identityapi.NativeLoginTransaction{TransactionId: transaction, RequestId: p.Attempt.RequestID, AttemptBindingSha256: binding[:], State: identityapi.NativeLoginState_NATIVE_LOGIN_STATE_PENDING, CreatedAt: timestamppb.New(*now), ExpiresAt: timestamppb.New(now.Add(enrollment.LoginTTL)), ReplayExpiresAt: timestamppb.New(now.Add(enrollment.LoginTTL + 720*time.Second)), PollAfterSeconds: 1}
	action := &identityapi.NativeLoginBrowserAction{ExpiresAt: tx.ExpiresAt}
	tx.Outcome = &identityapi.NativeLoginTransaction_BrowserAction{BrowserAction: action}
	for _, origin := range []string{enrollment.BrowserOrigin, enrollment.ControlOrigin, "https://admin.endlessnet.ru.evil.invalid", "http://admin.endlessnet.ru", "https://admin.endlessnet.ru:444"} {
		action.VerificationUrl = origin + n.consentPrefix + transaction
		err := n.validate(p, tx, binding, "opaque-poll-proof")
		if (err == nil) != (origin == enrollment.BrowserOrigin) {
			t.Fatal("unexpected consent origin acceptance", origin, err)
		}
		p.Secrets.TransactionID, p.Secrets.PollProof = transaction, "0123456789012345678901234567890123456789012"
		p.Login = state.LoginProgress{CreatedAt: *now, ExpiresAt: tx.ExpiresAt.AsTime(), ReplayUntil: tx.ReplayExpiresAt.AsTime(), VerificationURL: action.VerificationUrl}
		s.Profiles[loginProfile] = p
		if err := s.Validate(s.InstallationID); (err == nil) != (origin == enrollment.BrowserOrigin) {
			t.Fatal("protected state disagrees with native origin policy", origin, err)
		}
	}
}
