package main

import (
	"strings"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestNativePreferencePatchPreservesPresence(t *testing.T) {
	patch, err := nativePreferencePatch(`{"allowInbound":false,"acceptDns":true,"uiQuit":"LIFECYCLE_BEHAVIOR_DISCONNECT"}`)
	if err != nil {
		t.Fatal(err)
	}
	if patch.AllowInbound == nil || *patch.AllowInbound || patch.AcceptDns == nil || !*patch.AcceptDns || patch.AcceptRoutes != nil || patch.RuntimeStart != nil || patch.GetUiQuit() != ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT {
		t.Fatal("preference presence changed")
	}
	for _, raw := range []string{"", "{}", `{"acceptDns":null}`, `{"acceptDns":"false"}`, `{"unexpected":"sensitive-input"}`, `{"uiQuit":0}`, `{"uiQuit":99}`, `{"acceptDns":true,"acceptDns":false}`, strings.Repeat("x", 4097)} {
		if _, err := nativePreferencePatch(raw); err == nil || strings.Contains(err.Error(), "sensitive-input") {
			t.Fatal("invalid patch accepted or input echoed")
		}
	}
}

func TestNativePreferenceResetUsesOnlyExplicitDistinctKeys(t *testing.T) {
	keys, err := nativePreferenceResetKeys(" accept-dns, ui-quit ")
	if err != nil || len(keys) != 2 || keys[0] != ipc.PreferenceKey_PREFERENCE_KEY_ACCEPT_DNS || keys[1] != ipc.PreferenceKey_PREFERENCE_KEY_UI_QUIT {
		t.Fatal("reset keys changed", err)
	}
	for _, raw := range []string{"", "all", "accept-dns,accept-dns", "accept-dns,", "unknown", strings.Repeat("x", 1025)} {
		if _, err := nativePreferenceResetKeys(raw); err == nil {
			t.Fatal("implicit or invalid reset accepted")
		}
	}
}
