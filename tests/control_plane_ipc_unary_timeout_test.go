package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os/exec"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/endless-net/client/internal/testclient"
	"github.com/endless-net/client/internal/testcontrol"
	ipc "github.com/endless-net/client/ipc/v2"
)

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
			server := &http.Server{ReadHeaderTimeout: 3 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				select {
				case observed <- requestObservation{r.Method == http.MethodGet && r.URL.Path == ipc.PathStatus, time.Now()}:
				default:
				}
				if recovered.Load() {
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(ipc.StatusResponse{Metadata: ipc.NewMetadata(ipc.Version), NodeID: "ipc-timeout-recovered", State: ipc.StateConnected})
					return
				}
				if mode == "partial-body" {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"state":`))
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
					var response ipc.StatusResponse
					if err != nil || stderr.Len() != 0 || json.Unmarshal(stdout.Bytes(), &response) != nil ||
						response.NodeID != "ipc-timeout-recovered" || response.State != ipc.StateConnected ||
						response.IPCProtocol != ipc.Protocol || response.IPCNegotiatedVersion != ipc.Version {
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
