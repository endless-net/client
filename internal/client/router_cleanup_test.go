package client

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestRouterCleanupRetainsOnlyFailedStepsAndCancellation(t *testing.T) {
	calls := [3]int{}
	failed := true
	failure := errors.New("route removal failed")
	plan := &routerCleanupPlan{}
	for i := range calls {
		plan.pending = append(plan.pending, func(context.Context) error {
			calls[i]++
			if i == 1 && failed {
				return failure
			}
			return nil
		})
	}
	if err := plan.run(t.Context()); !errors.Is(err, failure) || calls != [3]int{1, 1, 1} || len(plan.pending) != 1 {
		t.Fatal("partial cleanup lost its failure", err, calls)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := plan.run(ctx); !errors.Is(err, context.Canceled) || calls != [3]int{1, 1, 1} || len(plan.pending) != 1 {
		t.Fatal("cancelled cleanup lost pending step", err, calls)
	}
	failed = false
	if err := plan.run(t.Context()); err != nil || calls != [3]int{1, 2, 1} || len(plan.pending) != 0 {
		t.Fatal("retry repeated completed removals", err, calls)
	}
	if err := plan.run(t.Context()); err != nil || calls != [3]int{1, 2, 1} {
		t.Fatal("completed cleanup repeated", err, calls)
	}
}

func TestInterfaceCleanupRequiresVerifiedAbsence(t *testing.T) {
	failure := errors.New("remove failed")
	inspectFailure := errors.New("inspection unavailable")
	for _, scenario := range []string{"removed", "present", "absent", "unknown", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			var inspected []string
			step := routerInterfaceCleanup("owned", func(name string) (bool, error) {
				inspected = append(inspected, name)
				if scenario == "unknown" {
					return false, inspectFailure
				}
				return scenario == "present", nil
			}, func(context.Context) error {
				if scenario == "removed" {
					return nil
				}
				return failure
			})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if scenario == "cancelled" {
				cancel()
			}
			err := step(ctx)
			if scenario == "removed" || scenario == "absent" {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, failure) {
				t.Fatal("unconfirmed cleanup reported success", err)
			}
			if scenario == "unknown" && !errors.Is(err, inspectFailure) {
				t.Fatal("lost observation failure", err)
			}
			if scenario == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatal("lost cancellation", err)
			}
			if scenario == "removed" || scenario == "cancelled" {
				if len(inspected) != 0 {
					t.Fatal("unexpected inspection", inspected)
				}
			} else if !reflect.DeepEqual(inspected, []string{"owned"}) {
				t.Fatal("inspected another interface", inspected)
			}
		})
	}
}
