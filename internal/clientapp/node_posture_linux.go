package clientapp

import (
	"os"
	"strings"
)

func platformPosture() (string, string, string) {
	raw, _ := os.ReadFile("/etc/os-release")
	var version string
	for _, line := range strings.Split(string(raw), "\n") {
		if v, ok := strings.CutPrefix(line, "VERSION_ID="); ok {
			version = postureText(strings.Trim(v, "\"'"))
			break
		}
	}
	kernel, _ := os.ReadFile("/proc/sys/kernel/osrelease")
	serial, _ := os.ReadFile("/sys/class/dmi/id/product_serial")
	return version, postureText(string(kernel)), systemSerial(string(serial))
}
