//go:build windows

package client

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

// QueryWindowsSessionOwner reads the logged-on user's SID without retaining
// the token handle. It must run in the authorized Windows service context.
func QueryWindowsSessionOwner(session uint32) (string, error) {
	if session == 0 || session == ^uint32(0) {
		return "", errors.New("windows user session required")
	}
	var token windows.Token
	if err := windows.WTSQueryUserToken(session, &token); err != nil {
		return "", errors.New("windows session token unavailable")
	}
	defer func() { _ = token.Close() }()
	user, err := token.GetTokenUser()
	if err != nil || user == nil || user.User.Sid == nil {
		return "", errors.New("windows session owner unavailable")
	}
	// Same canonical identity representation as clientipc/local's pipe peer.
	return user.User.Sid.String(), nil
}

// EnumerateWindowsUserSessions returns copied IDs, never native buffer pointers.
func EnumerateWindowsUserSessions() ([]uint32, error) {
	var sessions *windows.WTS_SESSION_INFO
	var count uint32
	if err := windows.WTSEnumerateSessions(0, 0, 1, &sessions, &count); err != nil {
		return nil, errors.New("windows session enumeration unavailable")
	}
	if sessions != nil {
		defer windows.WTSFreeMemory(uintptr(unsafe.Pointer(sessions)))
	}
	if count > maxWindowsUserSessions || (count != 0 && sessions == nil) {
		return nil, errors.New("windows session enumeration invalid")
	}
	result := make([]uint32, 0, count)
	for _, session := range unsafe.Slice(sessions, count) {
		if session.SessionID != 0 && session.SessionID != ^uint32(0) {
			result = append(result, session.SessionID)
		}
	}
	return result, nil
}
