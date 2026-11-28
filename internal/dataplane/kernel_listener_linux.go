package dataplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os/exec"
	"slices"
	"strconv"

	"github.com/endless-net/client/internal/state"
	"github.com/google/uuid"
	"github.com/mdlayher/genetlink"
	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

// A separate base chain survives every map/ACL refresh. The session's durable
// table receipt covers it if the owner dies before it can remove the device.
type linuxKernelListener struct {
	receipt      state.LinuxNetworkReceipt
	name         string
	index        int
	port         uint16
	installation uuid.UUID
}

// The caller owns the down device. If a guard is returned together with an
// error, destroy the device before retiring the guard, including on timeout.
func startLinuxKernelListener(ctx context.Context, conn *genetlink.Conn, iface *net.Interface, id linuxFilterIdentity, broker *net.UDPConn) (*linuxKernelListener, error) {
	if conn == nil || iface == nil || broker == nil || id.mark == 0 {
		return nil, ErrConfiguration
	}
	endpoint := broker.LocalAddr().(*net.UDPAddr).AddrPort()
	if endpoint.Addr() != netip.MustParseAddr("127.0.0.1") || endpoint.Port() == 0 {
		return nil, ErrConfiguration
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := broker.SyscallConn()
	if err != nil {
		return nil, err
	}
	var mark int
	var socketErr error
	if err := raw.Control(func(fd uintptr) {
		mark, socketErr = unix.GetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK)
	}); err != nil {
		return nil, err
	}
	if socketErr != nil {
		return nil, socketErr
	}
	if uint32(mark) != id.mark {
		return nil, ErrConfiguration
	}
	link, err := ownedLinuxPeer(ctx, iface, id.owner)
	if err != nil {
		return nil, err
	}
	if slices.Contains(link.Flags, "UP") {
		return nil, ErrAdapterOwnership
	}
	receipt := state.LinuxNetworkReceipt{Table: id.table, Owner: id.owner}
	handle, err := linuxNFTLookup(ctx, receipt)
	if err != nil || handle == 0 {
		return nil, ErrPolicyOwnership
	}
	reserve, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		return nil, err
	}
	defer reserve.Close()
	port := reserve.LocalAddr().(*net.UDPAddr).AddrPort().Port()
	v6, err := net.ListenUDP("udp6", &net.UDPAddr{IP: net.IPv6zero, Port: int(port)})
	if err != nil && !errors.Is(err, unix.EAFNOSUPPORT) {
		return nil, err
	}
	if v6 != nil {
		defer v6.Close()
	}
	installation, err := uuid.NewRandom()
	if err != nil {
		return nil, err
	}
	guard := &linuxKernelListener{installation: installation, receipt: receipt, name: iface.Name, index: iface.Index, port: port}
	chain := "inet " + receipt.Table + " " + guard.name
	rules := fmt.Sprintf("create chain %s { type filter hook input priority -31; policy accept; comment %q; }\n", chain, guard.comment()) +
		fmt.Sprintf("add rule %s iifname %q ip saddr 127.0.0.1 ip daddr 127.0.0.1 meta mark %d udp sport %d udp dport %d accept\n", chain, "lo", id.mark, endpoint.Port(), port) +
		fmt.Sprintf("add rule %s udp dport %d counter drop\n", chain, port)
	if err := linuxNFTApply(ctx, rules); err != nil {
		return guard, err
	}
	family, err := conn.GetFamily(link.Info.Kind)
	if err != nil {
		return guard, err
	}
	enc := netlink.NewAttributeEncoder()
	enc.Uint32(unix.WGDEVICE_A_IFINDEX, uint32(iface.Index))
	enc.Uint16(unix.WGDEVICE_A_LISTEN_PORT, port)
	data, err := enc.Encode()
	if err != nil {
		return guard, err
	}
	_, err = conn.Execute(genetlink.Message{Header: genetlink.Header{Command: unix.WG_CMD_SET_DEVICE, Version: 1}, Data: data}, family.ID, netlink.Request|netlink.Acknowledge)
	if err != nil {
		return guard, err
	}
	if v6 != nil {
		if err := v6.Close(); err != nil {
			return guard, err
		}
	}
	if err := reserve.Close(); err != nil {
		return guard, err
	}
	if err := exec.CommandContext(ctx, linuxIP, "link", "set", "dev", iface.Name, "up").Run(); err != nil {
		return guard, err
	}
	actual, err := kernelListenPort(conn, link.Info.Kind, iface.Index)
	if err != nil {
		return guard, err
	}
	if actual != port {
		return guard, ErrAdapterOwnership
	}
	return guard, nil
}

func (g *linuxKernelListener) comment() string {
	return fmt.Sprintf("endlessnet:%s:%d:%d:%s", g.receipt.Owner, g.index, g.port, g.installation)
}

func (g *linuxKernelListener) retire(ctx context.Context) error {
	devices, err := net.Interfaces()
	if err != nil {
		return ErrAdapterOwnership
	}
	for _, d := range devices {
		if d.Index == g.index {
			return ErrAdapterOwnership
		}
	}
	table, err := linuxNFTLookup(ctx, g.receipt)
	if err != nil || table == 0 {
		return err
	}
	raw, err := exec.CommandContext(ctx, linuxNFT, "-a", "-j", "list", "chains", "inet").Output()
	if err != nil {
		return ErrPolicyOwnership
	}
	var response struct {
		NFTables []struct {
			Chain *struct {
				Table, Name, Comment string
				Handle               uint64
			}
		}
	}
	if err := json.Unmarshal(raw, &response); err != nil || len(response.NFTables) == 0 {
		return ErrPolicyOwnership
	}
	for _, row := range response.NFTables {
		ch := row.Chain
		if ch == nil || ch.Table != g.receipt.Table || ch.Name != g.name {
			continue
		}
		if ch.Handle == 0 || ch.Comment != g.comment() {
			return ErrPolicyOwnership
		}
		return linuxNFTApply(ctx, "delete chain inet "+g.receipt.Table+" handle "+strconv.FormatUint(ch.Handle, 10)+"\n")
	}
	return nil
}
