package client

import (
	"strings"
	"testing"
)

func TestExitGuardDHCPRemainsSeparateNarrowInfrastructure(t *testing.T) {
	raw := string(exitGuardReadbackFixture("owned", "exit0", 51820, false))
	if !exitGuardRulesObserved([]byte(raw), "owned", "exit0", 51820, false, "") {
		t.Fatal("DHCP containment fixture rejected")
	}
	for _, bad := range []string{
		strings.ReplaceAll(raw, `"right":68`, `"right":5353`),
		strings.ReplaceAll(raw, `"right":67`, `"right":53`),
		strings.ReplaceAll(raw, `"right":546`, `"right":5353`),
		strings.ReplaceAll(raw, `"right":547`, `"right":443`),
		strings.ReplaceAll(raw, `"addr":"fe80::"`, `"addr":"::"`),
		strings.ReplaceAll(raw, `"ff02::1:2"`, `"ff02::fb"`),
	} {
		if exitGuardRulesObserved([]byte(bad), "owned", "exit0", 51820, false, "") {
			t.Fatal("widened infrastructure exemption accepted")
		}
	}
	batch := exitGuardDHCPBatch("owned", "exit0")
	if strings.Count(batch, "accept\n") != 3 || strings.Count(batch, `oifname != "exit0"`) != 3 || !strings.Contains(batch, "udp sport 68 udp dport 67") || strings.Count(batch, "udp sport 546 udp dport 547") != 2 || strings.Count(batch, "ip6 saddr fe80::/10") != 2 {
		t.Fatal("DHCP batch lacks bounded infrastructure scope")
	}
}
