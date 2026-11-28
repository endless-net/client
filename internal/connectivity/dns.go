package connectivity

import (
	"cmp"
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"strings"
	"time"

	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

var (
	ErrDNSAssignment  = fmt.Errorf("%w: invalid DNS assignment", ErrMap)
	ErrDNSUnsupported = fmt.Errorf("%w: DNS application unavailable", ErrMap)
	dnsHostLabel      = regexp.MustCompile("^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$")
)

// DNSRoutes contains the upstreams for each routing domain. The empty domain
// is the default route only when the assignment overrides the system resolver.
type DNSRoutes struct {
	services map[string][]serviceAddress
	current  func(time.Time) bool
	zones    map[string][]netip.Addr
	local    map[string]localDNSZone
}

func dnsDomain(raw string) (string, bool) {
	name := strings.TrimSuffix(raw, ".")
	if len(name) > 253 {
		return "", false
	}
	for label := range strings.SplitSeq(name, ".") {
		if !dnsHostLabel.MatchString(label) {
			return "", false
		}
	}
	return strings.ToLower(name), true
}

// CompileDNSRoutes validates a per-node assignment and orders its eligible upstreams.
func CompileDNSRoutes(d *coordinator.DNSConfig) (DNSRoutes, error) {
	var empty DNSRoutes
	if d == nil {
		return empty, nil
	}
	if d.Suffix != "" {
		if _, ok := dnsDomain(d.Suffix); !ok {
			return empty, ErrDNSAssignment
		}
	}
	for _, raw := range d.SearchDomains {
		if _, ok := dnsDomain(raw); !ok {
			return empty, ErrDNSAssignment
		}
	}
	routes := DNSRoutes{zones: make(map[string][]netip.Addr)}
	servers := slices.Clone(d.Nameservers)
	slices.SortFunc(servers, func(a, b coordinator.DNSNameserver) int {
		return cmp.Or(cmp.Compare(a.Priority, b.Priority), cmp.Compare(a.ID, b.ID))
	})
	ids := make(map[string]bool)
	for _, s := range servers {
		ip, err := netip.ParseAddr(s.Address)
		if !identifier(s.ID) || ids[s.ID] || err != nil || ip.Zone() != "" {
			return empty, ErrDNSAssignment
		}
		ip = ip.Unmap()
		if ip.IsUnspecified() || ip.IsMulticast() {
			return empty, ErrDNSAssignment
		}
		ids[s.ID] = true
		switch s.Scope {
		case "primary":
			if len(s.SplitDomains) != 0 {
				return empty, ErrDNSAssignment
			}
			if d.OverrideLocalDNS {
				routes.zones[""] = append(routes.zones[""], ip)
			}
		case "split":
			if len(s.SplitDomains) == 0 {
				return empty, ErrDNSAssignment
			}
			seen := make(map[string]bool)
			for _, raw := range s.SplitDomains {
				zone, ok := dnsDomain(raw)
				if !ok {
					return empty, ErrDNSAssignment
				}
				if !seen[zone] {
					routes.zones[zone] = append(routes.zones[zone], ip)
					seen[zone] = true
				}
			}
		default:
			return empty, ErrDNSAssignment
		}
	}
	if d.OverrideLocalDNS && len(routes.zones[""]) == 0 {
		return empty, ErrDNSAssignment
	}
	routes.local = make(map[string]localDNSZone, len(d.Zones))
	for _, source := range d.Zones {
		name, ok := dnsDomain(source.Name)
		if !ok || !strings.HasSuffix(source.Name, ".") || source.Version == 0 || routes.local[name] != nil {
			return empty, ErrDNSAssignment
		}
		records, err := compileLocalZone(name, source.Records)
		if err != nil {
			return empty, err
		}
		routes.local[name] = records
		routes.zones[name] = nil
	}
	return routes, nil
}

// DNS case insensitivity applies only to ASCII letters in a wire label.
func foldDNSName(name string) string {
	b := []byte(name)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

// Servers returns only the most specific route, in retry order. An empty
// result leaves an unmatched name with the system resolver.
func (r DNSRoutes) Servers(name string) []netip.Addr {
	return slices.Clone(r.zones[r.match(name)])
}

func (r DNSRoutes) match(name string) string {
	domain := foldDNSName(strings.TrimSuffix(name, "."))
	for domain != "" {
		if _, found := r.zones[domain]; found {
			return domain
		}
		_, domain, _ = strings.Cut(domain, ".")
	}
	return ""
}

// ResolverDomains lists domains to install in a per-link resolver. Plain
// domains expand short names; a leading tilde routes without expansion.
func (r DNSRoutes) ResolverDomains(search []string) ([]string, error) {
	var domains []string
	seen := make(map[string]bool)
	for _, raw := range search {
		domain, ok := dnsDomain(raw)
		if _, assigned := r.zones[r.match(domain)]; !ok || !assigned {
			return nil, ErrDNSUnsupported
		}
		if !seen[domain] {
			domains = append(domains, domain)
			seen[domain] = true
		}
	}
	var routes []string
	for zone := range r.zones {
		if zone == "" {
			routes = append(routes, "~.")
		} else if !seen[zone] {
			routes = append(routes, "~"+zone)
		}
	}
	for name := range r.services {
		if _, covered := r.zones[r.match(name)]; !covered {
			routes = append(routes, "~"+name)
		}
	}
	slices.Sort(routes)
	return append(domains, routes...), nil
}
