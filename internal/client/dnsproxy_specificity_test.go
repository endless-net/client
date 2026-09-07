package client

import (
	"encoding/binary"
	"slices"
	"testing"
	"time"
)

func TestSplitDNSSelectsMostSpecificDomainIncludingUnavailableUpstreams(t *testing.T) {
	for _, test := range []struct {
		name      string
		rules     []SplitDNSRule
		upstreams []string
		matched   bool
	}{
		{"specific first", []SplitDNSRule{{"corp.example", []string{"private"}}, {"example", []string{"parent"}}}, []string{"private"}, true},
		{"parent first", []SplitDNSRule{{"example", []string{"parent"}}, {"corp.example", []string{"private"}}}, []string{"private"}, true},
		{"unavailable specific", []SplitDNSRule{{"example", []string{"parent"}}, {"corp.example", nil}}, nil, true},
		{"unavailable first", []SplitDNSRule{{"corp.example", nil}, {"example", []string{"parent"}}}, nil, true},
		{"unavailable parent", []SplitDNSRule{{"example", nil}, {"corp.example", []string{"private"}}}, []string{"private"}, true},
		{"equal priorities", []SplitDNSRule{{"corp.example", []string{"private"}}, {"CORP.EXAMPLE.", []string{"backup"}}}, []string{"private", "backup"}, true},
		{"label boundary", []SplitDNSRule{{"orp.example", []string{"wrong"}}}, nil, false},
		{"empty domain", []SplitDNSRule{{"", []string{"wrong"}}}, nil, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			upstreams, matched := selectSplitDNSUpstreams("DB.CORP.EXAMPLE.", test.rules)
			if !slices.Equal(upstreams, test.upstreams) || matched != test.matched {
				t.Fatalf("selected %#v/%v", upstreams, matched)
			}
		})
	}
}

func TestUnavailableSpecificSplitDNSDoesNotForwardToParentOrGlobal(t *testing.T) {
	response, err := DNSProxyResponse(t.Context(), dnsTestQuery(t, 0x1234, "db.corp.example", dnsTypeA), DNSProxyOptions{
		SearchDomain:  "peers.internal",
		UpstreamAddrs: []string{"invalid-global-address"},
		SplitRules: []SplitDNSRule{
			{Domain: "example", Upstreams: []string{"invalid-parent-address"}},
			{Domain: "corp.example"},
		},
	}, time.Millisecond)
	// An attempted forward would return an address error instead of a DNS reply.
	if err != nil || len(response) < 12 || binary.BigEndian.Uint16(response[2:4])&0xf != dnsRCodeFail {
		t.Fatalf("private query did not fail closed: %v", err)
	}
}
