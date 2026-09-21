package client

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/netip"
	"reflect"
	"strconv"
	"strings"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
)

var errNativeExitCleared = errors.New("cleared native exit scope is not observed")

func (n *nativeExitExecutor) observeCleared(ctx context.Context, cfg Config) (*ipc.ExitNodeStatus, error) {
	if cfg.RPCState == nil || !cfg.RPCState.ExitCleared.bound(cfg, n.engine.opts.Interface) {
		return nil, errNativeExitCleared
	}
	r := cfg.RPCState.ExitCleared
	// Reconstruct only the durable address; this does not mutate native state.
	guard, err := n.guard(r.Protection)
	if err != nil {
		return nil, errNativeExitCleared
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	e := n.engine
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.opts.Interface != guard.interfaceName || e.exitGuard != nil || e.exitSelection != nil || e.exitFilter != nil {
		return nil, errNativeExitCleared
	}
	live := e.configured || e.device != nil || e.router != nil || e.tun != nil
	if live {
		if !e.configured || e.device == nil || e.router == nil || e.tun == nil || e.interface_ != guard.interfaceName || e.pathMap.Node.ID != cfg.NodeID || e.pathMap.Network.ID != cfg.NetworkID || cfg.CachedMap == nil || cfg.CachedMap.MapSignature == nil || e.pathMap.MapSignature == nil || cfg.CachedMap.MapSignature.PayloadHash != e.pathMap.MapSignature.PayloadHash || e.routerCfg.Interface != guard.interfaceName || e.routerCfg.RouteTable != cfg.WireGuardRouteTable || e.routerCfg.FirewallMark != 0 {
			return nil, errNativeExitCleared
		}
		for _, route := range e.routerCfg.Routes {
			if !route.IsValid() || route.Bits() == 0 {
				return nil, errNativeExitCleared
			}
		}
		raw, err := e.device.IpcGet()
		if err != nil || nativeExitClearedUAPI(e.uapi, raw, e.pathMap.Node.PublicKey) != nil {
			return nil, errNativeExitCleared
		}
	}
	if err := guard.ObserveAbsent(ctx); err != nil {
		return nil, errors.Join(errNativeExitCleared, ctx.Err())
	}
	run := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return guard.run(ctx, "", name, args...)
	}
	absent, err := exitPolicyRulesAbsent(ctx, guard.mark, run)
	if err != nil || !absent {
		return nil, errors.Join(errNativeExitCleared, ctx.Err())
	}
	for _, family := range []string{"-4", "-6"} {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		raw, err := run(ctx, "ip", "-j", "-N", family, "route", "show", "table", "all")
		if err != nil || !nativeExitDefaultsAbsent(raw, guard.mark, guard.interfaceName) {
			return nil, errors.Join(errNativeExitCleared, ctx.Err())
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nativeExitClearStatus(r.ActiveProfileID, false), nil
}

func nativeExitDefaultsAbsent(raw []byte, table uint32, device string) bool {
	if len(raw) == 0 || len(raw) > 1<<20 || !validExitPolicyTable(table) || device == "" {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if _, ok := exitGuardJSONValue(decoder, 0); !ok {
		return false
	}
	var rows []map[string]json.RawMessage
	if json.Unmarshal(raw, &rows) != nil || rows == nil || len(rows) > 4096 {
		return false
	}
	for _, row := range rows {
		var dst string
		if json.Unmarshal(row["dst"], &dst) != nil || dst == "" {
			return false
		}
		isDefault := dst == "default"
		if !isDefault {
			prefix, err := netip.ParsePrefix(dst)
			if err != nil {
				if _, err := netip.ParseAddr(dst); err != nil {
					return false
				}
			} else {
				isDefault = prefix.Bits() == 0
			}
		}
		if !isDefault {
			continue
		}
		id := uint64(254)
		if value, exists := row["table"]; exists {
			var label string
			if json.Unmarshal(value, &label) != nil {
				label = string(value)
			}
			if label == "main" {
				label = "254"
			}
			if label == "" || strings.Trim(label, "0123456789") != "" {
				return false
			}
			var err error
			id, err = strconv.ParseUint(label, 10, 32)
			if err != nil || id == 0 {
				return false
			}
		}
		var dev string
		if value, exists := row["dev"]; exists && json.Unmarshal(value, &dev) != nil {
			return false
		}
		if uint32(id) == table || dev == device {
			return false
		}
		// Multipath/nexthop indirection needs a separate nexthop-object proof.
		// Do not infer that an opaque default cannot reference this interface.
		if _, exists := row["multipath"]; exists {
			return false
		}
		if _, exists := row["nexthops"]; exists {
			return false
		}
		if _, exists := row["nhid"]; exists {
			return false
		}
	}
	return true
}

func nativeExitClearedUAPI(expected, actual, localPublic string) error {
	want, err := parseNativeExitUAPI(expected)
	if err != nil {
		return errNativeExitCleared
	}
	// Pinned wireguard-go device/uapi.go IpcGetOperation omits fwmark exactly
	// when zero. Committed set requests always contain an explicit fwmark.
	markPresent := false
	for _, line := range strings.Split(actual, "\n") {
		markPresent = markPresent || strings.HasPrefix(line, "fwmark=")
	}
	if !markPresent {
		actual = "fwmark=0\n" + actual
	}
	live, err := parseNativeExitUAPI(actual)
	if err != nil || localPublic == "" || want.localPublic != localPublic || live.localPublic != localPublic || want.mark != 0 || live.mark != 0 || len(want.peers) != len(live.peers) {
		return errNativeExitCleared
	}
	for key, peer := range live.peers {
		previous, exists := want.peers[key]
		if !exists || !peer.pskSeen || peer.endpoint != previous.endpoint || !reflect.DeepEqual(peer.routes, previous.routes) || subtle.ConstantTimeCompare(peer.pskDigest[:], previous.pskDigest[:]) != 1 {
			return errNativeExitCleared
		}
		for prefix := range peer.routes {
			if prefix.Bits() == 0 {
				return errNativeExitCleared
			}
		}
	}
	return nil
}
