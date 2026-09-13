package tests

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net/http"
	"os/exec"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/clientipc/v0/clientipcconnect"
	"github.com/endless-net/client/internal/testclient"
	"github.com/endless-net/client/internal/testcontrol"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// HC-052/053: the shipping CLI must distinguish an unopened/broken event
// stream from a successfully established subscription on each native transport.
func TestControlPlaneCLIIPCFailureBoundary(t *testing.T) {
	requireControlScenario(t)
	for _, mode := range []string{"timeout-before-snapshot", "eof-before-snapshot", "malformed-event", "status-before-snapshot"} {
		t.Run(mode, func(t *testing.T) {
			n := testclient.New(t, testcontrol.New(t))
			listener, err := listenCLIFaultIPC(n.Socket, n.Pipe)
			if err != nil {
				t.Fatal("could not create native IPC fault fixture")
			}
			observed := make(chan bool, 1)
			var recovered atomic.Bool
			_, healthy := clientipcconnect.NewClientServiceHandler(nativeUnaryTimeoutService{})
			protocols := new(http.Protocols)
			protocols.SetUnencryptedHTTP2(true)
			snapshot := func() *ipc.WatchEventsResponse {
				return &ipc.WatchEventsResponse{Sequence: 1, Metadata: &ipc.SnapshotMetadata{InstanceId: "timeout-host", Revision: 1, GeneratedAt: timestamppb.Now()},
					Event: &ipc.WatchEventsResponse_Snapshot{Snapshot: &ipc.SnapshotEvent{Runtime: &ipc.RuntimeInfo{InstanceId: "timeout-host", Protocol: rpc.Protocol, IpcVersion: rpc.Version, ContractSha256: rpc.Digest()}, Status: &ipc.Status{}}}}
			}
			server := &http.Server{Protocols: protocols, ReadHeaderTimeout: 3 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == clientipcconnect.ClientServiceGetRuntimeInfoProcedure {
					healthy.ServeHTTP(w, r)
					return
				}
				select {
				case observed <- r.Method == http.MethodPost && r.URL.Path == clientipcconnect.ClientServiceWatchEventsProcedure && r.Header.Get(rpc.DigestHeader) == rpc.Digest():
				default:
				}
				w.Header().Set("Content-Type", "application/grpc")
				w.Header().Set("Trailer", "Grpc-Status")
				w.WriteHeader(http.StatusOK)
				w.(http.Flusher).Flush()
				if recovered.Load() {
					writeNativeEventFrame(w, snapshot())
					w.(http.Flusher).Flush()
					<-r.Context().Done()
					return
				}
				switch mode {
				case "timeout-before-snapshot":
					<-r.Context().Done()
				case "malformed-event":
					_, _ = w.Write([]byte{0, 0, 0, 0, 1, 0xff})
				case "status-before-snapshot":
					first := snapshot()
					first.Event = &ipc.WatchEventsResponse_StatusChanged{StatusChanged: &ipc.Status{}}
					writeNativeEventFrame(w, first)
					second := snapshot()
					second.Sequence = 2
					writeNativeEventFrame(w, second)
				}
				w.Header().Set("Grpc-Status", "0")
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
			if mode == "timeout-before-snapshot" && elapsed < 2*time.Second {
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
			// Repair the response at the same endpoint. A fresh invocation must
			// now establish the subscription and exit normally at its deadline.
			recovered.Store(true)
			recoveryCtx, recoveryCancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer recoveryCancel()
			cmd = exec.CommandContext(recoveryCtx, n.Binary, args...)
			stdout.Reset()
			stderr.Reset()
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			started = time.Now()
			if err := cmd.Run(); err != nil || recoveryCtx.Err() != nil || time.Since(started) < 2*time.Second || stderr.Len() != 0 {
				t.Fatal("CLI did not recover a successful listening deadline after IPC repair (output withheld)")
			}
			event := &ipc.WatchEventsResponse{}
			cursor := nativeEventCursor{}
			if protojson.Unmarshal(bytes.TrimSpace(stdout.Bytes()), event) != nil || !cursor.accept(event) || cursor.instance != "timeout-host" {
				t.Fatal("recovered CLI did not emit exactly one valid native snapshot")
			}
			select {
			case valid := <-observed:
				if !valid {
					t.Fatal("recovered CLI requested an unexpected endpoint")
				}
			default:
				t.Fatal("recovered CLI did not reach the same native fixture")
			}
		})
	}
}

func writeNativeEventFrame(w http.ResponseWriter, event *ipc.WatchEventsResponse) {
	data, err := proto.Marshal(event)
	if err != nil {
		return
	}
	frame := make([]byte, 5+len(data))
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(data)))
	copy(frame[5:], data)
	_, _ = w.Write(frame)
}
