package tests

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	ipc "github.com/endless-net/client/ipc/v2"
)

// HC-053/HC-058: public IPC negotiation must reject incompatible callers before
// executing mutations. The transport is the real platform socket or named pipe.
func TestControlPlaneIPCNegotiation(t *testing.T) {
	s, n, id := controlScenario(t)
	endpoint := n.Socket
	if runtime.GOOS == "windows" {
		endpoint = n.Pipe
	}
	client, err := ipc.NewLocalClient(endpoint)
	if err != nil {
		t.Fatal("could not create public local IPC client")
	}
	defer client.HTTPClient.CloseIdleConnections()
	current := strconv.Itoa(ipc.Version)
	minimum := strconv.Itoa(ipc.MinSupportedVersion)
	future := strconv.Itoa(ipc.Version + 1) // Invalid wire probe, not a protocol upgrade.
	cases := []struct {
		name, protocol, version, min, code string
		status                             int
		duplicate                          bool
	}{
		{name: "missing", code: ipc.ErrorVersionRequired, status: http.StatusUpgradeRequired},
		{name: "wrong-protocol", protocol: "unsupported-ipc", version: current, min: minimum, code: ipc.ErrorProtocolUnsupported, status: http.StatusUpgradeRequired},
		{name: "malformed", protocol: ipc.Protocol, version: "invalid", min: minimum, code: ipc.ErrorInvalidVersionRange, status: http.StatusBadRequest},
		{name: "nonpositive", protocol: ipc.Protocol, version: current, min: "0", code: ipc.ErrorInvalidVersionRange, status: http.StatusBadRequest},
		{name: "reversed", protocol: ipc.Protocol, version: current, min: future, code: ipc.ErrorInvalidVersionRange, status: http.StatusBadRequest},
		{name: "disjoint", protocol: ipc.Protocol, version: future, min: future, code: ipc.ErrorVersionUnsupported, status: http.StatusUpgradeRequired},
		{name: "duplicate", protocol: ipc.Protocol, version: current, min: minimum, code: ipc.ErrorInvalidVersionRange, status: http.StatusBadRequest, duplicate: true},
	}
	request := func(t *testing.T, method, path, protocol, version, min string, duplicate bool) (int, []byte) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, method, client.BaseURL+path, strings.NewReader("{}"))
		if err != nil {
			t.Fatal("could not construct IPC negotiation probe")
		}
		req.Header.Set("Content-Type", "application/json")
		if protocol != "" {
			req.Header.Set(ipc.ProtocolHeader, protocol)
			req.Header.Set(ipc.VersionHeader, version)
			req.Header.Set(ipc.MinVersionHeader, min)
		}
		if duplicate {
			req.Header.Add(ipc.VersionHeader, version)
		}
		response, err := client.HTTPClient.Do(req)
		if err != nil {
			t.Fatal("native IPC negotiation request failed")
		}
		defer func() { _ = response.Body.Close() }()
		body, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
		if err != nil || len(body) > 1<<20 {
			t.Fatal("invalid IPC negotiation response size")
		}
		return response.StatusCode, body
	}
	assertConnected := func(t *testing.T) {
		t.Helper()
		status, err := n.Status()
		if err != nil {
			t.Fatal("IPC status request failed after rejected negotiation")
		}
		if status.NodeID != id || !status.CachedMapValid || status.UserDisconnected || status.DesiredState != ipc.DesiredConnected {
			t.Fatal("rejected IPC mutation changed identity or connection intent")
		}
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, op := range []struct{ method, path string }{{http.MethodGet, ipc.PathStatus}, {http.MethodPost, ipc.PathDisconnect}} {
				code, body := request(t, op.method, op.path, tc.protocol, tc.version, tc.min, tc.duplicate)
				var problem ipc.ErrorResponse
				if code != tc.status || json.Unmarshal(body, &problem) != nil || problem.ErrorCode != tc.code || problem.IPCProtocol != ipc.Protocol || problem.IPCVersion != ipc.Version || problem.IPCMinSupported != ipc.MinSupportedVersion {
					t.Fatal("IPC negotiation returned an unexpected status, error code or protocol metadata")
				}
				assertConnected(t)
			}
		})
	}
	code, body := request(t, http.MethodGet, ipc.PathStatus, ipc.Protocol, future, minimum, false)
	var overlap ipc.StatusResponse
	if code != http.StatusOK || json.Unmarshal(body, &overlap) != nil || overlap.IPCNegotiatedVersion != ipc.Version || overlap.NodeID != id {
		t.Fatal("overlapping IPC range did not negotiate the current protocol")
	}
	n.Stop()
	n.Start()
	// IPC endpoint readiness precedes restored runtime readiness, especially
	// while Windows recreates its native interface after process termination.
	n.AwaitStatus(func(v ipc.StatusResponse) bool {
		return v.NodeID == id && v.CachedMapValid && !v.UserDisconnected && v.DesiredState == ipc.DesiredConnected
	})
	var disconnected ipc.DisconnectResponse
	n.Service("disconnect", &disconnected)
	n.AwaitStatus(func(v ipc.StatusResponse) bool {
		return v.NodeID == id && v.UserDisconnected && v.DesiredState == ipc.DesiredDisconnected
	})
	var connected ipc.ConnectResponse
	n.Service("connect", &connected)
	n.AwaitStatus(func(v ipc.StatusResponse) bool {
		return v.NodeID == id && !v.UserDisconnected && v.DesiredState == ipc.DesiredConnected && v.CachedMapValid
	})
	registered := 0
	for _, event := range s.Events() {
		if event.Kind == "registered" {
			registered++
		}
	}
	if registered != 1 {
		t.Fatal("IPC negotiation or recovery caused a new enrollment")
	}
}
