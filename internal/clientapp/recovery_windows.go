//go:build windows

package clientapp

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/dataplane"
	"github.com/endless-net/client/internal/state"
	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

var recoveryPowerTargets sync.Map
var recoveryPowerSequence atomic.Uintptr
var recoveryPowerCallback = windows.NewCallback(func(id, kind, _ uintptr) uintptr {
	// PBT_APMRESUMEAUTOMATIC is delivered for every resume, including unattended
	// wake. Ignore RESUMESUSPEND to avoid a second attempt for the same wake.
	if kind == 0x12 {
		if value, ok := recoveryPowerTargets.Load(id); ok {
			w := value.(*ConnectionWorker)
			v := w.Observe()
			w.Recover(v.Profile, v.Generation)
		}
	}
	return 0
})

// underlaySnapshot reads only default IPv4 routes and addresses on their NICs.
// Client's split /32 overlay routes/adapter never participate, so Restore and
// cleanup cannot trigger a self-sustaining recovery loop. Nothing is logged.
func underlaySnapshot(maxRows, maxInterfaces int) (string, error) {
	routes, err := winipcfg.GetIPForwardTable2(windows.AF_INET)
	if err != nil || len(routes) > maxRows {
		return "", ErrConnectionWorker
	}
	parts, indices := underlayRoutes(routes)
	if len(indices) > maxInterfaces {
		return "", ErrConnectionWorker
	}
	for index := range indices {
		nic, err := net.InterfaceByIndex(int(index))
		if err != nil {
			return "", ErrConnectionWorker
		}
		addresses, err := nic.Addrs()
		if err != nil || len(addresses) > maxRows {
			return "", ErrConnectionWorker
		}
		parts = append(parts, fmt.Sprintf("%d/%d/%d", index, nic.Flags, nic.MTU))
		for _, address := range addresses {
			if ip, _, err := net.ParseCIDR(address.String()); err == nil && ip.To4() != nil {
				parts = append(parts, fmt.Sprintf("%d/%s", index, address))
			}
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, "|"), nil
}

func underlayRoutes(routes []winipcfg.MibIPforwardRow2) ([]string, map[uint32]bool) {
	indices := make(map[uint32]bool)
	var parts []string
	for _, route := range routes {
		prefix := route.DestinationPrefix.Prefix()
		if prefix.IsValid() && prefix.Bits() == 0 && !route.Loopback {
			indices[route.InterfaceIndex] = true
			parts = append(parts, fmt.Sprintf("%d/%s/%d", route.InterfaceIndex, route.NextHop.Addr(), route.Metric))
		}
	}
	return parts, indices
}

// Select the node's registration endpoint separately from the transport
// advertisement, which contains all prepared LAN addresses.
func underlayEndpoint(configured netip.AddrPort, maxRows, maxInterfaces int) (netip.AddrPort, error) {
	routes, err := winipcfg.GetIPForwardTable2(windows.AF_INET)
	if err != nil || len(routes) > maxRows {
		return netip.AddrPort{}, ErrConnectionWorker
	}
	_, indices := underlayRoutes(routes)
	if len(indices) > maxInterfaces {
		return netip.AddrPort{}, ErrConnectionWorker
	}
	addresses, err := dataplane.LANIPv4Interfaces()
	if err != nil || len(addresses) > maxRows {
		return netip.AddrPort{}, ErrConnectionWorker
	}
	if _, ok := addresses[configured.Addr()]; ok {
		return configured, nil
	}
	var candidates, connected []netip.Addr
	for address, index := range addresses {
		connected = append(connected, address)
		if indices[index] {
			candidates = append(candidates, address)
		}
	}
	if len(candidates) == 0 {
		candidates = connected
	}
	return selectUnderlayEndpoint(configured, candidates)
}

func selectUnderlayEndpoint(configured netip.AddrPort, candidates []netip.Addr) (netip.AddrPort, error) {
	var first netip.Addr
	for _, address := range candidates {
		if !address.Is4() || !address.IsGlobalUnicast() {
			continue
		}
		if address == configured.Addr() {
			return configured, nil
		}
		if !first.IsValid() || address.Less(first) {
			first = address
		}
	}
	if !first.IsValid() {
		return netip.AddrPort{}, connectivity.FailurePath
	}
	return netip.AddrPortFrom(first, configured.Port()), nil
}

// RunWindows owns read-only native event registration for the worker lifetime.
// It does not install services, mutate routes, or perform a network probe.
func (w *ConnectionWorker) RunWindows(ctx context.Context, maxRows, maxInterfaces int) error {
	if maxRows <= 0 || maxInterfaces <= 0 {
		return ErrConnectionWorker
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	power := windows.NewLazySystemDLL("powrprof.dll")
	register := power.NewProc("PowerRegisterSuspendResumeNotification")
	unregister := power.NewProc("PowerUnregisterSuspendResumeNotification")
	if register.Find() != nil || unregister.Find() != nil {
		return ErrConnectionWorker
	}
	id := recoveryPowerSequence.Add(1)
	if id == 0 {
		return ErrConnectionWorker
	}
	recoveryPowerTargets.Store(id, w)
	defer recoveryPowerTargets.Delete(id)
	parameters := struct{ Callback, Context uintptr }{recoveryPowerCallback, id}
	var handle windows.Handle
	result, _, _ := register.Call(2, uintptr(unsafe.Pointer(&parameters)), uintptr(unsafe.Pointer(&handle)))
	if result != 0 || handle == 0 {
		return ErrConnectionWorker
	}
	defer func() {
		_, _, _ = unregister.Call(uintptr(handle))
		runtime.KeepAlive(parameters)
	}()
	previous, err := underlaySnapshot(maxRows, maxInterfaces)
	if err != nil {
		return err
	}
	run, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.Run(run) }()
	poll := time.NewTicker(w.config.PollInterval)
	defer poll.Stop()
	for {
		select {
		case err := <-done:
			return err
		case <-poll.C:
			current, err := underlaySnapshot(maxRows, maxInterfaces)
			if err != nil {
				// A changing native table is unknown, never a fabricated event.
				continue
			}
			if current != previous {
				previous = current
				v := w.Observe()
				if v.Intent == state.IntentConnected {
					w.Recover(v.Profile, v.Generation)
				}
			}
		}
	}
}
