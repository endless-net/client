//go:build windows

package dataplane

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

type effectiveDNSRule struct {
	Namespaces   []string
	Servers      []string
	DirectAccess bool
	DNSSEC       bool
}

func readWindowsDNSPolicy(ctx context.Context) ([]effectiveDNSRule, error) {
	system, err := windows.GetSystemDirectory()
	if err != nil {
		return nil, err
	}
	// No map, registry or user-provided string is interpolated into this command.
	const script = `$ErrorActionPreference='Stop'
Import-Module "$PSHOME\Modules\DnsClient\DnsClient.psd1"
$rules = @(DnsClient\Get-DnsClientNrptPolicy -Effective | ForEach-Object {
[pscustomobject]@{Namespaces=@($_.Namespace);Servers=@($_.NameServers);DirectAccess=[bool]$_.DirectAccessEnabled;DNSSEC=[bool]$_.DnsSecValidationRequired}
})
ConvertTo-Json -InputObject $rules -Compress -Depth 3`
	cmd := exec.CommandContext(ctx, filepath.Join(system, "WindowsPowerShell", "v1.0", "powershell.exe"), "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	const limit = 1 << 20
	raw, readErr := io.ReadAll(io.LimitReader(out, limit+1))
	_ = out.Close()
	if len(raw) > limit || readErr != nil {
		_ = cmd.Process.Kill()
	}
	if err := errors.Join(readErr, cmd.Wait()); err != nil {
		return nil, err
	}
	if len(raw) > limit {
		return nil, ErrConfiguration
	}
	var rules []effectiveDNSRule
	if err := json.Unmarshal(raw, &rules); err != nil {
		return nil, err
	}
	if rules == nil {
		return nil, ErrConfiguration
	}
	return rules, nil
}

// Confirm every assigned exact name and suffix and reject more-specific
// overrides inside an assigned suffix. Withdrawal requires the old endpoint
// to have disappeared from the effective policy, including duplicated rules.
func matchesWindowsDNSPolicy(rules []effectiveDNSRule, old, next *windowsDNSAssignment) bool {
	wanted := map[string]bool{}
	server := ""
	if next != nil {
		server = next.proxy.addr.Addr().String()
		for _, name := range next.namespaces {
			wanted[name] = false
		}
	}
	for _, rule := range rules {
		var servers []string
		for _, list := range rule.Servers {
			servers = append(servers, strings.FieldsFunc(list, func(r rune) bool { return r == ',' || r == ';' || r == ' ' || r == '\t' })...)
		}
		if old != nil && slices.Contains(servers, old.proxy.addr.Addr().String()) {
			return false
		}
		for _, name := range rule.Namespaces {
			name = strings.ToLower(name)
			if name != "." {
				name = strings.TrimSuffix(name, ".")
			}
			if _, ok := wanted[name]; ok {
				if wanted[name] || len(servers) != 1 || servers[0] != server || rule.DirectAccess || rule.DNSSEC {
					return false
				}
				wanted[name] = true
				continue
			}
			if next != nil && slices.Contains(servers, server) {
				return false
			}
			if next == nil {
				continue
			}
			for suffix := range wanted {
				if suffix == "." || strings.HasPrefix(suffix, ".") && strings.HasSuffix(name, suffix) {
					return false
				}
			}
		}
	}
	for _, found := range wanted {
		if !found {
			return false
		}
	}
	return true
}

func verifyWindowsDNSPolicy(ctx context.Context, old, next *windowsDNSAssignment) error {
	for {
		rules, err := readWindowsDNSPolicy(ctx)
		if err != nil {
			return err
		}
		if matchesWindowsDNSPolicy(rules, old, next) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}
