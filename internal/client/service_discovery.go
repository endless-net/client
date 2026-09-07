package client

import (
	"encoding/binary"
	"net/netip"
	"sort"
	"strings"
	"time"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
)

// Catalog DNS is authoritative for each declared service zone. Missing or
// revoked hosts never fall through to an upstream resolver.
func serviceDNSResponse(request []byte, question dnsQuestion, opts DNSProxyOptions) ([]byte, bool) {
	var service *clientapi.AdvertisedService
	for index := range opts.NetworkMap.Network.Services {
		candidate := &opts.NetworkMap.Network.Services[index]
		if dnsNameInDomain(question.Name, candidate.DNSName) {
			if service != nil {
				return dnsErrorResponse(request, dnsRCodeFail), true
			}
			service = candidate
		}
	}
	if service == nil {
		return nil, false
	}
	if len(service.Hosts) > 1000 || len(service.Ports) > 1000 {
		return dnsErrorResponse(request, dnsRCodeFail), true
	}
	if opts.SigningTrust == nil || clientapi.ValidateNetworkMap(opts.NetworkMap) != nil || clientapi.VerifyNetworkMapSignatureWithTrustBundle(opts.NetworkMap, *opts.SigningTrust) != nil {
		return dnsErrorResponse(request, dnsRCodeFail), true
	}
	if service.ApprovalStatus != "approved" || len(service.Hosts) == 0 {
		return dnsErrorResponse(request, dnsRCodeNX), true
	}
	if question.Class != dnsClassIN {
		return dnsResponseHeader(request, dnsRCodeNoErr, 0, nil), true
	}
	now := time.Now()
	ttl := uint32(0) // No caching beyond the current signed authorization.
	if opts.NetworkMap.MapSignature.ExpiresAt.Before(now) {
		return dnsErrorResponse(request, dnsRCodeFail), true
	}
	records := peerDNSRecords(opts.NetworkMap)
	addresses := make(map[string]peerDNSRecord, len(records))
	for _, record := range records {
		addresses[record.nodeID] = record
	}
	hosts := append([]clientapi.ServiceHost(nil), service.Hosts...)
	sort.Slice(hosts, func(i, j int) bool { return hosts[i].NodeID < hosts[j].NodeID })
	var answers []byte
	count := uint16(0)
	limit := opts.responseLimit
	if limit == 0 {
		limit = dnsMaxUDPBytes
	}
	truncated := func() ([]byte, bool) {
		response := dnsResponseHeader(request, dnsRCodeNoErr, 0, nil)
		response[2] |= 0x02
		return response, true
	}
	for _, host := range hosts {
		record, ok := addresses[host.NodeID]
		if !ok {
			continue
		}
		target := DNSLabel(host.NodeID) + "." + service.DNSName
		name := normalizeDNSName(question.Name)
		var data []byte
		switch question.Type {
		case dnsTypeA, dnsTypeAAAA:
			if name != normalizeDNSName(service.DNSName) && name != normalizeDNSName(target) {
				continue
			}
			address := record.ipv4
			if question.Type == dnsTypeAAAA {
				address = record.ipv6
			}
			parsed, err := netip.ParseAddr(address)
			if err != nil {
				continue
			}
			data = parsed.AsSlice()
		case 33: // RFC 2782 SRV: _<service name>._<protocol>.<service DNS zone>.
			for _, port := range service.Ports {
				if name != normalizeDNSName("_"+service.Name+"._"+port.Protocol+"."+service.DNSName) {
					continue
				}
				data = binary.BigEndian.AppendUint16(nil, 0)
				data = binary.BigEndian.AppendUint16(data, 0)
				if port.Port > 65535 {
					return dnsErrorResponse(request, dnsRCodeFail), true
				}
				data = binary.BigEndian.AppendUint16(data, uint16(port.Port))
				for _, label := range strings.Split(target, ".") {
					if len(label) > 63 {
						return dnsErrorResponse(request, dnsRCodeFail), true
					}
					data = append(data, byte(len(label)))
					data = append(data, label...)
				}
				data = append(data, 0)
				if question.QuestionEnd+len(answers)+len(data)+12 > limit {
					return truncated()
				}
				answers = appendServiceDNSAnswer(answers, question.Type, ttl, data)
				count++
			}
			continue
		default:
			return dnsResponseHeader(request, dnsRCodeNoErr, 0, nil), true
		}
		if question.QuestionEnd+len(answers)+len(data)+12 > limit {
			return truncated()
		}
		answers = appendServiceDNSAnswer(answers, question.Type, ttl, data)
		count++
	}
	if count == 0 {
		return dnsErrorResponse(request, dnsRCodeNX), true
	}
	return dnsResponseHeader(request, dnsRCodeNoErr, count, answers), true
}

func appendServiceDNSAnswer(answers []byte, kind uint16, ttl uint32, data []byte) []byte {
	if len(data) > 65535 {
		return answers
	}
	answers = append(answers, 0xc0, 0x0c)
	answers = binary.BigEndian.AppendUint16(answers, kind)
	answers = binary.BigEndian.AppendUint16(answers, dnsClassIN)
	answers = binary.BigEndian.AppendUint32(answers, ttl)
	// Address and SRV data above are bounded by the validated DNS name/port.
	answers = binary.BigEndian.AppendUint16(answers, uint16(len(data)))
	return append(answers, data...)
}
