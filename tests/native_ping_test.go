package tests

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"testing"
	"time"
)

// nativePing invokes the OS ICMP implementation. The reference IP exists only
// inside the remote userspace stack, so a reply requires Client tunnel traffic.
// In particular, Windows can report an unreachable response with exit code 0;
// accept only an echo-reply line from the exact expected peer, with RTT data.
func nativePing(t *testing.T, address netip.Addr) bool {
	t.Helper()
	program := "ping"
	args := []string{"-n", "-c", "1", address.String()}
	switch runtime.GOOS {
	case "linux":
		family := "-4"
		if address.Is6() {
			family = "-6"
		}
		args = append([]string{family, "-W", "1"}, args...)
	case "darwin":
		if address.Is6() {
			program = "ping6"
		}
	case "windows":
		family := "-4"
		if address.Is6() {
			family = "-6"
		}
		args = []string{family, "-n", "1", "-w", "1000", address.String()}
	default:
		t.Fatal("native ICMP probe has no driver for this platform")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	output, err := cmd.CombinedOutput()
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatal("native ICMP probe could not execute")
		}
		return false
	}
	return nativePingReply(runtime.GOOS, address, string(output))
}

func nativePingReply(platform string, address netip.Addr, output string) bool {
	ip := regexp.QuoteMeta(address.String())
	pattern := `(?m)^\d+ bytes from ` + ip + `[:,] +icmp_seq=\d+ +(ttl|hlim)=\d+ +time[=<][0-9.]+ ?ms\r?$`
	if platform == "windows" {
		pattern = `(?m)^Reply from ` + ip + `: (bytes=\d+ )?time[=<][0-9.]+ms( TTL=\d+)?\r?$`
	}
	return regexp.MustCompile(pattern).MatchString(output)
}

func TestNativePingReplyRejectsUnreachableAndUnrelatedOutput(t *testing.T) {
	for _, tc := range []struct {
		platform, address, output string
		want                      bool
	}{
		{"linux", "100.94.0.20", "64 bytes from 100.94.0.20: icmp_seq=1 ttl=64 time=0.123 ms\n", true},
		{"darwin", "fd94::20", "16 bytes from fd94::20, icmp_seq=0 hlim=64 time=0.125 ms\n", true},
		{"windows", "100.94.0.20", "Reply from 100.94.0.20: bytes=32 time<1ms TTL=64\r\n", true},
		{"windows", "fd94::20", "Reply from fd94::20: time=2ms\r\n", true},
		{"windows", "100.94.0.20", "Reply from 100.94.0.20: Destination host unreachable.\r\n", false},
		{"windows", "100.94.0.20", "Reply from 100.94.0.21: bytes=32 time<1ms TTL=64\r\n", false},
		{"linux", "100.94.0.20", "From 100.94.0.20 icmp_seq=1 Destination Host Unreachable\n", false},
		{"linux", "100.94.0.20", "64 bytes from 100.94.0.200: icmp_seq=1 ttl=64 time=0.123 ms\n", false},
		{"darwin", "fd94::20", "Request timeout for icmp_seq 0\n", false},
		{"windows", "fd94::20", "Packets: Sent = 1, Received = 1, Lost = 0 (0% loss)\r\n", false},
	} {
		if got := nativePingReply(tc.platform, netip.MustParseAddr(tc.address), tc.output); got != tc.want {
			t.Errorf("%s output %q: got %t, want %t", tc.platform, tc.output, got, tc.want)
		}
	}
}
