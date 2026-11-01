package localapi

import (
	"bytes"
	"context"
	"testing"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/localipc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestRuntimeEnrollmentCapabilityMatchesOwnerClaimWithoutSideEffects(t *testing.T) {
	c, m, runner, _ := fixture(t)
	ctx := context.Background()
	owner := localipc.Principal{SID: "S-1-5-21-11"}
	before := append([]byte(nil), m.data...)
	initial, err := c.runtimeInfo(ctx, owner)
	if err != nil || len(initial.Runtime.Capabilities) != 1 || initial.Runtime.Capabilities[0].Capability != clientv0.Capability_CAPABILITY_ENROLLMENT || initial.Runtime.Capabilities[0].Restriction.Availability != clientv0.Availability_AVAILABILITY_AVAILABLE {
		t.Fatal("initial claim capability missing", err)
	}
	loaded, err := c.store.Load(ctx)
	if err != nil || loaded.OwnerSID != "" || loaded.Revision != 1 || !bytes.Equal(before, m.data) || runner.calls != 0 {
		t.Fatal("RuntimeInfo changed unclaimed state", err)
	}
	if _, err := c.begin(ctx, owner, request(1)); err != nil {
		t.Fatal("advertised initial claim did not work", err)
	}
	claimed, err := c.store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	before = append([]byte(nil), m.data...)
	wakeCount := runner.calls
	for _, test := range []struct {
		p         localipc.Principal
		access    clientv0.Access
		available bool
	}{
		{owner, clientv0.Access_ACCESS_OWNER, true},
		{localipc.Principal{SID: owner.SID, Administrator: true}, clientv0.Access_ACCESS_ADMINISTRATOR, true},
		{localipc.Principal{SID: "S-1-5-21-12"}, clientv0.Access_ACCESS_OBSERVER, false},
		{localipc.Principal{SID: "S-1-5-21-12", Administrator: true}, clientv0.Access_ACCESS_ADMINISTRATOR, false},
	} {
		info, err := c.runtimeInfo(ctx, test.p)
		if err != nil || info.Runtime.CallerAccess != test.access {
			t.Fatal("caller role projection differs", err)
		}
		capability := info.Runtime.Capabilities[0]
		available := capability.Restriction.Availability == clientv0.Availability_AVAILABILITY_AVAILABLE
		if available != test.available || capability.Platform != c.config.Build.Platform {
			t.Fatal("incorrect caller/platform capability")
		}
		if !available {
			if capability.Restriction.ReasonKey != "client.owner.required" {
				t.Fatal("owner restriction reason lost")
			}
			if _, err := c.begin(ctx, test.p, request(claimed.Revision)); status.Code(err) != codes.PermissionDenied {
				t.Fatal("foreign/admin capability did not match Enroll denial", err)
			}
		}
		// Returned messages are independent; no caller can poison another view.
		capability.Restriction.Availability = clientv0.Availability_AVAILABILITY_AVAILABLE
	}
	if !bytes.Equal(before, m.data) || runner.calls != wakeCount {
		t.Fatal("capability/denied calls changed state or dispatched work")
	}
}

func TestRuntimeInfoCannotAuthenticateMetadataAndFailsClosedOnCorruptStore(t *testing.T) {
	c, m, _, _ := fixture(t)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-caller-sid", "S-1-5-21-11", "x-administrator", "true"))
	if response, err := c.GetRuntimeInfo(ctx, &clientv0.GetRuntimeInfoRequest{}); response != nil || status.Code(err) != codes.Unauthenticated {
		t.Fatal("metadata became OS identity", err)
	}
	m.data = []byte("corrupt encrypted state")
	if response, err := c.runtimeInfo(context.Background(), localipc.Principal{SID: "S-1-5-21-11"}); response != nil || status.Code(err) != codes.Unavailable {
		t.Fatal("corrupt store produced capability grants", err)
	}
}
