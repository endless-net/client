package localapi

import (
	"bytes"
	"context"
	"testing"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/localipc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestGetSessionDoesNotClaimOrWrite(t *testing.T) {
	c, backend, runner, _ := fixture(t)
	before := append([]byte(nil), backend.data...)
	caller := localipc.Principal{SID: "S-1-5-21-11", Administrator: true}
	if _, err := c.getSession(context.Background(), caller, &clientv0.GetSessionRequest{}); status.Code(err) != codes.PermissionDenied {
		t.Fatal("unclaimed installation exposed session or claimed ownership", err)
	}
	if !bytes.Equal(before, backend.data) || runner.calls != 0 {
		t.Fatal("GetSession wrote state or woke a worker")
	}
}

func TestGetSessionOwnerExpiryAndPureRead(t *testing.T) {
	c, catalog, clock, caller := targetsFixture(t)
	ctx := context.Background()
	before, err := c.store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	first, err := c.getSession(ctx, caller, &clientv0.GetSessionRequest{})
	if err != nil || first.Session.State != clientv0.SessionState_SESSION_STATE_ACTIVE || !first.Session.ExpiresAt.AsTime().Equal(clock.Add(time.Hour)) || first.LoginAttempt != nil {
		t.Fatal("current user session was not projected independently of Node", err)
	}
	*clock = clock.Add(time.Hour)
	expired, err := c.getSession(ctx, caller, &clientv0.GetSessionRequest{Profile: &clientv0.ProfileRef{ProfileId: defaultID}})
	if err != nil || expired.Session.State != clientv0.SessionState_SESSION_STATE_EXPIRED || !expired.Session.ExpiresAt.AsTime().Equal(first.Session.ExpiresAt.AsTime()) {
		t.Fatal("expiry inferred incorrectly or extended", err)
	}
	after, err := c.store.Load(ctx)
	if err != nil || after.Revision != before.Revision || after.OwnerSID != before.OwnerSID || catalog.calls != 0 {
		t.Fatal("GetSession reconciled state or performed remote I/O", err)
	}
	if first.Metadata.InstanceId != runtimeID || first.Metadata.Revision != before.Revision || expired.Metadata.Revision != first.Metadata.Revision {
		t.Fatal("snapshot preconditions changed during pure read")
	}
	foreign := localipc.Principal{SID: "S-1-5-21-99", Administrator: true}
	if _, err := c.getSession(ctx, foreign, &clientv0.GetSessionRequest{}); status.Code(err) != codes.PermissionDenied {
		t.Fatal("foreign administrator read owner session", err)
	}
	if _, err := c.getSession(ctx, caller, &clientv0.GetSessionRequest{Profile: &clientv0.ProfileRef{ProfileId: requestID}}); status.Code(err) != codes.NotFound {
		t.Fatal("unknown profile silently selected active profile", err)
	}
}

func TestGetSessionRejectsUnknownInputAndDoesNotInventExpiry(t *testing.T) {
	c, _, _, _ := fixture(t)
	ctx := context.Background()
	caller := localipc.Principal{SID: "S-1-5-21-11"}
	if _, err := c.begin(ctx, caller, request(1)); err != nil {
		t.Fatal(err)
	}
	read, err := c.getSession(ctx, caller, &clientv0.GetSessionRequest{})
	if err != nil || read.Session.State != clientv0.SessionState_SESSION_STATE_NOT_AUTHENTICATED || read.Session.ExpiresAt != nil || read.Session.WarningAt != nil {
		t.Fatal("missing session became an expired or unbounded grant", err)
	}
	unknown := &clientv0.GetSessionRequest{}
	unknown.ProtoReflect().SetUnknown([]byte{0x98, 0x06, 0x01})
	nested := &clientv0.GetSessionRequest{Profile: &clientv0.ProfileRef{}}
	nested.Profile.ProtoReflect().SetUnknown([]byte{0x98, 0x06, 0x01})
	for _, input := range []*clientv0.GetSessionRequest{nil, unknown, nested, {Profile: &clientv0.ProfileRef{ProfileId: "invalid"}}} {
		if _, err := c.getSession(ctx, caller, input); status.Code(err) != codes.InvalidArgument {
			t.Fatal("invalid session lookup accepted", err)
		}
	}
}
