package client

import (
	"context"
	"encoding/binary"
	"errors"
	"reflect"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
)

type exitLANBPFSessionFixture struct {
	t                                                                   *testing.T
	mode                                                                api.ExitFamilyMode
	scenario                                                            string
	ns                                                                  *exitLANBootNamespace
	directory                                                           *exitLANBPFDirectory
	preparation                                                         *exitLANBPFPreparation
	kernel                                                              *exitLANBPFPinTestKernel
	saved                                                               *exitLANOwnership
	namespaceCloses, directoryCloses, blocks, checkpoints, observations int
	changed                                                             bool
	events                                                              []string
	cancel                                                              context.CancelFunc
}

func (f *exitLANBPFSessionFixture) ops() exitLANBPFSessionOps {
	return exitLANBPFSessionOps{
		namespace: func(ctx context.Context) (*exitLANBootNamespace, error) {
			f.events = append(f.events, "namespace")
			if f.scenario == "namespace" {
				return nil, errExitLANBPF
			}
			n, err := newExitLANBootNamespace(ctx, func(context.Context) (exitLANNamespaceIdentity, error) {
				id := exitLANNamespaceTestIdentity()
				if f.changed {
					id.Inode++
				}
				return id, nil
			}, func() error { f.namespaceCloses++; return nil })
			f.ns = n
			return n, err
		},
		directory: func(context.Context) (*exitLANBPFDirectory, error) {
			f.events = append(f.events, "directory")
			if f.blocks != 1 {
				f.t.Fatal("directory opened before containment")
			}
			if f.scenario == "directory" {
				return nil, errExitLANBPF
			}
			f.directory = &exitLANBPFDirectory{fd: 800, check: func(int) error { return nil }, closeFD: func(int) error { f.directoryCloses++; return nil }}
			return f.directory, nil
		},
		preparation: func(_ context.Context, mark uint32) (*exitLANBPFPreparation, error) {
			f.events = append(f.events, "preparation")
			if mark != 7 || f.directory == nil || f.blocks != 1 {
				f.t.Fatal("unbound preparation arguments/order")
			}
			if f.scenario == "preparation" {
				return nil, errExitLANBPF
			}
			p, link := newExitLANBPFLinkFixture(f.t, binary.LittleEndian)
			f.preparation = p
			f.kernel = &exitLANBPFPinTestKernel{link: link, pins: map[string]int{}}
			if len(p.links) != 0 {
				f.t.Fatal("session fixture must start unattached")
			}
			p.call = func(command int, attr []byte, buffers ...[]byte) (int, error) {
				if command == exitLANBPFLinkCreate {
					f.events = append(f.events, "attach")
					if f.blocks == 0 {
						f.t.Fatal("attach preceded containment")
					}
					if f.scenario == "attach" {
						return -1, errExitLANBPF
					}
				}
				if command == exitLANBPFObjectPin {
					f.events = append(f.events, "pin")
					if f.saved == nil || f.checkpoints != 1 {
						f.t.Fatal("pin preceded complete checkpoint")
					}
				}
				fd, err := f.kernel.call(command, attr, buffers...)
				if command == exitLANBPFObjectPin && f.scenario == "ambiguous_pin" && err == nil {
					return -1, errExitLANBPF
				}
				return fd, err
			}
			return p, nil
		},
		observe: func(ctx context.Context, id exitLANBPFLinkIdentity) error {
			f.events = append(f.events, "observe")
			f.observations++
			if f.saved == nil || len(f.kernel.pins) != 2+len(f.saved.Links) {
				f.t.Fatal("hook observation preceded complete pins")
			}
			found := false
			for _, link := range f.preparation.links {
				found = found || id == link.identity
			}
			if !found {
				f.t.Fatal("hook identity not owned")
			}
			if f.scenario == "observe" {
				return errExitLANBPF
			}
			return ctx.Err()
		},
	}
}

func (f *exitLANBPFSessionFixture) checkpoint(ctx context.Context, owned *exitLANOwnership) error {
	f.events = append(f.events, "checkpoint")
	f.checkpoints++
	if len(f.kernel.pins) != 0 || validateExitLANOwnership(owned) != nil || owned.Family != f.mode || owned.BootID != exitLANNamespaceTestIdentity().BootID || owned.NamespaceDevice != 4 || owned.NamespaceInode != 5 {
		f.t.Fatal("invalid pre-pin manifest")
	}
	if f.scenario == "checkpoint" {
		return errExitLANBPF
	}
	f.saved = cloneExitLANOwnership(owned)
	if f.scenario == "cancel_checkpoint" {
		f.cancel()
	}
	return ctx.Err()
}

func (f *exitLANBPFSessionFixture) blocked(ctx context.Context) error {
	f.events = append(f.events, "blocked")
	f.blocks++
	if f.scenario == "block_before" && f.blocks == 1 || f.scenario == "block_after" && f.blocks == 2 {
		return errExitLANBPF
	}
	if f.blocks == 2 && f.scenario == "namespace_after" {
		f.changed = true
	}
	return ctx.Err()
}

func (f *exitLANBPFSessionFixture) assertClosed() {
	f.t.Helper()
	if f.ns != nil && f.namespaceCloses != 1 || f.directory != nil && f.directoryCloses != 1 {
		f.t.Fatal("session handle cleanup not exactly once", f.namespaceCloses, f.directoryCloses)
	}
	if f.kernel != nil {
		f.kernel.link.k.assertClosed()
	}
}

func TestExitLANBPFSessionOwnsClosedSelectedArtifacts(t *testing.T) {
	for _, mode := range []api.ExitFamilyMode{api.ExitFamilyIPv4Only, api.ExitFamilyIPv6Only, api.ExitFamilyDualStack} {
		f := &exitLANBPFSessionFixture{t: t, mode: mode}
		s, err := prepareExitLANBPFSession(t.Context(), 7, mode, 4, 10, exitLANBPFTestScope, f.checkpoint, f.blocked, f.ops())
		if err != nil || s == nil || f.blocks != 2 || f.checkpoints != 1 || f.observations != len(f.saved.Links) || f.namespaceCloses != 0 || f.directoryCloses != 0 {
			t.Fatal("session preparation lost ownership/order", err, f.events)
		}
		outer := f.preparation.outer
		if f.kernel.link.k.objects[outer].slot != 0 {
			t.Fatal("preparation published LAN lease")
		}
		pins := len(f.kernel.pins)
		manifest := cloneExitLANOwnership(f.saved)
		// Inject a lease to prove session Close revokes before releasing FDs.
		deadline, clock := exitLANBPFTestDeadline(t)
		if err := publishExitLANBPFTestDeadline(f.preparation, t.Context(), deadline, clock); err != nil {
			t.Fatal(err)
		}
		if f.kernel.link.k.objects[outer].slot == 0 {
			t.Fatal("close fixture did not hold a lease")
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		f.assertClosed()
		if f.kernel.link.k.objects[outer].slot != 0 || len(f.kernel.pins) != pins || !reflect.DeepEqual(f.saved, manifest) {
			t.Fatal("Close retained lease or removed recovery pins/manifest")
		}
	}
}

func TestExitLANBPFSessionFailureKeepsClosedRecoveryArtifacts(t *testing.T) {
	for _, scenario := range []string{"namespace", "block_before", "directory", "preparation", "attach", "checkpoint", "cancel_checkpoint", "ambiguous_pin", "observe", "block_after", "namespace_after"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			f := &exitLANBPFSessionFixture{t: t, mode: api.ExitFamilyDualStack, scenario: scenario, cancel: cancel}
			s, err := prepareExitLANBPFSession(ctx, 7, f.mode, 4, 10, exitLANBPFTestScope, f.checkpoint, f.blocked, f.ops())
			if err == nil || s != nil {
				t.Fatal("incomplete session returned", scenario)
			}
			if scenario == "cancel_checkpoint" && !errors.Is(err, context.Canceled) {
				t.Fatal("session cancellation lost", err)
			}
			f.assertClosed()
			if f.saved != nil && (validateExitLANOwnership(f.saved) != nil || len(f.saved.Links) != 2) {
				t.Fatal("failed session lost complete manifest")
			}
			if scenario == "ambiguous_pin" && (f.saved == nil || len(f.kernel.pins) != 1) {
				t.Fatal("ambiguous pin not retained for recovery")
			}
			if scenario == "observe" || scenario == "block_after" || scenario == "namespace_after" {
				if f.saved == nil || len(f.kernel.pins) != 4 {
					t.Fatal("post-pin failure removed recovery artifacts")
				}
			}
		})
	}
}
