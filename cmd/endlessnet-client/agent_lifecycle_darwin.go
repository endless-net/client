//go:build darwin && cgo

package main

/*
#cgo CFLAGS: -fblocks
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation -framework EndpointSecurity
#include "darwin_power.h"
#include "darwin_logoff.h"
#include <IOKit/IOMessage.h>
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os/user"
	"runtime"
	"strconv"
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

type darwinLogoffEvent struct {
	username string
}

type darwinLogoffLifecycle struct {
	power     *darwinPowerLifecycle
	raw       chan darwinLogoffEvent
	lookupUID func(string) (string, error)
	lastSeq   uint64 // Endpoint Security delivers callbacks serially.
}

var activeDarwinLogoff atomic.Pointer[darwinLogoffLifecycle]
var darwinLogoffReady atomic.Bool

func darwinLogoffAvailable() bool { return darwinLogoffReady.Load() }

//export goDarwinLogoffEvent
func goDarwinLogoffEvent(username *C.char, length C.size_t, session C.uint64_t, sequence C.uint64_t) {
	source := activeDarwinLogoff.Load()
	if source == nil || source.power.ctx.Err() != nil {
		return
	}
	seq := uint64(sequence)
	if seq == 0 || (source.lastSeq != 0 && seq != source.lastSeq+1) || username == nil || length == 0 || length > 256 {
		source.power.fail(errors.New("Endpoint Security logout stream invalid or incomplete"))
		return
	}
	source.lastSeq = seq
	event := darwinLogoffEvent{username: C.GoStringN(username, C.int(length))}
	select {
	case source.raw <- event:
	default:
		source.power.fail(errors.New("Endpoint Security logout queue overflow"))
	}
}

//export goDarwinLogoffFailure
func goDarwinLogoffFailure() {
	if source := activeDarwinLogoff.Load(); source != nil && source.power.ctx.Err() == nil {
		source.power.fail(errors.New("Endpoint Security logout message invalid"))
	}
}

func (s *darwinLogoffLifecycle) process() {
	for {
		select {
		case <-s.power.ctx.Done():
			return
		case event := <-s.raw:
			uidText, err := s.lookupUID(event.username)
			if err != nil {
				s.power.fail(errors.New("Endpoint Security logout owner lookup failed"))
				return
			}
			uid, err := strconv.ParseUint(uidText, 10, 32)
			if err != nil {
				s.power.fail(errors.New("Endpoint Security logout UID invalid"))
				return
			}
			notification := client.RuntimeLifecycleNotification{Event: client.RuntimeUserLogoff, SessionOwner: "uid:" + strconv.FormatUint(uid, 10)}
			select {
			case s.power.events <- notification:
			case <-s.power.ctx.Done():
				return
			default:
				s.power.fail(errors.New("Endpoint Security lifecycle queue overflow"))
				return
			}
		}
	}
}

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

//export goDarwinPowerAckFailure
func goDarwinPowerAckFailure() {
	if source := activeDarwinPower.Load(); source != nil && source.ctx.Err() == nil {
		source.fail(errors.New("IOKit sleep acknowledgement failed"))
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
	logoff := &darwinLogoffLifecycle{power: source, raw: make(chan darwinLogoffEvent, 64), lookupUID: func(name string) (string, error) {
		account, err := user.Lookup(name)
		if err != nil {
			return "", err
		}
		return account.Uid, nil
	}}
	if !activeDarwinLogoff.CompareAndSwap(nil, logoff) {
		cancel()
		<-finished
		close(cleanup)
		<-done
		return errors.New("darwin logout source already active")
	}
	var nativeLogoff *C.darwin_logoff_source
	logoffStatus := C.darwin_logoff_start(&nativeLogoff)
	logoffDone := make(chan struct{})
	if logoffStatus == 0 {
		darwinLogoffReady.Store(true)
		go func() { defer close(logoffDone); logoff.process() }()
	} else {
		activeDarwinLogoff.CompareAndSwap(logoff, nil)
		close(logoffDone)
		log.Printf("Endpoint Security logout unavailable (status %d)", int(logoffStatus))
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
	darwinLogoffReady.Store(false)
	activeDarwinLogoff.CompareAndSwap(logoff, nil)
	C.darwin_logoff_stop(nativeLogoff)
	<-logoffDone
	<-stopDone
	<-finished
	close(cleanup)
	<-done
	return errors.Join(runErr, source.err)
}
