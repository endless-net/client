package client

import (
	"fmt"
	"os"
	"runtime"
	"strings"
)

type WireGuardApplyResult struct {
	OK         bool   `json:"ok"`
	Method     string `json:"method"`
	Interface  string `json:"interface,omitempty"`
	Changed    bool   `json:"changed"`
	Skipped    bool   `json:"skipped,omitempty"`
	Reason     string `json:"reason,omitempty"`
	DownError  string `json:"down_error,omitempty"`
	UpError    string `json:"up_error,omitempty"`
	SyncError  string `json:"sync_error,omitempty"`
	RouteError string `json:"route_error,omitempty"`
}

func RequireWireGuardPrivileges() error {
	if runtime.GOOS == "windows" {
		return nil
	}
	if uid := os.Geteuid(); uid != 0 {
		return fmt.Errorf("wireguard-go TUN and route configuration requires root privileges (effective uid %d)", uid)
	}
	return nil
}

func safeWireGuardInterfaceName(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 15 {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.' {
			continue
		}
		return false
	}
	return true
}
