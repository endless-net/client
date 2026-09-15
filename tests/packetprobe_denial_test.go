package tests

import "testing"

func TestPacketProbeDenialRequiresExplicitExchangeOutcome(t *testing.T) {
	for _, tc := range []struct {
		name   string
		code   int
		output string
		want   bool
	}{
		{"unavailable", 2, "application exchange unavailable\n", true},
		{"windows-unavailable", 2, "application exchange unavailable\r\n", true},
		{"dial", 2, "application exchange unavailable: dial\n", true},
		{"dial-os-code", 2, "application exchange unavailable: dial: errno=10051 timeout=false\n", true},
		{"dial-timeout", 2, "application exchange unavailable: dial: errno=0 timeout=true\r\n", true},
		{"dial-extra-data", 2, "application exchange unavailable: dial: errno=10051 timeout=false private-address\n", false},
		{"dial-negative-code", 2, "application exchange unavailable: dial: errno=-1 timeout=false\n", false},
		{"dial-overflow-code", 2, "application exchange unavailable: dial: errno=4294967296 timeout=false\n", false},
		{"dial-noncanonical", 2, "application exchange unavailable: dial: errno=001 timeout=0\n", false},
		{"write", 2, "application exchange unavailable: write\r\n", true},
		{"read", 2, "application exchange unavailable: read\n", true},
		{"unknown-stage", 2, "application exchange unavailable: unknown\n", false},
		{"stage-extra-output", 2, "application exchange unavailable: dial\nextra\n", false},
		{"stage-wrong-exit", 1, "application exchange unavailable: read\n", false},
		{"flag-error", 2, "flag provided but not defined: -invalid\n", false},
		{"empty", 2, "", false},
		{"panic", 2, "panic: fixture failure\n", false},
		{"extra-output", 2, "application exchange unavailable\nunexpected output\n", false},
		{"wrong-exit", 1, "application exchange unavailable\n", false},
		{"success", 0, "", false},
		{"nonce-mismatch", 1, "application response mismatch: different_bytes=32 zero_bytes=0\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := packetProbeReportsDenial(tc.code, []byte(tc.output)); got != tc.want {
				t.Fatalf("denial classification: got %t, want %t", got, tc.want)
			}
		})
	}
}
