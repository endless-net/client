//go:build linux || darwin

package client

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	ipc "github.com/unng-lab/endlessnet-client/ipc/v1"
)

func TestUnixServiceSocketIPCContract(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "endlessnet.sock")
	listener, err := ListenUnixServiceSocket(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{
		Handler: NewServiceIPCHandler(ServiceIPCHandlers{
			Authorize: func(r *http.Request, endpoint ServiceIPCEndpoint) error {
				return AuthorizeLocalServiceIPCForConfig(r, endpoint, Config{LocalOwnerID: "uid:" + strconv.Itoa(os.Geteuid())})
			},
			Status: func(ctx context.Context, req ipc.StatusRequest) (ipc.StatusResponse, error) {
				peer, ok := ServiceIPCPeerFromContext(ctx)
				if !ok || peer.Transport != ServiceIPCTransportUnixSocket || peer.User == "" || peer.Identity != peer.User {
					t.Fatalf("unix IPC peer = %#v ok=%v", peer, ok)
				}
				return ipc.StatusResponse{State: ipc.StateNeedsEnrollment}, nil
			},
			Events: func(ctx context.Context, req ipc.EventsRequest, writer ServiceIPCEventWriter) error {
				if err := writer.Send(ipc.Event{EventType: ipc.EventTypeHello, Sequence: 1}); err != nil {
					return err
				}
				return writer.Send(ipc.Event{EventType: ipc.EventTypeStatusChanged, Sequence: 2, Status: &ipc.StatusResponse{State: ipc.StateNeedsEnrollment}})
			},
			Connect: func(ctx context.Context, req ipc.ConnectRequest) (ipc.ConnectResponse, error) {
				return ipc.ConnectResponse{State: ipc.StateConnected}, nil
			},
		}),
		ConnContext: UnixServiceIPCConnContext,
	}
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		_ = server.Close()
		_ = listener.Close()
	})

	ipcClient, err := ipc.NewLocalClient(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var status ipc.StatusResponse
	if err := ipcClient.Request(ctx, http.MethodGet, ipc.PathStatus, nil, &status); err != nil {
		t.Fatal(err)
	}
	if status.State != ipc.StateNeedsEnrollment || status.IPCProtocol != ipc.Protocol {
		t.Fatalf("status = %#v", status)
	}

	var connect ipc.ConnectResponse
	connectErr := ipcClient.Request(ctx, http.MethodPost, ipc.PathConnect, ipc.ConnectRequest{}, &connect)
	if connectErr != nil || connect.State != ipc.StateConnected {
		t.Fatalf("owner connect = %#v err=%v", connect, connectErr)
	}

	stop := errors.New("stop")
	events := []ipc.Event{}
	err = ipcClient.Stream(ctx, http.MethodGet, ipc.PathEvents, nil, func(event ipc.Event) error {
		events = append(events, event)
		if len(events) == 2 {
			return stop
		}
		return nil
	})
	if !errors.Is(err, stop) {
		t.Fatalf("events stream error = %v; events=%#v", err, events)
	}
	if len(events) != 2 || events[0].EventType != ipc.EventTypeHello || events[1].EventType != ipc.EventTypeStatusChanged {
		t.Fatalf("events = %#v", events)
	}
}

func TestUnixServiceSocketRejectsBusySocket(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "busy.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	if second, err := ListenUnixServiceSocket(socketPath); err == nil {
		_ = second.Close()
		t.Fatal("ListenUnixServiceSocket unexpectedly replaced an active socket")
	}
}
