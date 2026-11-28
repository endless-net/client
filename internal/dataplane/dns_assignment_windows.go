package dataplane

import (
	"context"
	"crypto/rand"
	"errors"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	"golang.org/x/sys/windows"
)

// The Windows DNS client accepts server IPs in namespace rules. Reserve port
// 53 for both transports before a rule can reference the address. Different
// revisions may overlap while the OS is acknowledging a replacement.
type windowsDNSAssignment struct {
	proxy      *sessionDNSProxy
	namespaces []string
	search     []string
}

func prepareWindowsDNS(ctx context.Context, snapshot coordinator.NetworkMapSnapshot, timeout time.Duration, active func() bool, current func(time.Time) bool) (*windowsDNSAssignment, error) {
	config := connectivity.MapDNSConfig(snapshot)
	if config == nil {
		return nil, nil
	}
	routes, err := connectivity.CompileMapDNS(snapshot, current)
	if err != nil {
		return nil, err
	}
	a := &windowsDNSAssignment{}
	a.namespaces, a.search, err = windowsResolverNamespaces(routes, config.SearchDomains)
	if err != nil {
		return nil, err
	}
	for range 8 {
		var id [2]byte
		if _, err := rand.Read(id[:]); err != nil {
			return nil, err
		}
		address := netip.AddrPortFrom(netip.AddrFrom4([4]byte{127, 17, id[0], id[1]}), 53)
		a.proxy, err = openSessionDNSProxy(ctx, address, exclusiveListenConfig(), routes, timeout, active)
		if err == nil {
			return a, nil
		}
		// An existing exclusive Winsock binding reports access denied.
		if !errors.Is(err, windows.WSAEADDRINUSE) && !errors.Is(err, windows.WSAEACCES) {
			return nil, err
		}
	}
	return nil, err
}

func windowsResolverNamespaces(routes connectivity.DNSRoutes, search []string) ([]string, []string, error) {
	domains, err := routes.ResolverDomains(search)
	if err != nil {
		return nil, nil, err
	}
	if len(domains) == 0 {
		return nil, nil, connectivity.ErrDNSUnsupported
	}
	var namespaces, searchDomains []string
	for _, domain := range domains {
		if !strings.HasPrefix(domain, "~") {
			searchDomains = append(searchDomains, domain)
		}
		suffix := strings.TrimPrefix(domain, "~")
		if suffix == "." {
			namespaces = append(namespaces, ".")
			continue
		}
		if routes.ExactServiceDomain(suffix) {
			namespaces = append(namespaces, suffix)
			continue
		}
		// NRPT distinguishes an exact FQDN from a suffix. Cover both the
		// zone apex and names below it, including names absent from the zone.
		namespaces = append(namespaces, suffix, "."+suffix)
	}
	slices.Sort(namespaces)
	return namespaces, searchDomains, nil
}

// replaceDNSRule records both sides of the switch before changing native
// rules. Recovery can clean either registry image after an uncertain commit.
func (s *WindowsSession) replaceDNSRule(ctx context.Context, m connectivity.Map, server netip.Addr, namespaces []string) error {
	var next state.DNSRuleRecord
	var values []dnsRegistryValue
	if len(namespaces) != 0 {
		var err error
		next, values, err = newDNSRule(server, namespaces)
		if err != nil {
			return err
		}
	}
	journal, err := s.change(ctx, m, func(j *state.ResourceJournal) error {
		for i, record := range j.DNSRules {
			if record.Phase != state.EffectApplied {
				return state.ErrStale
			}
			j.DNSRules[i].Phase = state.EffectRemoving
		}
		if next.Key != "" {
			j.DNSRules = append(j.DNSRules, next)
		}
		return nil
	})
	if err != nil {
		return err
	}
	db := s.cleanup.dnsRules
	err = registryTransaction(ctx, func(tx windows.Handle) error {
		if next.Key != "" {
			if err := db.installTx(tx, next, values); err != nil {
				return err
			}
		}
		for _, record := range journal.DNSRules {
			if record.Phase == state.EffectRemoving {
				if err := db.removeTx(tx, record); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	_, err = s.change(ctx, m, func(j *state.ResourceJournal) error {
		if !slices.Equal(j.DNSRules, journal.DNSRules) {
			return state.ErrStale
		}
		j.DNSRules = nil
		if next.Key != "" {
			next.Phase = state.EffectApplied
			j.DNSRules = []state.DNSRuleRecord{next}
		}
		return nil
	})
	return err
}

// applyDNS runs while the lease is paused. A listener belongs to one signed
// map, so resuming a newer lease cannot authorize replies from the old one.
func (s *WindowsSession) applyDNS(ctx context.Context, m connectivity.Map) (err error) {
	snapshot, err := m.Snapshot()
	if err != nil {
		return err
	}
	config := connectivity.MapDNSConfig(snapshot)
	if config == nil && s.dns == nil {
		return nil
	}
	pending := s.beginDNS(snapshot)
	defer func() { s.finishDNS(pending, config == nil, err) }()
	next, err := prepareWindowsDNS(s.lifetime, snapshot, s.config.RelayIOTimeout, func() bool {
		return s.lease.Permissions().NewFlows && s.lease.MatchesMap(m)
	}, func(end time.Time) bool { return s.lease.GrantDeadlineCurrent(m, end) })
	if err != nil {
		return err
	}
	var server netip.Addr
	var namespaces []string
	var oldSearch, newSearch []string
	if s.dns != nil {
		oldSearch = s.dns.search
	}
	if next != nil {
		newSearch = next.search
		server = next.proxy.addr.Addr()
		namespaces = next.namespaces
	}
	changed := !slices.Equal(oldSearch, newSearch)
	// Remove obsolete short-name expansion before withdrawing its routing
	// rules. Introduce new suffixes only after their routes are effective.
	if changed && len(oldSearch) > 0 {
		err = s.changeSearch(ctx, oldSearch, nil)
	}
	if err == nil {
		err = s.replaceDNSRule(ctx, m, server, namespaces)
	}
	if err == nil {
		err = s.effectiveDNS(ctx, s.dns, next)
	}
	if err == nil && len(newSearch) > 0 {
		expected := oldSearch
		if changed {
			expected = nil
		}
		err = s.changeSearch(ctx, expected, newSearch)
	}
	if err == nil {
		err = s.activationFence(ctx, m)
	}
	if err != nil {
		// Keep both ports reserved until cleanup confirms OS withdrawal.
		s.dnsUncertain = next
		return err
	}
	if s.dns != nil {
		s.dns.proxy.close()
	}
	s.dns = next
	return nil
}
