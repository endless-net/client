package localapi

import (
	"context"
	"errors"
	"testing"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/localipc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestExactCurrentAdministratorTrustBeforeLogin(t *testing.T) {
	c, _, runner, _ := fixture(t)
	ctx := context.Background()
	owner := localipc.Principal{SID: "S-1-5-21-11"}
	admin := localipc.Principal{SID: "S-1-5-21-99", Administrator: true}
	started, err := c.begin(ctx, owner, request(1))
	if err != nil {
		t.Fatal(err)
	}
	if runner.calls != 0 || started.Attempt.Restriction == nil || started.Attempt.Restriction.ActionOwner != clientv0.ActionOwner_ACTION_OWNER_DEVICE_ADMINISTRATOR {
		t.Fatal("unconfirmed announcement dispatched login")
	}
	confirm := confirmation(t, c)
	if _, err = c.trust(ctx, owner, confirm); status.Code(err) != codes.PermissionDenied {
		t.Fatal("owner role replaced administrator authority")
	}
	source := c.config.Announcements.(*testAnnouncements)
	source.variant = 1 // Map key stays unchanged; another purpose changes.
	if _, err = c.trust(ctx, admin, confirm); status.Code(err) != codes.FailedPrecondition || runner.calls != 0 {
		t.Fatal("stale full announcement accepted")
	}
	if _, err = c.trust(ctx, admin, confirmation(t, c)); err != nil {
		t.Fatal(err)
	}
	if runner.calls != 1 {
		t.Fatal("confirmed durable attempt not dispatched")
	}
	source.variant = 2
	got, err := c.get(ctx, owner, getRequest())
	if err != nil || got.Attempt.Restriction.ReasonKey != "client.server_identity.changed" || runner.calls != 1 {
		t.Fatal("changed purpose bundle dispatched work", err)
	}
	source.err = errors.New("secret-bootstrap-value")
	got, err = c.get(ctx, owner, getRequest())
	if err != nil || got.Attempt.Restriction.ReasonKey != "client.server_identity.unavailable" || runner.calls != 1 {
		t.Fatal("bootstrap failure granted trust or lost local result", err)
	}
	if _, err = c.getIdentity(ctx, admin, &clientv0.GetServerIdentityRequest{}); status.Code(err) != codes.PermissionDenied {
		t.Fatal("foreign administrator read owner trust context")
	}
}
