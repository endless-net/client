//go:build windows

package local

import (
	"context"
	"errors"
	"net"
	"runtime"
	"strings"
	"syscall"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

const pipeSDDL = `D:P(D;;GA;;;NU)(A;;GA;;;SY)(A;;GA;;;BA)(A;;GRGW;;;IU)`

var impersonate = windows.NewLazySystemDLL("advapi32.dll").NewProc("ImpersonateNamedPipeClient")

func validateEndpoint(endpoint string) (string, error) {
	if endpoint == "" {
		endpoint = DefaultWindowsPipe
	}
	// Reject remote UNC pipes; this protocol only authorizes local OS peers.
	if !strings.HasPrefix(endpoint, `\\.\pipe\`) || len(endpoint) <= len(`\\.\pipe\`) {
		return "", errors.New("IPC endpoint must be a local Windows named pipe")
	}
	return endpoint, nil
}

func listen(endpoint string) (net.Listener, error) {
	endpoint, err := validateEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	return winio.ListenPipe(endpoint, &winio.PipeConfig{
		SecurityDescriptor: pipeSDDL, InputBufferSize: 64 << 10, OutputBufferSize: 64 << 10,
	})
}

func dial(ctx context.Context, endpoint string) (net.Conn, error) {
	return winio.DialPipeAccessImpLevel(ctx, endpoint,
		windows.GENERIC_READ|windows.GENERIC_WRITE, winio.PipeImpLevelImpersonation)
}

func identify(conn net.Conn) (Peer, error) {
	fd, ok := conn.(interface{ Fd() uintptr })
	if !ok {
		return Peer{}, errors.New("local pipe does not expose its handle")
	}
	// A dedicated goroutine lets Go destroy the locked OS thread if reverting
	// impersonation fails. Never return an impersonated thread to the scheduler.
	type identification struct {
		peer Peer
		err  error
	}
	result := make(chan identification, 1)
	go func() {
		peer, err := identifyHandle(fd.Fd())
		result <- identification{peer, err}
	}()
	identified := <-result
	return identified.peer, identified.err
}

func identifyHandle(handle uintptr) (peer Peer, err error) {
	runtime.LockOSThread()
	result, _, callErr := impersonate.Call(handle)
	if result == 0 {
		runtime.UnlockOSThread()
		if callErr != syscall.Errno(0) {
			return Peer{}, callErr
		}
		return Peer{}, errors.New("local pipe impersonation failed")
	}
	defer func() {
		if revertErr := windows.RevertToSelf(); revertErr != nil {
			peer, err = Peer{}, revertErr
			return // The dedicated goroutine exits with its thread still locked.
		}
		runtime.UnlockOSThread()
	}()
	var token windows.Token
	if err := windows.OpenThreadToken(windows.CurrentThread(), windows.TOKEN_QUERY, true, &token); err != nil {
		return Peer{}, err
	}
	defer func() { _ = token.Close() }()
	user, err := token.GetTokenUser()
	if err != nil {
		return Peer{}, err
	}
	if user == nil || user.User.Sid == nil {
		return Peer{}, errors.New("local pipe token has no user SID")
	}
	adminSID, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return Peer{}, err
	}
	admin, err := token.IsMember(adminSID)
	if err != nil {
		return Peer{}, err
	}
	return Peer{Identity: user.User.Sid.String(), Administrator: admin}, nil
}
