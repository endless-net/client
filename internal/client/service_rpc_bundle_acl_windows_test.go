//go:build windows

package client

import (
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestRPCBundleStoreRejectsWindowsFileReadableByEveryone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bundles.state")
	store, err := openClientRPCBundleStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.put("owner", "profile", []byte("archive")); err != nil {
		t.Fatal(err)
	}
	if _, err := openClientRPCBundleStore(path, nil); err != nil {
		t.Fatal("private ACL rejected", err)
	}
	setDiagnosticsWindowsDACL(t, path, false, true)
	if restored, err := openClientRPCBundleStore(path, nil); err == nil || restored != nil {
		t.Fatal("publicly readable archive accepted")
	}
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
