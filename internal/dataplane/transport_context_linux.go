package dataplane

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"net"
	"net/netip"
	"os"
	"sort"
	"strconv"
	"strings"
)

// An unresolved gateway disables the optional hint. Never solicit traffic merely
// to identify the underlay. Procfs reports routes in the caller's network namespace.
func linuxTransportContext(ctx context.Context, key []byte) (string, error) {
	read := func(path string) (string, error) {
		const limit = 256 << 10
		f, err := os.Open(path)
		if err != nil {
			return "", err
		}
		defer func() { _ = f.Close() }()
		b, err := io.ReadAll(io.LimitReader(f, limit+1))
		if err != nil || len(b) > limit || ctx.Err() != nil {
			return "", ErrNoAuthority
		}
		return string(b), nil
	}
	routes, err := read("/proc/net/route")
	if err != nil {
		return "", err
	}
	arp, err := read("/proc/net/arp")
	if err != nil {
		return "", err
	}
	return linuxContextDigest(key, routes, arp, net.InterfaceByName)
}

func linuxContextDigest(key []byte, routes, arp string, lookup func(string) (*net.Interface, error)) (string, error) {
	if len(key) != 32 || bytes.Equal(key, make([]byte, 32)) {
		return "", ErrNoAuthority
	}
	type gateway struct{ iface, addr string }
	required := make(map[gateway]bool)
	for _, line := range strings.Split(strings.TrimSpace(routes), "\n")[1:] {
		f := strings.Fields(line)
		if len(f) < 8 {
			return "", ErrNoAuthority
		}
		if f[1] != "00000000" || f[7] != "00000000" {
			continue
		}
		flags, err := strconv.ParseUint(f[3], 16, 32)
		if err != nil {
			return "", ErrNoAuthority
		}
		if flags&1 == 0 {
			continue
		}
		raw, err := strconv.ParseUint(f[2], 16, 32)
		if flags&2 == 0 || flags&0x200 != 0 || err != nil || raw == 0 {
			return "", ErrNoAuthority
		}
		var ip [4]byte
		binary.NativeEndian.PutUint32(ip[:], uint32(raw))
		required[gateway{f[0], netip.AddrFrom4(ip).String()}] = false
	}
	if len(required) == 0 {
		return "", ErrNoAuthority
	}
	validMAC := func(mac net.HardwareAddr) bool {
		return len(mac) == 6 && mac[0]&1 == 0 && !bytes.Equal(mac, make([]byte, 6))
	}
	var parts []string
	for _, line := range strings.Split(strings.TrimSpace(arp), "\n")[1:] {
		f := strings.Fields(line)
		if len(f) != 6 {
			return "", ErrNoAuthority
		}
		g := gateway{f[5], f[0]}
		seen, ok := required[g]
		if !ok {
			continue
		}
		flags, err := strconv.ParseUint(f[2], 0, 32)
		if seen || err != nil || flags&2 == 0 || f[1] != "0x1" {
			return "", ErrNoAuthority
		}
		remote, err := net.ParseMAC(f[3])
		if err != nil || !validMAC(remote) {
			return "", ErrNoAuthority
		}
		local, err := lookup(g.iface)
		if err != nil || local == nil || local.Flags&net.FlagUp == 0 || !validMAC(local.HardwareAddr) {
			return "", ErrNoAuthority
		}
		required[g] = true
		parts = append(parts, local.HardwareAddr.String()+"/"+g.addr+"/"+remote.String())
	}
	if len(parts) != len(required) {
		return "", ErrNoAuthority
	}
	sort.Strings(parts)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("endlessnet/client/linux-transport-context/v1\x00"))
	for _, p := range parts {
		mac.Write([]byte(p))
		mac.Write([]byte{0})
	}
	return hex.EncodeToString(mac.Sum(nil)), nil
}
