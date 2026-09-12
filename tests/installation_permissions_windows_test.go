//go:build windows

package tests

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Restrict only child processes on the opted-in disposable installer runner.
// This preserves the account SID; another local owner's rights are separate.
func assertInstalledPeerDenied(t *testing.T, binary string) {
	t.Helper()
	var source windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_ALL_ACCESS, &source); err != nil {
		t.Fatal("could not open the installer process token")
	}
	defer func() { _ = source.Close() }()
	adminSID, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		t.Fatal("could not identify the local administrators group")
	}
	disabled := windows.SIDAndAttributes{Sid: adminSID}
	var restricted windows.Token
	createRestricted := windows.NewLazySystemDLL("advapi32.dll").NewProc("CreateRestrictedToken")
	const disableMaxPrivilege = 1
	ok, _, _ := createRestricted.Call(uintptr(source), disableMaxPrivilege, 1, uintptr(unsafe.Pointer(&disabled)), 0, 0, 0, 0, uintptr(unsafe.Pointer(&restricted)))
	runtime.KeepAlive(adminSID)
	if ok == 0 {
		t.Fatal("could not create a restricted CLI process token")
	}
	defer func() { _ = restricted.Close() }()
	groups, err := restricted.GetTokenGroups()
	if err != nil {
		t.Fatal("could not verify restricted token groups")
	}
	foundDisabledAdmin := false
	for _, group := range groups.AllGroups() {
		if group.Sid.Equals(adminSID) {
			foundDisabledAdmin = group.Attributes&windows.SE_GROUP_USE_FOR_DENY_ONLY != 0 && group.Attributes&windows.SE_GROUP_ENABLED == 0
		}
	}
	if !foundDisabledAdmin {
		t.Fatal("restricted CLI token retained enabled administrator membership")
	}
	runRestricted := func(args ...string) (string, string, error) {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{Token: syscall.Token(restricted), HideWindow: true}
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		if ctx.Err() != nil {
			t.Fatal("restricted CLI command exceeded the harness deadline")
		}
		return stdout.String(), stderr.String(), err
	}
	version, _, err := runRestricted("version")
	if err != nil || !strings.HasPrefix(version, "endlessnet-client ") {
		t.Fatal("restricted token could not execute the installed CLI")
	}
	stdout, stderr, err := runRestricted("service", "local-forget", "--confirm-local-forget", "--timeout", "2s")
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 || stdout != "" {
		t.Fatal("restricted Windows peer did not reject administrator-only local-forget (output withheld)")
	}
	switch {
	case strings.Contains(stderr, "requires an administrator/root local peer"):
		t.Log("restricted Windows peer: administrator operation rejected by IPC authorization")
	case strings.Contains(strings.ToLower(stderr), "access is denied"):
		t.Log("restricted Windows peer: access rejected by named-pipe transport; application-role access not established")
	default:
		t.Fatal("restricted Windows peer failed for an unclassified reason (output withheld)")
	}
}
