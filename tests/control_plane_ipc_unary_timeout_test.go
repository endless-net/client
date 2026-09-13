package tests

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os/exec"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/clientipc/v0/clientipcconnect"
	"github.com/endless-net/client/internal/testclient"
	"github.com/endless-net/client/internal/testcontrol"
	"google.golang.org/protobuf/encoding/protojson"
)

type nativeUnaryTimeoutService struct {
	clientipcconnect.UnimplementedClientServiceHandler
}

func (nativeUnaryTimeoutService) GetRuntimeInfo(context.Context, *connect.Request[ipc.GetRuntimeInfoRequest]) (*connect.Response[ipc.GetRuntimeInfoResponse], error) {
	return connect.NewResponse(&ipc.GetRuntimeInfoResponse{Runtime: &ipc.RuntimeInfo{InstanceId: "timeout-host", Protocol: rpc.Protocol, IpcVersion: rpc.Version, ContractSha256: rpc.Digest()}}), nil
}

func (nativeUnaryTimeoutService) GetStatus(context.Context, *connect.Request[ipc.GetStatusRequest]) (*connect.Response[ipc.GetStatusResponse], error) {
	return connect.NewResponse(&ipc.GetStatusResponse{Status: &ipc.Status{NodeId: "ipc-timeout-recovered", ServiceState: ipc.ServiceState_SERVICE_STATE_CONNECTED}}), nil
}

// HC-052/053: unary CLI requests must bound both header and body waits on
// native IPC, and recover at the same endpoint after the stalled response.
func TestControlPlaneCLIIPCUnaryTimeout(t *testing.T) {
	requireControlScenario(t)
	for _, mode := range []string{"before-headers", "partial-body"} {
		t.Run(mode, func(t *testing.T) {
			n := testclient.New(t, testcontrol.New(t))
			listener, err := listenCLIFaultIPC(n.Socket, n.Pipe)
			if err != nil {
				t.Fatal("could not create unary IPC fault fixture")
			}
			type requestObservation struct {
				valid bool
				at    time.Time
			}
			observed := make(chan requestObservation, 2)
			var recovered atomic.Bool
			_, healthy := clientipcconnect.NewClientServiceHandler(nativeUnaryTimeoutService{})
			protocols := new(http.Protocols)
			protocols.SetUnencryptedHTTP2(true)
			server := &http.Server{Protocols: protocols, ReadHeaderTimeout: 3 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == clientipcconnect.ClientServiceGetRuntimeInfoProcedure {
					healthy.ServeHTTP(w, r)
					return
				}
				select {
				case observed <- requestObservation{r.Method == http.MethodPost && r.URL.Path == clientipcconnect.ClientServiceGetStatusProcedure &&
					r.Header.Get(rpc.ProtocolHeader) == rpc.Protocol && r.Header.Get(rpc.VersionHeader) == "0" && r.Header.Get(rpc.DigestHeader) == rpc.Digest(), time.Now()}:
				default:
				}
				if recovered.Load() {
					healthy.ServeHTTP(w, r)
					return
				}
				if mode == "partial-body" {
					w.Header().Set("Content-Type", "application/grpc")
					// Uncompressed frame promises ten bytes but supplies only one.
					_, _ = w.Write([]byte{0, 0, 0, 0, 10, 1})
					w.(http.Flusher).Flush()
				}
				<-r.Context().Done()
			})}
			go func() { _ = server.Serve(listener) }()
			t.Cleanup(func() { _ = server.Close(); _ = listener.Close() })
			args := []string{"service", "status", "--timeout", "1s"}
			if runtime.GOOS == "windows" {
				args = append(args, "--ipc-pipe", n.Pipe)
			} else {
				args = append(args, "--ipc-socket", n.Socket)
			}
			for _, success := range []bool{false, true} {
				recovered.Store(success)
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				cmd := exec.CommandContext(ctx, n.Binary, args...)
				cmd.WaitDelay = time.Second
				var stdout, stderr bytes.Buffer
				cmd.Stdout, cmd.Stderr = &stdout, &stderr
				started := time.Now()
				err := cmd.Run()
				elapsed, watchdogErr := time.Since(started), ctx.Err()
				cancel()
				if watchdogErr != nil {
					t.Fatal("unary IPC request exceeded the outer process deadline")
				}
				if success {
					response := &ipc.GetStatusResponse{}
					if err != nil || stderr.Len() != 0 || protojson.Unmarshal(stdout.Bytes(), response) != nil ||
						response.GetStatus().GetNodeId() != "ipc-timeout-recovered" || response.GetStatus().GetServiceState() != ipc.ServiceState_SERVICE_STATE_CONNECTED {
						t.Fatal("unary IPC did not recover the published response at the same endpoint")
					}
				} else {
					var exit *exec.ExitError
					if !errors.As(err, &exit) || exit.ExitCode() != 1 || elapsed < time.Second || stdout.Len() != 0 || stderr.Len() == 0 {
						t.Fatal("stalled unary IPC did not exit at its deadline with a diagnostic and no result")
					}
				}
				select {
				case request := <-observed:
					if !request.valid {
						t.Fatal("unary CLI requested an unexpected public IPC endpoint")
					}
					// Separate process startup allowance from the request's one-
					// second timeout, with two seconds for runner scheduling.
					if !success && time.Since(request.at) > 3*time.Second {
						t.Fatal("unary IPC ignored its configured request timeout")
					}
				default:
					t.Fatal("unary CLI did not reach the native IPC fixture")
				}
			}
		})
	}
}
