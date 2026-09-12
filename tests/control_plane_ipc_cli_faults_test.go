package tests

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"github.com/endless-net/client/internal/testclient"
	"github.com/endless-net/client/internal/testcontrol"
	ipc "github.com/endless-net/client/ipc/v2"
)

// HC-052/053: the shipping CLI must distinguish an unopened/broken event
// stream from a successfully established subscription on each native transport.
func TestControlPlaneCLIIPCFailureBoundary(t *testing.T) {
	requireControlScenario(t)
	for _, mode := range []string{"timeout-before-hello", "eof-before-hello", "malformed-event"} {
		t.Run(mode, func(t *testing.T) {
			n := testclient.New(t, testcontrol.New(t))
			listener, err := listenCLIFaultIPC(n.Socket, n.Pipe)
			if err != nil {
				t.Fatal("could not create native IPC fault fixture")
			}
			observed := make(chan bool, 1)
			server := &http.Server{ReadHeaderTimeout: 3 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				select {
				case observed <- r.Method == http.MethodGet && r.URL.Path == ipc.PathEvents:
				default:
				}
				w.Header().Set("Content-Type", "application/x-ndjson")
				w.WriteHeader(http.StatusOK)
				w.(http.Flusher).Flush()
				switch mode {
				case "timeout-before-hello":
					<-r.Context().Done()
				case "malformed-event":
					_, _ = w.Write([]byte("{\n"))
				}
			})}
			go func() { _ = server.Serve(listener) }()
			t.Cleanup(func() { _ = server.Close(); _ = listener.Close() })
			args := []string{"service", "events", "--timeout", "2s"}
			if runtime.GOOS == "windows" {
				args = append(args, "--ipc-pipe", n.Pipe)
			} else {
				args = append(args, "--ipc-socket", n.Socket)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, n.Binary, args...)
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			started := time.Now()
			err = cmd.Run()
			elapsed := time.Since(started)
			var exit *exec.ExitError
			if ctx.Err() != nil || !errors.As(err, &exit) || exit.ExitCode() != 1 {
				t.Fatal("broken IPC stream did not produce CLI exit 1 within its deadline (output withheld)")
			}
			if stdout.Len() != 0 || stderr.Len() == 0 {
				t.Fatal("broken IPC stream did not separate empty result output from its diagnostic")
			}
			if mode == "timeout-before-hello" && elapsed < 2*time.Second {
				t.Fatal("stalled IPC stream returned before its listening deadline")
			}
			select {
			case valid := <-observed:
				if !valid {
					t.Fatal("CLI did not request the public event endpoint")
				}
			default:
				t.Fatal("CLI failure occurred before reaching the native IPC fixture")
			}
		})
	}
}
