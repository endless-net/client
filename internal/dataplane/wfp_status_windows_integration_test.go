//go:build windows && integration

package dataplane

import (
	"testing"
	"time"

	"github.com/tailscale/wf"
	"golang.org/x/sys/windows"
)

// Queries fresh, nonexistent keys only. No transaction/create/delete or installed
// state is involved. This exercises actual Win64 DWORD return normalization.
func TestWindowsWFPMissingKeysReadOnly(t *testing.T) {
	api, err := openFilterAPI(100*time.Millisecond, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := api.close(); err != nil {
			t.Error(err)
		}
	})
	key, err := windows.GenerateGUID()
	if err != nil {
		t.Fatal(err)
	}
	if err := api.checkSublayer(key); !sublayerMissing(err) {
		t.Fatal("native missing sublayer was not recognized", err)
	}
	if _, err := api.check(&wf.Rule{ID: wf.RuleID(key)}); !filterMissing(err) {
		t.Fatal("native missing filter was not recognized", err)
	}
}

// The transaction is always aborted: no filter, route or installed state is
// changed. This exercises BFE's real sublayer normalization rather than a mock.
func TestWindowsWFPSublayerRoundtripAbortOnly(t *testing.T) {
	api, err := openFilterAPI(100*time.Millisecond, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := api.close(); err != nil {
			t.Error(err)
		}
	})
	key, err := windows.GenerateGUID()
	if err != nil {
		t.Fatal(err)
	}
	if err := api.begin(); err != nil {
		t.Fatal(err)
	}
	aborted := false
	t.Cleanup(func() {
		if !aborted {
			if err := api.abort(); err != nil {
				t.Error(err)
			}
		}
	})
	if err := api.sublayer(key); err != nil {
		t.Fatal(err)
	}
	if err := api.checkSublayer(key); err != nil {
		t.Fatal("own sublayer failed exact roundtrip", err)
	}
	if err := api.abort(); err != nil {
		t.Fatal(err)
	}
	aborted = true
	if err := api.checkSublayer(key); !sublayerMissing(err) {
		t.Fatal("aborted sublayer still present", err)
	}
}
