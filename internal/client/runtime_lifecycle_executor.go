package client

import (
	"context"
	"errors"
	"sync"
)

type runtimeLifecycleEngine interface {
	Suspend(context.Context) (WireGuardApplyResult, error)
	Resume(context.Context) error
	Down(context.Context) (WireGuardApplyResult, error)
}

// RuntimeLifecycleEffectError reports a logoff whose intent decision already
// committed. Retry effects through ordinary reconciliation (or resume's gated
// teardown), never replay that decision over a newer user intent.
type RuntimeLifecycleEffectError struct {
	Err error
}

func (e *RuntimeLifecycleEffectError) Error() string { return e.Err.Error() }
func (e *RuntimeLifecycleEffectError) Unwrap() error { return e.Err }

// RuntimeLifecycleExecutor serializes trusted OS events with each other and
// with the agent's ordinary network effects. The lock remains held throughout
// suspension, including failed teardown and failed resume-policy resolution.
// The owner must cancel the same lifetime used by its workers before Close.
type RuntimeLifecycleExecutor struct {
	mu                   sync.Mutex
	lifetime             context.Context
	mutations            *ClientRPCMutations
	engine               runtimeLifecycleEngine
	operationLock        sync.Locker
	wake                 func()
	refreshPolicy        func(context.Context) error
	observe              func(bool, error) error
	held                 bool
	closed               bool
	powerEvent           RuntimeLifecycleEvent
	powerIntentCommitted bool
}

func NewRuntimeLifecycleExecutor(lifetime context.Context, mutations *ClientRPCMutations, engine runtimeLifecycleEngine, operationLock sync.Locker, wake func(), refreshPolicy func(context.Context) error, observe func(bool, error) error) (*RuntimeLifecycleExecutor, error) {
	if lifetime == nil || mutations == nil || engine == nil || operationLock == nil || wake == nil || refreshPolicy == nil || observe == nil {
		return nil, errors.New("runtime lifecycle requires lifetime, mutations, engine, operation lock, wake, policy refresh and observation")
	}
	return &RuntimeLifecycleExecutor{lifetime: lifetime, mutations: mutations, engine: engine, operationLock: operationLock, wake: wake, refreshPolicy: refreshPolicy, observe: observe}, nil
}

func (e *RuntimeLifecycleExecutor) Handle(event RuntimeLifecycleEvent, sessionOwner string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return errors.New("runtime lifecycle executor is closed")
	}
	if err := e.lifetime.Err(); err != nil {
		return err
	}
	if event != RuntimeSuspend && event != RuntimeResume && event != RuntimeUserLogoff {
		return errors.New("invalid runtime lifecycle event")
	}
	if event != RuntimeUserLogoff && event != e.powerEvent {
		e.powerEvent = event
		e.powerIntentCommitted = false
	}
	// Persist Disconnect before waiting for an in-flight apply's effect lock;
	// its committed cancellation allows that apply to yield to teardown.
	var policyErr error
	if event == RuntimeUserLogoff || (event == RuntimeSuspend && !e.powerIntentCommitted) {
		policyErr = e.mutations.ApplyRuntimeLifecycleIntent(e.lifetime, event, sessionOwner)
		if event == RuntimeSuspend && policyErr == nil {
			e.powerIntentCommitted = true
		}
	}
	if event == RuntimeUserLogoff && policyErr != nil {
		return policyErr
	}
	if err := e.lifetime.Err(); err != nil {
		return errors.Join(policyErr, err)
	}
	if event == RuntimeUserLogoff {
		cfg := e.mutations.store.Read()
		if cfg.ConnectionIntent != nil && cfg.ConnectionIntent.DesiredState == ConnectionIntentDesiredConnected {
			return nil
		}
	}
	previouslyHeld := e.held
	if !e.held {
		e.operationLock.Lock()
		e.held = true
	}
	if err := e.lifetime.Err(); err != nil {
		if !previouslyHeld {
			e.release()
		}
		return errors.Join(policyErr, err)
	}
	switch event {
	case RuntimeSuspend:
		result, err := e.engine.Suspend(e.lifetime)
		downErr := runtimeLifecycleDownError(result, err)
		failure := errors.Join(policyErr, downErr)
		return errors.Join(failure, e.observe(downErr == nil, failure))
	case RuntimeResume:
		// Close the gate even for an unsolicited resume. Fetch current authority
		// before deciding intent; stale policy must not first commit Disconnect.
		result, err := e.engine.Suspend(e.lifetime)
		if err := runtimeLifecycleDownError(result, err); err != nil {
			return errors.Join(err, e.observe(false, err))
		}
		if err := e.refreshPolicy(e.lifetime); err != nil {
			return errors.Join(err, e.observe(true, err))
		}
		if !e.powerIntentCommitted {
			policyErr = e.mutations.ApplyRuntimeLifecycleIntent(e.lifetime, event, sessionOwner)
			if policyErr != nil {
				return errors.Join(policyErr, e.observe(true, policyErr))
			}
			e.powerIntentCommitted = true
		}
		// Resume only opens the engine gate. No new tunnel has been applied.
		if err := e.observe(true, nil); err != nil {
			return err
		}
		if err := e.engine.Resume(e.lifetime); err != nil {
			return errors.Join(err, e.observe(true, err))
		}
		e.release()
		e.wake()
		return nil
	case RuntimeUserLogoff:
		result, err := e.engine.Down(e.lifetime)
		downErr := runtimeLifecycleDownError(result, err)
		observationErr := e.observe(downErr == nil, downErr)
		if !previouslyHeld {
			e.release()
			e.wake() // Ordinary disconnected reconciliation retries a failed Down.
		}
		if err := errors.Join(downErr, observationErr); err != nil {
			return &RuntimeLifecycleEffectError{Err: err}
		}
		return nil
	}
	return nil
}

func runtimeLifecycleDownError(result WireGuardApplyResult, err error) error {
	if err != nil {
		return err
	}
	if !result.OK {
		return errors.New("runtime lifecycle teardown was not confirmed")
	}
	return nil
}

func (e *RuntimeLifecycleExecutor) release() {
	if e.held {
		e.held = false
		e.operationLock.Unlock()
	}
}

func (e *RuntimeLifecycleExecutor) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil
	}
	if e.lifetime.Err() == nil {
		return errors.New("cancel runtime workers before closing lifecycle executor")
	}
	e.closed = true
	// Engine gate is deliberately retained; shutdown must not resume networking.
	e.release()
	return nil
}
