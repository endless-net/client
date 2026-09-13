package tests

import (
	"fmt"
	"strings"
	"testing"

	"github.com/endless-net/client/internal/client"
)

// Diagnostic evidence from the test process, not an attestation of service state.
// Never print interface names, addresses, prefixes or underlying OS errors.
func installedInterfaceSummary(items []client.NetworkInterfaceStatus) string {
	var invalidIndex, invalidMTU, oversizedName, oversizedAddresses, oversizedPrefixes, oversizedFlags, failed int
	var maximumMTU int
	for _, item := range items {
		if item.Index < 0 || uint64(item.Index) > uint64(^uint32(0)) {
			invalidIndex++
		}
		if item.MTU < 0 || uint64(item.MTU) > uint64(^uint32(0)) {
			invalidMTU++
		}
		maximumMTU = max(maximumMTU, item.MTU)
		if len(item.Name) > 256 {
			oversizedName++
		}
		if len(item.Addresses) > 256 {
			oversizedAddresses++
		}
		if len(item.Prefixes) > 256 {
			oversizedPrefixes++
		}
		if len(item.Flags) > 32 {
			oversizedFlags++
		}
		if item.Error != "" {
			failed++
		}
	}
	return fmt.Sprintf("test_process_interfaces=%d max_mtu=%d invalid_index=%d invalid_mtu=%d oversized_name=%d oversized_addresses=%d oversized_prefixes=%d oversized_flags=%d inspection_errors=%d", len(items), maximumMTU, invalidIndex, invalidMTU, oversizedName, oversizedAddresses, oversizedPrefixes, oversizedFlags, failed)
}

func TestInstalledInterfaceSummaryWithholdsInterfaceMetadata(t *testing.T) {
	items := []client.NetworkInterfaceStatus{{Index: -1, MTU: -1, Name: strings.Repeat("private", 40), Addresses: make([]string, 257), Prefixes: make([]string, 257), Flags: make([]string, 33), Error: "private OS error"}, {Index: 1, MTU: 65536}}
	want := "test_process_interfaces=2 max_mtu=65536 invalid_index=1 invalid_mtu=1 oversized_name=1 oversized_addresses=1 oversized_prefixes=1 oversized_flags=1 inspection_errors=1"
	if got := installedInterfaceSummary(items); got != want {
		t.Fatal("incorrect or unredacted interface summary")
	}
}
