// Package dataplane owns the Client's tunnel adapters. WireGuard cryptokey
// routing is not the directed TCP/UDP authorization boundary: the supplied TUN
// must enforce the current policy and lease on both reads and writes.
package dataplane

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net/netip"
	"sort"
	"strconv"
	"sync"

	"github.com/endless-net/client/internal/connectivity"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	"github.com/tailscale/wireguard-go/conn"
	"github.com/tailscale/wireguard-go/device"
	"github.com/tailscale/wireguard-go/tun"
)

var ErrConfiguration = errors.New("invalid WireGuard configuration")
var ErrEngine = errors.New("WireGuard engine failed")
var ErrClosed = errors.New("WireGuard engine closed")

type wireGuardDevice interface {
	IpcSetOperation(io.Reader) error
	Up() error
	Close()
}

// Engine owns the device, its UDP bind and its supplied TUN after construction.
// Closing it stops these handles; it does not prove native route/filter removal.
// There is deliberately no raw UAPI, private-key getter or IpcGet diagnostic API.
type Engine struct {
	mu          sync.Mutex
	device      wireGuardDevice
	maxPeers    int
	listenPort  uint16
	bind        *leaseBind
	public      string
	peers       map[string]bool
	bindings    map[string]primaryPeerBinding
	closed      bool
	direct      *directTracker
	lease       *connectivity.Lease
	reservation *engineReservation
}

type primaryPeerBinding struct {
	node, prefix string
	generation   uint64
}

func primaryPeerBindings(peers []coordinator.Peer) map[string]primaryPeerBinding {
	result := make(map[string]primaryPeerBinding, len(peers))
	for _, p := range peers {
		result[p.PublicKey] = bindingForPrimaryPeer(p)
	}
	return result
}

func bindingForPrimaryPeer(p coordinator.Peer) primaryPeerBinding {
	b := primaryPeerBinding{node: p.ID}
	if len(p.AllowedIPs) == 1 {
		b.prefix = p.AllowedIPs[0]
	}
	if p.Transport != nil {
		b.generation = p.Transport.PeerAdvertisement.IntentGeneration
	}
	return b
}

func (*Engine) String() string   { return "[WireGuard engine]" }
func (*Engine) GoString() string { return "[WireGuard engine]" }

// NewEngine does not allocate an adapter or change addresses/routes/firewall.
// The native owner must supply its policy-guarded TUN, with durable journal
// preparation and deny rules already in place. Engine adds mandatory checks of
// the same current-intent Lease at TUN read/write and post-encryption UDP send.
// Invalid arguments retain caller ownership; success transfers both tunnel and
// bind to the Engine. The lease check alone is not a stateful ACL implementation.
// listenPort is explicit and nonzero; the endpoint publisher must use a confirmed
// BoundPort and the separately configured externally reachable Direct address.
func NewEngine(tunnel tun.Device, bind conn.Bind, maxPeers int, listenPort uint16, lease *connectivity.Lease) (*Engine, error) {
	if tunnel == nil || bind == nil || maxPeers <= 0 || listenPort == 0 || lease == nil {
		return nil, ErrConfiguration
	}
	// Upstream UAPI errors/logging may contain their input. Keep the engine
	// logger silent and return only typed owner errors from this boundary.
	direct := newDirectTracker()
	guard := &leaseBind{Bind: &directBind{Bind: bind, tracker: direct}, permissions: lease.Permissions, listenPort: listenPort}
	d := device.NewDevice(&leaseTUN{Device: tunnel, permissions: lease.Permissions}, guard, device.NewLogger(device.LogLevelSilent, ""))
	d.SetSessionStateFunc(direct.session)
	d.SetPriorityMessageOnEstablishmentFunc(direct.currentKey)
	return &Engine{device: d, maxPeers: maxPeers, listenPort: listenPort, bind: guard, direct: direct, lease: lease}, nil
}

// BoundPort reports only an acknowledged open socket, not peer reachability or
// authority. It does not use IpcGet or return any device/key diagnostics.
func (e *Engine) BoundPort() (uint16, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return 0, ErrClosed
	}
	if e.bind == nil || e.bind.port.Load() != uint32(e.listenPort) {
		return 0, ErrEngine
	}
	return e.listenPort, nil
}

// Apply accepts only the immutable verifier result. The caller must fence its
// durable intent/cursor and install the map's packet policy before this call.
// Apply does not imply a handshake, reachable peer, or product Connected state.
// Any failed update closes the engine: UAPI mutation is not transactional.
func (e *Engine) Apply(ctx context.Context, m connectivity.Map, private []byte) error {
	snapshot, err := m.Snapshot()
	if m.Denied() {
		e.Close()
		return connectivity.ErrRevoked
	}
	if err != nil {
		e.Close()
		return ErrConfiguration
	}
	return e.apply(ctx, snapshot, private)
}

func (e *Engine) apply(ctx context.Context, snapshot coordinator.NetworkMapSnapshot, private []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return ErrClosed
	}
	if ctx == nil || ctx.Err() != nil {
		e.close()
		if ctx != nil {
			return ctx.Err()
		}
		return ErrConfiguration
	}
	nextBindings := primaryPeerBindings(snapshot.Peers)
	reset := make(map[string]bool)
	for key, previous := range e.bindings {
		if next, exists := nextBindings[key]; exists && previous != next {
			reset[key] = true
		}
	}
	config, public, peers, err := configurationWithResets(snapshot, private, e.public, e.peers, e.maxPeers, e.listenPort, reset)
	if err != nil {
		e.close()
		return err
	}
	defer clear(config)
	if err := e.direct.configure(snapshot.Peers); err != nil {
		e.close()
		return err
	}
	if e.device.IpcSetOperation(bytes.NewReader(config)) != nil || ctx.Err() != nil {
		e.close()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrEngine
	}
	if e.device.Up() != nil || ctx.Err() != nil {
		e.close()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrEngine
	}
	if e.bind == nil || e.bind.port.Load() != uint32(e.listenPort) {
		e.close()
		return ErrEngine
	}
	e.public, e.peers, e.bindings = public, peers, nextBindings
	return nil
}

func (e *Engine) close() {
	if !e.closed {
		e.closed = true
		e.direct.close()
		e.device.Close()
		if e.reservation != nil {
			e.reservation.release()
		}
		e.peers = nil
		e.bindings = nil
	}
}

func (e *Engine) Close() { e.mu.Lock(); defer e.mu.Unlock(); e.close() }

// configuration projects only validated numeric endpoints and exact host
// prefixes to UAPI. ACLs stay at the packet boundary; AllowedIPs is never used
// as a substitute for direction/protocol/port authorization.
func configuration(m coordinator.NetworkMapSnapshot, private []byte, previousPublic string, previous map[string]bool, maxPeers int, listenPort uint16) ([]byte, string, map[string]bool, error) {
	return configurationWithResets(m, private, previousPublic, previous, maxPeers, listenPort, nil)
}

// A changed identity/address/intent requires peer.Stop and drained crypto
// workers before rebuilding the same public-key slot. Endpoint-only refreshes
// retain the existing session. This uses the pinned engine's own remove/add UAPI.
func configurationWithResets(m coordinator.NetworkMapSnapshot, private []byte, previousPublic string, previous map[string]bool, maxPeers int, listenPort uint16, reset map[string]bool) ([]byte, string, map[string]bool, error) {
	key, err := ecdh.X25519().NewPrivateKey(private)
	if err != nil || len(private) != 32 || bytes.Equal(private, make([]byte, 32)) || maxPeers <= 0 || listenPort == 0 || len(m.Peers) > maxPeers || m.Node.Status == "revoked" {
		return nil, "", nil, ErrConfiguration
	}
	public := base64.StdEncoding.EncodeToString(key.PublicKey().Bytes())
	if m.Node.PublicKey != public || previousPublic != "" && previousPublic != public {
		return nil, "", nil, ErrConfiguration
	}
	local, err := netip.ParseAddr(m.Node.AssignedIP)
	if err != nil || !local.Is4() || local.IsUnspecified() || local.IsMulticast() || local.IsLoopback() {
		return nil, "", nil, ErrConfiguration
	}
	peers, addresses := make(map[string]bool, len(m.Peers)), map[netip.Addr]bool{local: true}
	for _, p := range m.Peers {
		pub, err := base64.StdEncoding.Strict().DecodeString(p.PublicKey)
		if err != nil || len(pub) != 32 || bytes.Equal(pub, make([]byte, 32)) || base64.StdEncoding.EncodeToString(pub) != p.PublicKey || p.PublicKey == public || peers[p.PublicKey] || !p.ACLRestricted || len(p.AllowedPorts) != 0 || len(p.AllowedIPs) != 1 {
			return nil, "", nil, ErrConfiguration
		}
		prefix, err := netip.ParsePrefix(p.AllowedIPs[0])
		if err != nil || !prefix.Addr().Is4() || prefix.Bits() != 32 || prefix.String() != p.AllowedIPs[0] || addresses[prefix.Addr()] || prefix.Addr().IsUnspecified() || prefix.Addr().IsLoopback() || prefix.Addr().IsMulticast() {
			return nil, "", nil, ErrConfiguration
		}
		endpoint, err := netip.ParseAddrPort(p.Endpoint)
		if err != nil || !endpoint.Addr().Is4() || endpoint.Addr().IsUnspecified() || endpoint.Addr().IsLoopback() || endpoint.Addr().IsMulticast() || endpoint.Port() == 0 || endpoint.String() != p.Endpoint {
			return nil, "", nil, ErrConfiguration
		}
		peers[p.PublicKey], addresses[prefix.Addr()] = true, true
	}
	var output bytes.Buffer
	// The buffer is private and cleared after synchronous application. Do not
	// put it in a formatted error, log, event, state journal or returned status.
	if previousPublic == "" {
		output.WriteString("listen_port=" + strconv.Itoa(int(listenPort)) + "\n")
		output.WriteString("private_key=")
		output.WriteString(hex.EncodeToString(private))
		output.WriteString("\nreplace_peers=true\n")
	}
	removed := make([]string, 0, len(previous))
	for p := range previous {
		if !peers[p] || reset[p] {
			removed = append(removed, p)
		}
	}
	sort.Strings(removed)
	for _, p := range removed {
		pub, err := base64.StdEncoding.Strict().DecodeString(p)
		if err != nil || len(pub) != 32 {
			clear(output.Bytes())
			return nil, "", nil, ErrConfiguration
		}
		output.WriteString("public_key=" + hex.EncodeToString(pub) + "\nremove=true\n")
	}
	for _, p := range m.Peers {
		pub, _ := base64.StdEncoding.Strict().DecodeString(p.PublicKey)
		output.WriteString("public_key=" + hex.EncodeToString(pub) + "\nreplace_allowed_ips=true\nallowed_ip=" + p.AllowedIPs[0] + "\nendpoint=" + p.Endpoint + "\n")
	}
	output.WriteByte('\n')
	return output.Bytes(), public, peers, nil
}
