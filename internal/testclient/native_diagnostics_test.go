package testclient

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestNativeStartupStagesUseProfileAndWithholdContext(t *testing.T) {
	for _, tc := range []struct {
		name, status, logs  string
		failCall, wantCalls int
		wantError           bool
	}{
		{"native", `{"status":{"activeProfileId":"profile"}}`, `{"logs":[{"message":"WireGuard engine: routes complete"},{"message":"synthetic-private"},{"message":"WireGuard engine: routes complete synthetic-private"}]}`, 0, 2, false},
		{"missing-profile", `{"status":{}}`, `{}`, 0, 1, true},
		{"legacy-status", `{"state":"connected"}`, `{}`, 0, 1, true},
		{"status-failure", `{}`, `{}`, 1, 1, true},
		{"logs-failure", `{"status":{"activeProfileId":"profile"}}`, `{}`, 2, 2, true},
		{"legacy-logs", `{"status":{"activeProfileId":"profile"}}`, `{"protocol":"endlessnet-client-ipc","logs":[]}`, 0, 2, true},
		{"malformed-logs", `{"status":{"activeProfileId":"profile"}}`, `synthetic-private`, 0, 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			stages, err := nativeStartupStages(ctx, func(got context.Context, operation string, options ...string) ([]byte, error) {
				calls++
				if got != ctx {
					t.Fatal("diagnostic deadline replaced")
				}
				if calls == 1 && (operation != "status" || len(options) != 0) {
					t.Fatal("status must resolve current profile first")
				}
				if calls == 2 && (operation != "logs-recent" || !reflect.DeepEqual(options, []string{"--profile-id", "profile", "--page-size", "500"})) {
					t.Fatal("logs must use native profile and bounded page")
				}
				if calls == tc.failCall {
					return []byte("synthetic-private"), errors.New("synthetic-private")
				}
				if calls == 1 {
					return []byte(tc.status), nil
				}
				return []byte(tc.logs), nil
			})
			if calls != tc.wantCalls || (err != nil) != tc.wantError {
				t.Fatalf("calls=%d error=%v", calls, err)
			}
			if err != nil && (strings.Contains(err.Error(), "synthetic-private") || len(stages) != 0) {
				t.Fatal("failure leaked context")
			}
			if !tc.wantError && !reflect.DeepEqual(stages, []string{"routes complete"}) {
				t.Fatal("unrecognized log context escaped")
			}
		})
	}
}
