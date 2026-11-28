package underlay

import (
	"errors"
	"math/bits"
	"net"
	"net/netip"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

var tunnels sync.Map
var errNoInterface = errors.New("no underlay interface for destination")

// ExcludeTunnel is called after native adapter identity verification and
// before the first route is created. Cleanup releases this specific owner.
func ExcludeTunnel(luid uint64, guid windows.GUID) func() {
	key := winipcfg.LUID(luid)
	owner := &guid
	tunnels.Store(key, owner)
	return func() { tunnels.CompareAndDelete(key, owner) }
}

// A failed cleanup can outlive the adapter. Windows may then assign its
// LUID to a physical interface; only the verified GUID belongs to us.
func excludedTunnel(luid winipcfg.LUID, lookup func(winipcfg.LUID) (*windows.GUID, error)) (bool, error) {
	for {
		owner, ok := tunnels.Load(luid)
		if !ok {
			return false, nil
		}
		guid, err := lookup(luid)
		if err != nil {
			return false, err
		}
		if guid == nil {
			return false, errNoInterface
		}
		if *owner.(*windows.GUID) == *guid {
			return true, nil
		}
		if tunnels.CompareAndDelete(luid, owner) {
			return false, nil
		}
	}
}

func InterfaceIndex(addr netip.Addr) (uint32, error) {
	if !addr.Is4() {
		return 0, errNoInterface
	}
	var best uint32
	if err := windows.GetBestInterfaceEx(&windows.SockaddrInet4{Addr: addr.As4()}, &best); err != nil {
		return 0, err
	}
	luid, err := winipcfg.LUIDFromIndex(best)
	if err != nil {
		return 0, err
	}
	tunnel, err := excludedTunnel(luid, winipcfg.LUID.GUID)
	if err != nil {
		return 0, err
	}
	if !tunnel {
		return best, nil
	}
	rows, err := winipcfg.GetIPForwardTable2(windows.AF_INET)
	if err != nil {
		return 0, err
	}
	return bestOuterRoute(addr, rows, func(l winipcfg.LUID) (*winipcfg.MibIPInterfaceRow, error) {
		if excluded, err := excludedTunnel(l, winipcfg.LUID.GUID); err != nil || excluded {
			return nil, err
		}
		return l.IPInterface(windows.AF_INET)
	})
}

func bestOuterRoute(addr netip.Addr, rows []winipcfg.MibIPforwardRow2, iface func(winipcfg.LUID) (*winipcfg.MibIPInterfaceRow, error)) (uint32, error) {
	longest := -1
	var best uint32
	var cost uint64
	for _, r := range rows {
		prefix := r.DestinationPrefix.Prefix()
		if r.Loopback || r.ValidLifetime == 0 || !prefix.Contains(addr) || prefix.Bits() < longest {
			continue
		}
		i, err := iface(r.InterfaceLUID)
		if err != nil {
			return 0, err
		}
		if i == nil || !i.Connected || i.InterfaceIndex == 0 || i.InterfaceIndex != r.InterfaceIndex || i.InterfaceLUID != r.InterfaceLUID {
			continue
		}
		metric := uint64(i.Metric) + uint64(r.Metric)
		if prefix.Bits() > longest || metric < cost || metric == cost && r.InterfaceIndex < best {
			longest, cost, best = prefix.Bits(), metric, r.InterfaceIndex
		}
	}
	if best == 0 {
		return 0, errNoInterface
	}
	return best, nil
}

func Dialer(timeout time.Duration) *net.Dialer {
	return &net.Dialer{Timeout: timeout, Control: func(network, address string, conn syscall.RawConn) error {
		if network != "tcp4" && network != "udp4" {
			return nil
		}
		target, err := netip.ParseAddrPort(address)
		if err != nil {
			return err
		}
		ip := target.Addr().Unmap()
		if !ip.Is4() || ip.IsLoopback() {
			return nil
		}
		index, err := InterfaceIndex(ip)
		if err != nil {
			return err
		}
		var sockErr error
		if err := conn.Control(func(fd uintptr) {
			const ipUnicastIf = 31
			sockErr = windows.SetsockoptInt(windows.Handle(fd), windows.IPPROTO_IP, ipUnicastIf, int(bits.ReverseBytes32(index)))
		}); err != nil {
			return err
		}
		return sockErr
	}}
}
