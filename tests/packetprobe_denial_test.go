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
