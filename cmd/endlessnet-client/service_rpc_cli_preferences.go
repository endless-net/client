package main

import (
	"fmt"
	"strings"

	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/encoding/protojson"
)

func nativePreferencePatch(raw string) (*ipc.PreferencesPatch, error) {
	if strings.TrimSpace(raw) == "" || len(raw) > 4096 {
		return nil, fmt.Errorf("--patch requires a bounded protobuf JSON preference patch")
	}
	patch := &ipc.PreferencesPatch{}
	if err := protojson.Unmarshal([]byte(raw), patch); err != nil {
		return nil, fmt.Errorf("invalid preference patch")
	}
	if patch.AllowInbound == nil && patch.AcceptDns == nil && patch.AcceptRoutes == nil && patch.RuntimeStart == nil && patch.UiQuit == nil && patch.UserLogoff == nil && patch.Suspend == nil && patch.Resume == nil {
		return nil, fmt.Errorf("preference patch must contain at least one explicit value")
	}
	for _, value := range []*ipc.LifecycleBehavior{patch.RuntimeStart, patch.UiQuit, patch.UserLogoff, patch.Suspend, patch.Resume} {
		if value != nil && (*value < ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_KEEP_INTENT || *value > ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_PLATFORM_MANAGED) {
			return nil, fmt.Errorf("invalid lifecycle preference value")
		}
	}
	return patch, nil
}

func nativePreferenceResetKeys(raw string) ([]ipc.PreferenceKey, error) {
	if len(raw) > 1024 {
		return nil, fmt.Errorf("invalid reset keys")
	}
	known := map[string]ipc.PreferenceKey{
		"allow-inbound": ipc.PreferenceKey_PREFERENCE_KEY_ALLOW_INBOUND,
		"accept-dns":    ipc.PreferenceKey_PREFERENCE_KEY_ACCEPT_DNS,
		"accept-routes": ipc.PreferenceKey_PREFERENCE_KEY_ACCEPT_ROUTES,
		"runtime-start": ipc.PreferenceKey_PREFERENCE_KEY_RUNTIME_START,
		"ui-quit":       ipc.PreferenceKey_PREFERENCE_KEY_UI_QUIT,
		"user-logoff":   ipc.PreferenceKey_PREFERENCE_KEY_USER_LOGOFF,
		"suspend":       ipc.PreferenceKey_PREFERENCE_KEY_SUSPEND,
		"resume":        ipc.PreferenceKey_PREFERENCE_KEY_RESUME,
	}
	seen := map[ipc.PreferenceKey]bool{}
	var keys []ipc.PreferenceKey
	for _, name := range strings.Split(raw, ",") {
		key, ok := known[strings.TrimSpace(name)]
		if !ok || seen[key] {
			return nil, fmt.Errorf("--keys requires distinct known preference names")
		}
		seen[key] = true
		keys = append(keys, key)
	}
	return keys, nil
}
