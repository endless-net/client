package main

import "testing"

func TestProbeNetworkUsesExplicitIPv6WithoutIPv4Fallback(t *testing.T) {
	for _, protocol := range []string{"tcp", "udp"} {
		for _, tc := range []struct{ address, family string }{
			{"[fd94::20]:24001", "6"},
			{"[fe80::20%test]:24001", "6"},
			{"100.94.0.20:24001", "4"},
			{"peer.scenario.endlessnet:24001", "4"},
		} {
			if got := probeNetwork(protocol, tc.address); got != protocol+tc.family {
				t.Fatalf("address %q selected %q", tc.address, got)
			}
		}
	}
}
