package localapi

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/control"
	"github.com/endless-net/client/internal/localipc"
	"github.com/endless-net/client/internal/state"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type currentReadSource struct{ calls int }

func (s *currentReadSource) Fetch(context.Context, string) (control.Announcement, error) {
	s.calls++
	return control.Announcement{}, errors.New("current must not fetch")
}

func TestCurrentAttemptIsPureOwnerSnapshotWithOriginalInputs(t *testing.T) {
	c, backend, runner, now := fixture(t)
	caller := localipc.Principal{SID: "S-1-5-21-11"}
	original := request(1)
	original.Mode = clientv0.EnrollmentMode_ENROLLMENT_MODE_SERVER
	original.Hostname = "original-server-host"
	started, err := c.begin(t.Context(), caller, original)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := c.store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := c.store.Update(t.Context(), saved.Revision, func(s *state.State) error {
		p := s.Profiles[defaultID]
		p.RemoteInFlight = true
		p.InFlightAt = p.Attempt.CreatedAt
		s.Profiles[defaultID] = p
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// A current read after the deadline must not Observe or RecoverLostCall.
	*now = now.Add(24 * time.Hour)
	source := &currentReadSource{}
	c.config.Announcements = source
	ciphertext := bytes.Clone(backend.data)
	calls := runner.calls
	for _, profile := range []*clientv0.ProfileRef{nil, {ProfileId: defaultID}} {
		got, err := c.get(t.Context(), caller, &clientv0.GetEnrollmentAttemptRequest{Profile: profile, Selector: &clientv0.GetEnrollmentAttemptRequest_Current{Current: true}})
		if err != nil {
			t.Fatal(err)
		}
		attempt := got.Attempt
		if attempt.Mode != original.Mode || attempt.Hostname != original.Hostname || attempt.RequestId != original.RequestId || attempt.AttemptId != started.Attempt.AttemptId || attempt.Phase != started.Attempt.Phase || attempt.Certainty != clientv0.EnrollmentCertainty_ENROLLMENT_CERTAINTY_KNOWN {
			t.Fatal("current read lost original inputs or changed observed phase")
		}
		raw, err := proto.Marshal(attempt)
		if err != nil {
			t.Fatal(err)
		}
		private := saved.Profiles[defaultID]
		for _, secret := range [][]byte{private.Keys.IdentityPrivate, private.Keys.WireGuardPrivate, []byte(private.Secrets.RequestProof)} {
			if len(secret) == 0 || bytes.Contains(raw, secret) {
				t.Fatal("private data projected or missing fixture")
			}
		}
	}
	if !bytes.Equal(ciphertext, backend.data) || runner.calls != calls || source.calls != 0 {
		t.Fatal("current snapshot caused a state/remote effect")
	}
	for _, test := range []struct {
		principal localipc.Principal
		req       *clientv0.GetEnrollmentAttemptRequest
		code      codes.Code
	}{
		{localipc.Principal{SID: "S-1-5-21-99", Administrator: true}, &clientv0.GetEnrollmentAttemptRequest{Selector: &clientv0.GetEnrollmentAttemptRequest_Current{Current: true}}, codes.PermissionDenied},
		{localipc.Principal{}, &clientv0.GetEnrollmentAttemptRequest{Selector: &clientv0.GetEnrollmentAttemptRequest_Current{Current: true}}, codes.Unauthenticated},
		{caller, &clientv0.GetEnrollmentAttemptRequest{Profile: &clientv0.ProfileRef{ProfileId: "missing"}, Selector: &clientv0.GetEnrollmentAttemptRequest_Current{Current: true}}, codes.NotFound},
		{caller, &clientv0.GetEnrollmentAttemptRequest{Selector: &clientv0.GetEnrollmentAttemptRequest_Current{Current: false}}, codes.InvalidArgument},
		{caller, &clientv0.GetEnrollmentAttemptRequest{}, codes.InvalidArgument},
		{caller, nil, codes.InvalidArgument},
	} {
		if _, err := c.get(t.Context(), test.principal, test.req); status.Code(err) != test.code {
			t.Fatalf("got %v want %v", err, test.code)
		}
	}
	if !bytes.Equal(ciphertext, backend.data) || runner.calls != calls || source.calls != 0 {
		t.Fatal("rejected read changed state")
	}
}

func TestCurrentAttemptMissingDoesNotClaimOwner(t *testing.T) {
	c, backend, runner, _ := fixture(t)
	ciphertext := bytes.Clone(backend.data)
	_, err := c.get(t.Context(), localipc.Principal{SID: "S-1-5-21-11"}, &clientv0.GetEnrollmentAttemptRequest{Selector: &clientv0.GetEnrollmentAttemptRequest_Current{Current: true}})
	if status.Code(err) != codes.PermissionDenied || !bytes.Equal(ciphertext, backend.data) || runner.calls != 0 {
		t.Fatal("current read claimed initial ownership", err)
	}
	// Existing owner profile with no attempt yields NotFound, preserving all bytes.
	if err := c.store.Update(t.Context(), 1, func(s *state.State) error {
		s.OwnerSID = "S-1-5-21-11"
		s.ActiveProfileID = defaultID
		s.Profiles[defaultID] = state.Profile{ID: defaultID, DisplayName: "empty", ControlOrigin: c.config.DefaultControlOrigin}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ciphertext = bytes.Clone(backend.data)
	_, err = c.get(t.Context(), localipc.Principal{SID: "S-1-5-21-11"}, &clientv0.GetEnrollmentAttemptRequest{Selector: &clientv0.GetEnrollmentAttemptRequest_Current{Current: true}})
	if status.Code(err) != codes.NotFound || !bytes.Equal(ciphertext, backend.data) {
		t.Fatal("no-attempt read changed profile", err)
	}
}
