package client

import (
	"context"
	"encoding/binary"
	"errors"
	"io/fs"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
)

func TestExitLANBPFCleanupDetachesBeforeUnpinAndRetainsRetry(t *testing.T) {
	for _, scenario := range []string{"complete", "partial", "empty", "detach_error", "hook_present", "unlink_error", "cancel", "replacement", "reappears"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			owned := exitLANOwnershipFixture(api.ExitFamilyDualStack)
			mask := 15
			if scenario == "partial" {
				mask = 5
			}
			if scenario == "empty" {
				mask = 0
			}
			k := newExitLANRecoveryPinKernel(t, binary.LittleEndian, owned, mask)
			unlinks := 0
			detached, observed := map[uint32]bool{}, map[uint32]bool{}
			call := func(cmd int, attr []byte, buffers ...[]byte) (int, error) {
				if cmd != exitLANBPFLinkDetach {
					if scenario == "reappears" && unlinks == 4 && cmd == exitLANBPFObjectGet {
						names, _ := exitLANBPFPinNames(owned.Scope)
						k.pins[names[0]] = &exitLANRecoveryPinObject{kind: exitLANBPFArrayOfMaps, id: 999}
					}
					return k.call(cmd, attr, buffers...)
				}
				obj := k.fds[int(k.order.Uint32(attr))]
				if len(attr) != 4 || obj == nil || obj.kind != exitLANBPFNetfilterLink {
					t.Fatal("foreign detach")
				}
				if scenario == "detach_error" {
					return -1, errExitLANBPF
				}
				detached[obj.id] = true
				return 0, nil
			}
			d := &exitLANBPFDirectory{fd: 800, check: func(int) error { return nil }, closeFD: func(int) error { return nil }, unlink: func(_ int, name string) error {
				if len(observed) != 2 {
					t.Fatal("unpin before both hooks proved absent")
				}
				if scenario == "unlink_error" {
					return errExitLANBPF
				}
				if k.pins[name] == nil {
					return fs.ErrNotExist
				}
				delete(k.pins, name)
				unlinks++
				if scenario == "cancel" {
					cancel()
				}
				return nil
			}}
			pins, err := openExitLANBPFOwnedPins(ctx, d, owned, k.order, call, k.close)
			if err != nil {
				t.Fatal(err)
			}
			n, err := newExitLANBootNamespace(ctx, func(context.Context) (exitLANNamespaceIdentity, error) { return exitLANNamespaceTestIdentity(), nil }, func() error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			r := &exitLANBPFRecovery{namespace: n, directory: d, pins: pins}
			if scenario == "replacement" {
				names, _ := exitLANBPFPinNames(owned.Scope)
				k.pins[names[0]] = &exitLANRecoveryPinObject{kind: exitLANBPFArrayOfMaps, id: 999}
			}
			blocks := 0
			err = r.cleanup(ctx, func(context.Context) error { blocks++; return nil }, k.order, call, func(_ context.Context, id exitLANBPFLinkIdentity) error {
				for _, obj := range k.pins {
					if obj.id == id.id && !detached[id.id] {
						t.Fatal("absence before detach")
					}
				}
				if scenario == "hook_present" {
					return errExitLANBPF
				}
				observed[id.id] = true
				return nil
			})
			// An empty inventory must still inspect every expected hook.
			success := scenario == "complete" || scenario == "partial" || scenario == "empty"
			if success && (err != nil || len(k.pins) != 0 || len(observed) != 2 || blocks != 2) {
				t.Fatal("cleanup did not prove absence", err)
			}
			if !success && err == nil {
				t.Fatal("incomplete cleanup succeeded")
			}
			if scenario == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation lost", err)
			}
			if (scenario == "detach_error" || scenario == "hook_present" || scenario == "replacement") && unlinks != 0 {
				t.Fatal("failed preflight deleted pins")
			}
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
			for fd := range k.fds {
				if !k.closed[fd] {
					t.Fatal("FD leaked", fd)
				}
			}
		})
	}
}
