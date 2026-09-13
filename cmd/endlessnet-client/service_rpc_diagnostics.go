package main

import (
	"context"
	"strings"
	"time"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
	"google.golang.org/protobuf/types/known/durationpb"
)

func agentRPCDiagnostics(opts agentIPCOptions) client.ClientRPCDiagnosticsProvider {
	return func(ctx context.Context) (client.ClientRPCDiagnosticsObservation, error) {
		if err := ctx.Err(); err != nil {
			return client.ClientRPCDiagnosticsObservation{}, err
		}
		osVersion, _ := diagnosticsOSVersion()["version"].(string)
		inspection, available := opts.WireGuard.TryInspection()
		result := client.ClientRPCDiagnosticsObservation{OSVersion: osVersion, Tunnel: inspection, TunnelBusy: !available, Interfaces: client.LocalInterfaceStatuses()}
		if err := ctx.Err(); err != nil {
			return result, err
		}
		cfg := opts.ConfigStore.Read()
		networkMap, err := verifiedCachedNetworkMap(&cfg)
		if err != nil {
			return result, nil
		} // No unverified map metadata is exposed.
		result.VerifiedMap = true
		result.DNS = nativeMapDNSDiagnostics(networkMap)
		result.RouteConflicts = client.OverlayCIDRConflicts(networkMap, result.Interfaces, opts.WGInterface)
		result.Peers, result.TunnelPeers, result.PeerFailures = nativeDiagnosticPeers(networkMap, inspection)
		paths, available := opts.WireGuard.TryPathStatus(networkMap.Network.ID, networkMap.Node.ID, networkMap.Network.Revision)
		if available {
			result.PeerFailures = append(result.PeerFailures, nativeDiagnosticPaths(result.Peers, paths)...)
		} else {
			result.PeerFailures = append(result.PeerFailures, &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_UNAVAILABLE, ReasonKey: "diagnostics_current_paths_unavailable"})
		}
		return result, ctx.Err()
	}
}

// Only call with a verified profile map. These are configured DNS values, not
// evidence that an OS resolver applied them or that names resolve successfully.
func nativeMapDNSDiagnostics(networkMap clientapi.RegisterNodeResponse) *ipc.DnsDiagnostics {
	config := diagnosticsDNSConfiguration(networkMap)
	result := &ipc.DnsDiagnostics{SearchDomain: config.searchDomain, Servers: append([]string(nil), config.servers...), Ttl: durationpb.New(time.Duration(client.DefaultDNSTTLSeconds) * time.Second)}
	add := func(id, hostname, ipv4, ipv6 string) {
		label := client.DNSLabel(hostname)
		if strings.TrimSpace(id) == "" || label == "" || (ipv4 == "" && ipv6 == "") {
			return
		}
		record := &ipc.DnsRecord{NodeId: strings.TrimSpace(id), Hostname: strings.TrimSpace(hostname), Label: label, Fqdn: label + "." + config.searchDomain}
		if ipv4 != "" {
			record.Addresses = append(record.Addresses, ipv4)
		}
		if ipv6 != "" {
			record.Addresses = append(record.Addresses, ipv6)
		}
		result.Records = append(result.Records, record)
	}
	add(networkMap.Node.ID, networkMap.Node.Hostname, diagnosticNormalizedAddress(networkMap.Node.AssignedIP, false), diagnosticNormalizedAddress(networkMap.Node.AssignedIPv6, true))
	for _, peer := range networkMap.Peers {
		ipv4, ipv6 := diagnosticPeerDNSAddresses(peer.AllowedIPs)
		add(peer.ID, peer.Hostname, ipv4, ipv6)
	}
	return result
}
