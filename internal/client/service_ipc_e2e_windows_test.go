//go:build e2e && windows

package client_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Microsoft/go-winio"
	clientapi "github.com/endless-net/client-api/clientapi/v1"
	client "github.com/endless-net/client/internal/client"
	ipc "github.com/endless-net/client/ipc/v1"
)

func TestWindowsServiceNamedPipeIPCContract(t *testing.T) {
	pipe := `\\.\pipe\endlessnet-e2e-` + randomSuffix()
	listener, err := client.ListenWindowsServicePipe(pipe)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{
		Handler: client.NewServiceIPCHandler(client.ServiceIPCHandlers{
			Authorize: client.AuthorizeLocalServiceIPC,
			Status: func(ctx context.Context, req ipc.StatusRequest) (ipc.StatusResponse, error) {
				return ipc.StatusResponse{
					State:        ipc.StateNeedsEnrollment,
					DesiredState: ipc.DesiredConnected,
				}, nil
			},
			Events: func(ctx context.Context, req ipc.EventsRequest, writer client.ServiceIPCEventWriter) error {
				if err := writer.Send(ipc.Event{
					EventType: ipc.EventTypeHello,
					Sequence:  1,
				}); err != nil {
					return err
				}
				return writer.Send(ipc.Event{
					EventType: ipc.EventTypeStatusChanged,
					Sequence:  2,
					Status: &ipc.StatusResponse{
						State:        ipc.StateNeedsEnrollment,
						DesiredState: ipc.DesiredConnected,
					},
				})
			},
			Enroll: func(ctx context.Context, req ipc.EnrollRequest) (ipc.EnrollResponse, error) {
				token := strings.TrimSpace(req.EnrollToken)
				if token == "" {
					return ipc.EnrollResponse{}, ipc.NewError(http.StatusBadRequest, "enroll_token_required", errors.New("enroll_token is required"))
				}
				return ipc.EnrollResponse{
					StatusResponse: ipc.StatusResponse{State: ipc.StateConnected, NodeID: "node_e2e"},
					WireGuardApply: &ipc.WireGuardApplyResult{
						OK: true, Method: "wireguard-go", Interface: "endlessnet", Changed: true,
					},
				}, nil
			},
			Connect: func(ctx context.Context, req ipc.ConnectRequest) (ipc.ConnectResponse, error) {
				return ipc.ConnectResponse{State: ipc.StateConnected}, nil
			},
			Disconnect: func(ctx context.Context, req ipc.DisconnectRequest) (ipc.DisconnectResponse, error) {
				return ipc.DisconnectResponse{State: ipc.StateDisconnected, DesiredState: ipc.DesiredDisconnected, UserDisconnected: true}, nil
			},
			Logout: func(ctx context.Context, req ipc.LogoutRequest) (ipc.LogoutResponse, error) {
				return ipc.LogoutResponse{State: ipc.StateNeedsEnrollment}, nil
			},
			Networks: func(ctx context.Context, req ipc.NetworksRequest) (ipc.NetworksResponse, error) {
				return ipc.NetworksResponse{Networks: []clientapi.Network{{ID: "net_e2e", Name: "E2E"}}}, nil
			},
			SelectNetwork: func(ctx context.Context, req ipc.SelectNetworkRequest) (ipc.SelectNetworkResponse, error) {
				networkID := strings.TrimSpace(req.NetworkID)
				if networkID == "" {
					return ipc.SelectNetworkResponse{}, ipc.NewError(http.StatusBadRequest, "network_id_required", errors.New("network_id is required"))
				}
				return ipc.SelectNetworkResponse{SelectedNetworkID: networkID, SelectedNetwork: clientapi.Network{ID: networkID, Name: "E2E"}}, nil
			},
			Diagnostics: func(ctx context.Context, req ipc.DiagnosticsRequest) (ipc.DiagnosticsResponse, error) {
				return ipc.DiagnosticsResponse{Diagnostics: ipc.Diagnostics{
					GeneratedAt: "2026-07-18T00:00:00Z",
					Config: ipc.DiagnosticsConfig{
						NodeCredentialPresent: true,
					},
					LastErrors:     []string{},
					RecentLogs:     []ipc.LogEntry{},
					Interfaces:     []ipc.NetworkInterfaceStatus{},
					RouteConflicts: []ipc.OverlayCIDRConflict{},
				}}, nil
			},
			DiagnosticsBundle: func(ctx context.Context, req ipc.DiagnosticsBundleRequest) (ipc.DiagnosticsBundleResponse, error) {
				return ipc.DiagnosticsBundleResponse{Path: `C:\ProgramData\EndlessNet\Diagnostics\diagnostics-e2e.json`, CreatedAt: "2026-07-18T00:00:00Z", ExpiresAt: "2026-07-25T00:00:00Z", SizeBytes: 128}, nil
			},
			RecentLogs: func(ctx context.Context, req ipc.RecentLogsRequest) (ipc.RecentLogsResponse, error) {
				return ipc.RecentLogsResponse{Logs: []ipc.LogEntry{{Message: "ready"}}}, nil
			},
		}),
		ConnContext: client.WindowsServiceIPCConnContext,
	}
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		_ = server.Close()
		_ = listener.Close()
	})

	ipcClient, err := ipc.NewLocalClient(pipe)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = anonymousWindowsServiceIPCClient(pipe).Request(ctx, http.MethodGet, "/status", nil, nil)
	var unauthorized ipc.Error
	if !errors.As(err, &unauthorized) || unauthorized.Status != http.StatusForbidden || unauthorized.Code != ipc.ErrorUnauthorized {
		t.Fatalf("anonymous IPC status error = %#v, want unauthorized IPC error", err)
	}

	var status ipc.StatusResponse
	if err := ipcClient.Request(ctx, http.MethodGet, ipc.PathStatus, nil, &status); err != nil {
		t.Fatal(err)
	}
	if status.State != ipc.StateNeedsEnrollment {
		t.Fatalf("status = %#v", status)
	}
	assertIPCContractMetadata(t, status.Metadata)
	if status.DesiredState != ipc.DesiredConnected {
		t.Fatalf("status desired_state = %#v, want connected", status)
	}

	var enroll ipc.EnrollResponse
	if err := ipcClient.Request(ctx, http.MethodPost, ipc.PathEnroll, ipc.EnrollRequest{EnrollToken: "enr_e2e"}, &enroll); err != nil {
		t.Fatal(err)
	}
	if enroll.State != ipc.StateConnected || enroll.NodeID != "node_e2e" {
		t.Fatalf("enroll = %#v", enroll)
	}

	var connect ipc.ConnectResponse
	if err := ipcClient.Request(ctx, http.MethodPost, ipc.PathConnect, ipc.ConnectRequest{}, &connect); err != nil {
		t.Fatal(err)
	}
	if connect.State != ipc.StateConnected {
		t.Fatalf("connect = %#v", connect)
	}

	var disconnect ipc.DisconnectResponse
	if err := ipcClient.Request(ctx, http.MethodPost, ipc.PathDisconnect, ipc.DisconnectRequest{}, &disconnect); err != nil {
		t.Fatal(err)
	}
	if disconnect.State != ipc.StateDisconnected {
		t.Fatalf("disconnect = %#v", disconnect)
	}

	var networks ipc.NetworksResponse
	if err := ipcClient.Request(ctx, http.MethodGet, ipc.PathNetworks, nil, &networks); err != nil {
		t.Fatal(err)
	}
	if len(networks.Networks) != 1 || networks.Networks[0].ID != "net_e2e" {
		t.Fatalf("networks = %#v", networks)
	}

	var selected ipc.SelectNetworkResponse
	if err := ipcClient.Request(ctx, http.MethodPost, ipc.PathSelectNetwork, ipc.SelectNetworkRequest{NetworkID: "net_e2e"}, &selected); err != nil {
		t.Fatal(err)
	}
	if selected.SelectedNetworkID != "net_e2e" {
		t.Fatalf("selected = %#v", selected)
	}

	var diagnostics ipc.DiagnosticsResponse
	if err := ipcClient.Request(ctx, http.MethodGet, ipc.PathDiagnostics, nil, &diagnostics); err != nil {
		t.Fatal(err)
	}
	if !diagnostics.Diagnostics.Config.NodeCredentialPresent {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	var diagnosticsBundle ipc.DiagnosticsBundleResponse
	if err := ipcClient.Request(ctx, http.MethodPost, ipc.PathDiagnosticsBundle, ipc.DiagnosticsBundleRequest{}, &diagnosticsBundle); err != nil {
		t.Fatal(err)
	}
	if diagnosticsBundle.Path == "" || diagnosticsBundle.SizeBytes <= 0 {
		t.Fatalf("diagnostics bundle = %#v", diagnosticsBundle)
	}

	var logs ipc.RecentLogsResponse
	if err := ipcClient.Request(ctx, http.MethodGet, ipc.PathRecentLogs, nil, &logs); err != nil {
		t.Fatal(err)
	}
	if len(logs.Logs) != 1 || logs.Logs[0].Message != "ready" {
		t.Fatalf("logs = %#v", logs)
	}

	assertWindowsServiceIPCEventStream(t, ctx, ipcClient)

	var logout ipc.LogoutResponse
	if err := ipcClient.Request(ctx, http.MethodPost, ipc.PathLogout, ipc.LogoutRequest{}, &logout); err != nil {
		t.Fatal(err)
	}
	if logout.State != ipc.StateNeedsEnrollment {
		t.Fatalf("logout = %#v", logout)
	}

	err = ipcClient.Request(ctx, http.MethodPost, ipc.PathEnroll, ipc.EnrollRequest{}, nil)
	var ipcErr ipc.Error
	if !errors.As(err, &ipcErr) || ipcErr.Code != "enroll_token_required" || ipcErr.Status != http.StatusBadRequest {
		t.Fatalf("missing-token error = %#v, want stable enroll_token_required IPC error", err)
	}

	assertRawIPCNegotiationError(t, ctx, ipcClient, "", "", "", http.StatusUpgradeRequired, ipc.ErrorVersionRequired)
	currentVersion := fmt.Sprintf("%d", ipc.Version)
	minimumVersion := fmt.Sprintf("%d", ipc.MinSupportedVersion)
	unsupportedVersion := fmt.Sprintf("%d", ipc.Version+1)
	assertRawIPCNegotiationError(t, ctx, ipcClient, "unsupported-ipc", currentVersion, minimumVersion, http.StatusUpgradeRequired, ipc.ErrorProtocolUnsupported)
	assertRawIPCNegotiationError(t, ctx, ipcClient, ipc.Protocol, unsupportedVersion, unsupportedVersion, http.StatusUpgradeRequired, ipc.ErrorVersionUnsupported)
	assertRawIPCNegotiationError(t, ctx, ipcClient, ipc.Protocol, currentVersion, unsupportedVersion, http.StatusBadRequest, ipc.ErrorInvalidVersionRange)
	assertRawIPCError(t, ctx, ipcClient, http.MethodGet, "/unknown", "", http.StatusNotFound, ipc.ErrorNotFound)
	assertRawIPCError(t, ctx, ipcClient, http.MethodPost, "/status", "", http.StatusMethodNotAllowed, ipc.ErrorMethodNotAllowed)
	assertRawIPCError(t, ctx, ipcClient, http.MethodPost, "/events", "", http.StatusMethodNotAllowed, ipc.ErrorMethodNotAllowed)
	assertRawIPCError(t, ctx, ipcClient, http.MethodPost, "/connect", "{", http.StatusBadRequest, ipc.ErrorInvalidJSON)
	assertRawIPCError(t, ctx, ipcClient, http.MethodPost, "/connect", strings.Repeat("x", 64<<10+1), http.StatusRequestEntityTooLarge, ipc.ErrorRequestTooLarge)
}

func assertWindowsServiceIPCEventStream(t *testing.T, ctx context.Context, ipcClient *ipc.Client) {
	t.Helper()
	stop := errors.New("stop")
	events := []ipc.Event{}
	err := ipcClient.Stream(ctx, http.MethodGet, ipc.PathEvents, nil, func(event ipc.Event) error {
		events = append(events, event)
		if len(events) == 2 {
			return stop
		}
		return nil
	})
	if !errors.Is(err, stop) {
		t.Fatalf("events stream error = %v, want stop sentinel; events=%#v", err, events)
	}
	if len(events) != 2 {
		t.Fatalf("events stream = %#v, want hello and status", events)
	}
	assertIPCContractMetadata(t, events[0].Metadata)
	if events[0].EventType != ipc.EventTypeHello {
		t.Fatalf("hello event = %#v", events[0])
	}
	assertIPCContractMetadata(t, events[1].Metadata)
	if events[1].EventType != ipc.EventTypeStatusChanged {
		t.Fatalf("status event = %#v", events[1])
	}
	status := events[1].Status
	if status == nil || status.State != ipc.StateNeedsEnrollment || status.DesiredState != ipc.DesiredConnected {
		t.Fatalf("status event payload = %#v", events[1])
	}
}

func assertIPCContractMetadata(t *testing.T, metadata ipc.Metadata) {
	t.Helper()
	if metadata.IPCProtocol != ipc.Protocol {
		t.Fatalf("ipc_protocol = %q, want %q; metadata=%#v", metadata.IPCProtocol, ipc.Protocol, metadata)
	}
	if metadata.IPCVersion != ipc.Version {
		t.Fatalf("ipc_version = %d, want %d; metadata=%#v", metadata.IPCVersion, ipc.Version, metadata)
	}
	if metadata.IPCMinSupported != ipc.MinSupportedVersion {
		t.Fatalf("ipc_min_supported_version = %d, want %d; metadata=%#v", metadata.IPCMinSupported, ipc.MinSupportedVersion, metadata)
	}
	if metadata.IPCNegotiatedVersion != ipc.Version {
		t.Fatalf("ipc_negotiated_version = %d, want %d; metadata=%#v", metadata.IPCNegotiatedVersion, ipc.Version, metadata)
	}
}

func anonymousWindowsServiceIPCClient(pipe string) *ipc.Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return winio.DialPipeContext(ctx, pipe)
		},
	}
	return ipc.NewClient(&http.Client{Transport: transport})
}

func assertRawIPCError(t *testing.T, ctx context.Context, ipcClient *ipc.Client, method, path, body string, wantStatus int, wantCode string) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, method, ipcClient.BaseURL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set(ipc.ProtocolHeader, ipc.Protocol)
	req.Header.Set(ipc.VersionHeader, fmt.Sprintf("%d", ipc.Version))
	req.Header.Set(ipc.MinVersionHeader, fmt.Sprintf("%d", ipc.MinSupportedVersion))
	assertRawIPCErrorResponse(t, ipcClient, req, wantStatus, wantCode)
}

func assertRawIPCNegotiationError(t *testing.T, ctx context.Context, ipcClient *ipc.Client, protocol, currentVersion, minimumVersion string, wantStatus int, wantCode string) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ipcClient.BaseURL+ipc.PathStatus, nil)
	if err != nil {
		t.Fatal(err)
	}
	if protocol != "" {
		req.Header.Set(ipc.ProtocolHeader, protocol)
	}
	if currentVersion != "" {
		req.Header.Set(ipc.VersionHeader, currentVersion)
	}
	if minimumVersion != "" {
		req.Header.Set(ipc.MinVersionHeader, minimumVersion)
	}
	assertRawIPCErrorResponse(t, ipcClient, req, wantStatus, wantCode)
}

func assertRawIPCErrorResponse(t *testing.T, ipcClient *ipc.Client, req *http.Request, wantStatus int, wantCode string) {
	t.Helper()
	resp, err := ipcClient.HTTPClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != wantStatus {
		t.Fatalf("%s %s status = %d body=%s, want %d", req.Method, req.URL.Path, resp.StatusCode, string(raw), wantStatus)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("%s %s Content-Type = %q, want application/json", req.Method, req.URL.Path, got)
	}
	var payload struct {
		ErrorCode string `json:"error_code"`
		Error     string `json:"error"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("%s %s returned non-JSON error body %q: %v", req.Method, req.URL.Path, string(raw), err)
	}
	if payload.ErrorCode != wantCode || strings.TrimSpace(payload.Error) == "" {
		t.Fatalf("%s %s error payload = %#v, want code %s", req.Method, req.URL.Path, payload, wantCode)
	}
}

func randomSuffix() string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(raw[:])
}
