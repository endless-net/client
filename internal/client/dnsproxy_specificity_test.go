package client

import (
	"encoding/binary"
	"testing"
	"time"
)

func TestSplitDNSSelectsMostSpecificDomainIncludingUnavailableUpstreams(t *testing.T) {
	for _, test := range []struct {
		name     string
		rules    []SplitDNSRule
		upstream string
		matched  bool
	}{
		{"specific first", []SplitDNSRule{{"corp.example", "private"}, {"example", "parent"}}, "private", true},
		{"parent first", []SplitDNSRule{{"example", "parent"}, {"corp.example", "private"}}, "private", true},
		{"unavailable specific", []SplitDNSRule{{"example", "parent"}, {"corp.example", ""}}, "", true},
		{"unavailable first", []SplitDNSRule{{"corp.example", ""}, {"example", "parent"}}, "", true},
		{"unavailable parent", []SplitDNSRule{{"example", ""}, {"corp.example", "private"}}, "private", true},
		{"equal unavailable first", []SplitDNSRule{{"corp.example", ""}, {"CORP.EXAMPLE.", "private"}}, "", true},
		{"equal unavailable last", []SplitDNSRule{{"corp.example", "private"}, {"CORP.EXAMPLE.", ""}}, "", true},
		{"label boundary", []SplitDNSRule{{"orp.example", "wrong"}}, "", false},
		{"empty domain", []SplitDNSRule{{"", "wrong"}}, "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			upstream, matched := selectSplitDNSUpstream("DB.CORP.EXAMPLE.", test.rules)
			if upstream != test.upstream || matched != test.matched {
				t.Fatalf("selected %q/%v", upstream, matched)
			}
		})
	}
}

func TestUnavailableSpecificSplitDNSDoesNotForwardToParentOrGlobal(t *testing.T) {
	response, err := DNSProxyResponse(t.Context(), dnsTestQuery(t, 0x1234, "db.corp.example", dnsTypeA), DNSProxyOptions{
		SearchDomain: "peers.internal",
		UpstreamAddr: "invalid-global-address",
		SplitRules:   []SplitDNSRule{{Domain: "example", Upstream: "invalid-parent-address"}, {Domain: "corp.example"}},
	}, time.Millisecond)
	// An attempted forward would return an address error instead of a DNS reply.
	if err != nil || len(response) < 12 || binary.BigEndian.Uint16(response[2:4])&0xf != dnsRCodeFail {
		t.Fatalf("private query did not fail closed: %v", err)
	}
}
