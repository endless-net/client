package tests

import "testing"

func TestPacketProbeFailureReasonWhitelistsBoundedCounters(t *testing.T) {
	valid := "application response mismatch: different_bytes=32 zero_bytes=0"
	if packetProbeFailureReason([]byte(valid)) != valid {
		t.Fatal("valid bounded mismatch diagnostics were lost")
	}
	for _, value := range []string{
		valid + " unexpected payload",
		valid + "\nother output",
		"application response mismatch: different_bytes=33 zero_bytes=0",
		"application response mismatch: different_bytes=0 zero_bytes=0",
		"application response mismatch: different_bytes=1 zero_bytes=-1",
		"application response mismatch: different_bytes=1 zero_bytes=33",
		"arbitrary child output",
	} {
		if packetProbeFailureReason([]byte(value)) != "unclassified probe failure" {
			t.Fatal("untrusted or invalid probe output escaped diagnostic filtering")
		}
	}
}
