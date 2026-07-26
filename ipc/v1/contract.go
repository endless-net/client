package v1

import clientapi "github.com/unng-lab/endlessnet/clientapi/v1"

const (
	Protocol             = "endlessnet-client-ipc"
	Version              = 1
	MinSupportedVersion  = 1
	ProtocolHeader       = "X-EndlessNet-IPC-Protocol"
	VersionHeader        = "X-EndlessNet-IPC-Version"
	MinVersionHeader     = "X-EndlessNet-IPC-Min-Supported-Version"
	DefaultLocalEndpoint = "http://endlessnet.local"
	DefaultWindowsPipe   = `\\.\pipe\endlessnet-service`
	DefaultUnixSocket    = "/run/endlessnet/client.sock"
	DefaultDarwinSocket  = "/var/run/endlessnet/client.sock"
)

const (
	PathStatus            = "/status"
	PathEvents            = "/events"
	PathEnroll            = "/enroll"
	PathConnect           = "/connect"
	PathServerIdentity    = "/server-identity"
	PathTrustServer       = "/server-identity/trust"
	PathDisconnect        = "/disconnect"
	PathLogout            = "/logout"
	PathNetworks          = "/networks"
	PathSelectNetwork     = "/network/select"
	PathDiagnostics       = "/diagnostics"
	PathDiagnosticsBundle = "/diagnostics/bundle"
	PathRecentLogs        = "/logs/recent"
)

const (
	OperationStatus            = "status"
	OperationEvents            = "events"
	OperationEnroll            = "enroll"
	OperationConnect           = "connect"
	OperationServerIdentity    = "server_identity"
	OperationTrustServer       = "server_identity.trust"
	OperationDisconnect        = "disconnect"
	OperationLogout            = "logout"
	OperationNetworks          = "networks"
	OperationSelectNetwork     = "network.select"
	OperationDiagnostics       = "diagnostics"
	OperationDiagnosticsBundle = "diagnostics.bundle"
	OperationRecentLogs        = "logs.recent"
)

type EventType string

const (
	EventTypeHello         EventType = "hello"
	EventTypeStatusChanged EventType = "status_changed"
	EventTypeError         EventType = "error"
)

type ServiceState string

const (
	StateConnected             ServiceState = "Connected"
	StateDisconnected          ServiceState = "Disconnected"
	StateDegraded              ServiceState = "Degraded"
	StateError                 ServiceState = "Error"
	StateNeedsEnrollment       ServiceState = "NeedsEnrollment"
	StateNeedsApproval         ServiceState = "NeedsApproval"
	StateServerIdentityChanged ServiceState = "ServerIdentityChanged"
)

type ControlState string

const (
	ControlStatePendingApproval       ControlState = "pending_approval"
	ControlStateDegraded              ControlState = "degraded"
	ControlStateOfflineCache          ControlState = "offline_cache"
	ControlStateReady                 ControlState = "ready"
	ControlStateRegistered            ControlState = "registered"
	ControlStateCacheInvalid          ControlState = "cache_invalid"
	ControlStateError                 ControlState = "error"
	ControlStateNotRegistered         ControlState = "not_registered"
	ControlStateDisconnected          ControlState = "disconnected"
	ControlStateServerIdentityChanged ControlState = "server_identity_changed"
)

type DesiredState string

const (
	DesiredConnected    DesiredState = "connected"
	DesiredDisconnected DesiredState = "disconnected"
)

type Metadata struct {
	IPCProtocol          string `json:"ipc_protocol"`
	IPCVersion           int    `json:"ipc_version"`
	IPCMinSupported      int    `json:"ipc_min_supported_version"`
	IPCNegotiatedVersion int    `json:"ipc_negotiated_version,omitempty"`
	ServiceVersion       string `json:"service_version,omitempty"`
	ServiceCommit        string `json:"service_commit,omitempty"`
	ServiceBuildDate     string `json:"service_build_date,omitempty"`
}

func (m *Metadata) IPCMetadata() *Metadata { return m }

func NewMetadata(negotiatedVersion int) Metadata {
	return Metadata{
		IPCProtocol:          Protocol,
		IPCVersion:           Version,
		IPCMinSupported:      MinSupportedVersion,
		IPCNegotiatedVersion: negotiatedVersion,
	}
}

type ErrorResponse struct {
	Metadata
	ErrorCode string `json:"error_code"`
	Error     string `json:"error"`
}

type StatusRequest struct{}

type ConnectionIntentStatus struct {
	DesiredState DesiredState `json:"desired_state"`
	Reason       string       `json:"reason,omitempty"`
	UpdatedAt    string       `json:"updated_at,omitempty"`
}

type EndpointAddress struct {
	ID   string `json:"id"`
	Addr string `json:"addr"`
}

type RelayEndpoint struct {
	ID       string `json:"id"`
	Addr     string `json:"addr"`
	Protocol string `json:"protocol,omitempty"`
	Priority int    `json:"priority,omitempty"`
}

type PathCandidateStatus struct {
	Type                string  `json:"type"`
	Tier                string  `json:"tier,omitempty"`
	Priority            int     `json:"priority,omitempty"`
	State               string  `json:"state"`
	Endpoint            string  `json:"endpoint,omitempty"`
	RelayID             string  `json:"relay_id,omitempty"`
	Protocol            string  `json:"protocol,omitempty"`
	RTTMS               float64 `json:"rtt_ms,omitempty"`
	CheckedAt           string  `json:"checked_at,omitempty"`
	LastReachableAt     string  `json:"last_reachable_at,omitempty"`
	ConsecutiveFailures int     `json:"consecutive_failures,omitempty"`
	Reason              string  `json:"reason,omitempty"`
}

type PeerPathStatus struct {
	PeerID           string                `json:"peer_id"`
	Hostname         string                `json:"hostname"`
	Direct           PathCandidateStatus   `json:"direct"`
	Candidates       []PathCandidateStatus `json:"candidates,omitempty"`
	Relay            PathCandidateStatus   `json:"relay"`
	SelectedPath     string                `json:"selected_path"`
	SelectedEndpoint string                `json:"selected_endpoint,omitempty"`
	LastTransitionAt string                `json:"last_transition_at,omitempty"`
	SelectionReason  string                `json:"selection_reason,omitempty"`
}

type WireGuardInspection struct {
	OK         bool                       `json:"ok"`
	Interface  string                     `json:"interface"`
	MTU        int                        `json:"mtu,omitempty"`
	ListenPort int                        `json:"listen_port,omitempty"`
	PeerCount  int                        `json:"peer_count"`
	Peers      []WireGuardPeerInspection  `json:"peers"`
	Routes     []WireGuardRouteInspection `json:"routes,omitempty"`
	Error      string                     `json:"error,omitempty"`
}

type WireGuardPeerInspection struct {
	PublicKey                  string   `json:"public_key"`
	Endpoint                   string   `json:"endpoint,omitempty"`
	AllowedIPs                 []string `json:"allowed_ips"`
	LatestHandshakeUnix        int64    `json:"latest_handshake_unix,omitempty"`
	TransferRXBytes            uint64   `json:"transfer_rx_bytes"`
	TransferTXBytes            uint64   `json:"transfer_tx_bytes"`
	PersistentKeepaliveSeconds int      `json:"persistent_keepalive_seconds,omitempty"`
}

type WireGuardRouteInspection struct {
	Target        string `json:"target"`
	Interface     string `json:"interface,omitempty"`
	UsesInterface bool   `json:"uses_interface"`
	Error         string `json:"error,omitempty"`
}

type WireGuardApplyResult struct {
	OK         bool   `json:"ok"`
	Method     string `json:"method"`
	Interface  string `json:"interface,omitempty"`
	Changed    bool   `json:"changed"`
	Skipped    bool   `json:"skipped,omitempty"`
	Reason     string `json:"reason,omitempty"`
	DownError  string `json:"down_error,omitempty"`
	UpError    string `json:"up_error,omitempty"`
	SyncError  string `json:"sync_error,omitempty"`
	RouteError string `json:"route_error,omitempty"`
}

type NetworkInterfaceStatus struct {
	Name         string   `json:"name"`
	Index        int      `json:"index"`
	MTU          int      `json:"mtu"`
	Flags        []string `json:"flags,omitempty"`
	AddressCount int      `json:"address_count"`
	Addresses    []string `json:"addresses,omitempty"`
	Prefixes     []string `json:"prefixes,omitempty"`
	Error        string   `json:"error,omitempty"`
}

type OverlayCIDRConflict struct {
	OverlayCIDR   string `json:"overlay_cidr"`
	LocalPrefix   string `json:"local_prefix"`
	Interface     string `json:"interface"`
	AddressFamily string `json:"address_family"`
	Reason        string `json:"reason"`
}

type ControlProbe struct {
	OK         bool           `json:"ok"`
	URL        string         `json:"url,omitempty"`
	HTTPStatus int            `json:"http_status,omitempty"`
	Error      string         `json:"error,omitempty"`
	Attempts   []ControlProbe `json:"attempts,omitempty"`
}

type AgentSnapshotState string

const (
	AgentSnapshotAbsent   AgentSnapshotState = "absent"
	AgentSnapshotCurrent  AgentSnapshotState = "current"
	AgentSnapshotPrevious AgentSnapshotState = "previous"
)

type AgentStatus struct {
	StatePresent       bool               `json:"state_present"`
	SnapshotState      AgentSnapshotState `json:"snapshot_state"`
	TargetMapRevision  uint64             `json:"target_map_revision,omitempty"`
	ConnectionPaused   bool               `json:"connection_paused,omitempty"`
	GeneratedAt        string             `json:"generated_at,omitempty"`
	NodeID             string             `json:"node_id,omitempty"`
	NetworkID          string             `json:"network_id,omitempty"`
	NetworkName        string             `json:"network_name,omitempty"`
	OverlayIP          string             `json:"overlay_ip,omitempty"`
	OverlayIPv6        string             `json:"overlay_ipv6,omitempty"`
	MapRevision        uint64             `json:"map_revision,omitempty"`
	PeerCount          int                `json:"peer_count,omitempty"`
	STUNOK             bool               `json:"stun_ok"`
	RelayOK            bool               `json:"relay_ok"`
	SelectedRelay      RelayEndpoint      `json:"selected_relay,omitempty"`
	RelayAttemptCount  int                `json:"relay_attempt_count,omitempty"`
	PathCount          int                `json:"path_count,omitempty"`
	SelectedPathCounts map[string]int     `json:"selected_path_counts,omitempty"`
	Peers              []PeerPathStatus   `json:"peers,omitempty"`
	LastError          string             `json:"last_error,omitempty"`
}

type StatusResponse struct {
	Metadata
	State                     ServiceState            `json:"state"`
	ControlState              ControlState            `json:"control_state"`
	DesiredState              DesiredState            `json:"desired_state,omitempty"`
	UserDisconnected          bool                    `json:"user_disconnected,omitempty"`
	ConnectionIntent          *ConnectionIntentStatus `json:"connection_intent,omitempty"`
	ControlPlaneURLs          []string                `json:"control_plane_urls,omitempty"`
	AccountID                 string                  `json:"account_id,omitempty"`
	NodeID                    string                  `json:"node_id,omitempty"`
	NetworkID                 string                  `json:"network_id,omitempty"`
	NetworkName               string                  `json:"network_name,omitempty"`
	Hostname                  string                  `json:"hostname,omitempty"`
	EnrollmentRequestID       string                  `json:"enrollment_request_id,omitempty"`
	ApprovalURL               string                  `json:"approval_url,omitempty"`
	NodeApprovalState         string                  `json:"node_approval_state,omitempty"`
	OverlayCIDR               string                  `json:"overlay_cidr,omitempty"`
	OverlayIPv6CIDR           string                  `json:"overlay_ipv6_cidr,omitempty"`
	OverlayIP                 string                  `json:"overlay_ip,omitempty"`
	OverlayIPv6               string                  `json:"overlay_ipv6,omitempty"`
	MapRevision               uint64                  `json:"map_revision,omitempty"`
	PeerCount                 int                     `json:"peer_count,omitempty"`
	STUNEndpoints             []EndpointAddress       `json:"stun_endpoints,omitempty"`
	RelayEndpoints            []RelayEndpoint         `json:"relay_endpoints,omitempty"`
	MapSigningTrustPresent    bool                    `json:"map_signing_trust_present"`
	TokenPresent              bool                    `json:"token_present"`
	NodeCredentialPresent     bool                    `json:"node_credential_present"`
	DeviceFingerprintPresent  bool                    `json:"device_fingerprint_present"`
	IdentityPrivateKeyPresent bool                    `json:"identity_private_key_present"`
	PrivateKeyPresent         bool                    `json:"private_key_present"`
	CachedMapPresent          bool                    `json:"cached_map_present"`
	CachedMapValid            bool                    `json:"cached_map_valid"`
	RouteTable                string                  `json:"route_table,omitempty"`
	LocalStateError           string                  `json:"local_state_error,omitempty"`
	ApprovalError             string                  `json:"approval_error,omitempty"`
	CachedMapError            string                  `json:"cached_map_error,omitempty"`
	ConnectionIntentError     string                  `json:"connection_intent_error,omitempty"`
	RecoveryRequired          string                  `json:"recovery_required,omitempty"`
	Control                   *ControlProbe           `json:"control,omitempty"`
	Agent                     *AgentStatus            `json:"agent,omitempty"`
	WireGuard                 *WireGuardInspection    `json:"wireguard,omitempty"`
}

type EnrollRequest struct {
	EnrollToken    string `json:"enroll_token,omitempty"`
	Server         string `json:"server,omitempty"`
	Mode           string `json:"mode,omitempty"`
	Hostname       string `json:"hostname,omitempty"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

type EnrollResponse struct {
	StatusResponse
	WireGuardApply *WireGuardApplyResult `json:"wireguard_apply,omitempty"`
}

type ConnectRequest struct{}

type ConnectResponse struct {
	Metadata
	State                ServiceState         `json:"state"`
	ControlState         ControlState         `json:"control_state,omitempty"`
	DesiredState         DesiredState         `json:"desired_state,omitempty"`
	UserDisconnected     bool                 `json:"user_disconnected,omitempty"`
	NodeID               string               `json:"node_id,omitempty"`
	NetworkID            string               `json:"network_id,omitempty"`
	MapRevision          uint64               `json:"map_revision,omitempty"`
	WireGuard            WireGuardApplyResult `json:"wireguard"`
	ReenrollmentRequired bool                 `json:"reenrollment_required,omitempty"`
}

type ServerIdentityRequest struct{}

type ServerIdentityResponse struct {
	Metadata
	ControlPlaneURL string `json:"control_plane_url"`
	TrustedKeyID    string `json:"trusted_key_id"`
	AnnouncedKeyID  string `json:"announced_key_id"`
	Changed         bool   `json:"changed"`
}

type TrustServerRequest struct {
	Confirmed      bool   `json:"confirmed"`
	ConfirmedKeyID string `json:"confirmed_key_id"`
}

type TrustServerResponse struct {
	ConnectResponse
	ServerIdentityUpdated bool   `json:"server_identity_updated"`
	TrustedKeyID          string `json:"trusted_key_id,omitempty"`
}

type DisconnectRequest struct{}

type DisconnectResponse struct {
	Metadata
	State            ServiceState         `json:"state"`
	DesiredState     DesiredState         `json:"desired_state"`
	UserDisconnected bool                 `json:"user_disconnected"`
	WireGuard        WireGuardApplyResult `json:"wireguard"`
}

type LogoutRequest struct{}

type LogoutResponse struct {
	Metadata
	State ServiceState `json:"state"`
}

type NetworksRequest struct{}

type NetworksResponse struct {
	Metadata
	Networks          []clientapi.Network `json:"networks"`
	SelectedNetworkID string              `json:"selected_network_id"`
}

type SelectNetworkRequest struct {
	NetworkID   string `json:"network_id,omitempty"`
	NetworkName string `json:"network_name,omitempty"`
}

type SelectNetworkResponse struct {
	Metadata
	State             ServiceState      `json:"state"`
	DesiredState      DesiredState      `json:"desired_state"`
	SelectedNetworkID string            `json:"selected_network_id"`
	SelectedNetwork   clientapi.Network `json:"selected_network"`
	NodeID            string            `json:"node_id"`
	MapRevision       uint64            `json:"map_revision"`
}

type DiagnosticsRequest struct {
	LogLimit int `json:"log_limit,omitempty"`
}

type DiagnosticsClientInfo struct {
	Product    string `json:"product"`
	Version    string `json:"version"`
	Commit     string `json:"commit"`
	BuildDate  string `json:"build_date"`
	TargetOS   string `json:"target_os"`
	TargetArch string `json:"target_arch"`
}

type DiagnosticsRuntimeInfo struct {
	GOOS      string            `json:"goos"`
	GOARCH    string            `json:"goarch"`
	GOVersion string            `json:"go_version"`
	OS        DiagnosticsOSInfo `json:"os"`
}

type DiagnosticsOSInfo struct {
	Name             string `json:"name"`
	Version          string `json:"version,omitempty"`
	Major            uint32 `json:"major,omitempty"`
	Minor            uint32 `json:"minor,omitempty"`
	Build            uint32 `json:"build,omitempty"`
	PlatformID       uint32 `json:"platform_id,omitempty"`
	ProductType      uint8  `json:"product_type,omitempty"`
	SuiteMask        uint16 `json:"suite_mask,omitempty"`
	ServicePack      string `json:"service_pack,omitempty"`
	ServicePackMajor uint16 `json:"service_pack_major,omitempty"`
	ServicePackMinor uint16 `json:"service_pack_minor,omitempty"`
}

type DiagnosticsConfig struct {
	ControlPlaneURLs          []string `json:"control_plane_urls,omitempty"`
	NodeID                    string   `json:"node_id,omitempty"`
	MapRevision               uint64   `json:"map_revision,omitempty"`
	MapSigningTrustPresent    bool     `json:"map_signing_trust_present"`
	TokenPresent              bool     `json:"token_present"`
	IdentityPrivateKeyPresent bool     `json:"identity_private_key_present"`
	PrivateKeyPresent         bool     `json:"private_key_present"`
	NodeCredentialPresent     bool     `json:"node_credential_present"`
	DeviceFingerprintPresent  bool     `json:"device_fingerprint_present"`
	WireGuardRouteTable       string   `json:"wireguard_route_table,omitempty"`
	CachedMapPresent          bool     `json:"cached_map_present"`
}

type DiagnosticsDNSRecord struct {
	NodeID   string `json:"node_id"`
	Hostname string `json:"hostname"`
	Label    string `json:"label"`
	FQDN     string `json:"fqdn"`
	IPv4     string `json:"ipv4,omitempty"`
	IPv6     string `json:"ipv6,omitempty"`
}

type DiagnosticsDNSSummary struct {
	SearchDomain      string                 `json:"search_domain"`
	TTLSeconds        int                    `json:"ttl_seconds"`
	NetworkDNSServers []string               `json:"network_dns_servers"`
	RecordCount       int                    `json:"record_count"`
	Records           []DiagnosticsDNSRecord `json:"records"`
}

type DiagnosticsSubnetRoute struct {
	PeerID   string `json:"peer_id"`
	Hostname string `json:"hostname"`
	CIDR     string `json:"cidr"`
}

type DiagnosticsRouteSummary struct {
	OverlayCIDR         string                   `json:"overlay_cidr"`
	OverlayIPv6CIDR     string                   `json:"overlay_ipv6_cidr,omitempty"`
	PeerCount           int                      `json:"peer_count"`
	AllowedIPCount      int                      `json:"allowed_ip_count"`
	PeerRouteTargets    []string                 `json:"peer_route_targets"`
	SubnetRouteCount    int                      `json:"subnet_route_count"`
	SubnetRoutes        []DiagnosticsSubnetRoute `json:"subnet_routes"`
	DefaultRoutePresent bool                     `json:"default_route_present"`
	WireGuardRouteTable string                   `json:"wireguard_route_table,omitempty"`
}

type Diagnostics struct {
	GeneratedAt        string                   `json:"generated_at"`
	Client             DiagnosticsClientInfo    `json:"client"`
	Runtime            DiagnosticsRuntimeInfo   `json:"runtime"`
	Status             StatusResponse           `json:"status"`
	LastErrors         []string                 `json:"last_errors"`
	Config             DiagnosticsConfig        `json:"config"`
	DNSSummary         *DiagnosticsDNSSummary   `json:"dns_summary,omitempty"`
	RouteSummary       *DiagnosticsRouteSummary `json:"route_summary,omitempty"`
	RecentLogs         []LogEntry               `json:"recent_logs"`
	Interfaces         []NetworkInterfaceStatus `json:"interfaces"`
	RouteConflictCount int                      `json:"route_conflict_count"`
	RouteConflicts     []OverlayCIDRConflict    `json:"route_conflicts"`
}

type DiagnosticsResponse struct {
	Metadata
	Diagnostics Diagnostics `json:"diagnostics"`
}

type DiagnosticsBundleRequest struct {
	LogLimit int `json:"log_limit,omitempty"`
}

type DiagnosticsBundleResponse struct {
	Metadata
	Path      string `json:"path"`
	CreatedAt string `json:"created_at"`
	ExpiresAt string `json:"expires_at"`
	SizeBytes int64  `json:"size_bytes"`
	Reused    bool   `json:"reused"`
}

type RecentLogsRequest struct {
	Limit int `json:"limit,omitempty"`
}

type LogEntry struct {
	Timestamp string `json:"timestamp"`
	Message   string `json:"message"`
}

type RecentLogsResponse struct {
	Metadata
	Logs []LogEntry `json:"logs"`
}

type EventsRequest struct{}

type Event struct {
	Metadata
	EventType   EventType       `json:"event_type"`
	Sequence    int             `json:"sequence"`
	GeneratedAt string          `json:"generated_at"`
	Status      *StatusResponse `json:"status,omitempty"`
	ErrorCode   string          `json:"error_code,omitempty"`
	Error       string          `json:"error,omitempty"`
}
