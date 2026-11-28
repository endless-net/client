package connectivity

import (
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/state"
)

// FailureKind is a finite, safe classification. Backend text never crosses the
// runtime observation boundary, including through Error/GoString formatting.
type FailureKind uint8

const (
	FailureUnknown FailureKind = iota
	FailureAccess
	FailurePermission
	FailureConflict
	FailureApply
	FailurePath
	FailureCleanup
	FailureControl
	FailureTrust
	FailureCredential
	FailureRevoked
	FailureLogin
	FailureNodeRenewal
	FailureCheckpoint
	FailureProtocol
	FailureMap
	FailureConfiguration
	FailureCredentialInvalid
	FailureDNSAssignment
	FailureDNSUnsupported
)

func (FailureKind) Error() string { return "connection failure" }

type Phase uint8

const (
	PhaseUnknown Phase = iota
	PhaseConnecting
	PhaseConnected
	PhaseDisconnecting
	PhaseDisconnected
	PhaseFailed
)

// LocalObservation contains only measured public runtime facts, never a cached
// grant, credential, key, native handle or journal contents. Unknown during a
// native transition is intentional: status reads must not wait on an SDK call.
type LocalObservation struct {
	DNS                                *clientv0.DnsApplication
	Known, Applied                     bool
	SoleNodeReady                      bool
	ControlUnavailable                 bool
	EndpointRefreshUnconfirmed         bool
	Permission                         Permission
	MapRevision                        uint64
	MapPayloadHash                     string
	Address                            string
	PeerCount, DirectPaths, RelayPaths uint32
	GeneratedAt                        time.Time
	Resources                          []*clientv0.Resource
	Peers                              []*clientv0.Peer
	PeerTransports                     []*clientv0.PeerTransportStatus
}

type Observation struct {
	Profile        string
	Generation     uint64
	Intent         state.DesiredIntent
	Phase          Phase
	Failure        error
	CleanupFailure error
	CleanupPending bool
	RetryScheduled bool
	Local          LocalObservation
}

type ObservationSource interface {
	// ConnectionSnapshot is bounded and performs no Store/network/native SDK I/O.
	ConnectionSnapshot() Observation
}

type LocalObservationSource interface {
	ConnectionLocal() LocalObservation
}
