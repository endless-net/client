package dataplane

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"unsafe"

	"github.com/endless-net/client/api/transport"
	"github.com/endless-net/client/internal/wintundriver"
	"github.com/google/uuid"
	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/windows/driver"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

type windowsKernelAdapter interface {
	AdapterState() (driver.AdapterState, error)
	SetConfiguration(*driver.Interface, uint32) error
}

// The caller owns only the newly created adapter. Closing that handle removes
// the candidate; the shared driver remains available to other pairs.
func openWindowsPeerDevice(ctx context.Context, cfg directCipherConfig) (_ *windowsPeerDevice, resultErr error) {
	a := cfg.assignment
	if cfg.clock == nil || a.lease == nil {
		return nil, ErrConfiguration
	}
	_, changed := a.lease.SourceSignal()
	if !canFinishPreparation(ctx, cfg.clock, a, changed) {
		return nil, ErrNoAuthority
	}
	if a.offer.Profile.Engine != transport.WireGuard {
		return nil, errKernelUnavailable
	}
	if err := wintundriver.LoadWireGuardNT(); err != nil {
		return nil, errKernelUnavailable
	}
	broker, err := openWindowsPeerSocket(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if resultErr != nil {
			if closeErr := broker.Close(); closeErr != nil {
				resultErr = errors.Join(resultErr, closeErr)
			}
		}
	}()
	socket := broker.LocalAddr().(*net.UDPAddr).AddrPort()
	id, err := uuid.NewRandom()
	if err != nil {
		return nil, err
	}
	adapter, err := driver.CreateAdapter("EndlessNet-"+id.String(), "WireGuard", nil)
	if err != nil {
		// The SDK returned no owned adapter. A host policy or missing
		// privilege can make the driver unavailable for this profile.
		switch err {
		case windows.ERROR_ACCESS_DENIED, windows.ERROR_PRIVILEGE_NOT_HELD,
			windows.ERROR_DRIVER_BLOCKED, windows.ERROR_NOT_SUPPORTED:
			return nil, errKernelUnavailable
		}
		return nil, err
	}
	if err := configureWindowsKernelPeer(ctx, adapter, socket, cfg); err != nil {
		return nil, errors.Join(err, adapter.Close())
	}
	if err := verifyWindowsKernelPeer(adapter, socket, cfg); err != nil {
		return nil, errors.Join(err, adapter.Close())
	}
	if !canFinishPreparation(ctx, cfg.clock, a, changed) {
		return nil, errors.Join(ErrNoAuthority, adapter.Close())
	}
	guard, err := blockWindowsPeer(ctx, adapter)
	if err != nil {
		return nil, errors.Join(err, adapter.Close())
	}
	peer := &windowsPeerDevice{Adapter: adapter, guard: guard, assignment: a, sourceChanged: changed}
	if a.plan.LocalProbeIPv4 != "" {
		if err := peer.installProbeRoute(); err != nil {
			return nil, errors.Join(err, peer.Close())
		}
	}
	if !canFinishPreparation(ctx, cfg.clock, a, changed) {
		return nil, errors.Join(ErrNoAuthority, peer.Close())
	}
	peer.socket = broker
	return peer, nil
}

// Keep the candidate's WFP blocks until its adapter has been removed.
type windowsPeerDevice struct {
	*driver.Adapter
	guard         *nativeFilterAPI
	probeGuard    *nativeFilterAPI
	probeSocket   *windowsProbeSocket
	socket        *net.UDPConn
	outer         *nativeFilterAPI
	driverPort    uint16
	assignment    candidateAssignment
	sourceChanged <-chan struct{}
}

// The application address stays on Wintun. The candidate receives packets
// addressed there and sends packets sourced there.
func (d *windowsPeerDevice) prepareApplicationIP(id adapterIdentity) error {
	if d.Adapter == nil || d.probeGuard == nil || uint64(d.LUID()) != id.LUID {
		return ErrAdapterOwnership
	}
	_, err := setOwnedWeakHost(id, true, true)
	return err
}

// Preserve receive policy on the primary. Report an attempted mutation even
// on uncertain readback so its owner can restore the initial state.
func setOwnedWeakHost(id adapterIdentity, send, enableReceive bool) (bool, error) {
	luid := winipcfg.LUID(id.LUID)
	current, err := luid.Interface()
	if err != nil || adapterInfo(*current) != id {
		return false, ErrAdapterOwnership
	}
	row, err := luid.IPInterface(windows.AF_INET)
	if err != nil {
		return false, err
	}
	if row.InterfaceIndex != id.Index || uint64(row.InterfaceLUID) != id.LUID {
		return false, ErrAdapterOwnership
	}
	receive := row.WeakHostReceive || enableReceive
	if row.WeakHostSend == send && row.WeakHostReceive == receive {
		return false, nil
	}
	row.WeakHostSend, row.WeakHostReceive, row.SitePrefixLength = send, receive, 0
	if err := row.Set(); err != nil {
		return true, err
	}
	actual, err := luid.IPInterface(windows.AF_INET)
	if err != nil || actual.InterfaceIndex != id.Index || actual.InterfaceLUID != row.InterfaceLUID ||
		actual.WeakHostSend != send || actual.WeakHostReceive != receive {
		return true, ErrIPUncertain
	}
	return true, nil
}

// All created rows belong to this disposable, WFP-blocked adapter. Destroying
// the adapter also rolls back a partial installation.
func (d *windowsPeerDevice) installProbeRoute() error {
	local, remote, err := d.assignment.controlAddresses()
	if err != nil {
		return err
	}
	addresses, err := winipcfg.GetUnicastIPAddressTable(windows.AF_INET)
	if err != nil {
		return err
	}
	for _, row := range addresses {
		addr := row.Address.Addr()
		if addr == local || addr == remote {
			return ErrIPConflict
		}
	}
	routes, err := winipcfg.GetIPForwardTable2(windows.AF_INET)
	if err != nil {
		return err
	}
	for _, route := range routes {
		prefix := route.DestinationPrefix.Prefix()
		if prefix.Bits() > 0 && (prefix.Contains(local) || prefix.Contains(remote)) {
			return ErrIPConflict
		}
	}
	luid := winipcfg.LUID(d.LUID())
	var row winipcfg.MibUnicastIPAddressRow
	row.Init()
	row.InterfaceLUID = luid
	row.OnLinkPrefixLength = 32
	row.SkipAsSource = true
	row.DadState = winipcfg.DadStatePreferred
	if err := row.Address.SetAddr(local); err != nil {
		return err
	}
	if err := row.Create(); err != nil {
		return err
	}
	return luid.AddRoute(netip.PrefixFrom(remote, 32), netip.IPv4Unspecified(), 0)
}

func (d *windowsPeerDevice) Close() error {
	if d.probeSocket != nil {
		if err := d.probeSocket.Close(); err != nil {
			return err
		}
		d.probeSocket = nil
	}
	if d.Adapter != nil {
		if err := d.Adapter.Close(); err != nil {
			return err
		}
		d.Adapter = nil
	}
	if d.guard != nil {
		if err := d.guard.close(); err != nil {
			return err
		}
		d.guard = nil
	}
	if d.probeGuard != nil {
		if err := d.probeGuard.close(); err != nil {
			return err
		}
		d.probeGuard = nil
	}
	if d.outer != nil {
		if err := d.outer.close(); err != nil {
			return err
		}
		d.outer = nil
	}
	if d.socket != nil {
		err := d.socket.Close()
		d.socket = nil
		return err
	}
	return nil
}

// The owner must close the device after any activation failure. Keep the
// outer filters until removal, even if bringing the adapter back down fails.
func (d *windowsPeerDevice) activate(ctx context.Context, cfg directCipherConfig) error {
	if d.Adapter == nil || d.socket == nil || d.guard == nil || d.outer != nil {
		return ErrAdapterConflict
	}
	a := cfg.assignment
	if cfg.clock == nil || a.lease == nil || a.lease != d.assignment.lease || !a.sameProbe(d.assignment) {
		return ErrNoAuthority
	}
	if !canFinishPreparation(ctx, cfg.clock, a, d.sourceChanged) {
		return ErrNoAuthority
	}
	state, err := d.AdapterState()
	if err != nil {
		return err
	}
	if state != driver.AdapterStateDown {
		return ErrAdapterConflict
	}
	broker := d.socket.LocalAddr().(*net.UDPAddr).AddrPort()
	if err := verifyWindowsKernelPeer(d.Adapter, broker, cfg); err != nil {
		return err
	}
	v4, err := openExclusiveUDP(ctx, "udp4", "0.0.0.0:0")
	if err != nil {
		return err
	}
	port := v4.LocalAddr().(*net.UDPAddr).AddrPort().Port()
	v6, err := openExclusiveUDP(ctx, "udp6", net.JoinHostPort("::", strconv.Itoa(int(port))))
	if err != nil {
		return errors.Join(err, v4.Close())
	}
	settings := driver.Interface{Flags: driver.InterfaceHasListenPort, ListenPort: port}
	err = d.SetConfiguration(&settings, uint32(unsafe.Sizeof(settings)))
	if err == nil {
		actual, readErr := d.Configuration()
		err = readErr
		if err == nil {
			clear(actual.PrivateKey[:])
			if actual.ListenPort != port {
				err = ErrEngine
			}
		}
	}
	if err == nil {
		d.outer, err = blockWindowsDriverSocket(ctx, port, broker)
	}
	if err = errors.Join(err, v6.Close(), v4.Close()); err != nil {
		return err
	}
	if !canFinishPreparation(ctx, cfg.clock, a, d.sourceChanged) {
		return ErrNoAuthority
	}
	if err := d.SetAdapterState(driver.AdapterStateUp); err != nil {
		return err
	}
	d.driverPort = port
	if _, err := d.activeLink(); err != nil {
		return errors.Join(err, d.SetAdapterState(driver.AdapterStateDown))
	}
	if !canFinishPreparation(ctx, cfg.clock, a, d.sourceChanged) {
		return errors.Join(ErrNoAuthority, d.SetAdapterState(driver.AdapterStateDown))
	}
	return nil
}

// Install and verify the narrower protocol rules before releasing the initial
// full block. The caller closes the owned device on any transition failure.
func (d *windowsPeerDevice) beginRoutedProbe(ctx context.Context, cfg directCipherConfig) error {
	a := cfg.assignment
	if d.Adapter == nil || d.guard == nil || d.probeGuard != nil || d.outer == nil || cfg.clock == nil ||
		a.plan.LocalProbeIPv4 == "" || !a.sameProbe(d.assignment) ||
		!canFinishPreparation(ctx, cfg.clock, a, d.sourceChanged) {
		return ErrNoAuthority
	}
	local, remote, err := a.controlAddresses()
	if err != nil {
		return err
	}
	iface, err := d.LUID().Interface()
	if err != nil {
		return err
	}
	probe, err := installCandidateBlocks(ctx, windowsProbeRules(iface.InterfaceIndex, local, remote))
	if err != nil {
		return err
	}
	d.probeGuard = probe
	d.probeSocket, err = openWindowsProbeSocket(local, remote, iface.InterfaceIndex, iface.InterfaceLUID)
	if err != nil {
		return err
	}
	if !canFinishPreparation(ctx, cfg.clock, a, d.sourceChanged) {
		return ErrNoAuthority
	}
	if err := d.guard.close(); err != nil {
		return err
	}
	d.guard = nil
	return nil
}

func (d *windowsPeerDevice) activeLink() (kernelPeerLink, error) {
	if d.Adapter == nil || d.socket == nil || (d.guard == nil && d.probeGuard == nil) || d.outer == nil {
		return kernelPeerLink{}, ErrClosed
	}
	state, err := d.AdapterState()
	if err != nil {
		return kernelPeerLink{}, err
	}
	if state != driver.AdapterStateUp {
		return kernelPeerLink{}, ErrEngine
	}
	actual, err := d.Configuration()
	if err != nil {
		return kernelPeerLink{}, err
	}
	defer clear(actual.PrivateKey[:])
	broker := d.socket.LocalAddr().(*net.UDPAddr).AddrPort()
	if actual.ListenPort == 0 || actual.ListenPort != d.driverPort || actual.ListenPort == broker.Port() || actual.PeerCount != 1 || actual.FirstPeer().Endpoint.AddrPort() != broker {
		return kernelPeerLink{}, ErrEngine
	}
	return kernelPeerLink{d.socket, netip.AddrPortFrom(broker.Addr(), actual.ListenPort)}, nil
}

// configureWindowsKernelPeer configures a newly owned, down adapter. The
// external endpoint stays on the guarded bind. Activation belongs to the
// caller, after it has installed filters and rechecked the pair's authority.
func configureWindowsKernelPeer(ctx context.Context, adapter windowsKernelAdapter, socket netip.AddrPort, cfg directCipherConfig) error {
	a := cfg.assignment
	if adapter == nil || cfg.clock == nil || a.lease == nil || !socket.Addr().Is4() || !socket.Addr().IsLoopback() || socket.Port() == 0 {
		return ErrConfiguration
	}
	_, changed := a.lease.SourceSignal()
	digest, err := transport.ProfileDigest(a.offer.Profile)
	if err != nil || digest != a.offer.Digest {
		return ErrConfiguration
	}
	if a.offer.Profile.Engine != transport.WireGuard {
		return errKernelUnavailable
	}
	public, err := peerPublicKey(a.local, a.peer, cfg.private)
	if err != nil {
		return err
	}
	peerIPs, err := a.directPeerIPs()
	if err != nil {
		return err
	}
	state, err := adapter.AdapterState()
	if err != nil {
		return err
	}
	if state != driver.AdapterStateDown {
		return ErrAdapterConflict
	}

	// The SDK expects a contiguous Interface followed by its Peers and
	// AllowedIPs. Preallocation avoids copying the private key on growth.
	iface := driver.Interface{Flags: driver.InterfaceHasPrivateKey | driver.InterfaceReplacePeers | driver.InterfaceHasListenPort, PeerCount: 1}
	copy(iface.PrivateKey[:], cfg.private)
	defer clear(iface.PrivateKey[:])
	peer := driver.Peer{Flags: driver.PeerHasPublicKey | driver.PeerHasEndpoint | driver.PeerReplaceAllowedIPs | driver.PeerHasPersistentKeepalive, AllowedIPsCount: uint32(len(peerIPs))}
	copy(peer.PublicKey[:], public)
	if err := peer.Endpoint.SetAddrPort(socket); err != nil {
		return err
	}
	allowed := driver.AllowedIP{AddressFamily: windows.AF_INET, Cidr: 32}

	var builder driver.ConfigBuilder
	builder.Preallocate(uint32(unsafe.Sizeof(iface) + unsafe.Sizeof(peer) + uintptr(len(peerIPs))*unsafe.Sizeof(allowed)))
	builder.AppendInterface(&iface)
	builder.AppendPeer(&peer)
	for _, ip := range peerIPs {
		copy(allowed.Address[:], ip.AsSlice())
		builder.AppendAllowedIP(&allowed)
	}
	config, size := builder.Interface()
	defer clear(config.PrivateKey[:])
	if !canFinishPreparation(ctx, cfg.clock, a, changed) {
		return ErrNoAuthority
	}
	if err := adapter.SetConfiguration(config, size); err != nil {
		return err
	}
	if !canFinishPreparation(ctx, cfg.clock, a, changed) {
		return ErrNoAuthority
	}
	return nil
}

// Check the driver's own view before permitting activation. Exactly one peer
// and its assigned /32s bind encrypted output and decrypted input to this pair.
func verifyWindowsKernelPeer(adapter interface {
	Configuration() (*driver.Interface, error)
}, socket netip.AddrPort, cfg directCipherConfig) error {
	actual, err := adapter.Configuration()
	if err != nil {
		return err
	}
	if actual == nil {
		return ErrEngine
	}
	defer clear(actual.PrivateKey[:])
	if actual.PeerCount != 1 || actual.Flags&driver.InterfaceHasPublicKey == 0 || base64.StdEncoding.EncodeToString(actual.PublicKey[:]) != cfg.assignment.local.PublicKey {
		return ErrEngine
	}
	peer := actual.FirstPeer()
	defer clear(peer.PresharedKey[:])
	public, err := peerPublicKey(cfg.assignment.local, cfg.assignment.peer, actual.PrivateKey[:])
	if err != nil {
		return err
	}
	required := driver.PeerHasPublicKey | driver.PeerHasEndpoint
	if peer.Flags&required != required || !bytes.Equal(peer.PublicKey[:], public) || peer.Endpoint.AddrPort() != socket ||
		peer.PersistentKeepalive != 0 || peer.PresharedKey != [32]byte{} {
		return ErrEngine
	}
	expected, err := cfg.assignment.directPeerIPs()
	if err != nil {
		return err
	}
	if peer.AllowedIPsCount != uint32(len(expected)) {
		return ErrEngine
	}
	allowed := peer.FirstAllowedIP()
	seen := make(map[netip.Addr]bool, len(expected))
	for i := range expected {
		if allowed.AddressFamily != windows.AF_INET || allowed.Cidr != 32 {
			return ErrEngine
		}
		seen[netip.AddrFrom4([4]byte(allowed.Address[:4]))] = true
		if i+1 < len(expected) {
			allowed = allowed.NextAllowedIP()
		}
	}
	for _, ip := range expected {
		if !seen[ip] {
			return ErrEngine
		}
	}
	return nil
}
