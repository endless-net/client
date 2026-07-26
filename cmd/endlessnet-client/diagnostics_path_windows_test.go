//go:build windows

package main

import (
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestDiagnosticsStoreRejectsWindowsFileReadableByEveryone(t *testing.T) {
	dir := t.TempDir()
	prepareDiagnosticsTestDirectory(t, dir)
	store := newDiagnosticsStore(dir)
	bundle, err := store.Write(map[string]any{"status": "ok"})
	if err != nil {
		t.Fatal(err)
	}
	setDiagnosticsWindowsDACL(t, bundle.Path, false, true)
	if err := store.Prune(); err == nil {
		t.Fatal("Prune accepted a diagnostics bundle readable by Everyone")
	}
}

func prepareDiagnosticsTestDirectory(t *testing.T, path string) {
	t.Helper()
	setDiagnosticsWindowsDACL(t, path, true, false)
}

func setDiagnosticsWindowsDACL(t *testing.T, path string, inheritable, allowEveryone bool) {
	t.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	trustees := []string{"SY", "BA", user.User.Sid.String()}
	if allowEveryone {
		trustees = append(trustees, "WD")
	}
	flags := ""
	if inheritable {
		flags = "OICI"
	}
	var sddl strings.Builder
	sddl.WriteString("D:P")
	for _, trustee := range trustees {
		sddl.WriteString("(A;" + flags + ";FA;;;" + trustee + ")")
	}
	securityDescriptor, err := windows.SecurityDescriptorFromString(sddl.String())
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := securityDescriptor.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		dacl,
		nil,
	); err != nil {
		t.Fatal(err)
	}
}
