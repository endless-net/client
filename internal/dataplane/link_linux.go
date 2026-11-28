package dataplane

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/endless-net/client/api/transport"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

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
	routes  []netip.Addr
	direct  map[netip.Addr]net.Interface
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

func (l *linuxLink) configure(ctx context.Context, address netip.Addr, peers []netip.Addr) error {
	if !address.Is4() || !address.IsGlobalUnicast() {
		return ErrConfiguration
	}
	if _, err := l.checkOwnership(); err != nil {
		return err
	}
	if l.address.IsValid() && l.address != address {
		return ErrIPConflict
	}
	for _, peer := range peers {
		if !peer.Is4() || !peer.IsGlobalUnicast() || peer == address {
			return ErrConfiguration
		}
		if !slices.Contains(l.routes, peer) {
			existing, err := exec.CommandContext(ctx, linuxIP, "-4", "route", "show", "table", "all", "exact", peer.String()+"/32").Output()
			if err != nil {
				return ErrConfiguration
			}
			if len(existing) != 0 {
				return ErrIPConflict
			}
		}
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
		if !slices.Contains(peers, old) {
			if err := l.route(ctx, "del", old); err != nil {
				return err
			}
			delete(l.direct, old)
			l.routes = slices.DeleteFunc(l.routes, func(candidate netip.Addr) bool { return candidate == old })
		}
	}
	for _, peer := range peers {
		if !slices.Contains(l.routes, peer) {
			if err := l.route(ctx, "add", peer); err != nil {
				return err
			}
			l.routes = append(l.routes, peer)
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
	seen := make(map[netip.Addr]bool, len(l.routes))
	for _, record := range records {
		if !devices[record.Device] {
			continue
		}
		address, err := netip.ParseAddr(record.Destination)
		if err != nil {
			prefix, prefixErr := netip.ParsePrefix(record.Destination)
			if prefixErr != nil || prefix.Bits() != 32 {
				return ErrIPOwnership
			}
			address = prefix.Addr()
		}
		if record.Device != l.peerDevice(address) || record.Source != l.address.String() || seen[address] || !slices.Contains(l.routes, address) {
			return ErrIPOwnership
		}
		seen[address] = true
	}
	if len(seen) != len(l.routes) {
		return ErrIPOwnership
	}
	return ctx.Err()
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
	if !slices.Contains(l.routes, peer) {
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

func (l *linuxLink) route(ctx context.Context, action string, peer netip.Addr) error {
	return l.ip(ctx, "-4", "route", action, "table", "main", peer.String()+"/32",
		"dev", l.peerDevice(peer), "src", l.address.String(), "proto", linuxRouteProtocol)
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
