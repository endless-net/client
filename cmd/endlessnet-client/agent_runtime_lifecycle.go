package main

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/endless-net/client/internal/client"
)

type agentRuntimeLifecycleEngine interface {
	Suspend(context.Context) (client.WireGuardApplyResult, error)
	Resume(context.Context) error
	Down(context.Context) (client.WireGuardApplyResult, error)
}

func startAgentRuntimeLifecycle(ctx context.Context, cancel context.CancelCauseFunc, mutations *client.ClientRPCMutations, opts agentIPCOptions, events <-chan client.RuntimeLifecycleEvent, engine agentRuntimeLifecycleEngine) (func() error, error) {
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
	executor, err := client.NewRuntimeLifecycleExecutor(ctx, mutations, engine, opts.OperationMu, func() { requestAgentSync(opts) })
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
		var pending client.RuntimeLifecycleEvent
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-events:
				if !ok {
					cancel(errors.New("runtime lifecycle event source closed"))
					return
				}
				pending = event
			case <-retry:
			}
			if ctx.Err() != nil {
				return
			}
			if err := executor.Handle(pending, ""); err != nil {
				if ctx.Err() == nil {
					log.Print("runtime lifecycle transition pending; network gate retained")
				}
				timer.Reset(time.Second)
				retry = timer.C
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
