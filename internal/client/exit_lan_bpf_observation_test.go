package client

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
)

func exitLANBPFObservationFixture(t *testing.T, order binary.ByteOrder, mode api.ExitFamilyMode) (*exitLANBPFPreparation, *exitLANBPFLinkTestKernel) {
	t.Helper()
	p, k := newExitLANBPFLinkFixture(t, order)
	if err := p.attachClosed(t.Context(), mode, 4, -100); err != nil {
		t.Fatal(err)
	}
	links := append([]exitLANBPFLink(nil), p.links...)
	t.Cleanup(func() {
		if p.program >= 0 {
			p.links = links
		}
		_ = p.Close()
	})
	return p, k
}

func TestExitLANBPFPublicationRequiresLiveSelectedHooks(t *testing.T) {
	for _, mode := range []api.ExitFamilyMode{api.ExitFamilyIPv4Only, api.ExitFamilyIPv6Only, api.ExitFamilyDualStack} {
		p, k := exitLANBPFObservationFixture(t, binary.LittleEndian, mode)
		d, clock := exitLANBPFTestDeadline(t)
		calls := 0
		err := p.publishBootDeadline(t.Context(), d, clock, mode, 4, -100, func(ctx context.Context, id exitLANBPFLinkIdentity) error {
			if id != p.links[calls%len(p.links)].identity {
				return errExitLANBPF
			}
			calls++
			return ctx.Err()
		})
		if err != nil || calls != 2*len(p.links) || k.k.objects[p.outer].slot == 0 {
			t.Fatal("publication missing selected pre/post hooks", err, calls)
		}
	}
}

func TestExitLANBPFPublicationObservationFailureRevokes(t *testing.T) {
	for _, scenario := range []string{"pre", "post", "expiry", "nil", "unattached"} {
		t.Run(scenario, func(t *testing.T) {
			p, k := exitLANBPFObservationFixture(t, binary.LittleEndian, api.ExitFamilyIPv4Only)
			d, clock := exitLANBPFTestDeadline(t)
			if err := publishExitLANBPFTestDeadline(p, t.Context(), d, clock); err != nil {
				t.Fatal(err)
			}
			old := k.k.objects[p.outer].slot
			calls, expired := 0, false
			read := func(ctx context.Context) (exitLANClockSample, error) {
				sample, err := clock(ctx)
				if expired {
					sample.bootBefore = d.bootExpires
					sample.bootAfter = d.bootExpires
				}
				return sample, err
			}
			observe := func(context.Context, exitLANBPFLinkIdentity) error {
				calls++
				if scenario == "expiry" {
					expired = true
				}
				if scenario == "pre" || scenario == "post" && calls == 2 {
					return errExitLANBPF
				}
				return nil
			}
			if scenario == "nil" {
				observe = nil
			}
			if scenario == "unattached" {
				p.links = nil
				p.family = ""
			}
			err := p.publishBootDeadline(t.Context(), d, read, api.ExitFamilyIPv4Only, 4, -100, observe)
			if old == 0 || err == nil || k.k.objects[p.outer].slot != 0 {
				t.Fatal("failed observation retained lease", err)
			}
			if scenario == "post" && calls != 2 {
				t.Fatal("post-publication hook not checked")
			}
		})
	}
}

func TestExitLANBPFPublicationSerializesClose(t *testing.T) {
	p, k := exitLANBPFObservationFixture(t, binary.LittleEndian, api.ExitFamilyIPv4Only)
	d, clock := exitLANBPFTestDeadline(t)
	entered, release := make(chan struct{}), make(chan struct{})
	result, closed := make(chan error, 1), make(chan error, 1)
	go func() {
		first := true
		result <- p.publishBootDeadline(t.Context(), d, clock, api.ExitFamilyIPv4Only, 4, -100, func(ctx context.Context, _ exitLANBPFLinkIdentity) error {
			if first {
				first = false
				close(entered)
				select {
				case <-release:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return ctx.Err()
		})
	}()
	<-entered
	// The observer is inside the publication critical section, so Close must
	// wait instead of releasing the program/link FDs used by its second pass.
	locked := p.mu.TryLock()
	if locked {
		p.mu.Unlock()
	}
	go func() { closed <- p.Close() }()
	close(release)
	err, closeErr := <-result, <-closed
	if locked || err != nil || closeErr != nil {
		t.Fatal("Close crossed publication lock", locked, err, closeErr)
	}
	k.k.assertClosed()
}

func TestExitLANBPFObservationSelectedHeldLinks(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for _, mode := range []api.ExitFamilyMode{api.ExitFamilyIPv4Only, api.ExitFamilyIPv6Only, api.ExitFamilyDualStack} {
			p, k := exitLANBPFObservationFixture(t, order, mode)
			before, calls := k.steps, 0
			p.mu.Lock()
			err := p.observeHeldLinksLocked(t.Context(), mode, 4, -100, func(ctx context.Context, id exitLANBPFLinkIdentity) error {
				if id != p.links[calls].identity {
					return errExitLANBPF
				}
				calls++
				return ctx.Err()
			})
			p.mu.Unlock()
			if err != nil || calls != len(p.links) || k.steps-before != 2*(1+len(p.links)) {
				t.Fatal("missing fresh pre/post metadata or selected hook", err, calls, k.steps-before)
			}
		}
	}
}

func TestExitLANBPFObservationRejectsInvalidHeldSetBeforeRead(t *testing.T) {
	for _, scenario := range []string{"missing", "duplicate_fd", "duplicate_id", "program_fd", "wrong_family", "wrong_hook", "wrong_priority", "wrong_mode", "nil_callback"} {
		t.Run(scenario, func(t *testing.T) {
			p, k := exitLANBPFObservationFixture(t, binary.LittleEndian, api.ExitFamilyDualStack)
			mode := api.ExitFamilyDualStack
			called := false
			observe := func(context.Context, exitLANBPFLinkIdentity) error { called = true; return nil }
			switch scenario {
			case "missing":
				p.links = p.links[:1]
			case "duplicate_fd":
				p.links[1].fd = p.links[0].fd
			case "duplicate_id":
				p.links[1].identity.id = p.links[0].identity.id
			case "program_fd":
				p.links[0].fd = p.program
			case "wrong_family":
				p.links[0].identity.family = 10
			case "wrong_hook":
				p.links[0].identity.hook = 3
			case "wrong_priority":
				p.links[0].identity.priority++
			case "wrong_mode":
				mode = api.ExitFamilyIPv4Only
			case "nil_callback":
				observe = nil
			}
			before := k.steps
			p.mu.Lock()
			err := p.observeHeldLinksLocked(t.Context(), mode, 4, -100, observe)
			p.mu.Unlock()
			if err == nil || called || before != k.steps {
				t.Fatal("invalid set reached native observation", err)
			}
		})
	}
}

func TestExitLANBPFObservationRequiresFreshMetadataAndLiveHooks(t *testing.T) {
	for _, scenario := range []string{"pre_metadata", "wrong_program", "detached", "second_hook", "post_metadata", "post_program", "canceled", "pre_canceled", "read_failure"} {
		t.Run(scenario, func(t *testing.T) {
			p, k := exitLANBPFObservationFixture(t, binary.LittleEndian, api.ExitFamilyDualStack)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			change := func() { id := k.links[p.links[0].fd]; id.priority++; k.links[p.links[0].fd] = id }
			if scenario == "pre_metadata" {
				change()
			}
			if scenario == "wrong_program" {
				id := k.links[p.links[0].fd]
				id.programID++
				k.links[p.links[0].fd] = id
			}
			if scenario == "pre_canceled" {
				cancel()
			}
			if scenario == "read_failure" {
				k.fail = k.steps + 1
			}
			calls := 0
			p.mu.Lock()
			err := p.observeHeldLinksLocked(ctx, api.ExitFamilyDualStack, 4, -100, func(context.Context, exitLANBPFLinkIdentity) error {
				calls++
				if scenario == "detached" || scenario == "second_hook" && calls == 2 {
					return errExitLANBPF
				}
				if calls == 2 {
					if scenario == "post_metadata" {
						change()
					}
					if scenario == "post_program" {
						k.after = func(command int, attr, info []byte) {
							if command == exitLANBPFInfo && len(info) == 8 {
								k.k.order.PutUint32(info[4:], 101)
							}
						}
					}
					if scenario == "canceled" {
						cancel()
					}
				}
				return nil
			})
			p.mu.Unlock()
			if err == nil {
				t.Fatal("uncertain attachment accepted")
			}
			if (scenario == "canceled" || scenario == "pre_canceled") && !errors.Is(err, context.Canceled) {
				t.Fatal("lost cancellation", err)
			}
			if (scenario == "pre_metadata" || scenario == "wrong_program" || scenario == "pre_canceled" || scenario == "read_failure") && calls != 0 {
				t.Fatal("invalid metadata reached hook callback")
			}
			if scenario == "second_hook" && calls != 2 {
				t.Fatal("second family was not checked")
			}
		})
	}
}
