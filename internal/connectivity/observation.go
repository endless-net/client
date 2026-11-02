package connectivity

import (
	"time"

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
	Known, Applied         bool
	Permission             Permission
	MapRevision            uint64
	Address                string
	PeerCount, DirectPaths uint32
	GeneratedAt            time.Time
}

type Observation struct {
	Profile        string
	Generation     uint64
	Intent         state.DesiredIntent
	Phase          Phase
	Failure        error
	CleanupFailure error
	CleanupPending bool
	Local          LocalObservation
}

type ObservationSource interface {
	// ConnectionSnapshot is bounded and performs no Store/network/native SDK I/O.
	ConnectionSnapshot() Observation
}

type LocalObservationSource interface {
	ConnectionLocal() LocalObservation
}
