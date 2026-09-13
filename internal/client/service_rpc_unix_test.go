//go:build linux || darwin

package client

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestUnixServiceSocketRPCContract(t *testing.T) {
	// Keep the endpoint below Darwin's sockaddr_un length limit.
	dir, err := os.MkdirTemp("/tmp", "en-unix-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Remove(dir); err != nil {
			t.Error(err)
		}
	})
	socketPath := filepath.Join(dir, "rpc.sock")
	listener, err := local.Listen(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	info, err := os.Stat(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0660 {
		t.Fatalf("unprotected native socket mode: %v", info.Mode())
	}
	// A second runtime must not unlink or replace the live endpoint.
	if second, err := local.Listen(socketPath); err == nil {
		_ = second.Close()
		t.Fatal("native listener replaced a busy socket")
	}
	m := newRPCStoreTest(t)
	service := NewClientRPCService(m, &ipc.BuildIdentity{Version: "test"})
	server := local.NewServer(service.Handler())
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	defer func() {
		_ = server.Close()
		if err := <-done; err != http.ErrServerClosed {
			t.Errorf("serve: %v", err)
		}
	}()
	consumer, err := local.NewClient(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if _, err := consumer.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.store.Update(func(cfg *Config) error { cfg.LocalOwnerID = "uid:" + strconv.Itoa(os.Geteuid()); return nil }); err != nil {
		t.Fatal(err)
	}
	events, err := consumer.WatchEvents(ctx, connect.NewRequest(&ipc.WatchEventsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = events.Close() }()
	if !events.Receive() || events.Msg().Sequence != 1 || events.Msg().GetSnapshot() == nil {
		t.Fatalf("missing initial native snapshot: %v", events.Err())
	}
	request := rpcCreateRequest(t, m)
	request.ControlOrigin = "https://control.example.test"
	accepted, err := consumer.CreateProfile(ctx, connect.NewRequest(request))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := m.store.Read().LocalOwnerID, "uid:"+strconv.Itoa(os.Geteuid()); got != want {
		t.Fatalf("persisted owner = %q, want authenticated OS identity %q", got, want)
	}
	if !events.Receive() || events.Msg().Sequence != 2 || events.Msg().GetStatusChanged() == nil || events.Msg().GetSnapshot() != nil || events.Msg().Metadata.Revision != accepted.Msg.Operation.Metadata.Revision {
		t.Fatalf("missing committed status change: %v", events.Err())
	}
	if !events.Receive() || events.Msg().Sequence != 3 || !proto.Equal(events.Msg().GetOperationChanged(), accepted.Msg.Operation) {
		t.Fatalf("missing ordered native operation event: %v", events.Err())
	}
	// Successful owner-only access also proves that the failed second Listen
	// left the original socket reachable. The event stream remains open.
	profiles, err := consumer.ListProfiles(ctx, connect.NewRequest(&ipc.ListProfilesRequest{}))
	if err != nil {
		t.Fatalf("owner catalog with an open stream: %v", err)
	}
	if len(profiles.Msg.Profiles) != 1 || profiles.Msg.Profiles[0].Id != accepted.Msg.Operation.ProfileId {
		t.Fatal("owner catalog did not expose the committed profile")
	}
	if _, err := consumer.GetStatus(ctx, connect.NewRequest(&ipc.GetStatusRequest{})); err != nil {
		t.Fatalf("owner status with an open stream: %v", err)
	}
	_ = events.Close()
	consumer.Close()
	_ = server.Close()
	if _, err := os.Lstat(socketPath); !os.IsNotExist(err) {
		t.Fatalf("closed listener left a socket behind: %v", err)
	}
}
