package client

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)
type commandInputRunner func(ctx context.Context, input string, name string, args ...string) ([]byte, error)

type WireGuardInspectOptions struct {
	Interface    string
	RouteTargets []string
	WGCommand    string
	IPCommand    string
	Runner       CommandRunner
}

type WireGuardInspection struct {
	OK         bool                       `json:"ok"`
	Interface  string                     `json:"interface"`
	MTU        int                        `json:"mtu,omitempty"`
	ListenPort int                        `json:"listen_port,omitempty"`
	PeerCount  int                        `json:"peer_count"`
	Peers      []WireGuardPeerInspection  `json:"peers"`
	Routes     []WireGuardRouteInspection `json:"routes,omitempty"`
	Error      string                     `json:"error,omitempty"`
}

type WireGuardPeerInspection struct {
	PublicKey                  string   `json:"public_key"`
	Endpoint                   string   `json:"endpoint,omitempty"`
	AllowedIPs                 []string `json:"allowed_ips"`
	LatestHandshakeUnix        int64    `json:"latest_handshake_unix,omitempty"`
	TransferRXBytes            uint64   `json:"transfer_rx_bytes"`
	TransferTXBytes            uint64   `json:"transfer_tx_bytes"`
	PersistentKeepaliveSeconds int      `json:"persistent_keepalive_seconds,omitempty"`
}

type WireGuardRouteInspection struct {
	Target        string `json:"target"`
	Interface     string `json:"interface,omitempty"`
	UsesInterface bool   `json:"uses_interface"`
	Error         string `json:"error,omitempty"`
}

func InspectWireGuard(ctx context.Context, opts WireGuardInspectOptions) WireGuardInspection {
	iface := strings.TrimSpace(opts.Interface)
	inspection := WireGuardInspection{
		Interface: iface,
		Peers:     []WireGuardPeerInspection{},
		Routes:    []WireGuardRouteInspection{},
	}
	if iface == "" {
		inspection.Error = "wireguard interface is required"
		return inspection
	}
	runner := opts.Runner
	if runner == nil {
		runner = runCommand
	}
	wgCommand := strings.TrimSpace(opts.WGCommand)
	if wgCommand == "" {
		wgCommand = "wg"
	}
	ipCommand := strings.TrimSpace(opts.IPCommand)
	if ipCommand == "" {
		ipCommand = "ip"
	}
	dump, err := runner(ctx, wgCommand, "show", iface, "dump")
	if err != nil {
		inspection.Error = commandError(err, dump)
		return inspection
	}
	if err := parseWireGuardDump(&inspection, string(dump)); err != nil {
		inspection.Error = err.Error()
		return inspection
	}
	inspection.OK = true
	if link, err := runner(ctx, ipCommand, "-o", "link", "show", "dev", iface); err == nil {
		inspection.MTU = parseLinkMTU(string(link))
	}
	for _, target := range opts.RouteTargets {
		target = strings.TrimSpace(target)
		if target == "" {
			continue
		}
		inspection.Routes = append(inspection.Routes, inspectRoute(ctx, runner, ipCommand, iface, target))
	}
	return inspection
}

func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}

func parseWireGuardDump(inspection *WireGuardInspection, dump string) error {
	lines := strings.Split(strings.TrimSpace(dump), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		return fmt.Errorf("wireguard dump is empty")
	}
	ifaceFields := strings.Fields(lines[0])
	if len(ifaceFields) < 3 {
		return fmt.Errorf("wireguard interface dump has %d fields, want at least 3", len(ifaceFields))
	}
	if listenPort, err := strconv.Atoi(ifaceFields[2]); err == nil && listenPort > 0 {
		inspection.ListenPort = listenPort
	}
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 8 {
			return fmt.Errorf("wireguard peer dump has %d fields, want 8", len(fields))
		}
		peer := WireGuardPeerInspection{
			PublicKey:  fields[0],
			AllowedIPs: splitAllowedIPs(fields[3]),
		}
		if fields[2] != "(none)" {
			peer.Endpoint = fields[2]
		}
		if latestHandshake, err := strconv.ParseInt(fields[4], 10, 64); err == nil && latestHandshake > 0 {
			peer.LatestHandshakeUnix = latestHandshake
		}
		if rx, err := strconv.ParseUint(fields[5], 10, 64); err == nil {
			peer.TransferRXBytes = rx
		}
		if tx, err := strconv.ParseUint(fields[6], 10, 64); err == nil {
			peer.TransferTXBytes = tx
		}
		if keepalive, err := strconv.Atoi(fields[7]); err == nil && keepalive > 0 {
			peer.PersistentKeepaliveSeconds = keepalive
		}
		inspection.Peers = append(inspection.Peers, peer)
	}
	inspection.PeerCount = len(inspection.Peers)
	return nil
}

func splitAllowedIPs(raw string) []string {
	if strings.TrimSpace(raw) == "" || raw == "(none)" {
		return []string{}
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func parseLinkMTU(raw string) int {
	fields := strings.Fields(raw)
	for i, field := range fields {
		if field != "mtu" || i+1 >= len(fields) {
			continue
		}
		mtu, err := strconv.Atoi(fields[i+1])
		if err == nil {
			return mtu
		}
	}
	return 0
}

func inspectRoute(ctx context.Context, runner CommandRunner, ipCommand, iface, target string) WireGuardRouteInspection {
	out, err := runner(ctx, ipCommand, "route", "get", target)
	route := WireGuardRouteInspection{Target: target}
	if err != nil {
		route.Error = commandError(err, out)
		return route
	}
	route.Interface = parseRouteInterface(string(out))
	route.UsesInterface = route.Interface == iface
	return route
}

func parseRouteInterface(raw string) string {
	fields := strings.Fields(raw)
	for i, field := range fields {
		if field == "dev" && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	return ""
}

func commandError(err error, output []byte) string {
	message := strings.TrimSpace(string(output))
	if message == "" {
		return err.Error()
	}
	return message
}
