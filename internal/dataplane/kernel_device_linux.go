package dataplane

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/cilium/ebpf/link"
	"github.com/endless-net/client/api/transport"
	"github.com/mdlayher/genetlink"
	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

// linuxPeerDevice has no host route or external listener. Each file descriptor
// remains attached to the candidate's anonymous namespace when used elsewhere.
type linuxPeerDevice struct {
	outputFilters []link.Link
	filter        link.Link
	plain         *os.File
	wire          *net.UDPConn
	endpoint      netip.AddrPort
}

func (d *linuxPeerDevice) Close() {
	if d.plain != nil {
		_ = d.plain.Close()
	}
	for _, l := range d.outputFilters {
		_ = l.Close()
	}
	if d.filter != nil {
		_ = d.filter.Close()
	}
	if d.wire != nil {
		_ = d.wire.Close()
	}
}

func openLinuxPeerDevice(ctx context.Context, cfg directCipherConfig) (*linuxPeerDevice, error) {
	if ctx.Err() != nil || cfg.mtu < 576 || cfg.mtu > 65535 || cfg.clock == nil {
		return nil, ErrConfiguration
	}
	sample, err := cfg.clock.Sample()
	if err != nil || cfg.assignment.permission(sample.Wall, true) != nil {
		return nil, ErrNoAuthority
	}
	type result struct {
		device *linuxPeerDevice
		err    error
	}
	done := make(chan result, 1)
	go func() {
		runtime.LockOSThread()
		if err := unix.Unshare(unix.CLONE_NEWNET); err != nil {
			runtime.UnlockOSThread()
			if errors.Is(err, unix.EPERM) || errors.Is(err, unix.ENOSYS) {
				err = errKernelUnavailable
			}
			done <- result{err: err}
			return
		}
		// Returning while locked retires this thread. No other Go goroutine
		// can run on it in the candidate's namespace.
		device, err := configureLinuxPeerDevice(ctx, cfg)
		done <- result{device, err}
	}()
	r := <-done
	return r.device, r.err
}

func configureLinuxPeerDevice(ctx context.Context, cfg directCipherConfig) (_ *linuxPeerDevice, err error) {
	const name = "en-peer"
	engine := cfg.assignment.offer.Profile.Engine
	if err := exec.CommandContext(ctx, linuxIP, "link", "set", "lo", "up").Run(); err != nil {
		return nil, err
	}
	device := &linuxPeerDevice{}
	defer func() {
		if err != nil {
			device.Close()
		}
	}()
	device.wire, err = net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return nil, err
	}
	conn, err := genetlink.Dial(nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	iface, err := prepareLinuxPeerLink(ctx, conn, cfg, device.wire.LocalAddr().(*net.UDPAddr).AddrPort(), name, "")
	if err != nil {
		return nil, err
	}
	if err := exec.CommandContext(ctx, linuxIP, "link", "set", "dev", name, "mtu", strconv.Itoa(cfg.mtu), "up").Run(); err != nil {
		return nil, err
	}
	port, err := kernelListenPort(conn, engine, iface.Index)
	if err != nil {
		return nil, err
	}
	device.endpoint = netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), port)
	var padding uint16
	if engine == transport.AmneziaWG {
		padding = cfg.assignment.offer.Profile.AWG.Padding[3]
	}
	device.filter, err = attachLinuxPeerIngress(iface.Index, padding, device.wire.LocalAddr().(*net.UDPAddr).AddrPort().Port(), port)
	if err != nil {
		return nil, err
	}
	if engine == transport.AmneziaWG {
		if err := device.attachOutput(iface.Index, device.wire.LocalAddr().(*net.UDPAddr).AddrPort().Port(), port); err != nil {
			return nil, err
		}
	}
	// Inject network-layer packets directly into this NOARP device. A packet
	// socket also receives decrypted IPv4 before the isolated IP stack routes it.
	var protocol [2]byte
	binary.BigEndian.PutUint16(protocol[:], unix.ETH_P_IP)
	ipv4 := binary.NativeEndian.Uint16(protocol[:])
	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	device.plain = os.NewFile(uintptr(fd), "direct-peer-ip")
	if err := unix.SetsockoptInt(fd, unix.SOL_PACKET, unix.PACKET_IGNORE_OUTGOING, 1); err != nil {
		return nil, err
	}
	if err := unix.Bind(fd, &unix.SockaddrLinklayer{Protocol: ipv4, Ifindex: iface.Index}); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return device, nil
}

// Prepare the device while it is down. Interface activation, service routes
// and application routes belong to the caller after policy installation.
func prepareLinuxPeerLink(ctx context.Context, conn *genetlink.Conn, cfg directCipherConfig, endpoint netip.AddrPort, name, owner string) (*net.Interface, error) {
	if conn == nil || cfg.clock == nil {
		return nil, ErrConfiguration
	}
	sample, err := cfg.clock.Sample()
	if err != nil || ctx.Err() != nil || cfg.assignment.permission(sample.Wall, true) != nil {
		return nil, ErrNoAuthority
	}
	if err := createLinuxKernelLink(ctx, name, cfg.assignment.offer.Profile.Engine, owner); err != nil {
		return nil, err
	}
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return nil, err
	}
	if err := configureKernelWGPeer(ctx, conn, iface.Index, endpoint, cfg); err != nil {
		// Even when preparation was cancelled, retire the newly allocated link.
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		if removeErr := deleteLinuxPeerIndex(cleanup, iface.Index); removeErr != nil {
			return nil, errors.Join(err, removeErr)
		}
		return nil, err
	}
	return iface, nil
}

var linuxPeerLinkName = regexp.MustCompile(`^ep[0-9a-f]{13}$`)

// Both markers are set by the same exclusive RTM_NEWLINK. WireGuard ignores
// IFLA_IFALIAS during creation on kernels which accept it on a later change.
func linuxPeerScope(owner string) (prefix string, group uint32) {
	digest := sha256.Sum256([]byte("endlessnet:peer:" + owner))
	return "ep" + hex.EncodeToString(digest[:4]), binary.BigEndian.Uint32(digest[4:8])&0x3fffffff | 0x40000000
}

// RTM_NEWLINK reports an unsupported link kind as EOPNOTSUPP. Keep other
// failures distinct so an invalid request or a name collision stops preparation.
func createLinuxKernelLink(ctx context.Context, name, engine, owner string) error {
	if engine != transport.WireGuard && engine != transport.AmneziaWG {
		return ErrConfiguration
	}
	attrs := netlink.NewAttributeEncoder()
	attrs.String(unix.IFLA_IFNAME, name)
	if owner != "" {
		prefix, group := linuxPeerScope(owner)
		if !strings.HasPrefix(name, prefix) || !linuxPeerLinkName.MatchString(name) {
			return ErrConfiguration
		}
		attrs.Uint32(unix.IFLA_GROUP, group)
	}
	attrs.Nested(unix.IFLA_LINKINFO, func(info *netlink.AttributeEncoder) error {
		info.String(unix.IFLA_INFO_KIND, engine)
		return nil
	})
	data, err := attrs.Encode()
	if err != nil {
		return err
	}
	conn, err := netlink.Dial(unix.NETLINK_ROUTE, nil)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	_, err = conn.Execute(netlink.Message{
		Header: netlink.Header{Type: unix.RTM_NEWLINK, Flags: netlink.Request | netlink.Acknowledge | netlink.Create | netlink.Excl},
		Data:   append(make([]byte, unix.SizeofIfInfomsg), data...),
	})
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.EPERM) {
		return errKernelUnavailable
	}
	if err != nil || owner == "" {
		return err
	}
	// A crash before the alias is set leaves only an unconfigured, down
	// interface. The atomic group and name identify that intermediate state.
	return exec.CommandContext(ctx, linuxIP, "link", "set", "dev", name, "alias", "endlessnet:peer:"+owner).Run()
}
