package main

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/endless-net/client/internal/client"
)

type agentRuntimeLifecycleEngine interface {
	Suspend(context.Context) (client.WireGuardApplyResult, error)
	Resume(context.Context) error
	Down(context.Context) (client.WireGuardApplyResult, error)
}

func startAgentRuntimeLifecycle(ctx context.Context, cancel context.CancelCauseFunc, mutations *client.ClientRPCMutations, opts agentIPCOptions, events <-chan client.RuntimeLifecycleNotification, engine agentRuntimeLifecycleEngine) (func() error, error) {
	if events == nil {
		return func() error { return nil }, nil
	}
	if cancel == nil {
		return nil, errors.New("runtime lifecycle requires cancellation")
	}
	if mutations == nil {
		// Headless agents still own durable lifecycle state without an IPC host.
		var err error
		mutations, err = client.NewClientRPCMutations(opts.ConfigStore)
		if err != nil {
			return nil, err
		}
	}
	var pendingLogoff *client.RuntimeLifecycleNotification
	refreshSource := func(ctx context.Context) error {
		before := opts.ConfigStore.Read()
		if before.NodeID == "" {
			return nil // Unenrolled runtime has only local policy.
		}
		if opts.Offline {
			return mutations.RefreshRuntimeLifecyclePolicy(ctx, before, before)
		}
		return refreshAgentPolicySnapshot(ctx, opts.ConfigStore, opts.Timeout, func(before, candidate client.Config) error {
			return mutations.RefreshRuntimeLifecyclePolicy(ctx, before, candidate)
		})
	}
	refresh := func(ctx context.Context) error {
		if err := refreshSource(ctx); err != nil {
			return err
		}
		if pendingLogoff != nil {
			// Resume has already confirmed Down under the effect lock. Resolve
			// an earlier failed logoff before it may release that lock, using
			// refreshed policy and the current owner rather than stale intent.
			owner := opts.ConfigStore.Read().LocalOwnerID
			if owner != "" && strings.EqualFold(owner, pendingLogoff.SessionOwner) {
				if err := mutations.ApplyRuntimeLifecycleIntent(ctx, client.RuntimeUserLogoff, pendingLogoff.SessionOwner); err != nil {
					return err
				}
			}
			pendingLogoff = nil
		}
		return nil
	}
	executor, err := client.NewRuntimeLifecycleExecutor(ctx, mutations, engine, opts.OperationMu, func() { requestAgentSync(opts) }, refresh, func(stopped bool, failure error) error {
		return observeAgentRuntimeLifecycle(ctx, mutations, opts, stopped, failure)
	})
	if err != nil {
		return nil, err
	}
	done := make(chan struct{})
	var closeErr error
	go func() {
		defer close(done)
		defer func() { closeErr = executor.Close() }()
		timer := time.NewTimer(time.Hour)
		timer.Stop()
		defer timer.Stop()
		var retry <-chan time.Time
		var pendingPower *client.RuntimeLifecycleNotification
		handle := func(event client.RuntimeLifecycleNotification) {
			if event.Event == client.RuntimeUserLogoff {
				owner := opts.ConfigStore.Read().LocalOwnerID
				if event.SessionOwner == "" || owner == "" || !strings.EqualFold(event.SessionOwner, owner) {
					if pendingLogoff != nil && pendingLogoff.SessionOwner == event.SessionOwner {
						pendingLogoff = nil
					}
					return
				}
			} else if event.Event != client.RuntimeSuspend && event.Event != client.RuntimeResume {
				cancel(errors.New("invalid runtime lifecycle notification"))
				return
			}
			var pending *client.RuntimeLifecycleNotification
			if err := executor.Handle(event.Event, event.SessionOwner); err != nil && ctx.Err() == nil {
				log.Print("runtime lifecycle transition pending")
				var effectErr *client.RuntimeLifecycleEffectError
				if event.Event != client.RuntimeUserLogoff || !errors.As(err, &effectErr) {
					pending = &event
				}
			}
			if event.Event == client.RuntimeUserLogoff {
				pendingLogoff = pending
			} else {
				pendingPower = pending
			}
		}
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-events:
				if !ok {
					cancel(errors.New("runtime lifecycle event source closed"))
					return
				}
				handle(event)
			case <-retry:
				retry = nil
				if pendingPower != nil {
					handle(*pendingPower)
				}
				if pendingLogoff != nil {
					handle(*pendingLogoff)
				}
			}
			if ctx.Err() != nil {
				return
			}
			if pendingPower != nil || pendingLogoff != nil {
				if retry == nil {
					timer.Reset(time.Second)
					retry = timer.C
				}
			} else {
				timer.Stop()
				retry = nil
			}
		}
	}()
	return func() error {
		// Cancellation wakes the consumer even if the agent loop is waiting on
		// its held operation lock. Release precedes joining other workers.
		cancel(nil)
		<-done
		return closeErr
	}, nil
}
