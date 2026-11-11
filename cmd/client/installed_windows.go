//go:build windows

package main

import (
	"errors"
	"strings"

	"github.com/endless-net/client/internal/clientapp"
	"github.com/endless-net/client/internal/windowsservice"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// MSI owns this service key and runs installed-init as SYSTEM. Both initialization
// and the service read the same explicit settings, using the same DPAPI identity.
func installedEnvironment() (map[string]string, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil || user.User.Sid.String() != "S-1-5-18" {
		return nil, clientapp.ErrConfig
	}
	var token windows.Token
	if err := windows.OpenThreadToken(windows.CurrentThread(), windows.TOKEN_QUERY, true, &token); err == nil {
		_ = token.Close()
		return nil, clientapp.ErrConfig
	} else if !errors.Is(err, windows.ERROR_NO_TOKEN) {
		return nil, clientapp.ErrConfig
	}
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Services\`+windowsservice.ServiceName, registry.QUERY_VALUE)
	if err != nil {
		return nil, clientapp.ErrConfig
	}
	values, _, readErr := key.GetStringsValue("Environment")
	closeErr := key.Close()
	if readErr != nil || closeErr != nil {
		return nil, clientapp.ErrConfig
	}
	return parseInstalledEnvironment(values)
}

func parseInstalledEnvironment(values []string) (map[string]string, error) {
	result := make(map[string]string, len(values))
	for _, value := range values {
		name, content, found := strings.Cut(value, "=")
		if _, exists := result[name]; !found || exists || !strings.HasPrefix(name, "ENDLESSNET_CLIENT_") || name != strings.ToUpper(name) || strings.ContainsAny(name, "\x00\r\n ") || strings.ContainsRune(content, 0) {
			return nil, clientapp.ErrConfig
		}
		result[name] = content
	}
	if len(result) == 0 || result["ENDLESSNET_CLIENT_SERVICE_SID"] != "S-1-5-18" {
		return nil, clientapp.ErrConfig
	}
	return result, nil
}
