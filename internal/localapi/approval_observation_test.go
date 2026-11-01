package localapi

import (
	"context"
	"testing"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/control"
	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/localipc"
	"github.com/endless-net/client/internal/state"
)

func TestBrowserReadReplayAndCancelKeepOriginalSendRecovery(t *testing.T) {
	for _, operation := range []string{"read", "replay", "cancel"} {
		t.Run(operation, func(t *testing.T) {
			c, _, _, now := fixture(t)
			ctx := context.Background()
			caller := localipc.Principal{SID: "S-1-5-21-11"}
			started, err := c.begin(ctx, caller, request(1))
			if err != nil {
				t.Fatal(err)
			}
			s, err := c.store.Load(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err := c.store.Update(ctx, s.Revision, func(s *state.State) error {
				p := s.Profiles[defaultID]
				a, err := p.Attempt.LoginSucceeded(*now)
				if err != nil {
					return err
				}
				p.Attempt = &a
				p.Secrets.RegistrationID = "7f026568-506c-4e97-b277-2ede0a5d5632"
				s.Profiles[defaultID] = p
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			s, err = c.store.Load(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err := c.store.Update(ctx, s.Revision, func(s *state.State) error {
				p, err := control.PrepareApprovalState(s.Profiles[defaultID], control.ApprovalTarget{NetworkName: "Requested network"}, "https://approval.example/", *now)
				if err != nil {
					return err
				}
				s.Profiles[defaultID] = p
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			s, err = c.store.Load(ctx)
			if err != nil {
				t.Fatal(err)
			}
			deadline := s.Profiles[defaultID].Attempt.ApprovalUntil
			sent := deadline.Add(-time.Second)
			if err := c.store.Update(ctx, s.Revision, func(s *state.State) error {
				p := s.Profiles[defaultID]
				a, err := p.Attempt.Observe(sent)
				if err != nil {
					return err
				}
				p.Attempt = &a
				p.RemoteInFlight, p.InFlightAt = true, sent
				s.Profiles[defaultID] = p
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			*now = deadline.Add(time.Second)
			s, err = c.store.Load(ctx)
			if err != nil {
				t.Fatal(err)
			}
			switch operation {
			case "read":
				_, err = c.get(ctx, caller, getRequest())
			case "replay":
				_, err = c.begin(ctx, caller, request(s.Revision))
			case "cancel":
				_, err = c.cancelAttempt(ctx, caller, &clientv0.CancelEnrollmentAttemptRequest{Mutation: request(s.Revision).Mutation, AttemptId: started.Attempt.AttemptId})
			}
			if err != nil {
				t.Fatal("browser observation failed", operation, err)
			}
			after, err := c.store.Load(ctx)
			if err != nil {
				t.Fatal(err)
			}
			p := after.Profiles[defaultID]
			want := enrollment.PendingApproval
			if operation == "cancel" {
				want = enrollment.Cancelled
			}
			if p.Attempt.Phase != want || !p.Attempt.Uncertain || !p.Attempt.RecoveryUntil.Equal(sent.Add(enrollment.RecoveryTTL)) || !p.Attempt.ApprovalUntil.Equal(deadline) || p.Attempt.Binding != s.Profiles[defaultID].Attempt.Binding || p.Secrets.RegistrationRequest != s.Profiles[defaultID].Secrets.RegistrationRequest {
				t.Fatal("observation lost original send recovery", operation, p.Attempt.Phase)
			}
		})
	}
}
