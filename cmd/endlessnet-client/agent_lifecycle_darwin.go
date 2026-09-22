//go:build darwin && cgo

package main

/*
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation
#include "darwin_power.h"
#include <IOKit/IOMessage.h>
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/endless-net/client/internal/client"
)

type darwinPowerLifecycle struct {
	ctx    context.Context
	cancel context.CancelFunc
	events chan<- client.RuntimeLifecycleNotification
	once   sync.Once
	err    error
}

var activeDarwinPower atomic.Pointer[darwinPowerLifecycle]

func (s *darwinPowerLifecycle) fail(err error) {
	s.once.Do(func() {
		s.err = err
		s.cancel()
	})
}

func (s *darwinPowerLifecycle) deliver(event client.RuntimeLifecycleEvent, deadline time.Time) error {
	transition, cancel := context.WithDeadline(s.ctx, deadline)
	defer cancel()
	completion := make(chan error, 1)
	notification := client.RuntimeLifecycleNotification{Event: event, Completion: completion, Deadline: deadline}
	select {
	case <-transition.Done():
		return transition.Err()
	case s.events <- notification:
	}
	select {
	case <-transition.Done():
		return transition.Err()
	case err := <-completion:
		if !time.Now().Before(deadline) {
			return context.DeadlineExceeded
		}
		return err
	}
}

//export goDarwinPowerEvent
func goDarwinPowerEvent(message C.uint32_t) {
	source := activeDarwinPower.Load()
	if source == nil || source.ctx.Err() != nil {
		return
	}
	var event client.RuntimeLifecycleEvent
	switch message {
	case C.kIOMessageSystemWillSleep:
		event = client.RuntimeSuspend
	case C.kIOMessageSystemHasPoweredOn:
		event = client.RuntimeResume
	default:
		return
	}
	// Apple waits at most 30 seconds for the sleep acknowledgement. Keep five
	// seconds for IOKit delivery and the callback's return to the kernel.
	if err := source.deliver(event, time.Now().Add(25*time.Second)); err != nil && source.ctx.Err() == nil {
		source.fail(fmt.Errorf("darwin power transition: %w", err))
	}
}

func runAgentPlatformLifecycle(parent context.Context, run func(context.Context, <-chan client.RuntimeLifecycleNotification) error) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	events := make(chan client.RuntimeLifecycleNotification, 64)
	source := &darwinPowerLifecycle{ctx: ctx, cancel: cancel, events: events}
	if !activeDarwinPower.CompareAndSwap(nil, source) {
		return errors.New("darwin power source already active")
	}
	defer activeDarwinPower.CompareAndSwap(source, nil)

	ready := make(chan *C.darwin_power_source, 1)
	finished := make(chan struct{})
	cleanup := make(chan struct{})
	done := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		native := C.darwin_power_start()
		ready <- native
		if native == nil {
			close(finished)
			close(done)
			return
		}
		C.darwin_power_run(native)
		close(finished)
		<-cleanup
		C.darwin_power_destroy(native)
		close(done)
	}()
	native := <-ready
	if native == nil {
		return errors.New("register IOKit system power source")
	}
	stopDone := make(chan struct{})
	go func() {
		defer close(stopDone)
		select {
		case <-ctx.Done():
			C.darwin_power_stop(native)
		case <-finished:
			if ctx.Err() == nil {
				source.fail(errors.New("IOKit power run loop stopped"))
			}
		}
	}()
	runErr := run(ctx, events)
	cancel()
	<-stopDone
	<-finished
	close(cleanup)
	<-done
	return errors.Join(runErr, source.err)
}
