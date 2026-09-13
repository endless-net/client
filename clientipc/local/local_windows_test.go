//go:build windows

package local

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/Microsoft/go-winio"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/clientipc/v0/clientipcconnect"
	"golang.org/x/sys/windows"
)

func TestWindowsPipeSDDLDeniesNetworkAndLimitsInteractiveUsers(t *testing.T) {
	const want = `D:P(D;;GA;;;NU)(A;;GA;;;SY)(A;;GA;;;BA)(A;;GRGW;;;IU)`
	if pipeSDDL != want {
		t.Fatal("native pipe security descriptor differs from the required access policy")
	}
}

func TestWindowsPipeRejectsAnonymousBeforeRPCDispatch(t *testing.T) {
	endpoint := fmt.Sprintf(`\\.\pipe\endlessnet-anonymous-test-%d`, time.Now().UnixNano())
	listener, err := Listen(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	_, handler := clientipcconnect.NewClientServiceHandler(testService{digest: rpc.Digest()}, connect.WithInterceptors(rpc.Guard{
		Authorize: func(ctx context.Context, _ ipc.Access, _ string) error {
			if _, ok := PeerFromContext(ctx); !ok {
				return rpc.Error(connect.CodeUnauthenticated, ipc.ErrorCode_ERROR_CODE_UNAUTHENTICATED)
			}
			return nil
		},
	}))
	var dispatched atomic.Int32
	server := NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if peer, ok := PeerFromContext(r.Context()); !ok || peer.Identity == "" {
			t.Error("unidentified pipe connection reached HTTP dispatch")
		}
		dispatched.Add(1)
		handler.ServeHTTP(w, r)
	}))
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	defer func() {
		_ = server.Close()
		if err := <-done; err != http.ErrServerClosed {
			t.Errorf("serve: %v", err)
		}
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	anonymous, err := NewClient(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer anonymous.Close()
	anonymous.transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
		return winio.DialPipeAccessImpLevel(ctx, endpoint,
			windows.GENERIC_READ|windows.GENERIC_WRITE, winio.PipeImpLevelAnonymous)
	}
	if _, err := anonymous.Bootstrap(ctx); err == nil {
		t.Fatal("anonymous pipe client passed native bootstrap")
	}
	if ctx.Err() != nil {
		t.Fatal("anonymous rejection depended on the request timeout")
	}
	if dispatched.Load() != 0 {
		t.Fatal("anonymous pipe request reached RPC dispatch")
	}
	owner, err := NewClient(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	if _, err := owner.Bootstrap(ctx); err != nil {
		t.Fatal("authenticated client could not bootstrap after anonymous rejection", err)
	}
	if dispatched.Load() != 1 {
		t.Fatal("authenticated bootstrap did not dispatch exactly once")
	}
}
