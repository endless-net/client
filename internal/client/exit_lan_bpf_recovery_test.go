package client

import (
	"context"
	"errors"
	"reflect"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
)

func TestExitLANBPFRecoveryScopesAndHandleOwnership(t *testing.T) {
	for _, scenario := range []string{"complete", "partial", "empty", "boot", "device", "inode", "invalid", "pre_cancel", "namespace_error", "namespace_partial_error", "block_before", "directory_error", "directory_partial_error", "pins_error", "pins_partial_error", "block_after", "namespace_after", "cancel_after"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			owned := exitLANOwnershipFixture(api.ExitFamilyDualStack)
			switch scenario {
			case "boot":
				owned.BootID = "22345678-9abc-4def-8123-456789abcdef"
			case "device":
				owned.NamespaceDevice++
			case "inode":
				owned.NamespaceInode++
			case "invalid":
				owned.Scope = "../untrusted"
			case "pre_cancel":
				cancel()
			}
			original := cloneExitLANOwnership(owned)
			events := []string{}
			namespaceOpened, directoryOpened, inventoryOpened := false, false, false
			namespaceCloses, directoryCloses, blocks := 0, 0, 0
			fdCloses := map[int]int{}
			changed := false
			var inventory *exitLANBPFOwnedPins
			ops := exitLANBPFRecoveryOps{
				namespace: func(ctx context.Context) (*exitLANBootNamespace, error) {
					events = append(events, "namespace")
					if scenario == "namespace_error" {
						return nil, errExitLANBPF
					}
					n, err := newExitLANBootNamespace(ctx, func(context.Context) (exitLANNamespaceIdentity, error) {
						id := exitLANNamespaceTestIdentity()
						if changed {
							id.Inode++
						}
						return id, nil
					}, func() error { events = append(events, "close_namespace"); namespaceCloses++; return nil })
					namespaceOpened = n != nil
					if scenario == "namespace_partial_error" && err == nil {
						return n, errExitLANBPF
					}
					return n, err
				},
				directory: func(context.Context) (*exitLANBPFDirectory, error) {
					events = append(events, "directory")
					if blocks != 1 {
						t.Fatal("directory opened before BLOCK")
					}
					if scenario == "directory_error" {
						return nil, errExitLANBPF
					}
					directoryOpened = true
					d := &exitLANBPFDirectory{fd: 800, check: func(int) error { return nil }, closeFD: func(int) error { events = append(events, "close_directory"); directoryCloses++; return nil }}
					if scenario == "directory_partial_error" {
						return d, errExitLANBPF
					}
					return d, nil
				},
				pins: func(_ context.Context, d *exitLANBPFDirectory, manifest *exitLANOwnership) (*exitLANBPFOwnedPins, error) {
					events = append(events, "pins")
					if blocks != 1 || d == nil || d.fd != 800 || !reflect.DeepEqual(manifest, original) {
						t.Fatal("pin inventory scope/order changed")
					}
					if scenario == "pins_error" {
						return nil, errExitLANBPF
					}
					fds := [4]int{11, 12, 13, 14}
					if scenario == "partial" {
						fds = [4]int{11, -1, 13, -1}
					}
					if scenario == "empty" {
						fds = [4]int{-1, -1, -1, -1}
					}
					inventory = &exitLANBPFOwnedPins{ownership: cloneExitLANOwnership(manifest), fds: fds, leaseRevoked: fds[0] >= 0, closeFD: func(fd int) error {
						events = append(events, "close_pin")
						fdCloses[fd]++
						if fdCloses[fd] != 1 {
							t.Fatal("pin FD closed twice")
						}
						return nil
					}}
					inventoryOpened = true
					// A factory's argument must not alias the caller's durable record.
					manifest.Links[0].ID++
					if scenario == "pins_partial_error" {
						return inventory, errExitLANBPF
					}
					return inventory, nil
				},
			}
			blocked := func(context.Context) error {
				events = append(events, "blocked")
				blocks++
				if scenario == "block_before" && blocks == 1 || scenario == "block_after" && blocks == 2 {
					return errExitLANBPF
				}
				if scenario == "namespace_after" && blocks == 2 {
					changed = true
				}
				if scenario == "cancel_after" && blocks == 2 {
					cancel()
				}
				return nil
			}
			r, err := recoverExitLANBPF(ctx, owned, blocked, ops)
			success := scenario == "complete" || scenario == "partial" || scenario == "empty"
			if success {
				if err != nil || r == nil || blocks != 2 || namespaceCloses != 0 || directoryCloses != 0 || len(fdCloses) != 0 {
					t.Fatal("recovery failed to retain handles", err, events)
				}
				if err := r.Close(); err != nil {
					t.Fatal(err)
				}
				if err := r.Close(); err != nil {
					t.Fatal(err)
				}
			} else if err == nil || r != nil {
				t.Fatal("unsafe/incomplete recovery returned", scenario)
			}
			if (scenario == "pre_cancel" || scenario == "cancel_after") && !errors.Is(err, context.Canceled) {
				t.Fatal("recovery cancellation lost", err)
			}
			if scenario == "invalid" || scenario == "pre_cancel" {
				if len(events) != 0 {
					t.Fatal("invalid input caused effects", events)
				}
			}
			if scenario == "boot" || scenario == "device" || scenario == "inode" {
				if blocks != 0 || directoryOpened || inventoryOpened {
					t.Fatal("foreign namespace reached cleanup")
				}
			}
			if namespaceOpened && namespaceCloses != 1 || directoryOpened && directoryCloses != 1 {
				t.Fatal("failed/successful recovery leaked handles", events)
			}
			if inventoryOpened {
				want := 4
				if scenario == "partial" {
					want = 2
				}
				if scenario == "empty" {
					want = 0
				}
				if len(fdCloses) != want || !inventory.closed || !reflect.DeepEqual(inventory.ownership, original) {
					t.Fatal("inventory close lost ownership or FD cleanup", events)
				}
				if scenario == "empty" && inventory.leaseRevoked {
					t.Fatal("missing map gained revocation proof")
				}
			}
			if !reflect.DeepEqual(owned, original) {
				t.Fatal("recovery altered caller's durable manifest")
			}
			// FD disposal must precede release of the directory and namespace.
			for i, event := range events {
				if event == "close_directory" {
					for _, later := range events[i+1:] {
						if later == "close_pin" {
							t.Fatal("directory closed before pin handles")
						}
					}
				}
				if event == "close_namespace" && i != len(events)-1 {
					t.Fatal("namespace handle released before child cleanup", events)
				}
			}
		})
	}
}
