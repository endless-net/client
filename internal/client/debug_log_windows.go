//go:build windows

package client

import (
	"fmt"
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	debugKernel32 = windows.NewLazySystemDLL("kernel32.dll")
	debugWTSAPI32 = windows.NewLazySystemDLL("wtsapi32.dll")
	debugUserenv  = windows.NewLazySystemDLL("userenv.dll")

	procWTSGetActiveConsoleSessionID = debugKernel32.NewProc("WTSGetActiveConsoleSessionId")
	procWTSQueryUserToken            = debugWTSAPI32.NewProc("WTSQueryUserToken")
	procGetUserProfileDirectory      = debugUserenv.NewProc("GetUserProfileDirectoryW")
)

func debugLogUserHome() (string, error) {
	home, err := os.UserHomeDir()
	if err == nil && strings.TrimSpace(home) != "" && !strings.Contains(strings.ToLower(home), `\system32\config\systemprofile`) {
		return home, nil
	}
	if interactiveHome, interactiveErr := windowsInteractiveUserHome(); interactiveErr == nil && strings.TrimSpace(interactiveHome) != "" {
		return interactiveHome, nil
	}
	if err == nil && strings.TrimSpace(home) != "" {
		return home, nil
	}
	return "", fmt.Errorf("resolve debug log user home: %w", err)
}

func windowsInteractiveUserHome() (string, error) {
	sessionID, _, _ := procWTSGetActiveConsoleSessionID.Call()
	if uint32(sessionID) == 0xffffffff {
		return "", fmt.Errorf("no active console session")
	}
	var token windows.Handle
	ret, _, err := procWTSQueryUserToken.Call(sessionID, uintptr(unsafe.Pointer(&token)))
	if ret == 0 {
		return "", fmt.Errorf("WTSQueryUserToken: %w", err)
	}
	defer func() { _ = windows.CloseHandle(token) }()
	var size uint32
	_, _, sizeErr := procGetUserProfileDirectory.Call(uintptr(token), 0, uintptr(unsafe.Pointer(&size)))
	if size == 0 {
		return "", fmt.Errorf("GetUserProfileDirectory returned empty size: %w", sizeErr)
	}
	buf := make([]uint16, size)
	ret, _, err = procGetUserProfileDirectory.Call(uintptr(token), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if ret == 0 {
		return "", fmt.Errorf("GetUserProfileDirectory: %w", err)
	}
	return strings.TrimSpace(windows.UTF16ToString(buf)), nil
}
