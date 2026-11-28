package dataplane

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/endless-net/client/api/transport"
	"github.com/tailscale/wireguard-go/tun"
	"golang.org/x/sys/unix"
)

const linuxIP = "/usr/sbin/ip"
const linuxRouteProtocol = "99"

type linuxLink struct {
	probes  map[netip.Addr]linuxProbeBinding
	device  tun.Device
	name    string
	index   int
	address netip.Addr
	routes  []netip.Prefix
	direct  map[netip.Addr]net.Interface
	exit    *linuxExitRoutes
}

type linuxPacketDevice struct{ tun.Device }

// NativeTun.Write on Linux returns bytes. The packet switch uses the Device
// interface's packet count and submits complete IP packets without offload.
func (d linuxPacketDevice) Write(bufs [][]byte, offset int) (int, error) {
	for i, buf := range bufs {
		if offset < 0 || offset >= len(buf) {
			return i, ErrPacketIO
		}
		written, err := d.Device.Write(bufs[i:i+1], offset)
		if err != nil {
			return i, err
		}
		if written != len(buf)-offset {
			return i, ErrPacketIO
		}
	}
	return len(bufs), nil
}

func openLinuxLink(ctx context.Context, name string, mtu int) (*linuxLink, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if !linuxObjectName.MatchString(name) || mtu < 576 || mtu > 1500 {
		return nil, ErrConfiguration
	}
	ifr, err := unix.NewIfreq(name)
	if err != nil {
		return nil, ErrConfiguration
	}
	fd, err := unix.Open("/dev/net/tun", unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, linuxLinkError(err)
	}
	ifr.SetUint16(unix.IFF_TUN | unix.IFF_NO_PI | unix.IFF_TUN_EXCL)
	if err := unix.IoctlIfreq(fd, unix.TUNSETIFF, ifr); err != nil {
		_ = unix.Close(fd)
		return nil, linuxLinkError(err)
	}
	if ifr.Name() != name {
		_ = unix.Close(fd)
		return nil, ErrAdapterOwnership
	}
	if err := unix.SetNonblock(fd, true); err != nil {
		_ = unix.Close(fd)
		return nil, linuxLinkError(err)
	}
	// The runtime poller must see an attached, nonblocking descriptor.
	file := os.NewFile(uintptr(fd), "/dev/net/tun")
	device, err := tun.CreateTUNFromFile(file, mtu)
	if err != nil {
		return nil, linuxLinkError(err)
	}
	iface, err := net.InterfaceByName(name)
	if err != nil || iface.Index <= 0 || ctx.Err() != nil {
		_ = device.Close()
		return nil, ErrAdapterOwnership
	}
	return &linuxLink{device: linuxPacketDevice{device}, name: name, index: iface.Index}, nil
}

func linuxLinkError(err error) error {
	switch {
	case errors.Is(err, unix.EPERM), errors.Is(err, unix.EACCES):
		return ErrAdapterPermission
	case errors.Is(err, unix.EBUSY), errors.Is(err, unix.EEXIST):
		return ErrAdapterConflict
	default:
		return ErrConfiguration
	}
}

func (l *linuxLink) checkOwnership() (*net.Interface, error) {
	iface, err := net.InterfaceByName(l.name)
	if err != nil || iface.Index != l.index {
		return nil, ErrAdapterOwnership
	}
	// Check the live fd as well as the inventory. A reused ifindex or name
	// alone never authorizes writes to an interface.
	ifr, err := unix.NewIfreq(l.name)
	if err != nil {
		return nil, ErrAdapterOwnership
	}
	raw, err := l.device.File().SyscallConn()
	if err != nil {
		return nil, ErrAdapterOwnership
	}
	var ioctlErr error
	err = raw.Control(func(fd uintptr) { ioctlErr = unix.IoctlIfreq(int(fd), unix.TUNGETIFF, ifr) })
	if err != nil || ioctlErr != nil || ifr.Name() != l.name {
		return nil, ErrAdapterOwnership
	}
	return iface, nil
}

// Linux decides IPv4 forwarding on the incoming interface. The TUN exists
// only for this session. Keep host defaults and physical interfaces outside
// the session's writes.
func (l *linuxLink) routerForwarding(enable bool) error {
	return linuxTunnelForwarding("/proc/sys/net/ipv4/conf", l.name, enable, func() error {
		_, err := l.checkOwnership()
		return err
	})
}

func linuxTunnelForwarding(root, name string, enable bool, owned func() error) error {
	if !linuxObjectName.MatchString(name) || owned == nil {
		return ErrAdapterOwnership
	}
	path := filepath.Join(root, name, "forwarding")
	if err := owned(); err != nil {
		return err
	}
	value, err := os.ReadFile(path)
	if err != nil {
		return errors.Join(ErrConfiguration, err)
	}
	switch strings.TrimSpace(string(value)) {
	case "1":
		return owned()
	case "0":
		if !enable {
			return ErrIPConflict
		}
	default:
		return ErrConfiguration
	}
	// Verify the link again with the descriptor held. Name reuse cannot
	// retarget a descriptor opened for the owned interface.
	f, err := os.OpenFile(path, os.O_WRONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return errors.Join(linuxLinkError(err), err)
	}
	err = owned()
	if err == nil {
		_, err = f.WriteString("1\n")
	}
	if err = errors.Join(err, f.Close()); err != nil {
		return err
	}
	return linuxTunnelForwarding(root, name, false, owned)
}

func (l *linuxLink) configure(ctx context.Context, address netip.Addr, destinations []netip.Prefix) error {
	if !address.Is4() || !address.IsGlobalUnicast() {
		return ErrConfiguration
	}
	if _, err := l.checkOwnership(); err != nil {
		return err
	}
	if l.address.IsValid() && l.address != address {
		return ErrIPConflict
	}
	if l.address.IsValid() {
		if err := l.verify(ctx); err != nil {
			return err
		}
	}
	for _, destination := range destinations {
		if !destination.Addr().Is4() || destination != destination.Masked() || destination.Bits() == 0 || destination.Contains(address) {
			return ErrConfiguration
		}
	}
	if err := l.checkRouteSpace(ctx, address, destinations); err != nil {
		return err
	}
	if !l.address.IsValid() {
		if err := linuxAddressAvailable(address); err != nil {
			return err
		}
		if err := l.ip(ctx, "-4", "address", "add", address.String()+"/32", "dev", l.name); err != nil {
			return err
		}
		if err := l.ip(ctx, "link", "set", "dev", l.name, "up"); err != nil {
			return err
		}
		l.address = address
	}
	for _, old := range slices.Clone(l.routes) {
		if !slices.Contains(destinations, old) {
			if err := l.route(ctx, "del", old); err != nil {
				return err
			}
			delete(l.direct, old.Addr())
			l.routes = slices.DeleteFunc(l.routes, func(candidate netip.Prefix) bool { return candidate == old })
		}
	}
	for _, destination := range destinations {
		if !slices.Contains(l.routes, destination) {
			if err := l.route(ctx, "add", destination); err != nil {
				return err
			}
			l.routes = append(l.routes, destination)
		}
	}
	return l.verify(ctx)
}

func (l *linuxLink) verify(ctx context.Context) error {
	iface, err := l.checkOwnership()
	if err != nil || iface.Flags&net.FlagUp == 0 {
		return ErrAdapterOwnership
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return ErrIPOwnership
	}
	expected := map[netip.Addr]bool{l.address: true}
	for _, probe := range l.probes {
		expected[probe.local] = true
	}
	for _, value := range addrs {
		prefix, err := netip.ParsePrefix(value.String())
		if err == nil && prefix.Addr().Is4() {
			if prefix.Bits() != 32 || !expected[prefix.Addr()] {
				return ErrIPOwnership
			}
			delete(expected, prefix.Addr())
		}
	}
	if len(expected) != 0 {
		return ErrIPOwnership
	}
	devices := map[string]bool{l.name: true}
	for _, selected := range l.direct {
		if actual, err := net.InterfaceByIndex(selected.Index); err != nil || actual.Name != selected.Name {
			return ErrAdapterOwnership
		}
		devices[selected.Name] = true
	}
	raw, err := exec.CommandContext(ctx, linuxIP, "-j", "-4", "route", "show", "table", "main",
		"proto", linuxRouteProtocol).Output()
	if err != nil {
		return ErrIPOwnership
	}
	var records []struct {
		Destination string `json:"dst"`
		Source      string `json:"prefsrc"`
		Device      string `json:"dev"`
	}
	if err := json.Unmarshal(raw, &records); err != nil {
		return ErrIPOwnership
	}
	seen := make(map[netip.Prefix]bool, len(l.routes))
	for _, record := range records {
		if !devices[record.Device] {
			continue
		}
		address, err := linuxInventoryPrefix(record.Destination)
		if err != nil {
			return err
		}
		if record.Device != l.routeDevice(address) || record.Source != l.address.String() || seen[address] || !slices.Contains(l.routes, address) {
			return ErrIPOwnership
		}
		seen[address] = true
	}
	if len(seen) != len(l.routes) {
		return ErrIPOwnership
	}
	if l.exit != nil {
		found, err := linuxReadExitRule(ctx, linuxRouteIP, l.exit.table, true)
		if err != nil {
			return err
		}
		if !found {
			return ErrIPOwnership
		}
		if err := l.exit.check(ctx, l.name, l.address, true); err != nil {
			return err
		}
	}
	return l.checkRouteSpace(ctx, l.address, l.routes)
}

func (l *linuxLink) peerDevice(peer netip.Addr) string {
	if selected, ok := l.direct[peer]; ok {
		return selected.Name
	}
	return l.name
}

// The caller serializes path changes with policy and map updates. A nil target
// restores the route through the session TUN. Never create a missing route or
// displace a conflicting route after losing ownership.
func (l *linuxLink) switchPeer(ctx context.Context, peer netip.Addr, target *net.Interface, owner string) error {
	if !slices.Contains(l.routes, netip.PrefixFrom(peer, 32)) {
		return ErrIPOwnership
	}
	if old, ok := l.direct[peer]; ok && target == nil {
		links, err := net.Interfaces()
		if err != nil {
			return ErrAdapterOwnership
		}
		present := slices.ContainsFunc(links, func(iface net.Interface) bool { return iface.Index == old.Index && iface.Name == old.Name })
		if !present {
			routes, err := exec.CommandContext(ctx, linuxIP, "-4", "route", "show", "table", "all", "exact", peer.String()+"/32").Output()
			if err != nil || len(routes) != 0 {
				return ErrIPConflict
			}
			// Link removal also removed its route. Exclusive add leaves a
			// competing main-table route intact if one appears meanwhile.
			if err := l.ip(ctx, "-4", "route", "add", "table", "main", peer.String()+"/32", "dev", l.name, "src", l.address.String(), "proto", linuxRouteProtocol); err != nil {
				return err
			}
			delete(l.direct, peer)
			return l.verify(ctx)
		}
	}
	if err := l.verify(ctx); err != nil {
		return err
	}
	name := l.name
	if target != nil {
		iface, err := net.InterfaceByIndex(target.Index)
		if err != nil || iface.Name != target.Name || !linuxPeerLinkName.MatchString(iface.Name) || iface.Flags&net.FlagUp == 0 {
			return ErrAdapterOwnership
		}
		if _, err := ownedLinuxPeer(ctx, iface, owner); err != nil {
			return err
		}
		name = iface.Name
	}
	if l.peerDevice(peer) == name {
		return nil
	}
	if err := l.ip(ctx, "-4", "route", "change", "table", "main", peer.String()+"/32", "dev", name, "src", l.address.String(), "proto", linuxRouteProtocol); err != nil {
		return err
	}
	if target == nil {
		delete(l.direct, peer)
	} else {
		if l.direct == nil {
			l.direct = make(map[netip.Addr]net.Interface)
		}
		l.direct[peer] = *target
	}
	return l.verify(ctx)
}

func ownedLinuxPeer(ctx context.Context, iface *net.Interface, owner string) (linuxPeerLinkInfo, error) {
	prefix, group := linuxPeerScope(owner)
	details, err := linuxLinkDetails(ctx, "dev", iface.Name)
	if err != nil || len(details) != 1 {
		return linuxPeerLinkInfo{}, ErrAdapterOwnership
	}
	link := details[0]
	if !linuxPeerLinkName.MatchString(link.Name) || link.Index != iface.Index || link.Alias != "endlessnet:peer:"+owner ||
		!strings.HasPrefix(link.Name, prefix) || link.Group != strconv.FormatUint(uint64(group), 10) ||
		(link.Info.Kind != transport.WireGuard && link.Info.Kind != transport.AmneziaWG) {
		return linuxPeerLinkInfo{}, ErrAdapterOwnership
	}

	return link, nil
}

// Stage the route while the old firewall policy still denies application
// traffic on the candidate. The caller commits the negotiated path and its
// policy together. Restore the previous device on rejection or cancellation.
func (l *linuxLink) commitPeerPath(ctx context.Context, peer netip.Addr, target *net.Interface, owner string, commit func() error) error {
	if target == nil || commit == nil {
		return ErrConfiguration
	}
	previous, wasDirect := l.direct[peer]
	if err := l.switchPeer(ctx, peer, target, owner); err != nil {
		// A read-back failure can follow a successful route mutation.
		if current, ok := l.direct[peer]; !ok || current.Index != target.Index || wasDirect && previous.Index == target.Index {
			return err
		}
		return errors.Join(err, l.restorePeerPath(ctx, peer, owner, previous, wasDirect))
	}
	err := ctx.Err()
	if err == nil {
		err = commit()
	}
	if err == nil {
		return nil
	}
	return errors.Join(err, l.restorePeerPath(ctx, peer, owner, previous, wasDirect))
}

func (l *linuxLink) restorePeerPath(ctx context.Context, peer netip.Addr, owner string, previous net.Interface, wasDirect bool) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if wasDirect {
		return l.switchPeer(cleanup, peer, &previous, owner)
	}
	return l.switchPeer(cleanup, peer, nil, owner)
}

func (l *linuxLink) route(ctx context.Context, action string, destination netip.Prefix) error {
	return l.routeInTable(ctx, action, "main", destination)
}

func (l *linuxLink) ip(ctx context.Context, args ...string) error {
	if _, err := l.checkOwnership(); err != nil {
		return err
	}
	if err := exec.CommandContext(ctx, linuxIP, args...).Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrIPOwnership
	}
	return nil
}

func linuxAddressAvailable(address netip.Addr) error {
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return ErrConfiguration
	}
	for _, value := range addresses {
		if prefix, err := netip.ParsePrefix(value.String()); err == nil && prefix.Addr() == address {
			return ErrIPConflict
		}
	}
	return nil
}

type linuxInventoryRoute struct {
	Destination string          `json:"dst"`
	Device      string          `json:"dev"`
	Source      string          `json:"prefsrc"`
	Table       json.RawMessage `json:"table"`
	Protocol    json.RawMessage `json:"protocol"`
	Scope       json.RawMessage `json:"scope"`
	Type        json.RawMessage `json:"type"`
	Metric      uint32          `json:"metric"`
	Flags       []string        `json:"flags"`
	Gateway     json.RawMessage `json:"gateway"`
	Via         json.RawMessage `json:"via"`
	Nexthops    json.RawMessage `json:"nexthops"`
	NexthopID   json.RawMessage `json:"nhid"`
	Encap       json.RawMessage `json:"encap"`
	Metrics     json.RawMessage `json:"metrics"`
	MTU         json.RawMessage `json:"mtu"`
	TOS         json.RawMessage `json:"tos"`
}

func (r linuxInventoryRoute) direct() bool {
	scope, kind, tos := strings.Trim(string(r.Scope), "\""), strings.Trim(string(r.Type), "\""), strings.Trim(string(r.TOS), "\"")
	return (scope == "" || scope == "link" || scope == "253") &&
		(kind == "" || kind == "unicast" || kind == "1") &&
		(tos == "" || tos == "0" || tos == "0x00") &&
		r.Metric == 0 && len(r.Flags) == 0 &&
		r.Gateway == nil && r.Via == nil && r.Nexthops == nil && r.NexthopID == nil &&
		r.Encap == nil && r.Metrics == nil && r.MTU == nil
}

func (l *linuxLink) checkRouteSpace(ctx context.Context, address netip.Addr, destinations []netip.Prefix) error {
	if len(destinations) == 0 {
		return ctx.Err()
	}
	raw, err := exec.CommandContext(ctx, linuxIP, "-j", "-N", "-4", "route", "show", "table", "all").Output()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrIPOwnership
	}
	return checkLinuxRouteDevices(raw, l.routeDevice, address, l.routes, destinations, l.exit)
}

func checkLinuxRouteSpace(raw []byte, device string, source netip.Addr, owned, destinations []netip.Prefix, exit *linuxExitRoutes) error {
	return checkLinuxRouteDevices(raw, func(netip.Prefix) string { return device }, source, owned, destinations, exit)
}

func checkLinuxRouteDevices(raw []byte, device func(netip.Prefix) string, source netip.Addr, owned, destinations []netip.Prefix, exit *linuxExitRoutes) error {
	var rows []linuxInventoryRoute
	if err := json.Unmarshal(raw, &rows); err != nil || rows == nil {
		return ErrIPOwnership
	}
	targets := make(map[netip.Prefix]bool, len(destinations))
	for _, p := range destinations {
		targets[p] = true
	}
	existing := make(map[netip.Prefix]bool, len(owned))
	for _, p := range owned {
		existing[p] = true
	}
	for _, r := range rows {
		if exit != nil && strings.Trim(string(r.Table), "\"") == exit.table {
			// The dedicated table has its own complete ownership check.
			continue
		}
		dst := r.Destination
		if dst == "default" {
			dst = "0.0.0.0/0"
		}
		p, err := linuxInventoryPrefix(dst)
		if err != nil {
			return ErrIPOwnership
		}
		covered := false
		for bits := p.Bits(); bits >= 0; bits-- {
			if targets[netip.PrefixFrom(p.Addr(), bits).Masked()] {
				covered = true
				break
			}
		}
		if !covered {
			continue
		}
		table, protocol := strings.Trim(string(r.Table), "\""), strings.Trim(string(r.Protocol), "\"")
		if !existing[p] || !r.direct() || r.Device != device(p) || r.Source != source.String() || (table != "" && table != "254" && table != "main") || protocol != linuxRouteProtocol {
			return ErrIPConflict
		}
		delete(existing, p)
	}
	for _, p := range destinations {
		if existing[p] {
			return ErrIPOwnership
		}
	}
	return nil
}

func linuxInventoryPrefix(dst string) (netip.Prefix, error) {
	if addr, err := netip.ParseAddr(dst); err == nil && addr.Is4() {
		return netip.PrefixFrom(addr, 32), nil
	}
	p, err := netip.ParsePrefix(dst)
	if err != nil || !p.Addr().Is4() || p.Masked().String() != dst {
		return netip.Prefix{}, ErrIPOwnership
	}
	return p, nil
}

func verifyLinuxRouteInventory(raw []byte, address netip.Addr, routes []netip.Prefix) error {
	var records []linuxInventoryRoute
	if err := json.Unmarshal(raw, &records); err != nil || records == nil || len(records) != len(routes) {
		return ErrIPOwnership
	}
	expected := make(map[netip.Prefix]bool, len(routes))
	for _, p := range routes {
		expected[p] = true
	}
	for _, record := range records {
		prefix, err := linuxInventoryPrefix(record.Destination)
		if err != nil {
			return ErrIPOwnership
		}
		if !record.direct() || record.Source != address.String() || !expected[prefix] {
			return ErrIPOwnership
		}
		delete(expected, prefix)
	}
	return nil
}

func (l *linuxLink) routeDevice(p netip.Prefix) string {
	if p.Bits() == 32 {
		return l.peerDevice(p.Addr())
	}
	return l.name
}

func (l *linuxLink) routeInTable(ctx context.Context, action, table string, destination netip.Prefix) error {
	return l.ip(ctx, "-4", "route", action, "table", table, destination.String(),
		"dev", l.routeDevice(destination), "src", l.address.String(), "proto", linuxRouteProtocol, "scope", "link", "metric", "0")
}
