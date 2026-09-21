package client

import (
	"errors"
	"net/netip"
	"reflect"
	"strconv"
	"strings"

	api "github.com/endless-net/client-api/clientapi/v1"
)

var errNativeExitUAPI = errors.New("native exit WireGuard state is unconfirmed")

// Caller holds engine.mu. Compare the live device against the committed UAPI,
// including the endpoint selected by direct/relay path reconciliation. A map's
// direct endpoint alone is not evidence of the applied relay endpoint. Neither
// the private key in IpcGet nor its raw error may escape this boundary.
func nativeExitUAPIObserved(e *WireGuardEngine, selection *ClientExitSelection) error {
	if e == nil || e.device == nil || e.exitGuard == nil {
		return errNativeExitUAPI
	}
	raw, err := e.device.IpcGet()
	if err != nil {
		return errNativeExitUAPI
	}
	return nativeExitUAPITextObserved(e.uapi, raw, selection, e.exitGuard.mark)
}

type nativeExitUAPIPeer struct {
	endpoint string
	routes   map[netip.Prefix]bool
}

type nativeExitUAPIState struct {
	mark  uint32
	peers map[string]nativeExitUAPIPeer
}

func nativeExitUAPITextObserved(expected, actual string, selection *ClientExitSelection, mark uint32) error {
	if selection == nil || !exitGuardFamilyValid(selection.Family) || mark == 0 {
		return errNativeExitUAPI
	}
	want, err := parseNativeExitUAPI(expected)
	if err != nil {
		return errNativeExitUAPI
	}
	live, err := parseNativeExitUAPI(actual)
	if err != nil {
		return errNativeExitUAPI
	}
	if want.mark != mark || live.mark != mark || !reflect.DeepEqual(want.peers, live.peers) {
		return errNativeExitUAPI
	}
	selected, exists := live.peers[selection.Host.PublicKey]
	if !exists || selected.endpoint == "" {
		return errNativeExitUAPI
	}
	ipv4 := netip.MustParsePrefix("0.0.0.0/0")
	ipv6 := netip.MustParsePrefix("::/0")
	if selected.routes[ipv4] != (selection.Family != api.ExitFamilyIPv6Only) || selected.routes[ipv6] != (selection.Family != api.ExitFamilyIPv4Only) {
		return errNativeExitUAPI
	}
	owners := make(map[netip.Prefix]string)
	for key, peer := range live.peers {
		for prefix := range peer.routes {
			if old, exists := owners[prefix]; exists && old != key {
				return errNativeExitUAPI
			}
			owners[prefix] = key
			if prefix.Bits() == 0 && key != selection.Host.PublicKey {
				return errNativeExitUAPI
			}
		}
	}
	return nil
}

// This parser deliberately extracts only stable security-relevant fields.
// Counters and handshakes vary between reads. Expected configuration directives
// and live private/preshared keys are never retained in the comparison model.
func parseNativeExitUAPI(raw string) (nativeExitUAPIState, error) {
	state := nativeExitUAPIState{peers: map[string]nativeExitUAPIPeer{}}
	if raw == "" || len(raw) > 1<<20 || strings.Count(raw, "\n") > 32768 {
		return state, errNativeExitUAPI
	}
	var peerKey string
	markSeen := false
	endpointSeen := map[string]bool{}
	routeCount := 0
	for _, line := range strings.Split(raw, "\n") {
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || key == "" {
			return state, errNativeExitUAPI
		}
		switch key {
		case "fwmark":
			if markSeen || peerKey != "" {
				return state, errNativeExitUAPI
			}
			markSeen = true
			number, err := strconv.ParseUint(value, 10, 32)
			if err != nil {
				return state, errNativeExitUAPI
			}
			state.mark = uint32(number)
		case "public_key":
			decoded, err := wireGuardHexToKey(value)
			if err != nil || len(value) != 64 || len(state.peers) >= 4096 {
				return state, errNativeExitUAPI
			}
			if _, exists := state.peers[decoded]; exists {
				return state, errNativeExitUAPI
			}
			peerKey = decoded
			state.peers[peerKey] = nativeExitUAPIPeer{routes: map[netip.Prefix]bool{}}
		case "endpoint":
			if peerKey == "" || endpointSeen[peerKey] {
				return state, errNativeExitUAPI
			}
			endpointSeen[peerKey] = true
			endpoint, err := netip.ParseAddrPort(value)
			if err != nil || endpoint.Port() == 0 {
				return state, errNativeExitUAPI
			}
			peer := state.peers[peerKey]
			peer.endpoint = endpoint.String()
			state.peers[peerKey] = peer
		case "allowed_ip":
			if peerKey == "" || routeCount >= 4096 {
				return state, errNativeExitUAPI
			}
			routeCount++
			prefix, err := netip.ParsePrefix(value)
			if err != nil || prefix.Addr().Is4In6() || prefix != prefix.Masked() {
				return state, errNativeExitUAPI
			}
			peer := state.peers[peerKey]
			if peer.routes[prefix] {
				return state, errNativeExitUAPI
			}
			peer.routes[prefix] = true
		case "errno":
			if value != "0" {
				return state, errNativeExitUAPI
			}
		}
	}
	if !markSeen {
		return state, errNativeExitUAPI
	}
	return state, nil
}
