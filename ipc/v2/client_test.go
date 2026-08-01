package v2

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(ProtocolHeader) != Protocol || r.Header.Get(VersionHeader) != "2" || r.Header.Get(MinVersionHeader) != "2" {
			t.Fatalf("request IPC headers = %#v", r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == PathDisconnect {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"ipc_protocol":"endlessnet-client-ipc","ipc_version":2,"ipc_min_supported_version":2,"error_code":"unauthorized","error":"denied"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ipc_protocol":"endlessnet-client-ipc","ipc_version":2,"ipc_min_supported_version":2,"ipc_negotiated_version":2,"state":"Connected","control_state":"ready"}`))
	}))
	defer server.Close()

	client := NewClient(server.Client())
	client.BaseURL = server.URL
	var status StatusResponse
	if err := client.Request(context.Background(), http.MethodGet, PathStatus, nil, &status); err != nil {
		t.Fatal(err)
	}
	if status.State != StateConnected {
		t.Fatalf("status = %#v", status)
	}

	err := client.Request(context.Background(), http.MethodPost, PathDisconnect, DisconnectRequest{}, nil)
	var ipcErr Error
	if !errors.As(err, &ipcErr) || ipcErr.Code != "unauthorized" || ipcErr.Status != http.StatusForbidden {
		t.Fatalf("disconnect error = %#v, want IPC unauthorized", err)
	}
}

func TestClientRejectsUnknownFieldsAndTrailingJSON(t *testing.T) {
	var status StatusResponse
	if err := decodeJSON([]byte(`{"state":"Connected","future_optional":true}`), &status); err == nil {
		t.Fatal("decodeJSON accepted an unknown field")
	}
	if err := decodeJSON(bytes.Join([][]byte{[]byte(`{"state":"Connected"}`), []byte(`{}`)}, []byte(" ")), &status); err == nil {
		t.Fatal("decodeJSON accepted trailing JSON")
	}
}

func TestClientDecodesEnrollmentApplyResult(t *testing.T) {
	var enrollment EnrollResponse
	if err := decodeJSON([]byte(`{"state":"Degraded","control_state":"degraded","wireguard_apply":{"ok":true,"method":"wireguard-go","interface":"endlessnet","changed":true}}`), &enrollment); err != nil {
		t.Fatal(err)
	}
	if enrollment.WireGuardApply == nil || !enrollment.WireGuardApply.OK || enrollment.WireGuardApply.Method != "wireguard-go" {
		t.Fatalf("enrollment apply = %#v", enrollment.WireGuardApply)
	}
}

func TestClientStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(`{"ipc_protocol":"endlessnet-client-ipc","ipc_version":2,"ipc_min_supported_version":2,"ipc_negotiated_version":2,"event_type":"hello","sequence":1,"generated_at":"2026-07-21T00:00:00Z"}` + "\n"))
		_, _ = w.Write([]byte(`{"ipc_protocol":"endlessnet-client-ipc","ipc_version":2,"ipc_min_supported_version":2,"ipc_negotiated_version":2,"event_type":"status_changed","sequence":2,"generated_at":"2026-07-21T00:00:01Z","status":{"ipc_protocol":"endlessnet-client-ipc","ipc_version":2,"ipc_min_supported_version":2,"ipc_negotiated_version":2,"state":"Connected","control_state":"ready"}}` + "\n"))
	}))
	defer server.Close()

	client := NewClient(server.Client())
	client.BaseURL = server.URL
	stop := errors.New("stop")
	events := []Event{}
	err := client.Stream(context.Background(), http.MethodGet, PathEvents, nil, func(event Event) error {
		events = append(events, event)
		if len(events) == 2 {
			return stop
		}
		return nil
	})
	if !errors.Is(err, stop) {
		t.Fatalf("stream error = %v, want stop sentinel", err)
	}
	if len(events) != 2 || events[0].EventType != EventTypeHello || events[1].EventType != EventTypeStatusChanged {
		t.Fatalf("events = %#v", events)
	}
}
