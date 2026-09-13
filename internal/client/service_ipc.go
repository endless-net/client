package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	ipc "github.com/endless-net/client/ipc/v2"
)

type ServiceIPCEndpoint struct {
	Method            string              `json:"method"`
	Path              string              `json:"path"`
	Operation         string              `json:"operation"`
	RequiredPrivilege ServiceIPCPrivilege `json:"required_privilege"`
	Mutation          bool                `json:"mutation"`
}

type ServiceIPCPrivilege string

const (
	ServiceIPCPrivilegeObserver      ServiceIPCPrivilege = "observer"
	ServiceIPCPrivilegeOwner         ServiceIPCPrivilege = "owner"
	ServiceIPCPrivilegeAdministrator ServiceIPCPrivilege = "administrator"
)

const (
	ServiceIPCTransportWindowsNamedPipe = "windows_named_pipe"
	ServiceIPCTransportUnixSocket       = "unix_socket"
)

type ServiceIPCPeer struct {
	Transport string `json:"transport"`
	User      string `json:"user"`
	Identity  string `json:"identity"`
	Admin     bool   `json:"admin"`
}

type serviceIPCPeerContextKey struct{}

func ContextWithServiceIPCPeer(ctx context.Context, peer ServiceIPCPeer) context.Context {
	return context.WithValue(ctx, serviceIPCPeerContextKey{}, peer)
}

func ServiceIPCPeerFromContext(ctx context.Context) (ServiceIPCPeer, bool) {
	peer, ok := ctx.Value(serviceIPCPeerContextKey{}).(ServiceIPCPeer)
	if !ok {
		return ServiceIPCPeer{}, false
	}
	if strings.TrimSpace(peer.Transport) == "" || strings.TrimSpace(peer.User) == "" || strings.TrimSpace(peer.Identity) == "" {
		return ServiceIPCPeer{}, false
	}
	return peer, true
}

func AuthorizeLocalServiceIPC(r *http.Request, endpoint ServiceIPCEndpoint) error {
	return AuthorizeLocalServiceIPCForConfig(r, endpoint, Config{})
}

func AuthorizeLocalServiceIPCForConfig(r *http.Request, endpoint ServiceIPCEndpoint, cfg Config) error {
	peer, ok := ServiceIPCPeerFromContext(r.Context())
	if !ok || !supportedServiceIPCTransport(peer.Transport) {
		return ipc.NewError(http.StatusForbidden, ipc.ErrorUnauthorized, errors.New("service IPC requires an identified local peer"))
	}
	return authorizeIdentifiedServiceIPCPeer(peer, endpoint, cfg)
}

func authorizeIdentifiedServiceIPCPeer(peer ServiceIPCPeer, endpoint ServiceIPCEndpoint, cfg Config) error {
	if endpoint.RequiredPrivilege == ServiceIPCPrivilegeObserver || peer.Admin {
		return nil
	}
	if endpoint.RequiredPrivilege == ServiceIPCPrivilegeOwner {
		ownerID := strings.TrimSpace(cfg.LocalOwnerID)
		if ownerID != "" && strings.EqualFold(ownerID, strings.TrimSpace(peer.Identity)) {
			return nil
		}
		if ownerID == "" && endpoint.Operation == ipc.OperationEnroll {
			if serviceIPCConfigHasEnrollment(cfg) {
				return ipc.NewError(http.StatusForbidden, ipc.ErrorAdministratorRequired, errors.New("existing ownerless device enrollment requires an administrator/root peer"))
			}
			return nil
		}
		return ipc.NewError(http.StatusForbidden, ipc.ErrorOwnerRequired, fmt.Errorf("%s requires the local user that enrolled this device or an administrator/root peer", endpoint.Path))
	}
	return ipc.NewError(http.StatusForbidden, ipc.ErrorAdministratorRequired, fmt.Errorf("%s requires an administrator/root local peer", endpoint.Path))
}

func serviceIPCConfigHasEnrollment(cfg Config) bool {
	return strings.TrimSpace(cfg.Token) != "" ||
		strings.TrimSpace(cfg.ActiveAccountID) != "" ||
		strings.TrimSpace(cfg.NodeID) != "" ||
		strings.TrimSpace(cfg.NetworkID) != "" ||
		strings.TrimSpace(cfg.NodeCredential) != "" ||
		strings.TrimSpace(cfg.NodeApprovalState) != "" ||
		strings.TrimSpace(cfg.EnrollmentRequestID) != "" ||
		strings.TrimSpace(cfg.EnrollmentPollToken) != "" ||
		cfg.EnrollmentRequest != nil ||
		cfg.CachedMap != nil
}

func ClaimLocalServiceIPCOwner(ctx context.Context, store *ConfigStore) (bool, error) {
	peer, ok := ServiceIPCPeerFromContext(ctx)
	if !ok || !supportedServiceIPCTransport(peer.Transport) {
		return false, ipc.NewError(http.StatusForbidden, ipc.ErrorUnauthorized, errors.New("service IPC requires an identified local peer"))
	}
	identity := strings.TrimSpace(peer.Identity)
	claimed := false
	err := store.Update(func(cfg *Config) error {
		ownerID := strings.TrimSpace(cfg.LocalOwnerID)
		if ownerID == "" {
			if serviceIPCConfigHasEnrollment(*cfg) && !peer.Admin {
				return ipc.NewError(http.StatusForbidden, ipc.ErrorAdministratorRequired, errors.New("existing ownerless device enrollment requires an administrator/root peer"))
			}
			cfg.LocalOwnerID = identity
			claimed = true
			return nil
		}
		if strings.EqualFold(ownerID, identity) || peer.Admin {
			return nil
		}
		return ipc.NewError(http.StatusForbidden, ipc.ErrorOwnerRequired, errors.New("device enrollment is owned by another local user; an administrator/root peer is required"))
	})
	return claimed, err
}

func ReleaseLocalServiceIPCOwnerClaim(ctx context.Context, store *ConfigStore) error {
	peer, ok := ServiceIPCPeerFromContext(ctx)
	if !ok || !supportedServiceIPCTransport(peer.Transport) {
		return ipc.NewError(http.StatusForbidden, ipc.ErrorUnauthorized, errors.New("service IPC requires an identified local peer"))
	}
	identity := strings.TrimSpace(peer.Identity)
	return store.Update(func(cfg *Config) error {
		if strings.EqualFold(strings.TrimSpace(cfg.LocalOwnerID), identity) && !serviceIPCConfigHasEnrollment(*cfg) {
			cfg.LocalOwnerID = ""
		}
		return nil
	})
}

func supportedServiceIPCTransport(transport string) bool {
	switch strings.ToLower(strings.TrimSpace(transport)) {
	case ServiceIPCTransportWindowsNamedPipe, ServiceIPCTransportUnixSocket:
		return true
	default:
		return false
	}
}

type ServiceIPCHandlers struct {
	Status      func(context.Context, ipc.StatusRequest) (ipc.StatusResponse, error)
	Enroll      func(context.Context, ipc.EnrollRequest) (ipc.EnrollResponse, error)
	Connect     func(context.Context, ipc.ConnectRequest) (ipc.ConnectResponse, error)
	Logout      func(context.Context, ipc.LogoutRequest) (ipc.LogoutResponse, error)
	LocalForget func(context.Context, ipc.LocalForgetRequest) (ipc.LocalForgetResponse, error)
}

func serviceIPCEndpoint(method, path, operation string, privilege ServiceIPCPrivilege, mutation bool) ServiceIPCEndpoint {
	return ServiceIPCEndpoint{
		Method: method, Path: path, Operation: operation,
		RequiredPrivilege: privilege, Mutation: mutation,
	}
}
