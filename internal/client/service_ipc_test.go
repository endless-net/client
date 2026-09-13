package client

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	ipc "github.com/endless-net/client/ipc/v2"
)

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
