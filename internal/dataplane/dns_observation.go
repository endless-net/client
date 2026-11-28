//go:build linux || windows

package dataplane

import (
	"errors"
	"sync/atomic"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/connectivity"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	"google.golang.org/protobuf/proto"
)

type dnsObservation struct {
	dnsResult atomic.Pointer[clientv0.DnsApplication]
}

func (s *dnsObservation) beginDNS(snapshot coordinator.NetworkMapSnapshot) *clientv0.DnsApplication {
	pending := &clientv0.DnsApplication{AccountId: snapshot.Network.AccountID, NetworkId: snapshot.Network.ID, NodeId: snapshot.Node.ID, TargetMapRevision: snapshot.Revision.Network, State: clientv0.DnsApplyState_DNS_APPLY_STATE_PENDING}
	if prev := s.dnsResult.Load(); prev != nil && prev.AccountId == pending.AccountId && prev.NetworkId == pending.NetworkId && prev.NodeId == pending.NodeId {
		pending.LastAppliedMapRevision = prev.LastAppliedMapRevision
	}
	s.dnsResult.Store(pending)
	return pending
}

// Keep a failed update distinct from the last revision confirmed by the system resolver.
// Published snapshots are immutable and may be read concurrently with apply.
func (s *dnsObservation) finishDNS(prior *clientv0.DnsApplication, cleanup bool, err error) {
	if prior == nil {
		return
	}
	next := proto.CloneOf(prior)
	next.Failure = clientv0.DnsApplyFailure_DNS_APPLY_FAILURE_UNSPECIFIED
	switch {
	case err != nil:
		next.State = clientv0.DnsApplyState_DNS_APPLY_STATE_FAILED
		switch {
		case cleanup:
			next.Failure = clientv0.DnsApplyFailure_DNS_APPLY_FAILURE_OS_CLEANUP
		case errors.Is(err, connectivity.ErrDNSUnsupported):
			next.Failure = clientv0.DnsApplyFailure_DNS_APPLY_FAILURE_UNSUPPORTED
		case errors.Is(err, connectivity.ErrDNSAssignment):
			next.Failure = clientv0.DnsApplyFailure_DNS_APPLY_FAILURE_INVALID_ASSIGNMENT
		default:
			next.Failure = clientv0.DnsApplyFailure_DNS_APPLY_FAILURE_OS_APPLY
		}
	case cleanup:
		next.State = clientv0.DnsApplyState_DNS_APPLY_STATE_WITHDRAWN
		next.LastAppliedMapRevision = nil
	default:
		next.State = clientv0.DnsApplyState_DNS_APPLY_STATE_APPLIED
		next.LastAppliedMapRevision = proto.Uint64(next.TargetMapRevision)
	}
	s.dnsResult.Store(next)
}
