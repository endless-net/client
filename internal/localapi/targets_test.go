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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type targetCatalog struct {
	items []*clientv0.EnrollmentTarget
	calls int
	err   error
}

func (c *targetCatalog) Fetch(context.Context, state.Profile) ([]*clientv0.EnrollmentTarget, error) {
	c.calls++
	if c.err != nil {
		return nil, c.err
	}
	var result []*clientv0.EnrollmentTarget
	for _, v := range c.items {
		result = append(result, proto.Clone(v).(*clientv0.EnrollmentTarget))
	}
	return result, nil
}
func targetsFixture(t *testing.T) (*Controller, *targetCatalog, *time.Time, localipc.Principal) {
	t.Helper()
	c, _, _, clock := fixture(t)
	caller := localipc.Principal{SID: "S-1-5-21-11"}
	ctx := context.Background()
	if _, err := c.begin(ctx, caller, request(1)); err != nil {
		t.Fatal(err)
	}
	if err := c.store.Update(ctx, 2, func(s *state.State) error {
		p := s.Profiles[defaultID]
		a, err := p.Attempt.LoginSucceeded(*clock)
		if err != nil {
			return err
		}
		p.Attempt = &a
		p.Secrets.Access = "native-access"
		p.Secrets.SessionID = "native-session"
		p.Secrets.SessionExpiresAt = clock.Add(time.Hour)
		s.Profiles[defaultID] = p
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	catalog := &targetCatalog{items: []*clientv0.EnrollmentTarget{{AccountId: "account-a", NetworkId: "network-a", Restriction: &clientv0.Restriction{Availability: clientv0.Availability_AVAILABILITY_AVAILABLE}}, {AccountId: "account-a", NetworkId: "network-b", Restriction: &clientv0.Restriction{Availability: clientv0.Availability_AVAILABILITY_AVAILABLE}}, {AccountId: "account-b", NetworkId: "network-c", Restriction: &clientv0.Restriction{Availability: clientv0.Availability_AVAILABILITY_AVAILABLE}}}}
	c.config.Targets = catalog
	c.config.TargetPageKey = [32]byte{5}
	return c, catalog, clock, caller
}
func targetListRequest(t *testing.T, c *Controller) *clientv0.ListEnrollmentTargetsRequest {
	t.Helper()
	s, err := c.store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return &clientv0.ListEnrollmentTargetsRequest{AttemptId: s.Profiles[defaultID].Attempt.AttemptID, Page: &clientv0.PageRequest{PageSize: 1}}
}

func TestTargetsOwnerCursorExpiryRevisionAndCatalogBinding(t *testing.T) {
	c, catalog, clock, caller := targetsFixture(t)
	ctx := context.Background()
	req := targetListRequest(t, c)
	first, err := c.listTargets(ctx, caller, req)
	if err != nil || len(first.Targets) != 1 || len(first.Page.NextPageToken) != 151 {
		t.Fatal("first page", err)
	}
	req.Page.PageToken = first.Page.NextPageToken
	second, err := c.listTargets(ctx, caller, req)
	if err != nil || second.Targets[0].NetworkId != "network-b" || catalog.calls != 2 {
		t.Fatal("next page did not reauthorize", err)
	}
	if _, err = c.listTargets(ctx, localipc.Principal{SID: "foreign", Administrator: true}, req); status.Code(err) != codes.PermissionDenied {
		t.Fatal("foreign administrator reused page token")
	}
	original := req.Page.PageToken
	req.Page.PageToken = original[:len(original)-1] + "A"
	if req.Page.PageToken == original {
		req.Page.PageToken = original[:len(original)-1] + "B"
	}
	if _, err = c.listTargets(ctx, caller, req); status.Code(err) != codes.Aborted {
		t.Fatal("tampered page token accepted")
	}
	req.Page.PageToken = original
	catalog.items[0].NetworkName = "changed"
	if _, err = c.listTargets(ctx, caller, req); status.Code(err) != codes.Aborted {
		t.Fatal("changed catalog continued stale query")
	}
	catalog.items[0].NetworkName = ""
	*clock = clock.Add(300 * time.Second)
	if _, err = c.listTargets(ctx, caller, req); status.Code(err) != codes.Aborted {
		t.Fatal("expired page token revived")
	}
}

func TestSelectionReauthorizesAndPersistsOneRegistrationID(t *testing.T) {
	c, catalog, _, caller := targetsFixture(t)
	ctx := context.Background()
	list, err := c.listTargets(ctx, caller, targetListRequest(t, c))
	if err != nil {
		t.Fatal(err)
	}
	req := &clientv0.SelectEnrollmentTargetRequest{Mutation: &clientv0.MutationContext{ExpectedInstanceId: runtimeID, ExpectedRevision: list.Metadata.Revision}, AttemptId: targetListRequest(t, c).AttemptId, AccountId: "account-a", NetworkId: "network-a"}
	catalog.err = control.ErrTargetDenied
	if _, err = c.selectTarget(ctx, caller, req); status.Code(err) != codes.PermissionDenied {
		t.Fatal("cached target replaced current authorization")
	}
	s, err := c.store.Load(ctx)
	if err != nil || s.Profiles[defaultID].Attempt.Phase != enrollment.ChoosingContext || s.Profiles[defaultID].Secrets.RegistrationID != "" {
		t.Fatal("denial mutated registration", err)
	}
	catalog.err = nil
	selected, err := c.selectTarget(ctx, caller, req)
	if err != nil || selected.Attempt.Phase != clientv0.EnrollmentPhase_ENROLLMENT_PHASE_REGISTERING || selected.Attempt.NodeId != nil {
		t.Fatal("selection claimed admission", err)
	}
	s, err = c.store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	id := s.Profiles[defaultID].Secrets.RegistrationID
	if !validID(id) {
		t.Fatal("registration ID not persisted before dispatch")
	}
	continued := targetListRequest(t, c)
	continued.Page.PageToken = list.Page.NextPageToken
	if _, err = c.listTargets(ctx, caller, continued); status.Code(err) != codes.Aborted {
		t.Fatal("revision change did not invalidate page token")
	}
	req.Mutation.ExpectedRevision = s.Revision
	if _, err = c.selectTarget(ctx, caller, req); err != nil {
		t.Fatal(err)
	}
	s, err = c.store.Load(ctx)
	if err != nil || s.Profiles[defaultID].Secrets.RegistrationID != id {
		t.Fatal("selection retry replaced registration ID", err)
	}
	req.Mutation.ExpectedRevision = s.Revision
	req.NetworkId = "network-b"
	if _, err = c.selectTarget(ctx, caller, req); status.Code(err) != codes.FailedPrecondition {
		t.Fatal("selected context silently replaced")
	}
}

func TestNoTargetsHasExplicitAccessAdministratorReason(t *testing.T) {
	c, catalog, _, caller := targetsFixture(t)
	catalog.items = nil
	_, err := c.listTargets(context.Background(), caller, targetListRequest(t, c))
	s := status.Convert(err)
	if s.Code() != codes.PermissionDenied {
		t.Fatal(err)
	}
	if len(s.Details()) != 1 {
		t.Fatal("missing typed no-targets reason")
	}
	failure, ok := s.Details()[0].(*clientv0.Failure)
	if !ok || failure.ReasonKey != "client.enrollment.no_targets" || failure.ActionOwner != clientv0.ActionOwner_ACTION_OWNER_ACCESS_ADMINISTRATOR {
		t.Fatal("no-targets action owner missing")
	}
}
