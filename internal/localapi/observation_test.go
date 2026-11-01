package localapi

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/endless-net/client/internal/localipc"
	"github.com/endless-net/client/internal/state"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestAttemptReadRefreshesConcurrentWorkerCAS(t *testing.T) {
	c, _, _, now := fixture(t)
	ctx := context.Background()
	caller := localipc.Principal{SID: "S-1-5-21-11"}
	started, err := c.begin(ctx, caller, request(1))
	if err != nil {
		t.Fatal(err)
	}
	before, err := c.store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Second)
	interleaved := false
	c.config.Now = func() time.Time {
		if !interleaved {
			interleaved = true
			if err := c.store.Update(ctx, before.Revision, func(current *state.State) error {
				p := current.Profiles[defaultID]
				next, err := p.Attempt.Observe(*now)
				if err != nil {
					return err
				}
				p.Attempt = &next
				current.Profiles[defaultID] = p
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		}
		return *now
	}
	got, err := c.get(ctx, caller, getRequest())
	if err != nil {
		t.Fatal("read exposed internal worker CAS race", err)
	}
	after, err := c.store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !interleaved || got.Attempt.Metadata.Revision != after.Revision || after.Revision != before.Revision+1 || got.Attempt.AttemptId != started.Attempt.AttemptId || after.Profiles[defaultID].Attempt.Binding != before.Profiles[defaultID].Attempt.Binding || !after.Profiles[defaultID].Attempt.Deadline.Equal(before.Profiles[defaultID].Attempt.Deadline) || !bytes.Equal(after.Profiles[defaultID].Keys.IdentityPrivate, before.Profiles[defaultID].Keys.IdentityPrivate) {
		t.Fatal("reread changed original identity/key/deadline or lost latest revision")
	}
}

func TestAttemptReadContentionRemainsBounded(t *testing.T) {
	c, _, _, now := fixture(t)
	ctx := context.Background()
	caller := localipc.Principal{SID: "S-1-5-21-11"}
	if _, err := c.begin(ctx, caller, request(1)); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Second)
	writes := 0
	// With permanent contention every observation loses CAS, so its write
	// callback (and therefore its project clock) is never entered.
	c.config.Now = func() time.Time {
		writes++
		if writes > 6 {
			t.Fatal("unbounded read CAS retries")
		}
		current, err := c.store.Load(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.store.Update(ctx, current.Revision, func(*state.State) error { return nil }); err != nil {
			t.Fatal(err)
		}
		return *now
	}
	if response, err := c.get(ctx, caller, getRequest()); response != nil || status.Code(err) != codes.Aborted || writes != 6 {
		t.Fatal("persistent contention was success or unbounded", err, writes)
	}
}
