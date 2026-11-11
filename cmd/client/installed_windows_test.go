//go:build windows

package main

import (
	"testing"

	"golang.org/x/sys/windows"
)

func TestInstalledEnvironmentIsExplicitAndCannotOverrideOtherProcessSettings(t *testing.T) {
	sid := "ENDLESSNET_CLIENT_SERVICE_SID=S-1-5-18"
	values, err := parseInstalledEnvironment([]string{sid, "ENDLESSNET_CLIENT_CONTROL_ORIGIN=https://control.example", "ENDLESSNET_CLIENT_TEST=a=b"})
	if err != nil || values["ENDLESSNET_CLIENT_TEST"] != "a=b" || len(values) != 3 {
		t.Fatal("registry environment parsing")
	}
	for _, invalid := range [][]string{nil, {sid, sid}, {sid, "PATH=untrusted"}, {sid, "ENDLESSNET_CLIENT_TEST"}, {sid, "endlessnet_client_test=value"}, {"ENDLESSNET_CLIENT_SERVICE_SID=S-1-5-19"}} {
		if values, err := parseInstalledEnvironment(invalid); err == nil || values != nil {
			t.Fatal("invalid service settings accepted")
		}
	}
}

func TestInstalledEnvironmentRefusesInteractiveCaller(t *testing.T) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	if user.User.Sid.String() == "S-1-5-18" {
		t.Skip("ordinary caller boundary")
	}
	if values, err := installedEnvironment(); err == nil || values != nil {
		t.Fatal("interactive process read installed service configuration")
	}
}
