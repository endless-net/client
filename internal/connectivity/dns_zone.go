package connectivity

import (
	"net/netip"
	"strings"

	coordinator "github.com/endless-net/coordinator/api/enrollment"
	"golang.org/x/net/dns/dnsmessage"
)

// A present name with no records is an empty non-terminal, or the zone apex.
type localDNSZone map[string][]dnsmessage.Resource

func compileLocalZone(apex string, records []coordinator.DNSRecord) (localDNSZone, error) {
	zone := localDNSZone{apex: nil}
	type recordSetKey struct{ name, kind string }
	type addressSet struct {
		ttl       uint32
		addresses map[netip.Addr]bool
	}
	sets := make(map[recordSetKey]addressSet)
	for _, r := range records {
		name, ok := dnsDomain(r.Name)
		if !ok || !strings.HasSuffix(r.Name, ".") || (name != apex && !strings.HasSuffix(name, "."+apex)) {
			return nil, ErrDNSAssignment
		}
		owner, err := dnsmessage.NewName(name + ".")
		if err != nil {
			return nil, ErrDNSAssignment
		}
		rr := dnsmessage.Resource{Header: dnsmessage.ResourceHeader{Name: owner, Class: dnsmessage.ClassINET, TTL: r.TTL}}
		switch r.Type {
		case "A", "AAAA":
			ip, err := netip.ParseAddr(r.Value)
			if err != nil || ip.Zone() != "" || ip.Is4() != (r.Type == "A") {
				return nil, ErrDNSAssignment
			}
			key := recordSetKey{name, r.Type}
			set, exists := sets[key]
			if !exists {
				set = addressSet{ttl: r.TTL, addresses: make(map[netip.Addr]bool)}
				sets[key] = set
			}
			if set.ttl != r.TTL || set.addresses[ip] {
				return nil, ErrDNSAssignment
			}
			set.addresses[ip] = true
			if ip.Is4() {
				rr.Header.Type = dnsmessage.TypeA
				rr.Body = &dnsmessage.AResource{A: ip.As4()}
			} else {
				rr.Header.Type = dnsmessage.TypeAAAA
				rr.Body = &dnsmessage.AAAAResource{AAAA: ip.As16()}
			}
		case "CNAME":
			target, valid := dnsDomain(r.Value)
			if !valid || !strings.HasSuffix(r.Value, ".") {
				return nil, ErrDNSAssignment
			}
			alias, err := dnsmessage.NewName(target + ".")
			if err != nil {
				return nil, ErrDNSAssignment
			}
			rr.Header.Type = dnsmessage.TypeCNAME
			rr.Body = &dnsmessage.CNAMEResource{CNAME: alias}
		default:
			return nil, ErrDNSAssignment
		}
		if existing := zone[name]; len(existing) > 0 && (existing[0].Header.Type == dnsmessage.TypeCNAME || rr.Header.Type == dnsmessage.TypeCNAME) {
			return nil, ErrDNSAssignment
		}
		zone[name] = append(zone[name], rr)
		for parent := name; parent != apex; {
			_, parent, _ = strings.Cut(parent, ".")
			if _, ok := zone[parent]; ok {
				break
			}
			zone[parent] = nil
		}
	}
	return zone, nil
}

func (routes DNSRoutes) localReply(zone localDNSZone, h dnsmessage.Header, q dnsmessage.Question) ([]byte, error) {
	answer := dnsmessage.Message{Header: dnsmessage.Header{ID: h.ID, Response: true, Authoritative: true, RecursionDesired: h.RecursionDesired}, Questions: []dnsmessage.Question{q}}
	name := foldDNSName(strings.TrimSuffix(q.Name.String(), "."))
	if q.Class != dnsmessage.ClassINET && q.Class != dnsmessage.ClassANY {
		answer.RCode = dnsmessage.RCodeRefused
	} else {
		seen := make(map[string]bool)
		for zone != nil || routes.services[name] != nil {
			records, found := zone[name]
			if routes.services[name] != nil {
				records, found = routes.serviceRecords(name)
			}
			if !found {
				answer.RCode = dnsmessage.RCodeNameError
				break
			}
			if len(records) > 0 && records[0].Header.Type == dnsmessage.TypeCNAME && q.Type != dnsmessage.TypeCNAME && q.Type != dnsmessage.TypeALL {
				if seen[name] {
					answer.Answers = nil
					answer.RCode = dnsmessage.RCodeServerFailure
					break
				}
				seen[name] = true
				alias := records[0]
				answer.Answers = append(answer.Answers, alias)
				name = strings.TrimSuffix(alias.Body.(*dnsmessage.CNAMEResource).CNAME.String(), ".")
				zone = routes.local[routes.match(name)]
				continue
			}
			for _, r := range records {
				if r.Header.Type == q.Type || q.Type == dnsmessage.TypeALL {
					answer.Answers = append(answer.Answers, r)
				}
			}
			break
		}
	}
	packet, err := answer.Pack()
	if err != nil || len(packet) > 65535 {
		return nil, ErrDNSQuery
	}
	return packet, nil
}
