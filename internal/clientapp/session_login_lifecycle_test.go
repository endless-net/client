package clientapp

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSessionLoginWorkerSharesMandatoryLifetime(t *testing.T) {
	for _, fail := range []bool{false, true} {
		h, s := &hooks{}, &shutdown{}
		started := make(chan struct{})
		session := func(ctx context.Context) error {
			close(started)
			if fail {
				return errors.New("synthetic private failure")
			}
			return waitCancelled(ctx)
		}
		w, err := NewWorkers(h, s, testSignals(t), waitCancelled, waitCancelled, waitCancelled, session)
		if err != nil {
			t.Fatal(err)
		}
		if err := h.hook.OnStart(budget(t)); err != nil {
			t.Fatal(err)
		}
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("session login loop did not start")
		}
		if fail {
			select {
			case <-w.done:
			case <-time.After(time.Second):
				t.Fatal("session worker failure did not join peers")
			}
			if s.calls.Load() != 1 {
				t.Fatal("mandatory failure did not request one shutdown")
			}
			if err := h.hook.OnStop(budget(t)); !errors.Is(err, ErrWorkers) {
				t.Fatal("worker failure reported healthy", err)
			}
		} else {
			if err := h.hook.OnStop(budget(t)); err != nil {
				t.Fatal(err)
			}
			if s.calls.Load() != 0 {
				t.Fatal("normal stop requested failure shutdown")
			}
		}
	}
}
