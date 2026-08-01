//go:build windows

package client

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"syscall"

	"github.com/Microsoft/go-winio"
	ipc "github.com/endless-net/client/ipc/v1"
	"golang.org/x/sys/windows"
)

const (
	WindowsServicePipeSDDL = `D:P(D;;GA;;;NU)(A;;GA;;;SY)(A;;GA;;;BA)(A;;GRGW;;;IU)`
)

var procImpersonateNamedPipeClient = windows.NewLazySystemDLL("advapi32.dll").NewProc("ImpersonateNamedPipeClient")

func ListenWindowsServicePipe(name string) (net.Listener, error) {
	if name == "" {
		name = ipc.DefaultWindowsPipe
	}
	return winio.ListenPipe(name, &winio.PipeConfig{
		SecurityDescriptor: WindowsServicePipeSDDL,
		InputBufferSize:    64 * 1024,
		OutputBufferSize:   64 * 1024,
	})
}

func WindowsServiceIPCConnContext(ctx context.Context, conn net.Conn) context.Context {
	peer, err := WindowsServiceIPCPeerForConn(conn)
	if err != nil {
		return ctx
	}
	return ContextWithServiceIPCPeer(ctx, peer)
}

func WindowsServiceIPCPeerForConn(conn net.Conn) (ServiceIPCPeer, error) {
	if conn == nil {
		return ServiceIPCPeer{}, errors.New("service IPC connection is nil")
	}
	fdConn, ok := conn.(interface{ Fd() uintptr })
	if !ok {
		return ServiceIPCPeer{}, fmt.Errorf("service IPC connection %T does not expose a Windows handle", conn)
	}
	handle := windows.Handle(fdConn.Fd())
	if handle == 0 || handle == windows.InvalidHandle {
		return ServiceIPCPeer{}, errors.New("service IPC connection has an invalid Windows handle")
	}
	user, err := windowsNamedPipeClientUser(handle)
	if err != nil {
		return ServiceIPCPeer{}, err
	}
	identity, admin, err := windowsNamedPipeClientSecurity(handle)
	if err != nil {
		return ServiceIPCPeer{}, fmt.Errorf("identify service IPC named-pipe peer token: %w", err)
	}
	return ServiceIPCPeer{
		Transport: ServiceIPCTransportWindowsNamedPipe,
		User:      user,
		Identity:  identity,
		Admin:     admin,
	}, nil
}

func windowsNamedPipeClientUser(handle windows.Handle) (string, error) {
	var user [512]uint16
	if err := windows.GetNamedPipeHandleState(handle, nil, nil, nil, nil, &user[0], uint32(len(user))); err != nil {
		return "", fmt.Errorf("identify service IPC named-pipe peer: %w", err)
	}
	name := strings.TrimSpace(windows.UTF16ToString(user[:]))
	if name == "" {
		return "", errors.New("service IPC named-pipe peer did not provide a Windows user name")
	}
	return name, nil
}

func windowsNamedPipeClientSecurity(handle windows.Handle) (string, bool, error) {
	if err := impersonateNamedPipeClient(handle); err != nil {
		return "", false, err
	}
	defer func() { _ = windows.RevertToSelf() }()
	var token windows.Token
	thread := windows.CurrentThread()
	if err := windows.OpenThreadToken(thread, windows.TOKEN_QUERY, true, &token); err != nil {
		return "", false, err
	}
	defer func() { _ = token.Close() }()
	tokenUser, err := token.GetTokenUser()
	if err != nil {
		return "", false, err
	}
	if tokenUser == nil || tokenUser.User.Sid == nil {
		return "", false, errors.New("service IPC named-pipe peer token did not provide a user SID")
	}
	identity := strings.TrimSpace(tokenUser.User.Sid.String())
	if identity == "" {
		return "", false, errors.New("service IPC named-pipe peer token provided an empty user SID")
	}
	adminSID, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return "", false, err
	}
	admin, err := token.IsMember(adminSID)
	if err != nil {
		return "", false, err
	}
	return identity, admin, nil
}

func impersonateNamedPipeClient(handle windows.Handle) error {
	ret, _, err := procImpersonateNamedPipeClient.Call(uintptr(handle))
	if ret != 0 {
		return nil
	}
	if err != syscall.Errno(0) {
		return err
	}
	return errors.New("ImpersonateNamedPipeClient failed")
}
