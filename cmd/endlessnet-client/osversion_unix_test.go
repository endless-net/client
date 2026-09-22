//go:build linux || darwin

package main

import (
	"errors"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestDiagnosticsOSVersionUsesOnlyKernelRelease(t *testing.T) {
	got := diagnosticsOSVersionWithUname(func(info *unix.Utsname) error {
		copy(info.Release[:], "6.12.0-custom")
		copy(info.Nodename[:], "private-hostname")
		copy(info.Version[:], "private-build-description")
		return nil
	})
	kernel := "Linux"
	if runtime.GOOS == "darwin" {
		kernel = "Darwin"
	}
	if len(got) != 2 || got["name"] != runtime.GOOS || got["version"] != kernel+" kernel 6.12.0-custom" {
		t.Fatalf("unexpected public OS metadata: %v", got)
	}
}

func TestDiagnosticsOSVersionRejectsUnavailableOrMalformedRelease(t *testing.T) {
	for _, scenario := range []string{"error", "empty", "unterminated", "control", "non_ascii"} {
		t.Run(scenario, func(t *testing.T) {
			got := diagnosticsOSVersionWithUname(func(info *unix.Utsname) error {
				switch scenario {
				case "error":
					return errors.New("private inspection failure")
				case "unterminated":
					copy(info.Release[:], strings.Repeat("x", len(info.Release)))
				case "control":
					copy(info.Release[:], "6.12\nprivate")
				case "non_ascii":
					info.Release[0] = 255
				}
				return nil
			})
			if len(got) != 1 || got["name"] != runtime.GOOS {
				t.Fatal("unavailable version fabricated", got)
			}
		})
	}
}
