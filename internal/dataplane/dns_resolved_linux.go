package dataplane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"time"

	"net"

	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

const resolvedBus = "org.freedesktop.resolve1"

// A successful resolved D-Bus write cannot steer applications which send
// directly to a different nameserver listed in resolv.conf.
func usesResolvedStub(data []byte) bool {
	found := false
	for line := range strings.SplitSeq(string(data), "\n") {
		if n := strings.IndexAny(line, "#;"); n >= 0 {
			line = line[:n]
		}
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "nameserver" {
			continue
		}
		if len(fields) != 2 || fields[1] != "127.0.0.53" {
			return false
		}
		found = true
	}
	return found
}

func checkResolvedStub() error {
	data, err := os.ReadFile("/etc/resolv.conf")
	if err != nil || !usesResolvedStub(data) {
		return connectivity.ErrDNSUnsupported
	}
	return nil
}

type resolvedLinkState struct {
	servers, domains string
	defaultRoute     bool
}

// busctl returns typed JSON. Preserve exact address, port, SNI and domain
// order so a change by another writer is observable before replacement.
func readResolvedLink(ctx context.Context, index int) (resolvedLinkState, error) {
	var out resolvedLinkState
	if index <= 0 {
		return out, ErrAdapterOwnership
	}
	for _, prop := range []string{"DNSEx", "Domains", "DefaultRoute"} {
		raw, err := exec.CommandContext(ctx, "/usr/bin/busctl", "--system", "--json=short", "get-property", resolvedBus, "/org/freedesktop/resolve1/link/_3"+strconv.Itoa(index), resolvedBus+".Link", prop).Output()
		if err != nil {
			return out, ErrConfiguration
		}
		var value struct {
			Type string
			Data json.RawMessage
		}
		if json.Unmarshal(raw, &value) != nil {
			return out, ErrConfiguration
		}
		var compact bytes.Buffer
		if json.Compact(&compact, value.Data) != nil {
			return out, ErrConfiguration
		}
		switch prop {
		case "DNSEx":
			if value.Type != "a(iayqs)" {
				return out, ErrConfiguration
			}
			out.servers = compact.String()
		case "Domains":
			if value.Type != "a(sb)" {
				return out, ErrConfiguration
			}
			out.domains = compact.String()
		case "DefaultRoute":
			if value.Type != "b" || json.Unmarshal(value.Data, &out.defaultRoute) != nil {
				return out, ErrConfiguration
			}
		}
	}
	return out, nil
}

func resolverState(addr netip.AddrPort, domains []string) resolvedLinkState {
	ip := addr.Addr().As4()
	dns, _ := json.Marshal([]any{[]any{2, [4]int{int(ip[0]), int(ip[1]), int(ip[2]), int(ip[3])}, addr.Port(), ""}})
	entries := make([][2]any, 0, len(domains))
	for _, d := range domains {
		entries = append(entries, [2]any{strings.TrimPrefix(d, "~"), strings.HasPrefix(d, "~")})
	}
	zones, _ := json.Marshal(entries)
	return resolvedLinkState{servers: string(dns), domains: string(zones)}
}

// resolved can send the same question to multiple links on a tied route.
// Reject a competing equally specific or narrower route to a private zone.
func checkResolvedDomains(ctx context.Context, index int, domains []string) error {
	raw, err := exec.CommandContext(ctx, "/usr/bin/busctl", "--system", "--json=short", "get-property", resolvedBus, "/org/freedesktop/resolve1", resolvedBus+".Manager", "Domains").Output()
	if err != nil {
		return ErrConfiguration
	}
	var result struct {
		Type string
		Data [][]json.RawMessage
	}
	if json.Unmarshal(raw, &result) != nil || result.Type != "a(isb)" {
		return ErrConfiguration
	}
	zones := make(map[string]bool, len(domains))
	for _, domain := range domains {
		zones[strings.TrimPrefix(domain, "~")] = true
	}
	for _, entry := range result.Data {
		var link int
		var name string
		if len(entry) != 3 || json.Unmarshal(entry[0], &link) != nil || json.Unmarshal(entry[1], &name) != nil {
			return ErrConfiguration
		}
		if link == index {
			continue
		}
		if name == "." && zones["."] {
			return ErrIPConflict
		}
		name = strings.ToLower(strings.TrimSuffix(name, "."))
		for name != "" {
			if zones[name] {
				return ErrIPConflict
			}
			_, name, _ = strings.Cut(name, ".")
		}
	}
	return nil
}

type linuxSessionDNS struct {
	serviceDirectory    bool
	retiring            *sessionDNSProxy
	uncertain           bool
	proxy               *sessionDNSProxy
	assignment          *coordinator.DNSConfig
	domains             []string
	installed, previous resolvedLinkState
}

func resolvedLinkPresent(ctx context.Context, index int) (bool, error) {
	output, err := exec.CommandContext(ctx, "/usr/bin/busctl", "--system", "--list", "tree", resolvedBus).Output()
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		return false, ErrConfiguration
	}
	const root = "/org/freedesktop/resolve1/link"
	expected := root + "/_3" + strconv.Itoa(index)
	foundRoot := false
	for path := range strings.SplitSeq(string(output), "\n") {
		if path == expected {
			return true, nil
		}
		if path == root {
			foundRoot = true
		}
	}
	if !foundRoot {
		return false, ErrConfiguration
	}
	return false, nil
}

// The kernel and resolved consume link deletion separately. The session
// keeps its loopback ports while this receipt awaits confirmation.
func waitResolvedRemoval(ctx context.Context, receipt state.LinuxNetworkReceipt) error {
	if receipt.DNSIndex == 0 {
		return nil
	}
	for {
		present, err := resolvedLinkPresent(ctx, receipt.DNSIndex)
		if err != nil || !present {
			return err
		}
		// Interface indices may be reused. Leave a foreign link alone
		// once its DNS servers no longer reference the recorded endpoints.
		if link, err := net.InterfaceByIndex(receipt.DNSIndex); err == nil && link.Name != receipt.Table {
			current, err := readResolvedLink(ctx, receipt.DNSIndex)
			if err != nil {
				return err
			}
			own, err := containsDNSPort(current.servers, receipt)
			if err != nil || !own {
				return err
			}
			return ErrIPConflict
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func containsDNSPort(servers string, receipt state.LinuxNetworkReceipt) (bool, error) {
	var entries [][]json.RawMessage
	if json.Unmarshal([]byte(servers), &entries) != nil {
		return false, ErrConfiguration
	}
	for _, entry := range entries {
		var family int
		var address []int
		var port uint16
		if len(entry) != 4 || json.Unmarshal(entry[0], &family) != nil || json.Unmarshal(entry[1], &address) != nil || json.Unmarshal(entry[2], &port) != nil {
			return false, ErrConfiguration
		}
		if family == 2 && reflect.DeepEqual(address, []int{127, 0, 0, 1}) && port != 0 && (port == receipt.DNSNewPort || port == receipt.DNSOldPort) {
			return true, nil
		}
	}
	return false, nil
}
func (s *LinuxSession) recordDNS(ctx context.Context, index int, oldPort, newPort uint16) error {
	next := s.receipt
	next.DNSIndex, next.DNSOldPort, next.DNSNewPort = index, oldPort, newPort
	if err := s.ledger.Replace(ctx, s.receipt, next); err != nil {
		return err
	}
	s.receipt = next
	return nil
}

func (d *linuxSessionDNS) stop() {
	if d.retiring != nil {
		d.retiring.close()
		d.retiring = nil
	}
	if d.proxy != nil {
		d.proxy.close()
		d.proxy = nil
	}
}
func (s *LinuxSession) applyDNS(ctx context.Context, m connectivity.Map, snapshot coordinator.NetworkMapSnapshot) error {
	config := connectivity.MapDNSConfig(snapshot)
	services := connectivity.HasMapDNSDirectory(snapshot)
	d, link := &s.dns, s.link
	if d.uncertain {
		return ErrIPConflict
	}
	if d.proxy == nil && config == nil {
		return nil
	}
	if config != nil {
		if err := checkResolvedStub(); err != nil {
			return err
		}
	}
	if _, err := link.checkOwnership(); err != nil {
		return err
	}
	current, err := readResolvedLink(ctx, link.index)
	if err != nil {
		return err
	}
	if d.proxy != nil {
		if current != d.installed {
			return ErrIPConflict
		}
		if !services && !d.serviceDirectory && reflect.DeepEqual(config, d.assignment) {
			return checkResolvedDomains(ctx, link.index, d.domains)
		}
	} else {
		if current.servers != "[]" || current.domains != "[]" {
			return ErrIPConflict
		}
		d.previous = current
	}
	set := func(property string, args ...string) error {
		if _, err := link.checkOwnership(); err != nil {
			return err
		}
		argv := append([]string{property, strconv.Itoa(link.index)}, args...)
		if exec.CommandContext(ctx, "/usr/bin/resolvectl", argv...).Run() != nil {
			return ErrConfiguration
		}
		return nil
	}
	// Keep the previous port reserved until resolved confirms that it stopped
	// using it. The old listener can no longer forward a private question.
	retire := func() {
		d.uncertain = true

		d.retiring = d.proxy
		if d.retiring != nil {
			d.retiring.cancel()
		}
		d.proxy = nil
	}
	var domains []string
	expect := d.previous
	if config == nil {
		retire()
		if err = set("domain", ""); err != nil {
			return err
		}
		if err = set("dns", ""); err != nil {
			return err
		}
		if err = set("default-route", strconv.FormatBool(expect.defaultRoute)); err != nil {
			return err
		}
	} else {
		routes, err := connectivity.CompileMapDNS(snapshot, func(end time.Time) bool { return s.lease.GrantDeadlineCurrent(m, end) })
		if err != nil {
			return err
		}
		domains, err = routes.ResolverDomains(config.SearchDomains)
		if err != nil || len(domains) == 0 {
			return connectivity.ErrDNSUnsupported
		}
		if err := checkResolvedDomains(ctx, link.index, domains); err != nil {
			return err
		}
		p, err := openSessionDNSProxy(s.ctx, netip.MustParseAddrPort("127.0.0.1:0"), net.ListenConfig{}, routes, s.config.Relay.IOTimeout, func() bool { return s.lease.Permissions().NewFlows && (!services || s.lease.MatchesMap(m)) })
		if err != nil {
			return err
		}
		var oldPort uint16
		if d.proxy != nil {
			oldPort = d.proxy.addr.Port()
		}
		if err := s.recordDNS(ctx, link.index, oldPort, p.addr.Port()); err != nil {
			p.close()
			return err
		}
		retire()
		d.proxy = p
		expect = resolverState(p.addr, domains)
		if err = set("default-route", "no"); err != nil {
			return err
		}
		if err = set("dns", p.addr.String()); err != nil {
			return err
		}
		if err = set("domain", domains...); err != nil {
			return err
		}
	}
	actual, err := readResolvedLink(ctx, link.index)
	if err != nil || actual != expect {
		return errors.Join(ErrIPConflict, err)
	}
	if config == nil {
		if err := s.recordDNS(ctx, 0, 0, 0); err != nil {
			return err
		}
	}
	if d.retiring != nil {
		d.retiring.close()
		d.retiring = nil
	}
	d.uncertain = false
	d.serviceDirectory = services
	d.installed, d.assignment, d.domains = actual, config, domains
	return nil
}
