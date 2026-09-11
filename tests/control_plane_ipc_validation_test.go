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

// HC-053: invalid local requests must fail before executing a state mutation.
func TestControlPlaneIPCRequestValidation(t *testing.T) {
	_, n, id := controlScenario(t)
	endpoint := n.Socket
	if runtime.GOOS == "windows" {
		endpoint = n.Pipe
	}
	local, err := ipc.NewLocalClient(endpoint)
	if err != nil {
		t.Fatal("could not create native IPC transport")
	}
	defer local.HTTPClient.CloseIdleConnections()
	for _, tc := range []struct {
		name, method, path, body, code string
		status                         int
	}{
		{"wrong-method", http.MethodGet, ipc.PathDisconnect, "", ipc.ErrorMethodNotAllowed, http.StatusMethodNotAllowed},
		{"unknown-route", http.MethodPost, "/unsupported-operation", "{}", ipc.ErrorNotFound, http.StatusNotFound},
		{"invalid-json", http.MethodPost, ipc.PathDisconnect, "{", ipc.ErrorInvalidJSON, http.StatusBadRequest},
		{"unknown-field", http.MethodPost, ipc.PathDisconnect, `{"unexpected":true}`, ipc.ErrorInvalidJSON, http.StatusBadRequest},
		{"trailing-json", http.MethodPost, ipc.PathDisconnect, "{} {}", ipc.ErrorInvalidJSON, http.StatusBadRequest},
		{"null", http.MethodPost, ipc.PathDisconnect, "null", ipc.ErrorInvalidJSON, http.StatusBadRequest},
		{"whitespace-null", http.MethodPost, ipc.PathDisconnect, " \nnull\t", ipc.ErrorInvalidJSON, http.StatusBadRequest},
		{"array", http.MethodPost, ipc.PathDisconnect, "[]", ipc.ErrorInvalidJSON, http.StatusBadRequest},
		{"boolean", http.MethodPost, ipc.PathDisconnect, "true", ipc.ErrorInvalidJSON, http.StatusBadRequest},
		{"number", http.MethodPost, ipc.PathDisconnect, "42", ipc.ErrorInvalidJSON, http.StatusBadRequest},
		{"string", http.MethodPost, ipc.PathDisconnect, `"disconnect"`, ipc.ErrorInvalidJSON, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			request, err := http.NewRequestWithContext(ctx, tc.method, local.BaseURL+tc.path, strings.NewReader(tc.body))
			if err != nil {
				t.Fatal("could not construct invalid IPC request")
			}
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set(ipc.ProtocolHeader, ipc.Protocol)
			request.Header.Set(ipc.VersionHeader, strconv.Itoa(ipc.Version))
			request.Header.Set(ipc.MinVersionHeader, strconv.Itoa(ipc.MinSupportedVersion))
			response, err := local.HTTPClient.Do(request)
			if err != nil {
				t.Fatal("native IPC request validation failed at the transport")
			}
			defer func() { _ = response.Body.Close() }()
			body, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
			var problem ipc.ErrorResponse
			if err != nil || len(body) > 1<<20 || response.StatusCode != tc.status || json.Unmarshal(body, &problem) != nil || problem.ErrorCode != tc.code {
				t.Fatal("invalid IPC request did not return the specified typed error")
			}
			if problem.IPCProtocol != ipc.Protocol || problem.IPCVersion != ipc.Version {
				t.Fatal("invalid IPC response omitted server protocol metadata")
			}
			status, err := n.Status()
			if err != nil || status.NodeID != id || !status.CachedMapValid || status.UserDisconnected || status.DesiredState != ipc.DesiredConnected {
				t.Fatal("invalid IPC request changed identity or connection intent")
			}
		})
	}
	var disconnected ipc.DisconnectResponse
	n.Service("disconnect", &disconnected)
	n.AwaitStatus(func(v ipc.StatusResponse) bool { return v.NodeID == id && v.UserDisconnected })
	var connected ipc.ConnectResponse
	n.Service("connect", &connected)
	n.AwaitStatus(func(v ipc.StatusResponse) bool { return v.NodeID == id && !v.UserDisconnected && v.CachedMapValid })
}
