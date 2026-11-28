package dataplane

import (
	"encoding/binary"
	"time"
)

type tcpFragmentKey struct {
	peer     string
	outbound bool
	id       uint16
}

type tcpFragmentGrant struct {
	source, destination uint16
	flags               byte
	epoch               uint64
	firstLength         int
	end                 time.Time
	discardAt           time.Time
}

type tcpFragments map[tcpFragmentKey]tcpFragmentGrant

// Called with the switch locked after authenticating the source/destination
// pair. An epoch binds the peer key, addresses, intent and grant set.
func (s *packetSwitch) fragmentWindow(peer string, raw []byte, outbound bool) (time.Time, bool) {
	grants := s.grants[peer]
	flags := binary.BigEndian.Uint16(raw[6:8])
	if raw[9] != 6 || flags&0x3fff == 0 {
		return grants.peerPacketWindow(raw, outbound)
	}
	return s.fragments.window(tcpFragmentKey{peer, outbound, binary.BigEndian.Uint16(raw[4:6])}, s.routeEpoch[peer], grants, raw, time.Now())
}

func (f *tcpFragments) window(key tcpFragmentKey, epoch uint64, grants packetGrants, raw []byte, now time.Time) (time.Time, bool) {
	flags := binary.BigEndian.Uint16(raw[6:8])
	offset := int(flags&0x1fff) * 8
	header := int(raw[0]&15) * 4
	n := len(raw) - header
	if header < 20 || n <= 0 || flags&0xc000 != 0 || (flags&0x2000 != 0 && n%8 != 0) || offset+n > 65535-20 {
		return time.Time{}, false
	}
	entry, exists := (*f)[key]
	if offset > 0 {
		return entry.end, exists && now.Before(entry.discardAt) && entry.epoch == epoch && entry.firstLength > 0 && offset >= entry.firstLength
	}
	if exists && now.Before(entry.discardAt) {
		// Reused IP ID: subsequent payload can no longer be attributed
		// unambiguously to either first fragment.
		entry.firstLength = 0
		entry.discardAt = now.Add(30 * time.Second)
		(*f)[key] = entry
		return time.Time{}, false
	}
	if *f == nil {
		*f = make(tcpFragments)
	}
	const maxDatagrams = 256
	if !exists && len(*f) >= maxDatagrams {
		for k, v := range *f {
			if !now.Before(v.discardAt) {
				delete(*f, k)
			}
		}
		if len(*f) >= maxDatagrams {
			return time.Time{}, false
		}
	}
	end, allowed := grants.peerPacketWindow(raw, key.outbound)
	if !allowed {
		n = 0
	}
	first := tcpFragmentGrant{epoch: epoch, firstLength: n, end: end, discardAt: now.Add(30 * time.Second)}
	if allowed {
		first.source = binary.BigEndian.Uint16(raw[header:])
		first.destination = binary.BigEndian.Uint16(raw[header+2:])
		first.flags = raw[header+13]
	}
	(*f)[key] = first
	return end, allowed
}
