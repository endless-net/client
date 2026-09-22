//go:build darwin && cgo

package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/endless-net/client/internal/client"
)

func TestDarwinPowerDeliveryWaitsForExecutor(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	events := make(chan client.RuntimeLifecycleNotification, 1)
	source := &darwinPowerLifecycle{ctx: ctx, cancel: cancel, events: events}
	result := make(chan error, 1)
	go func() { result <- source.deliver(client.RuntimeSuspend, time.Now().Add(time.Second)) }()
	var event client.RuntimeLifecycleNotification
	select {
	case event = <-events:
	case <-time.After(2 * time.Second):
		t.Fatal("suspend event not delivered")
	}
	if event.Event != client.RuntimeSuspend || event.Completion == nil || event.Deadline.IsZero() {
		t.Fatal("missing bounded suspend completion")
	}
	select {
	case <-result:
		t.Fatal("power callback returned before executor confirmation")
	default:
	}
	event.Completion <- nil
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestDarwinPowerLateAndFailedCompletion(t *testing.T) {
	for _, test := range []struct {
		name     string
		deadline time.Duration
		result   error
	}{
		{name: "expired", deadline: -time.Second},
		{name: "executor failed", deadline: time.Second, result: errors.New("teardown failed")},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			events := make(chan client.RuntimeLifecycleNotification, 1)
			source := &darwinPowerLifecycle{ctx: ctx, cancel: cancel, events: events}
			result := make(chan error, 1)
			go func() { result <- source.deliver(client.RuntimeSuspend, time.Now().Add(test.deadline)) }()
			if test.deadline > 0 {
				var event client.RuntimeLifecycleNotification
				select {
				case event = <-events:
				case <-time.After(2 * time.Second):
					t.Fatal("suspend event not delivered")
				}
				event.Completion <- test.result
			}
			if err := <-result; err == nil {
				t.Fatal("unconfirmed suspend accepted")
			}
		})
	}
}
