package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	ipc "github.com/endless-net/client/ipc/v1"
)

func TestServiceIPCHandlerStatusAndActions(t *testing.T) {
	handler := NewServiceIPCHandler(ServiceIPCHandlers{
		Status: func(ctx context.Context, req ipc.StatusRequest) (ipc.StatusResponse, error) {
			return ipc.StatusResponse{State: ipc.StateConnected, NodeID: "node_1"}, nil
		},
		Connect: func(ctx context.Context, req ipc.ConnectRequest) (ipc.ConnectResponse, error) {
			return ipc.ConnectResponse{State: ipc.StateConnected}, nil
		},
	})

	status := httptest.NewRecorder()
	handler.ServeHTTP(status, newServiceIPCTestRequest(http.MethodGet, "/status", nil))
	if status.Code != http.StatusOK {
		t.Fatalf("status code = %d body=%s", status.Code, status.Body.String())
	}
	var statusPayload map[string]any
	if err := json.Unmarshal(status.Body.Bytes(), &statusPayload); err != nil {
		t.Fatal(err)
	}
	if statusPayload["state"] != "Connected" || statusPayload["node_id"] != "node_1" {
		t.Fatalf("status payload = %#v", statusPayload)
	}

	connect := httptest.NewRecorder()
	handler.ServeHTTP(connect, newServiceIPCTestRequest(http.MethodPost, "/connect", bytes.NewBufferString(`{}`)))
	if connect.Code != http.StatusOK || !bytes.Contains(connect.Body.Bytes(), []byte("Connected")) {
		t.Fatalf("connect code = %d body=%s", connect.Code, connect.Body.String())
	}
}

func TestServiceIPCHandlerEventsStream(t *testing.T) {
	handler := NewServiceIPCHandler(ServiceIPCHandlers{
		Events: func(ctx context.Context, req ipc.EventsRequest, writer ServiceIPCEventWriter) error {
			if err := writer.Send(ipc.Event{EventType: ipc.EventTypeHello, Sequence: 1}); err != nil {
				return err
			}
			return writer.Send(ipc.Event{EventType: ipc.EventTypeStatusChanged, Sequence: 2, Status: &ipc.StatusResponse{State: ipc.StateConnected}})
		},
	})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, newServiceIPCTestRequest(http.MethodGet, ipc.PathEvents, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("events status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/x-ndjson" {
		t.Fatalf("events Content-Type = %q, want application/x-ndjson", got)
	}
	lines := bytes.Split(bytes.TrimSpace(rec.Body.Bytes()), []byte("\n"))
	if len(lines) != 2 {
		t.Fatalf("events lines = %d body=%s, want 2", len(lines), rec.Body.String())
	}
	var hello map[string]any
	if err := json.Unmarshal(lines[0], &hello); err != nil {
		t.Fatal(err)
	}
	if hello["event_type"] != string(ipc.EventTypeHello) {
		t.Fatalf("hello event = %#v", hello)
	}
	var status map[string]any
	if err := json.Unmarshal(lines[1], &status); err != nil {
		t.Fatal(err)
	}
	if status["event_type"] != string(ipc.EventTypeStatusChanged) {
		t.Fatalf("status event = %#v", status)
	}
}

func TestServiceIPCHandlerStableErrors(t *testing.T) {
	handler := NewServiceIPCHandler(ServiceIPCHandlers{
		Logout: func(ctx context.Context, req ipc.LogoutRequest) (ipc.LogoutResponse, error) {
			return ipc.LogoutResponse{}, ipc.NewError(http.StatusForbidden, "unauthorized", errUnauthorizedForTest{})
		},
	})

	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   string
		status int
		code   string
	}{
		{name: "method", method: http.MethodPost, path: "/status", status: http.StatusMethodNotAllowed, code: ipc.ErrorMethodNotAllowed},
		{name: "unknown", method: http.MethodGet, path: "/unknown", status: http.StatusNotFound, code: ipc.ErrorNotFound},
		{name: "missing", method: http.MethodGet, path: "/diagnostics", status: http.StatusNotImplemented, code: ipc.ErrorNotImplemented},
		{name: "json", method: http.MethodPost, path: "/connect", body: "{", status: http.StatusBadRequest, code: ipc.ErrorInvalidJSON},
		{name: "unknown_field", method: http.MethodPost, path: "/connect", body: `{"unexpected":true}`, status: http.StatusBadRequest, code: ipc.ErrorInvalidJSON},
		{name: "trailing_json", method: http.MethodPost, path: "/connect", body: `{} {}`, status: http.StatusBadRequest, code: ipc.ErrorInvalidJSON},
		{name: "too_large", method: http.MethodPost, path: "/connect", body: string(bytes.Repeat([]byte("x"), serviceIPCMaxRequestBodyBytes+1)), status: http.StatusRequestEntityTooLarge, code: ipc.ErrorRequestTooLarge},
		{name: "action", method: http.MethodPost, path: "/logout", body: `{}`, status: http.StatusForbidden, code: "unauthorized"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
			if tc.name != "method" {
				setTestServiceIPCRequestHeaders(req)
			}
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status = %d body=%s, want %d", rec.Code, rec.Body.String(), tc.status)
			}
			if got := rec.Header().Get("Content-Type"); got != "application/json" {
				t.Fatalf("Content-Type = %q, want application/json", got)
			}
			var payload struct {
				ErrorCode string `json:"error_code"`
				Error     string `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if payload.ErrorCode != tc.code || payload.Error == "" {
				t.Fatalf("error payload = %#v, want code %s", payload, tc.code)
			}
		})
	}
}

func TestServiceIPCHandlerAuthorizerSeparatesObserverAndAdministratorEndpoints(t *testing.T) {
	seen := []ServiceIPCEndpoint{}
	handler := NewServiceIPCHandler(ServiceIPCHandlers{
		Authorize: func(r *http.Request, endpoint ServiceIPCEndpoint) error {
			seen = append(seen, endpoint)
			if endpoint.RequiredPrivilege == ServiceIPCPrivilegeObserver {
				return nil
			}
			if r.Header.Get("X-EndlessNet-IPC-Role") == "admin" {
				return nil
			}
			return ipc.NewError(http.StatusForbidden, ipc.ErrorUnauthorized, errUnauthorizedForTest{})
		},
		Status: func(ctx context.Context, req ipc.StatusRequest) (ipc.StatusResponse, error) {
			return ipc.StatusResponse{State: ipc.StateConnected}, nil
		},
		Connect: func(ctx context.Context, req ipc.ConnectRequest) (ipc.ConnectResponse, error) {
			return ipc.ConnectResponse{State: ipc.StateConnected}, nil
		},
	})

	status := httptest.NewRecorder()
	handler.ServeHTTP(status, newServiceIPCTestRequest(http.MethodGet, "/status", nil))
	if status.Code != http.StatusOK {
		t.Fatalf("read-only status code = %d body=%s", status.Code, status.Body.String())
	}

	denied := httptest.NewRecorder()
	handler.ServeHTTP(denied, newServiceIPCTestRequest(http.MethodPost, "/connect", bytes.NewBufferString(`{}`)))
	if denied.Code != http.StatusForbidden {
		t.Fatalf("mutating status = %d body=%s, want 403", denied.Code, denied.Body.String())
	}
	var deniedPayload struct {
		ErrorCode string `json:"error_code"`
		Error     string `json:"error"`
	}
	if err := json.Unmarshal(denied.Body.Bytes(), &deniedPayload); err != nil {
		t.Fatal(err)
	}
	if deniedPayload.ErrorCode != ipc.ErrorUnauthorized || deniedPayload.Error == "" {
		t.Fatalf("denied payload = %#v, want unauthorized JSON error", deniedPayload)
	}

	allowedReq := newServiceIPCTestRequest(http.MethodPost, "/connect", bytes.NewBufferString(`{}`))
	allowedReq.Header.Set("X-EndlessNet-IPC-Role", "admin")
	allowed := httptest.NewRecorder()
	handler.ServeHTTP(allowed, allowedReq)
	if allowed.Code != http.StatusOK || !bytes.Contains(allowed.Body.Bytes(), []byte("Connected")) {
		t.Fatalf("authorized connect code = %d body=%s", allowed.Code, allowed.Body.String())
	}

	if len(seen) != 3 {
		t.Fatalf("authorized endpoints = %#v, want 3 checks", seen)
	}
	if seen[0].RequiredPrivilege != ServiceIPCPrivilegeObserver || seen[0].Mutation || seen[0].Operation != "status" || seen[0].Path != "/status" {
		t.Fatalf("read-only endpoint metadata = %#v", seen[0])
	}
	if seen[1].RequiredPrivilege != ServiceIPCPrivilegeOwner || !seen[1].Mutation || seen[1].Operation != "connect" || seen[1].Path != "/connect" {
		t.Fatalf("mutating endpoint metadata = %#v", seen[1])
	}
}

func TestAuthorizeLocalServiceIPCRequiresIdentifiedLocalPeer(t *testing.T) {
	handler := NewServiceIPCHandler(ServiceIPCHandlers{
		Authorize: AuthorizeLocalServiceIPC,
		Status: func(ctx context.Context, req ipc.StatusRequest) (ipc.StatusResponse, error) {
			return ipc.StatusResponse{State: ipc.StateConnected}, nil
		},
		Connect: func(ctx context.Context, req ipc.ConnectRequest) (ipc.ConnectResponse, error) {
			return ipc.ConnectResponse{State: ipc.StateConnected}, nil
		},
	})

	denied := httptest.NewRecorder()
	handler.ServeHTTP(denied, newServiceIPCTestRequest(http.MethodGet, "/status", nil))
	if denied.Code != http.StatusForbidden {
		t.Fatalf("anonymous status code = %d body=%s, want 403", denied.Code, denied.Body.String())
	}
	var deniedPayload struct {
		ErrorCode string `json:"error_code"`
		Error     string `json:"error"`
	}
	if err := json.Unmarshal(denied.Body.Bytes(), &deniedPayload); err != nil {
		t.Fatal(err)
	}
	if deniedPayload.ErrorCode != ipc.ErrorUnauthorized || deniedPayload.Error == "" {
		t.Fatalf("denied payload = %#v, want unauthorized JSON error", deniedPayload)
	}

	peerCtx := ContextWithServiceIPCPeer(context.Background(), ServiceIPCPeer{
		Transport: ServiceIPCTransportWindowsNamedPipe,
		User:      `ENDLESSNET\alice`,
		Identity:  "S-1-5-21-1000",
	})
	statusReq := newServiceIPCTestRequest(http.MethodGet, "/status", nil).WithContext(peerCtx)
	status := httptest.NewRecorder()
	handler.ServeHTTP(status, statusReq)
	if status.Code != http.StatusOK {
		t.Fatalf("identified status code = %d body=%s, want 200", status.Code, status.Body.String())
	}

	connectReq := newServiceIPCTestRequest(http.MethodPost, "/connect", bytes.NewBufferString(`{}`)).WithContext(peerCtx)
	connect := httptest.NewRecorder()
	handler.ServeHTTP(connect, connectReq)
	if connect.Code != http.StatusForbidden || !bytes.Contains(connect.Body.Bytes(), []byte(ipc.ErrorOwnerRequired)) {
		t.Fatalf("non-owner connect code = %d body=%s, want owner_required", connect.Code, connect.Body.String())
	}
}

func TestAuthorizeLocalServiceIPCReservesPrivilegedOperationsForAdminPeer(t *testing.T) {
	privilegedEndpoint := ServiceIPCEndpoint{
		Method: http.MethodPost, Path: "/admin/restart", Operation: "admin.restart",
		RequiredPrivilege: ServiceIPCPrivilegeAdministrator, Mutation: true,
	}
	userReq := httptest.NewRequest(http.MethodPost, privilegedEndpoint.Path, bytes.NewBufferString(`{}`)).WithContext(ContextWithServiceIPCPeer(context.Background(), ServiceIPCPeer{
		Transport: ServiceIPCTransportWindowsNamedPipe,
		User:      `ENDLESSNET\alice`,
		Identity:  "S-1-5-21-1000",
	}))
	err := AuthorizeLocalServiceIPC(userReq, privilegedEndpoint)
	if err == nil {
		t.Fatal("AuthorizeLocalServiceIPC allowed non-admin peer for privileged endpoint")
	}
	var ipcErr ipc.Error
	if !errors.As(err, &ipcErr) || ipcErr.Status != http.StatusForbidden || ipcErr.Code != ipc.ErrorAdministratorRequired {
		t.Fatalf("privileged endpoint error = %#v, want administrator-required IPC error", err)
	}

	adminReq := httptest.NewRequest(http.MethodPost, privilegedEndpoint.Path, bytes.NewBufferString(`{}`)).WithContext(ContextWithServiceIPCPeer(context.Background(), ServiceIPCPeer{
		Transport: ServiceIPCTransportWindowsNamedPipe,
		User:      `ENDLESSNET\admin`,
		Identity:  "S-1-5-21-500",
		Admin:     true,
	}))
	if err := AuthorizeLocalServiceIPC(adminReq, privilegedEndpoint); err != nil {
		t.Fatalf("AuthorizeLocalServiceIPC denied admin peer: %v", err)
	}

	observerEndpoint := privilegedEndpoint
	observerEndpoint.Path = "/status"
	observerEndpoint.Operation = "status"
	observerEndpoint.RequiredPrivilege = ServiceIPCPrivilegeObserver
	observerEndpoint.Mutation = false
	if err := AuthorizeLocalServiceIPC(userReq, observerEndpoint); err != nil {
		t.Fatalf("AuthorizeLocalServiceIPC denied observer endpoint: %v", err)
	}
}

func TestAuthorizeLocalServiceIPCAllowsOwnerAndAdministrator(t *testing.T) {
	endpoint := serviceIPCEndpoint(http.MethodPost, ipc.PathConnect, ipc.OperationConnect, ServiceIPCPrivilegeOwner, true)
	requestFor := func(identity string, admin bool) *http.Request {
		peer := ServiceIPCPeer{
			Transport: ServiceIPCTransportWindowsNamedPipe,
			User:      `ENDLESSNET\user`,
			Identity:  identity,
			Admin:     admin,
		}
		return httptest.NewRequest(http.MethodPost, endpoint.Path, bytes.NewBufferString(`{}`)).WithContext(ContextWithServiceIPCPeer(context.Background(), peer))
	}

	cfg := Config{LocalOwnerID: "S-1-5-21-1000"}
	if err := AuthorizeLocalServiceIPCForConfig(requestFor("S-1-5-21-1000", false), endpoint, cfg); err != nil {
		t.Fatalf("owner denied: %v", err)
	}
	if err := AuthorizeLocalServiceIPCForConfig(requestFor("S-1-5-21-2000", true), endpoint, cfg); err != nil {
		t.Fatalf("administrator denied: %v", err)
	}
	err := AuthorizeLocalServiceIPCForConfig(requestFor("S-1-5-21-2000", false), endpoint, cfg)
	var ipcErr ipc.Error
	if !errors.As(err, &ipcErr) || ipcErr.Status != http.StatusForbidden || ipcErr.Code != ipc.ErrorOwnerRequired {
		t.Fatalf("other user error = %#v, want owner_required", err)
	}

	enroll := serviceIPCEndpoint(http.MethodPost, ipc.PathEnroll, ipc.OperationEnroll, ServiceIPCPrivilegeOwner, true)
	if err := AuthorizeLocalServiceIPCForConfig(requestFor("S-1-5-21-1000", false), enroll, Config{}); err != nil {
		t.Fatalf("initial enrollment denied: %v", err)
	}
	err = AuthorizeLocalServiceIPCForConfig(requestFor("S-1-5-21-1000", false), enroll, Config{NodeID: "existing-node"})
	if !errors.As(err, &ipcErr) || ipcErr.Status != http.StatusForbidden || ipcErr.Code != ipc.ErrorAdministratorRequired {
		t.Fatalf("ownerless existing enrollment error = %#v, want administrator_required", err)
	}
}

func TestClaimLocalServiceIPCOwnerPersistsFirstPeerAndRejectsAnother(t *testing.T) {
	store, err := OpenConfigStore(filepath.Join(t.TempDir(), "client.json"))
	if err != nil {
		t.Fatal(err)
	}
	contextFor := func(identity string, admin bool) context.Context {
		return ContextWithServiceIPCPeer(context.Background(), ServiceIPCPeer{
			Transport: ServiceIPCTransportWindowsNamedPipe,
			User:      `ENDLESSNET\user`,
			Identity:  identity,
			Admin:     admin,
		})
	}
	claimed, err := ClaimLocalServiceIPCOwner(contextFor("S-1-5-21-1000", false), store)
	if err != nil {
		t.Fatal(err)
	}
	if !claimed {
		t.Fatal("first peer did not claim local ownership")
	}
	if got := store.Read().LocalOwnerID; got != "S-1-5-21-1000" {
		t.Fatalf("local owner = %q, want first peer SID", got)
	}
	_, err = ClaimLocalServiceIPCOwner(contextFor("S-1-5-21-2000", false), store)
	var ipcErr ipc.Error
	if !errors.As(err, &ipcErr) || ipcErr.Code != ipc.ErrorOwnerRequired {
		t.Fatalf("second peer claim error = %#v, want owner_required", err)
	}
	if _, err := ClaimLocalServiceIPCOwner(contextFor("S-1-5-21-500", true), store); err != nil {
		t.Fatalf("administrator override denied: %v", err)
	}
	if got := store.Read().LocalOwnerID; got != "S-1-5-21-1000" {
		t.Fatalf("administrator override changed local owner to %q", got)
	}
}

func TestClaimLocalServiceIPCOwnerRequiresAdministratorForExistingOwnerlessEnrollment(t *testing.T) {
	store, err := OpenConfigStore(filepath.Join(t.TempDir(), "client.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(func(cfg *Config) error {
		cfg.NodeID = "existing-node"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	contextFor := func(admin bool) context.Context {
		return ContextWithServiceIPCPeer(context.Background(), ServiceIPCPeer{
			Transport: ServiceIPCTransportWindowsNamedPipe,
			User:      `ENDLESSNET\user`,
			Identity:  "S-1-5-21-1000",
			Admin:     admin,
		})
	}
	_, err = ClaimLocalServiceIPCOwner(contextFor(false), store)
	var ipcErr ipc.Error
	if !errors.As(err, &ipcErr) || ipcErr.Code != ipc.ErrorAdministratorRequired {
		t.Fatalf("non-admin existing enrollment claim error = %#v, want administrator_required", err)
	}
	if _, err := ClaimLocalServiceIPCOwner(contextFor(true), store); err != nil {
		t.Fatalf("administrator existing enrollment claim denied: %v", err)
	}
	if got := store.Read().LocalOwnerID; got != "S-1-5-21-1000" {
		t.Fatalf("administrator claim local owner = %q", got)
	}
}

func TestReleaseLocalServiceIPCOwnerClaimOnlyReleasesUnenrolledReservation(t *testing.T) {
	store, err := OpenConfigStore(filepath.Join(t.TempDir(), "client.json"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := ContextWithServiceIPCPeer(context.Background(), ServiceIPCPeer{
		Transport: ServiceIPCTransportWindowsNamedPipe,
		User:      `ENDLESSNET\user`,
		Identity:  "S-1-5-21-1000",
	})
	claimed, err := ClaimLocalServiceIPCOwner(ctx, store)
	if err != nil || !claimed {
		t.Fatalf("initial claim = %t, err=%v", claimed, err)
	}
	if err := ReleaseLocalServiceIPCOwnerClaim(ctx, store); err != nil {
		t.Fatal(err)
	}
	if got := store.Read().LocalOwnerID; got != "" {
		t.Fatalf("released local owner = %q, want empty", got)
	}
	claimed, err = ClaimLocalServiceIPCOwner(ctx, store)
	if err != nil || !claimed {
		t.Fatalf("second claim = %t, err=%v", claimed, err)
	}
	if err := store.Update(func(cfg *Config) error {
		cfg.NodeID = "enrolled-node"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := ReleaseLocalServiceIPCOwnerClaim(ctx, store); err != nil {
		t.Fatal(err)
	}
	if got := store.Read().LocalOwnerID; got != "S-1-5-21-1000" {
		t.Fatalf("enrolled local owner = %q, want preserved", got)
	}
}

func TestServiceIPCPeerContextRejectsIncompletePeer(t *testing.T) {
	for _, peer := range []ServiceIPCPeer{
		{},
		{Transport: ServiceIPCTransportWindowsNamedPipe},
		{User: `ENDLESSNET\alice`, Identity: "S-1-5-21-1000"},
		{Transport: ServiceIPCTransportWindowsNamedPipe, User: `ENDLESSNET\alice`},
		{Transport: "tcp", User: `ENDLESSNET\alice`, Identity: "S-1-5-21-1000"},
	} {
		req := httptest.NewRequest(http.MethodGet, "/status", nil)
		if peer != (ServiceIPCPeer{}) {
			req = req.WithContext(ContextWithServiceIPCPeer(context.Background(), peer))
		}
		err := AuthorizeLocalServiceIPC(req, serviceIPCEndpoint(http.MethodGet, "/status", "status", ServiceIPCPrivilegeObserver, false))
		if err == nil {
			t.Fatalf("AuthorizeLocalServiceIPC(%#v) succeeded, want denial", peer)
		}
		var ipcErr ipc.Error
		if !errors.As(err, &ipcErr) || ipcErr.Status != http.StatusForbidden || ipcErr.Code != ipc.ErrorUnauthorized {
			t.Fatalf("AuthorizeLocalServiceIPC(%#v) error = %#v, want unauthorized IPC error", peer, err)
		}
	}
}

func TestServiceIPCVersionNegotiation(t *testing.T) {
	handler := NewServiceIPCHandler(ServiceIPCHandlers{
		Status: func(ctx context.Context, req ipc.StatusRequest) (ipc.StatusResponse, error) {
			version, ok := ServiceIPCNegotiatedVersionFromContext(ctx)
			if !ok || version != ipc.Version {
				t.Fatalf("negotiated version = %d ok=%t, want %d", version, ok, ipc.Version)
			}
			return ipc.StatusResponse{State: ipc.StateConnected}, nil
		},
	})
	for _, tc := range []struct {
		name, protocol, current, minimum, code string
		status                                 int
		negotiated                             bool
	}{
		{name: "missing", status: http.StatusUpgradeRequired, code: ipc.ErrorVersionRequired},
		{name: "protocol", protocol: "other", current: "1", minimum: "1", status: http.StatusUpgradeRequired, code: ipc.ErrorProtocolUnsupported},
		{name: "malformed", protocol: ipc.Protocol, current: "x", minimum: "1", status: http.StatusBadRequest, code: ipc.ErrorInvalidVersionRange},
		{name: "nonpositive", protocol: ipc.Protocol, current: "1", minimum: "0", status: http.StatusBadRequest, code: ipc.ErrorInvalidVersionRange},
		{name: "reversed", protocol: ipc.Protocol, current: "1", minimum: "2", status: http.StatusBadRequest, code: ipc.ErrorInvalidVersionRange},
		{name: "v1", protocol: ipc.Protocol, current: "1", minimum: "1", status: http.StatusOK, negotiated: true},
		{name: "future disjoint", protocol: ipc.Protocol, current: "2", minimum: "2", status: http.StatusUpgradeRequired, code: ipc.ErrorVersionUnsupported},
		{name: "overlap", protocol: ipc.Protocol, current: "3", minimum: "1", status: http.StatusOK, negotiated: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, ipc.PathStatus, nil)
			if tc.protocol != "" {
				req.Header.Set(ipc.ProtocolHeader, tc.protocol)
			}
			if tc.current != "" {
				req.Header.Set(ipc.VersionHeader, tc.current)
			}
			if tc.minimum != "" {
				req.Header.Set(ipc.MinVersionHeader, tc.minimum)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status=%d body=%s want=%d", rec.Code, rec.Body.String(), tc.status)
			}
			var payload ipc.ErrorResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if payload.IPCProtocol != ipc.Protocol || payload.IPCVersion != ipc.Version || payload.IPCMinSupported != ipc.MinSupportedVersion {
				t.Fatalf("server metadata = %#v", payload.Metadata)
			}
			if tc.negotiated {
				if payload.IPCNegotiatedVersion != ipc.Version {
					t.Fatalf("negotiated=%d want=%d", payload.IPCNegotiatedVersion, ipc.Version)
				}
			} else if payload.IPCNegotiatedVersion != 0 {
				t.Fatalf("failed negotiation exposed version %d", payload.IPCNegotiatedVersion)
			}
			if tc.code != "" && payload.ErrorCode != tc.code {
				t.Fatalf("code=%q want=%q", payload.ErrorCode, tc.code)
			}
		})
	}

	duplicate := newServiceIPCTestRequest(http.MethodGet, ipc.PathStatus, nil)
	duplicate.Header.Add(ipc.VersionHeader, "1")
	duplicateResult := httptest.NewRecorder()
	handler.ServeHTTP(duplicateResult, duplicate)
	if duplicateResult.Code != http.StatusBadRequest || !bytes.Contains(duplicateResult.Body.Bytes(), []byte(ipc.ErrorInvalidVersionRange)) {
		t.Fatalf("duplicate version headers status=%d body=%s", duplicateResult.Code, duplicateResult.Body.String())
	}
}

func TestServiceIPCMutationValidationPrecedesLock(t *testing.T) {
	steps := []string{}
	locker := &recordingServiceIPCLocker{steps: &steps}
	handler := NewServiceIPCHandler(ServiceIPCHandlers{
		Authorize: func(r *http.Request, endpoint ServiceIPCEndpoint) error {
			if version, ok := ServiceIPCNegotiatedVersionFromContext(r.Context()); !ok || version != ipc.Version {
				t.Fatalf("authorization ran without negotiated version")
			}
			steps = append(steps, "authorize")
			return nil
		},
		MutationLock: locker,
		Connect: func(ctx context.Context, req ipc.ConnectRequest) (ipc.ConnectResponse, error) {
			steps = append(steps, "action")
			return ipc.ConnectResponse{State: ipc.StateConnected}, nil
		},
	})
	body := &recordingServiceIPCReader{reader: bytes.NewBufferString(`{}`), steps: &steps}
	req := httptest.NewRequest(http.MethodPost, ipc.PathConnect, body)
	setTestServiceIPCRequestHeaders(req)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	want := []string{"authorize", "decode", "lock", "action", "unlock"}
	if fmt.Sprint(steps) != fmt.Sprint(want) {
		t.Fatalf("steps=%v want=%v", steps, want)
	}

	steps = nil
	missingVersionBody := &recordingServiceIPCReader{reader: bytes.NewBufferString(`{}`), steps: &steps}
	missing := httptest.NewRecorder()
	handler.ServeHTTP(missing, httptest.NewRequest(http.MethodPost, ipc.PathConnect, missingVersionBody))
	if missing.Code != http.StatusUpgradeRequired || len(steps) != 0 {
		t.Fatalf("missing-version status=%d steps=%v body=%s", missing.Code, steps, missing.Body.String())
	}
}

func TestServiceIPCEndpointPrivilegeMatrix(t *testing.T) {
	seen := map[string]ServiceIPCEndpoint{}
	handler := NewServiceIPCHandler(ServiceIPCHandlers{
		Authorize: func(r *http.Request, endpoint ServiceIPCEndpoint) error {
			seen[endpoint.Path] = endpoint
			return ipc.NewError(http.StatusForbidden, ipc.ErrorUnauthorized, errors.New("stop after metadata capture"))
		},
	})
	for _, tc := range []struct {
		method, path string
		privilege    ServiceIPCPrivilege
		mutation     bool
	}{
		{http.MethodGet, ipc.PathStatus, ServiceIPCPrivilegeObserver, false},
		{http.MethodGet, ipc.PathEvents, ServiceIPCPrivilegeObserver, false},
		{http.MethodPost, ipc.PathEnroll, ServiceIPCPrivilegeOwner, true},
		{http.MethodPost, ipc.PathConnect, ServiceIPCPrivilegeOwner, true},
		{http.MethodGet, ipc.PathServerIdentity, ServiceIPCPrivilegeObserver, false},
		{http.MethodPost, ipc.PathTrustServer, ServiceIPCPrivilegeAdministrator, true},
		{http.MethodPost, ipc.PathDisconnect, ServiceIPCPrivilegeOwner, true},
		{http.MethodPost, ipc.PathLogout, ServiceIPCPrivilegeOwner, true},
		{http.MethodGet, ipc.PathNetworks, ServiceIPCPrivilegeObserver, false},
		{http.MethodPost, ipc.PathSelectNetwork, ServiceIPCPrivilegeOwner, true},
		{http.MethodGet, ipc.PathDiagnostics, ServiceIPCPrivilegeOwner, false},
		{http.MethodPost, ipc.PathDiagnosticsBundle, ServiceIPCPrivilegeOwner, false},
		{http.MethodGet, ipc.PathRecentLogs, ServiceIPCPrivilegeOwner, false},
	} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, newServiceIPCTestRequest(tc.method, tc.path, bytes.NewBufferString(`{}`)))
		endpoint, ok := seen[tc.path]
		if !ok || endpoint.RequiredPrivilege != tc.privilege || endpoint.Mutation != tc.mutation {
			t.Fatalf("endpoint %s = %#v found=%t, want privilege=%s mutation=%t", tc.path, endpoint, ok, tc.privilege, tc.mutation)
		}
	}
}

func TestServiceIPCServerBoundsResponsesAndEvents(t *testing.T) {
	huge := string(bytes.Repeat([]byte("x"), serviceIPCMaxResponseBodyBytes))
	handler := NewServiceIPCHandler(ServiceIPCHandlers{
		Status: func(ctx context.Context, req ipc.StatusRequest) (ipc.StatusResponse, error) {
			return ipc.StatusResponse{State: ipc.StateConnected, ControlPlaneURLs: []string{huge}}, nil
		},
		Events: func(ctx context.Context, req ipc.EventsRequest, writer ServiceIPCEventWriter) error {
			return writer.Send(ipc.Event{EventType: ipc.EventTypeError, Sequence: 1, Error: huge})
		},
	})
	for _, path := range []string{ipc.PathStatus, ipc.PathEvents} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, newServiceIPCTestRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusInternalServerError || rec.Body.Len() > serviceIPCMaxResponseBodyBytes {
			t.Fatalf("%s status=%d bytes=%d body-prefix=%q", path, rec.Code, rec.Body.Len(), rec.Body.String()[:min(rec.Body.Len(), 128)])
		}
		var payload ipc.ErrorResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if payload.ErrorCode != ipc.ErrorResponseTooLarge {
			t.Fatalf("%s payload=%#v", path, payload)
		}
	}
}

type recordingServiceIPCReader struct {
	reader io.Reader
	steps  *[]string
	seen   bool
}

func (r *recordingServiceIPCReader) Read(p []byte) (int, error) {
	if !r.seen {
		*r.steps = append(*r.steps, "decode")
		r.seen = true
	}
	return r.reader.Read(p)
}

type recordingServiceIPCLocker struct{ steps *[]string }

func (l *recordingServiceIPCLocker) Lock()   { *l.steps = append(*l.steps, "lock") }
func (l *recordingServiceIPCLocker) Unlock() { *l.steps = append(*l.steps, "unlock") }

func newServiceIPCTestRequest(method, path string, body io.Reader) *http.Request {
	req := httptest.NewRequest(method, path, body)
	setTestServiceIPCRequestHeaders(req)
	return req
}

func setTestServiceIPCRequestHeaders(req *http.Request) {
	req.Header.Set(ipc.ProtocolHeader, ipc.Protocol)
	req.Header.Set(ipc.VersionHeader, "1")
	req.Header.Set(ipc.MinVersionHeader, "1")
}

type errUnauthorizedForTest struct{}

func (errUnauthorizedForTest) Error() string { return "not authorized" }
