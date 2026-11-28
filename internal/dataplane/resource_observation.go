package dataplane

import (
	"slices"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/connectivity"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

type resourcePathState struct {
	lastResourceSwitch connectivity.ClockSample
	switchedProofs     []resourceObservation
	resourceEvidence   map[coordinator.ServiceHost]resourceObservation
}

type resourceObservation struct {
	intent     uint64
	endpoint   string
	generation uint64
	checked    time.Time
	received   connectivity.ClockSample
}

func resourceObservations(m coordinator.NetworkMapSnapshot, statuses []*clientv0.PeerTransportStatus, sample connectivity.ClockSample, previous map[coordinator.ServiceHost]resourceObservation) map[coordinator.ServiceHost]resourceObservation {
	checked := make(map[string]time.Time, len(statuses))
	for _, v := range statuses {
		if (v.GetSelectedPath() == clientv0.PathKind_PATH_KIND_DIRECT || v.GetSelectedPath() == clientv0.PathKind_PATH_KIND_RELAY) && v.GetCheckedAt() != nil && v.GetCheckedAt().IsValid() {
			checked[v.PeerId] = v.CheckedAt.AsTime()
		}
	}
	live := make(map[coordinator.ServiceHost]resourceObservation, len(checked))
	for _, peer := range m.Peers {
		if at, ok := checked[peer.ID]; ok {
			proof := resourceObservation{endpoint: peer.Endpoint, generation: peer.EndpointGeneration, checked: at, received: sample}
			if peer.Transport != nil {
				proof.intent = peer.Transport.PeerAdvertisement.IntentGeneration
			}
			host := coordinator.ServiceHost{NodeID: peer.ID, PublicKey: peer.PublicKey}
			// Polling the same confirmation must not restart its lifetime.
			if old, ok := previous[host]; ok && old.intent == proof.intent && old.endpoint == proof.endpoint && old.generation == proof.generation && old.checked.Equal(at) {
				proof.received = old.received
			}
			live[host] = proof
		}
	}
	return live
}

func (o resourceObservation) current(now connectivity.ClockSample) bool {
	const lifetime = 30 * time.Second
	received := o.received
	if received.Epoch == "" || now.Epoch != received.Epoch || now.Uptime < received.Uptime || now.UpperUptime < now.Uptime || now.Wall.Before(received.Wall) {
		return false
	}
	age := received.Wall.Sub(o.checked)
	return age >= 0 && age < lifetime && now.Wall.Sub(o.checked) < lifetime && now.UpperUptime-received.Uptime < lifetime-age
}

func observedResourcePaths(m coordinator.NetworkMapSnapshot, evidence map[coordinator.ServiceHost]resourceObservation, now connectivity.ClockSample) ([]connectivity.ResourcePath, error) {
	live := make(map[coordinator.ServiceHost]bool, len(evidence))
	for _, peer := range m.Peers {
		host := coordinator.ServiceHost{NodeID: peer.ID, PublicKey: peer.PublicKey}
		proof, ok := evidence[host]
		var intent uint64
		if peer.Transport != nil {
			intent = peer.Transport.PeerAdvertisement.IntentGeneration
		}
		if ok && proof.intent == intent && proof.endpoint == peer.Endpoint && proof.generation == peer.EndpointGeneration && proof.current(now) {
			live[host] = true
		}
	}
	return eligibleResourcePaths(m, func(h coordinator.ServiceHost) bool { return live[h] })
}

func (s *resourcePathState) reconcile(m coordinator.NetworkMapSnapshot, sample connectivity.ClockSample, interval time.Duration, statuses []*clientv0.PeerTransportStatus, packets *packetSwitch, current connectivity.Map, apply func([]connectivity.ResourcePath) error) error {
	if last := s.lastResourceSwitch; last.Epoch == sample.Epoch && sample.Uptime >= last.Uptime && sample.Uptime-last.Uptime < 3*interval && !slices.ContainsFunc(s.switchedProofs, func(p resourceObservation) bool { return !p.current(sample) }) {
		return nil
	}
	if statuses != nil {
		s.resourceEvidence = resourceObservations(m, statuses, sample, s.resourceEvidence)
	}
	paths, err := observedResourcePaths(m, s.resourceEvidence, sample)
	if err != nil {
		return err
	}
	rules, _, err := recipientRulesForPaths(m, paths)
	if err != nil {
		return err
	}
	return packets.replaceResourceRules(current, rules, func() error {
		if err := apply(paths); err != nil {
			return err
		}
		// Updating AllowedIPs resets peer queues. Give the new session time to confirm.
		s.lastResourceSwitch = sample
		s.switchedProofs = s.switchedProofs[:0]
		for _, path := range paths {
			s.switchedProofs = append(s.switchedProofs, s.resourceEvidence[path.Route.Connector])
		}
		return nil
	})
}
