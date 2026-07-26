//go:build darwin

package client

import (
	"strconv"
	"strings"
)

func platformUserspaceTUNName(requested string) string {
	requested = strings.TrimSpace(requested)
	if requested == "utun" {
		return requested
	}
	if suffix, ok := strings.CutPrefix(requested, "utun"); ok {
		if index, err := strconv.Atoi(suffix); err == nil && index >= 0 {
			return requested
		}
	}
	return "utun"
}
