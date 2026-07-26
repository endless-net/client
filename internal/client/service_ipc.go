package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"

	ipc "github.com/unng-lab/endlessnet-client/ipc/v1"
)

const (
	serviceIPCMaxRequestBodyBytes  = 64 << 10
	serviceIPCMaxResponseBodyBytes = 1 << 20
)

type ServiceIPCEventWriter interface {
	Send(ipc.Event) error
}

type ServiceIPCStreamAction[Request any] func(context.Context, Request, ServiceIPCEventWriter) error

type ServiceIPCAuthorizer func(*http.Request, ServiceIPCEndpoint) error

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
	Authorize         ServiceIPCAuthorizer
	Status            func(context.Context, ipc.StatusRequest) (ipc.StatusResponse, error)
	Events            func(context.Context, ipc.EventsRequest, ServiceIPCEventWriter) error
	Enroll            func(context.Context, ipc.EnrollRequest) (ipc.EnrollResponse, error)
	Connect           func(context.Context, ipc.ConnectRequest) (ipc.ConnectResponse, error)
	ServerIdentity    func(context.Context, ipc.ServerIdentityRequest) (ipc.ServerIdentityResponse, error)
	TrustServer       func(context.Context, ipc.TrustServerRequest) (ipc.TrustServerResponse, error)
	Disconnect        func(context.Context, ipc.DisconnectRequest) (ipc.DisconnectResponse, error)
	Logout            func(context.Context, ipc.LogoutRequest) (ipc.LogoutResponse, error)
	Networks          func(context.Context, ipc.NetworksRequest) (ipc.NetworksResponse, error)
	SelectNetwork     func(context.Context, ipc.SelectNetworkRequest) (ipc.SelectNetworkResponse, error)
	Diagnostics       func(context.Context, ipc.DiagnosticsRequest) (ipc.DiagnosticsResponse, error)
	DiagnosticsBundle func(context.Context, ipc.DiagnosticsBundleRequest) (ipc.DiagnosticsBundleResponse, error)
	RecentLogs        func(context.Context, ipc.RecentLogsRequest) (ipc.RecentLogsResponse, error)
	MutationLock      sync.Locker
}

func NewServiceIPCHandler(handlers ServiceIPCHandlers) http.Handler {
	mux := http.NewServeMux()
	registerServiceIPCEndpoint(mux, serviceIPCEndpoint(http.MethodGet, ipc.PathStatus, ipc.OperationStatus, ServiceIPCPrivilegeObserver, false), handlers.Status, handlers.Authorize, handlers.MutationLock)
	registerServiceIPCStreamEndpoint(mux, serviceIPCEndpoint(http.MethodGet, ipc.PathEvents, ipc.OperationEvents, ServiceIPCPrivilegeObserver, false), handlers.Events, handlers.Authorize)
	registerServiceIPCEndpoint(mux, serviceIPCEndpoint(http.MethodPost, ipc.PathEnroll, ipc.OperationEnroll, ServiceIPCPrivilegeOwner, true), handlers.Enroll, handlers.Authorize, handlers.MutationLock)
	registerServiceIPCEndpoint(mux, serviceIPCEndpoint(http.MethodPost, ipc.PathConnect, ipc.OperationConnect, ServiceIPCPrivilegeOwner, true), handlers.Connect, handlers.Authorize, handlers.MutationLock)
	registerServiceIPCEndpoint(mux, serviceIPCEndpoint(http.MethodGet, ipc.PathServerIdentity, ipc.OperationServerIdentity, ServiceIPCPrivilegeObserver, false), handlers.ServerIdentity, handlers.Authorize, handlers.MutationLock)
	registerServiceIPCEndpoint(mux, serviceIPCEndpoint(http.MethodPost, ipc.PathTrustServer, ipc.OperationTrustServer, ServiceIPCPrivilegeAdministrator, true), handlers.TrustServer, handlers.Authorize, handlers.MutationLock)
	registerServiceIPCEndpoint(mux, serviceIPCEndpoint(http.MethodPost, ipc.PathDisconnect, ipc.OperationDisconnect, ServiceIPCPrivilegeOwner, true), handlers.Disconnect, handlers.Authorize, handlers.MutationLock)
	registerServiceIPCEndpoint(mux, serviceIPCEndpoint(http.MethodPost, ipc.PathLogout, ipc.OperationLogout, ServiceIPCPrivilegeOwner, true), handlers.Logout, handlers.Authorize, handlers.MutationLock)
	registerServiceIPCEndpoint(mux, serviceIPCEndpoint(http.MethodGet, ipc.PathNetworks, ipc.OperationNetworks, ServiceIPCPrivilegeObserver, false), handlers.Networks, handlers.Authorize, handlers.MutationLock)
	registerServiceIPCEndpoint(mux, serviceIPCEndpoint(http.MethodPost, ipc.PathSelectNetwork, ipc.OperationSelectNetwork, ServiceIPCPrivilegeOwner, true), handlers.SelectNetwork, handlers.Authorize, handlers.MutationLock)
	registerServiceIPCEndpoint(mux, serviceIPCEndpoint(http.MethodGet, ipc.PathDiagnostics, ipc.OperationDiagnostics, ServiceIPCPrivilegeOwner, false), handlers.Diagnostics, handlers.Authorize, handlers.MutationLock)
	registerServiceIPCEndpoint(mux, serviceIPCEndpoint(http.MethodPost, ipc.PathDiagnosticsBundle, ipc.OperationDiagnosticsBundle, ServiceIPCPrivilegeOwner, false), handlers.DiagnosticsBundle, handlers.Authorize, handlers.MutationLock)
	registerServiceIPCEndpoint(mux, serviceIPCEndpoint(http.MethodGet, ipc.PathRecentLogs, ipc.OperationRecentLogs, ServiceIPCPrivilegeOwner, false), handlers.RecentLogs, handlers.Authorize, handlers.MutationLock)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		negotiatedRequest, ipcErr := negotiateServiceIPCRequest(r)
		if ipcErr != nil {
			writeServiceIPCError(w, r.Context(), *ipcErr)
			return
		}
		writeServiceIPCError(w, negotiatedRequest.Context(), ipc.NewError(http.StatusNotFound, ipc.ErrorNotFound, fmt.Errorf("%s is not an EndlessNet service IPC endpoint", r.URL.Path)))
	})
	return mux
}

func serviceIPCEndpoint(method, path, operation string, privilege ServiceIPCPrivilege, mutation bool) ServiceIPCEndpoint {
	return ServiceIPCEndpoint{
		Method: method, Path: path, Operation: operation,
		RequiredPrivilege: privilege, Mutation: mutation,
	}
}

func registerServiceIPCEndpoint[Request, Response any](mux *http.ServeMux, endpoint ServiceIPCEndpoint, action func(context.Context, Request) (Response, error), authorize ServiceIPCAuthorizer, mutationLock sync.Locker) {
	mux.HandleFunc(endpoint.Path, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != endpoint.Method {
			writeServiceIPCError(w, r.Context(), ipc.NewError(http.StatusMethodNotAllowed, ipc.ErrorMethodNotAllowed, fmt.Errorf("%s requires %s", endpoint.Path, endpoint.Method)))
			return
		}
		negotiatedRequest, ipcErr := negotiateServiceIPCRequest(r)
		if ipcErr != nil {
			writeServiceIPCError(w, r.Context(), *ipcErr)
			return
		}
		r = negotiatedRequest
		if authorize != nil {
			if err := authorize(r, endpoint); err != nil {
				writeServiceIPCAuthorizationError(w, r.Context(), err)
				return
			}
		}
		request, ok := readServiceIPCRequest[Request](w, r)
		if !ok {
			return
		}
		if action == nil {
			writeServiceIPCError(w, r.Context(), ipc.NewError(http.StatusNotImplemented, ipc.ErrorNotImplemented, fmt.Errorf("%s is not implemented", endpoint.Path)))
			return
		}
		if endpoint.Mutation && mutationLock != nil {
			mutationLock.Lock()
			defer mutationLock.Unlock()
		}
		response, err := action(r.Context(), request)
		if err != nil {
			writeServiceIPCActionError(w, r.Context(), err)
			return
		}
		writeServiceIPCJSON(w, r.Context(), &response)
	})
}

func registerServiceIPCStreamEndpoint[Request any](mux *http.ServeMux, endpoint ServiceIPCEndpoint, action ServiceIPCStreamAction[Request], authorize ServiceIPCAuthorizer) {
	mux.HandleFunc(endpoint.Path, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != endpoint.Method {
			writeServiceIPCError(w, r.Context(), ipc.NewError(http.StatusMethodNotAllowed, ipc.ErrorMethodNotAllowed, fmt.Errorf("%s requires %s", endpoint.Path, endpoint.Method)))
			return
		}
		negotiatedRequest, ipcErr := negotiateServiceIPCRequest(r)
		if ipcErr != nil {
			writeServiceIPCError(w, r.Context(), *ipcErr)
			return
		}
		r = negotiatedRequest
		if authorize != nil {
			if err := authorize(r, endpoint); err != nil {
				writeServiceIPCAuthorizationError(w, r.Context(), err)
				return
			}
		}
		request, ok := readServiceIPCRequest[Request](w, r)
		if !ok {
			return
		}
		if action == nil {
			writeServiceIPCError(w, r.Context(), ipc.NewError(http.StatusNotImplemented, ipc.ErrorNotImplemented, fmt.Errorf("%s is not implemented", endpoint.Path)))
			return
		}
		writer := &serviceIPCNDJSONEventWriter{w: w, ctx: r.Context()}
		if err := action(r.Context(), request, writer); err != nil {
			if !writer.wrote {
				writeServiceIPCActionError(w, r.Context(), err)
			}
			return
		}
	})
}

func writeServiceIPCAuthorizationError(w http.ResponseWriter, ctx context.Context, err error) {
	var ipcErr ipc.Error
	if errors.As(err, &ipcErr) {
		writeServiceIPCError(w, ctx, ipcErr)
		return
	}
	writeServiceIPCError(w, ctx, ipc.NewError(http.StatusForbidden, ipc.ErrorUnauthorized, err))
}

func writeServiceIPCActionError(w http.ResponseWriter, ctx context.Context, err error) {
	var ipcErr ipc.Error
	if errors.As(err, &ipcErr) {
		writeServiceIPCError(w, ctx, ipcErr)
		return
	}
	writeServiceIPCError(w, ctx, ipc.NewError(http.StatusInternalServerError, ipc.ErrorRequestFailed, err))
}

func readServiceIPCRequest[Request any](w http.ResponseWriter, r *http.Request) (Request, bool) {
	var request Request
	if r.Body == nil {
		return request, true
	}
	defer func() { _ = r.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(r.Body, serviceIPCMaxRequestBodyBytes+1))
	if err != nil {
		writeServiceIPCError(w, r.Context(), ipc.NewError(http.StatusBadRequest, ipc.ErrorInvalidJSON, err))
		return request, false
	}
	if len(raw) > serviceIPCMaxRequestBodyBytes {
		writeServiceIPCError(w, r.Context(), ipc.NewError(http.StatusRequestEntityTooLarge, ipc.ErrorRequestTooLarge, errors.New("request body is too large")))
		return request, false
	}
	if strings.TrimSpace(string(raw)) == "" {
		return request, true
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeServiceIPCError(w, r.Context(), ipc.NewError(http.StatusBadRequest, ipc.ErrorInvalidJSON, err))
		return request, false
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("request body contains trailing JSON")
		}
		writeServiceIPCError(w, r.Context(), ipc.NewError(http.StatusBadRequest, ipc.ErrorInvalidJSON, err))
		return request, false
	}
	return request, true
}

func writeServiceIPCJSON(w http.ResponseWriter, ctx context.Context, value any) {
	if value == nil {
		value = &ipc.LogoutResponse{}
	}
	setServiceIPCProtocolMetadata(ctx, value)
	raw, err := json.Marshal(value)
	if err != nil {
		writeServiceIPCError(w, ctx, ipc.NewError(http.StatusInternalServerError, ipc.ErrorRequestFailed, err))
		return
	}
	if len(raw)+1 > serviceIPCMaxResponseBodyBytes {
		writeServiceIPCError(w, ctx, ipc.NewError(http.StatusInternalServerError, ipc.ErrorResponseTooLarge, errors.New("service IPC response is too large")))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	raw = append(raw, '\n')
	_, _ = w.Write(raw)
}

type serviceIPCMetadataCarrier interface {
	IPCMetadata() *ipc.Metadata
}

func setServiceIPCProtocolMetadata(ctx context.Context, value any) {
	carrier, ok := value.(serviceIPCMetadataCarrier)
	if !ok {
		return
	}
	version, _ := ServiceIPCNegotiatedVersionFromContext(ctx)
	metadata := carrier.IPCMetadata()
	serviceVersion, serviceCommit, serviceBuildDate := metadata.ServiceVersion, metadata.ServiceCommit, metadata.ServiceBuildDate
	*metadata = ipc.NewMetadata(version)
	metadata.ServiceVersion, metadata.ServiceCommit, metadata.ServiceBuildDate = serviceVersion, serviceCommit, serviceBuildDate
}

type serviceIPCNDJSONEventWriter struct {
	w     http.ResponseWriter
	ctx   context.Context
	wrote bool
}

func (w *serviceIPCNDJSONEventWriter) Send(value ipc.Event) error {
	if w == nil || w.w == nil {
		return errors.New("service IPC event writer is nil")
	}
	version, _ := ServiceIPCNegotiatedVersionFromContext(w.ctx)
	value.Metadata = ipc.NewMetadata(version)
	if value.Status != nil {
		serviceVersion, serviceCommit, serviceBuildDate := value.Status.ServiceVersion, value.Status.ServiceCommit, value.Status.ServiceBuildDate
		value.Status.Metadata = ipc.NewMetadata(version)
		value.Status.ServiceVersion, value.Status.ServiceCommit, value.Status.ServiceBuildDate = serviceVersion, serviceCommit, serviceBuildDate
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(raw)+1 > serviceIPCMaxResponseBodyBytes {
		return ipc.NewError(http.StatusInternalServerError, ipc.ErrorResponseTooLarge, errors.New("service IPC event is too large"))
	}
	if !w.wrote {
		w.w.Header().Set("Content-Type", "application/x-ndjson")
		w.wrote = true
	}
	raw = append(raw, '\n')
	if _, err := w.w.Write(raw); err != nil {
		return err
	}
	if flusher, ok := w.w.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}

func writeServiceIPCError(w http.ResponseWriter, ctx context.Context, ipcErr ipc.Error) {
	payload := ipc.ErrorResponse{
		ErrorCode: ipcErr.Code,
		Error:     boundedServiceIPCErrorMessage(ipcErr.Error()),
	}
	version, _ := ServiceIPCNegotiatedVersionFromContext(ctx)
	payload.Metadata = ipc.NewMetadata(version)
	raw, err := json.Marshal(payload)
	if err != nil {
		raw = []byte(`{"ipc_protocol":"endlessnet-client-ipc","ipc_version":1,"ipc_min_supported_version":1,"error_code":"request_failed","error":"service IPC error encoding failed"}`)
		ipcErr.Status = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(ipcErr.Status)
	raw = append(raw, '\n')
	_, _ = w.Write(raw)
}

func boundedServiceIPCErrorMessage(value string) string {
	const maxErrorBytes = 8 << 10
	if len(value) <= maxErrorBytes {
		return value
	}
	return value[:maxErrorBytes] + "..."
}

type serviceIPCNegotiatedVersionContextKey struct{}

func ServiceIPCNegotiatedVersionFromContext(ctx context.Context) (int, bool) {
	if ctx == nil {
		return 0, false
	}
	version, ok := ctx.Value(serviceIPCNegotiatedVersionContextKey{}).(int)
	return version, ok && version > 0
}

func negotiateServiceIPCRequest(r *http.Request) (*http.Request, *ipc.Error) {
	protocolValues := r.Header.Values(ipc.ProtocolHeader)
	currentValues := r.Header.Values(ipc.VersionHeader)
	minimumValues := r.Header.Values(ipc.MinVersionHeader)
	protocol := strings.TrimSpace(r.Header.Get(ipc.ProtocolHeader))
	currentRaw := strings.TrimSpace(r.Header.Get(ipc.VersionHeader))
	minimumRaw := strings.TrimSpace(r.Header.Get(ipc.MinVersionHeader))
	if protocol == "" || currentRaw == "" || minimumRaw == "" {
		err := ipc.NewError(http.StatusUpgradeRequired, ipc.ErrorVersionRequired, errors.New("service IPC protocol and version range headers are required"))
		return r, &err
	}
	if len(protocolValues) != 1 || protocol != ipc.Protocol {
		err := ipc.NewError(http.StatusUpgradeRequired, ipc.ErrorProtocolUnsupported, fmt.Errorf("unsupported service IPC protocol %q", protocol))
		return r, &err
	}
	if len(currentValues) != 1 || len(minimumValues) != 1 {
		err := ipc.NewError(http.StatusBadRequest, ipc.ErrorInvalidVersionRange, errors.New("service IPC version range headers must each occur exactly once"))
		return r, &err
	}
	current, currentErr := strconv.Atoi(currentRaw)
	minimum, minimumErr := strconv.Atoi(minimumRaw)
	if currentErr != nil || minimumErr != nil || current <= 0 || minimum <= 0 || minimum > current {
		err := ipc.NewError(http.StatusBadRequest, ipc.ErrorInvalidVersionRange, errors.New("service IPC version range is invalid"))
		return r, &err
	}
	lower := max(minimum, ipc.MinSupportedVersion)
	upper := min(current, ipc.Version)
	if lower > upper {
		err := ipc.NewError(http.StatusUpgradeRequired, ipc.ErrorVersionUnsupported, fmt.Errorf("service IPC version ranges do not overlap: client=%d-%d server=%d-%d", minimum, current, ipc.MinSupportedVersion, ipc.Version))
		return r, &err
	}
	ctx := context.WithValue(r.Context(), serviceIPCNegotiatedVersionContextKey{}, upper)
	return r.WithContext(ctx), nil
}
