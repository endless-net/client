package main

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/endless-net/client/internal/client"
)

func TestAgentExitSupervisorCancelsAgentBeforeJoiningSibling(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	failed, release, siblingStopped := make(chan struct{}), make(chan struct{}), make(chan struct{})
	exitDone, rpcDone := make(chan error, 1), make(chan error, 1)
	want := errors.New("exit worker failed")
	go func() {
		<-ctx.Done()
		<-release
		close(siblingStopped)
		rpcDone <- ctx.Err()
	}()
	done := make(chan error, 1)
	go func() {
		done <- superviseAgentRPCRuntime(ctx, cancel, func(err error) {
			if err != want {
				t.Error("lost original runtime failure")
			}
			cancel()
			close(failed)
		}, exitDone, rpcDone)
	}()
	exitDone <- want
	select {
	case <-failed:
	case <-time.After(3 * time.Second):
		t.Fatal("agent cancellation waited for sibling teardown")
	}
	select {
	case <-done:
		t.Fatal("supervisor returned before sibling stopped")
	default:
	}
	close(release)
	select {
	case err := <-done:
		if err != want {
			t.Fatal("supervisor lost worker failure", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("supervisor did not join")
	}
	select {
	case <-siblingStopped:
	default:
		t.Fatal("sibling still running")
	}
}

func TestAgentExitSupervisorNormalStopAndUnexpectedCompletion(t *testing.T) {
	for _, normal := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		exitDone := make(chan error, 1)
		failures := 0
		if normal {
			cancel()
			exitDone <- context.Canceled
		} else {
			exitDone <- nil
		}
		err := superviseAgentRPCRuntime(ctx, cancel, func(error) { failures++ }, exitDone, nil)
		if normal && (err != nil || failures != 0) {
			t.Fatal("ordinary stop became runtime failure", err)
		}
		if !normal && (err == nil || failures != 1) {
			t.Fatal("unexpected worker completion was ignored", err)
		}
	}
}

func TestAgentNativeExitHostRunsWithoutIPCAndJoinsLockedWorker(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("native exit adapter is Linux-only")
	}
	store, err := client.OpenConfigStore(filepath.Join(t.TempDir(), "client.json"))
	if err != nil {
		t.Fatal(err)
	}
	engine, err := client.NewWireGuardEngine(client.WireGuardEngineOptions{Interface: "endlessnet"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	lock := &sync.Mutex{}
	lock.Lock()
	defer lock.Unlock()
	ctx, fail := context.WithCancelCause(t.Context())
	defer fail(nil)
	stop, mutations, err := startAgentRPC(ctx, fail, agentIPCOptions{ConfigStore: store, OperationMu: lock, WireGuard: engine})
	if err != nil || mutations == nil {
		t.Fatal("IPC-disabled agent skipped runtime host", err)
	}
	done := make(chan error, 1)
	go func() { done <- stop() }()
	select {
	case err := <-done:
		if err != nil || ctx.Err() != nil {
			t.Fatal("normal runtime stop failed", err, context.Cause(ctx))
		}
	case <-time.After(3 * time.Second):
		t.Fatal("IPC-disabled exit worker ignored cancellation while waiting for runtime lock")
	}
	if err := stop(); err != nil {
		t.Fatal("stop was not idempotent", err)
	}
}
