package v2

const (
	Protocol            = "endlessnet-client-ipc"
	Version             = 2
	MinSupportedVersion = 2
)

const (
	PathEnroll  = "/enroll"
	PathConnect = "/connect"
)

const (
	OperationEnroll  = "enroll"
	OperationConnect = "connect"
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
	StateRecovering            ServiceState = "Recovering"
	StateRecoveryBlocked       ServiceState = "RecoveryBlocked"
	StatePolicyBlocked         ServiceState = "PolicyBlocked"
	StateNeedsLogin            ServiceState = "NeedsLogin"
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
	ControlStateRecovering            ControlState = "recovering"
	ControlStateRecoveryBlocked       ControlState = "recovery_blocked"
	ControlStatePolicyBlocked         ControlState = "policy_blocked"
	ControlStateNeedsLogin            ControlState = "needs_login"
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
	RequestID string `json:"request_id,omitempty"`
}

type StatusRequest struct{}

type ConnectionIntentStatus struct {
	DesiredState DesiredState `json:"desired_state"`
	Reason       string       `json:"reason,omitempty"`
	UpdatedAt    string       `json:"updated_at,omitempty"`
}

type RecoveryStatus struct {
	OperationID string       `json:"operation_id,omitempty"`
	State       ServiceState `json:"state"`
	ErrorCode   string       `json:"error_code,omitempty"`
	RequestID   string       `json:"request_id,omitempty"`
	Retryable   bool         `json:"retryable,omitempty"`
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
	Ephemeral                 bool                    `json:"ephemeral,omitempty"`
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
	Recovery                  *RecoveryStatus         `json:"recovery,omitempty"`
	Control                   *ControlProbe           `json:"control,omitempty"`
	Agent                     *AgentStatus            `json:"agent,omitempty"`
	WireGuard                 *WireGuardInspection    `json:"wireguard,omitempty"`
}

type ConnectResponse struct {
	Metadata
	State            ServiceState         `json:"state"`
	ControlState     ControlState         `json:"control_state,omitempty"`
	DesiredState     DesiredState         `json:"desired_state,omitempty"`
	UserDisconnected bool                 `json:"user_disconnected,omitempty"`
	NodeID           string               `json:"node_id,omitempty"`
	NetworkID        string               `json:"network_id,omitempty"`
	MapRevision      uint64               `json:"map_revision,omitempty"`
	WireGuard        WireGuardApplyResult `json:"wireguard"`
}
