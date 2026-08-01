package client

import (
	"fmt"
	"net/netip"
	"regexp"
	"sort"
	"strings"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
)

const (
	DefaultDNSRootSuffix = "endlessnet"
	DefaultDNSTTLSeconds = 30
)

var dnsLabelInvalidChars = regexp.MustCompile(`[^a-z0-9-]+`)

type DNSAddressFamily string

const (
	DNSAddressIPv4 DNSAddressFamily = "A"
	DNSAddressIPv6 DNSAddressFamily = "AAAA"
)

type PeerDNSResolution struct {
	Query              string   `json:"query"`
	FQDN               string   `json:"fqdn"`
	SearchDomain       string   `json:"search_domain"`
	Hostname           string   `json:"hostname"`
	NodeID             string   `json:"node_id"`
	Address            string   `json:"address"`
	Family             string   `json:"family"`
	TTLSeconds         int      `json:"ttl_seconds"`
	Conflict           bool     `json:"conflict"`
	ConflictPolicy     string   `json:"conflict_policy,omitempty"`
	ConflictingNodeIDs []string `json:"conflicting_node_ids,omitempty"`
}

type peerDNSRecord struct {
	hostname string
	label    string
	nodeID   string
	ipv4     string
	ipv6     string
}

func DefaultDNSDomain(networkName string) string {
	label := DNSLabel(networkName)
	if label == "" {
		label = "default"
	}
	return label + "." + DefaultDNSRootSuffix
}

func DNSLabel(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = dnsLabelInvalidChars.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	for strings.Contains(value, "--") {
		value = strings.ReplaceAll(value, "--", "-")
	}
	if len(value) > 63 {
		value = strings.Trim(value[:63], "-")
	}
	return value
}

func ResolvePeerDNSName(response clientapi.RegisterNodeResponse, query, searchDomain string, family DNSAddressFamily) (PeerDNSResolution, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return PeerDNSResolution{}, fmt.Errorf("dns name is required")
	}
	searchDomain = normalizeDNSName(searchDomain)
	if searchDomain == "" {
		searchDomain = DefaultDNSDomain(response.Network.Name)
	}
	searchDomain = normalizeDNSName(searchDomain)
	if family == "" {
		family = DNSAddressIPv4
	}
	if family != DNSAddressIPv4 && family != DNSAddressIPv6 {
		return PeerDNSResolution{}, fmt.Errorf("unsupported DNS record type %q", family)
	}
	records := peerDNSRecords(response)
	if len(records) == 0 {
		return PeerDNSResolution{}, fmt.Errorf("cached network map has no DNS-addressable nodes")
	}
	label, fqdn, err := resolveDNSQueryLabel(query, searchDomain)
	if err != nil {
		return PeerDNSResolution{}, err
	}
	matches := make([]peerDNSRecord, 0, 1)
	for _, record := range records {
		if record.label == label {
			matches = append(matches, record)
		}
	}
	if len(matches) == 0 {
		return PeerDNSResolution{}, fmt.Errorf("DNS name %q is not present in cached network map", query)
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].nodeID == matches[j].nodeID {
			return matches[i].hostname < matches[j].hostname
		}
		return matches[i].nodeID < matches[j].nodeID
	})
	selected := matches[0]
	address := selected.ipv4
	if family == DNSAddressIPv6 {
		address = selected.ipv6
	}
	if address == "" {
		return PeerDNSResolution{}, fmt.Errorf("DNS name %q has no %s address in cached network map", query, family)
	}
	resolution := PeerDNSResolution{
		Query:        query,
		FQDN:         fqdn,
		SearchDomain: searchDomain,
		Hostname:     selected.hostname,
		NodeID:       selected.nodeID,
		Address:      address,
		Family:       string(family),
		TTLSeconds:   DefaultDNSTTLSeconds,
	}
	if len(matches) > 1 {
		resolution.Conflict = true
		resolution.ConflictPolicy = "lowest-node-id-wins"
		for _, match := range matches {
			resolution.ConflictingNodeIDs = append(resolution.ConflictingNodeIDs, match.nodeID)
		}
	}
	return resolution, nil
}

func peerDNSRecords(response clientapi.RegisterNodeResponse) []peerDNSRecord {
	records := make([]peerDNSRecord, 0, 1+len(response.Peers))
	if record, ok := nodeDNSRecord(response.Node); ok {
		records = append(records, record)
	}
	for _, peer := range response.Peers {
		if record, ok := peerDNSRecordFromPeer(peer); ok {
			records = append(records, record)
		}
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].label == records[j].label {
			return records[i].nodeID < records[j].nodeID
		}
		return records[i].label < records[j].label
	})
	return records
}

func nodeDNSRecord(node clientapi.Node) (peerDNSRecord, bool) {
	label := DNSLabel(node.Hostname)
	if label == "" || strings.TrimSpace(node.ID) == "" {
		return peerDNSRecord{}, false
	}
	return peerDNSRecord{
		hostname: strings.TrimSpace(node.Hostname),
		label:    label,
		nodeID:   strings.TrimSpace(node.ID),
		ipv4:     normalizeDNSAddress(node.AssignedIP, false),
		ipv6:     normalizeDNSAddress(node.AssignedIPv6, true),
	}, true
}

func peerDNSRecordFromPeer(peer clientapi.Peer) (peerDNSRecord, bool) {
	label := DNSLabel(peer.Hostname)
	if label == "" || strings.TrimSpace(peer.ID) == "" {
		return peerDNSRecord{}, false
	}
	record := peerDNSRecord{
		hostname: strings.TrimSpace(peer.Hostname),
		label:    label,
		nodeID:   strings.TrimSpace(peer.ID),
	}
	for _, allowed := range peer.AllowedIPs {
		addr, ok := addressFromAllowedIP(allowed)
		if !ok {
			continue
		}
		if addr.Is6() {
			if record.ipv6 == "" {
				record.ipv6 = addr.String()
			}
			continue
		}
		if record.ipv4 == "" {
			record.ipv4 = addr.String()
		}
	}
	if record.ipv4 == "" && record.ipv6 == "" {
		return peerDNSRecord{}, false
	}
	return record, true
}

func addressFromAllowedIP(value string) (netip.Addr, bool) {
	value = strings.TrimSpace(strings.TrimSuffix(value, ","))
	if value == "" {
		return netip.Addr{}, false
	}
	if prefix, err := netip.ParsePrefix(value); err == nil {
		addr := prefix.Addr()
		if prefix.Bits() == addr.BitLen() {
			return addr, true
		}
		return netip.Addr{}, false
	}
	if addr, err := netip.ParseAddr(value); err == nil {
		return addr, true
	}
	return netip.Addr{}, false
}

func normalizeDNSAddress(value string, wantIPv6 bool) string {
	addr, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil || addr.Is6() != wantIPv6 {
		return ""
	}
	return addr.String()
}

func resolveDNSQueryLabel(query, searchDomain string) (string, string, error) {
	name := normalizeDNSName(query)
	if name == "" {
		return "", "", fmt.Errorf("dns name is required")
	}
	if strings.Contains(name, ".") {
		suffix := "." + searchDomain
		if name == searchDomain {
			return "", "", fmt.Errorf("dns name %q points at the search domain, not a peer", query)
		}
		if !strings.HasSuffix(name, suffix) {
			return "", "", fmt.Errorf("DNS name %q is outside search domain %q", query, searchDomain)
		}
		label := strings.TrimSuffix(name, suffix)
		if strings.Contains(label, ".") || DNSLabel(label) != label {
			return "", "", fmt.Errorf("DNS name %q is not a single peer label in %q", query, searchDomain)
		}
		return label, label + "." + searchDomain, nil
	}
	label := DNSLabel(name)
	if label == "" || label != name {
		return "", "", fmt.Errorf("DNS short name %q is invalid", query)
	}
	return label, label + "." + searchDomain, nil
}

func normalizeDNSName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimSuffix(value, ".")
	return strings.Trim(value, ".")
}
