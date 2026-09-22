package client

import (
	"context"
	"encoding/binary"
	"errors"
	"reflect"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
)

const exitLANCheckpointBootID = "12345678-9abc-4def-8123-456789abcdef"

func TestExitLANBPFAmbiguousPinRetainsDurableServiceOwnership(t *testing.T) {
	m, id, protection := rpcExitLANOwnershipFixture(t)
	p, directory, kernel := newExitLANBPFPinFixture(t, binary.LittleEndian, api.ExitFamilyIPv4Only)
	checkpointed, ambiguous := false, false
	var expected *exitLANOwnership
	p.call = func(command int, attr []byte, buffers ...[]byte) (int, error) {
		if command == exitLANBPFObjectPin && !checkpointed {
			t.Fatal("pin preceded durable service checkpoint")
		}
		fd, err := kernel.call(command, attr, buffers...)
		if command == exitLANBPFObjectPin && err == nil && !ambiguous {
			ambiguous = true
			return -1, errExitLANBPF
		}
		return fd, err
	}
	created, err := p.pinClosed(t.Context(), directory, exitLANBPFTestScope, exitLANCheckpointBootID, 4, 5, func(ctx context.Context, ownership *exitLANOwnership) error {
		expected = cloneExitLANOwnership(ownership)
		if err := m.checkpointExitLANOwnership(ctx, id, protection, ownership); err != nil {
			return err
		}
		checkpointed = true
		return nil
	})
	if err == nil || !checkpointed || !ambiguous || len(created) != 0 || len(kernel.pins) != 1 || kernel.pins[exitLANBPFTestScope+"_lease"] != p.outer {
		t.Fatal("ambiguous first pin scenario not reached", err)
	}
	recovered := reopenRPCStoreFromDisk(t, m.store).Read()
	if expected == nil || len(expected.Links) != 1 || !reflect.DeepEqual(recovered.RPCState.ExitProtection.LAN, expected) || !reflect.DeepEqual(recovered.RPCState.ExitChange.Protection.LAN, expected) {
		t.Fatal("unreported pin lost complete durable recovery manifest")
	}
	if kernel.link.k.objects[p.outer].slot != 0 {
		t.Fatal("ambiguous pin left lease open")
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	kernel.link.k.assertClosed()
	if err := directory.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestExitLANBPFPinRequiresFullCheckpointBeforeEffects(t *testing.T) {
	for _, mode := range []api.ExitFamilyMode{api.ExitFamilyIPv4Only, api.ExitFamilyIPv6Only, api.ExitFamilyDualStack} {
		p, directory, kernel := newExitLANBPFPinFixture(t, binary.LittleEndian, mode)
		var saved *exitLANOwnership
		callbacks := 0
		p.call = func(command int, attr []byte, buffers ...[]byte) (int, error) {
			if command == exitLANBPFObjectPin && saved == nil {
				t.Fatal("pin preceded ownership checkpoint")
			}
			return kernel.call(command, attr, buffers...)
		}
		created, err := p.pinClosed(t.Context(), directory, exitLANBPFTestScope, exitLANCheckpointBootID, 4, 5, func(ctx context.Context, owned *exitLANOwnership) error {
			callbacks++
			if ctx.Err() != nil || len(kernel.pins) != 0 || kernel.link.k.objects[p.outer].slot != 0 {
				t.Fatal("checkpoint did not precede closed pin effects")
			}
			if validateExitLANOwnership(owned) != nil || owned.Scope != exitLANBPFTestScope || owned.BootID != exitLANCheckpointBootID || owned.NamespaceDevice != 4 || owned.NamespaceInode != 5 || owned.Family != mode || len(owned.Links) != len(p.links) {
				t.Fatal("checkpoint omitted complete scope")
			}
			for i, link := range p.links {
				if owned.Links[i].ID != link.identity.id || owned.Links[i].ProgramID != link.identity.programID || owned.Links[i].Family != link.identity.family {
					t.Fatal("checkpoint substituted link identity")
				}
			}
			saved = cloneExitLANOwnership(owned)
			// The callback receives a clone, not the internal comparison record.
			owned.Scope = "callback-mutated"
			owned.Links[0].ID++
			return nil
		})
		if err != nil || callbacks != 1 || len(created) != 2+len(p.links) || len(kernel.pins) != len(created) || saved == nil {
			t.Fatal("checkpointed pinning failed", err)
		}
		if err := p.Close(); err != nil {
			t.Fatal(err)
		}
		kernel.link.k.assertClosed()
		if err := directory.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestExitLANBPFCheckpointFailurePreventsPins(t *testing.T) {
	for _, scenario := range []string{"nil", "error", "cancel", "metadata", "directory", "existing_pin"} {
		t.Run(scenario, func(t *testing.T) {
			p, directory, kernel := newExitLANBPFPinFixture(t, binary.LittleEndian, api.ExitFamilyDualStack)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			callbacks := 0
			if scenario == "existing_pin" {
				kernel.pins[exitLANBPFTestScope+"_ipv6"] = p.links[1].fd
			}
			before := len(kernel.pins)
			checkpoint := func(context.Context, *exitLANOwnership) error {
				callbacks++
				switch scenario {
				case "error":
					return errExitLANBPF
				case "cancel":
					cancel()
				case "metadata":
					id := kernel.link.links[p.links[0].fd]
					id.priority++
					kernel.link.links[p.links[0].fd] = id
				case "directory":
					directory.check = func(int) error { return errExitLANBPF }
				}
				return nil
			}
			if scenario == "nil" {
				checkpoint = nil
			}
			created, err := p.pinClosed(ctx, directory, exitLANBPFTestScope, exitLANCheckpointBootID, 4, 5, checkpoint)
			if err == nil || len(created) != 0 || len(kernel.pins) != before {
				t.Fatal("failed checkpoint allowed pin effects", err)
			}
			if scenario == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal("checkpoint cancellation lost", err)
			}
			if scenario == "existing_pin" && callbacks != 0 {
				t.Fatal("foreign pin collision gained ownership checkpoint")
			}
			if scenario != "nil" && scenario != "existing_pin" && callbacks != 1 {
				t.Fatal("checkpoint not exercised")
			}
			if err := p.Close(); err != nil {
				t.Fatal(err)
			}
			kernel.link.k.assertClosed()
			if err := directory.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestExitLANBPFPartialPinFailureRetainsFullCheckpoint(t *testing.T) {
	// Discover the bounded ABI sequence once, then inject failure at every
	// syscall after persistence, including metadata and pin readback calls.
	steps := exitLANCheckpointFailureAttempt(t, 0, false)
	for step := 1; step <= steps; step++ {
		exitLANCheckpointFailureAttempt(t, step, false)
	}
	// Simulate ambiguous return after the kernel created each pin. The complete
	// prewritten manifest covers even a pin missing from the returned name list.
	for pin := 1; pin <= 4; pin++ {
		exitLANCheckpointFailureAttempt(t, pin, true)
	}
}

func exitLANCheckpointFailureAttempt(t *testing.T, failAt int, ambiguous bool) int {
	t.Helper()
	p, directory, kernel := newExitLANBPFPinFixture(t, binary.LittleEndian, api.ExitFamilyDualStack)
	var saved, expected *exitLANOwnership
	steps, pins := 0, 0
	failed := false
	p.call = func(command int, attr []byte, buffers ...[]byte) (int, error) {
		if saved != nil {
			steps++
		}
		if command == exitLANBPFObjectPin {
			if saved == nil {
				t.Fatal("partial pin has no durable manifest")
			}
			pins++
		}
		if !ambiguous && failAt != 0 && saved != nil && steps == failAt {
			failed = true
			return -1, errExitLANBPF
		}
		fd, err := kernel.call(command, attr, buffers...)
		if ambiguous && command == exitLANBPFObjectPin && pins == failAt && err == nil {
			failed = true
			return -1, errExitLANBPF
		}
		return fd, err
	}
	created, err := p.pinClosed(t.Context(), directory, exitLANBPFTestScope, exitLANCheckpointBootID, 4, 5, func(_ context.Context, owned *exitLANOwnership) error {
		saved, expected = cloneExitLANOwnership(owned), cloneExitLANOwnership(owned)
		return nil
	})
	if saved == nil || len(saved.Links) != 2 || validateExitLANOwnership(saved) != nil || !reflect.DeepEqual(saved, expected) {
		t.Fatal("partial failure lost complete manifest")
	}
	if failAt == 0 {
		if err != nil || len(created) != 4 {
			t.Fatal("baseline pin sequence failed", err)
		}
	} else if !failed || err == nil {
		t.Fatal("injected post-checkpoint failure not propagated", failAt, ambiguous, err)
	}
	if ambiguous && (len(kernel.pins) != failAt || len(created) != failAt-1) {
		t.Fatal("ambiguous pin fixture did not create unreported object")
	}
	if kernel.link.k.objects[p.outer].slot != 0 {
		t.Fatal("pin failure reopened lease")
	}
	observedSteps := steps
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	kernel.link.k.assertClosed()
	if err := directory.Close(); err != nil {
		t.Fatal(err)
	}
	return observedSteps
}
