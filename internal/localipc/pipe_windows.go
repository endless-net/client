//go:build windows

package localipc

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"regexp"
	"runtime"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/net/http2"
	"golang.org/x/sys/windows"
	"google.golang.org/grpc/credentials"
)

const pipeAccess = windows.FILE_READ_DATA | windows.FILE_WRITE_DATA | windows.FILE_READ_ATTRIBUTES | windows.FILE_WRITE_ATTRIBUTES | windows.READ_CONTROL | windows.SYNCHRONIZE

var errPipe = errors.New("local pipe authentication failed")
var instancePattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// PipeConfig is installation-owned. ServerSID is the trusted OS service account,
// not a SID supplied by an RPC caller. ContractDigest is the exact installed SDK
// descriptor digest; a zero placeholder cannot start the boundary.
type PipeConfig struct {
	InstanceID       string
	ServerSID        string
	ContractDigest   [32]byte
	HandshakeTimeout time.Duration
}

func (c PipeConfig) validate() error {
	if !instancePattern.MatchString(c.InstanceID) || c.InstanceID == "00000000-0000-0000-0000-000000000000" || c.ContractDigest == [32]byte{} ||
		c.HandshakeTimeout <= 0 || c.HandshakeTimeout > 30*time.Second {
		return errPipe
	}
	sid, err := windows.StringToSid(c.ServerSID)
	if err != nil || sid.String() != c.ServerSID {
		return errPipe
	}
	return nil
}

func (c PipeConfig) path() string { return `\\.\pipe\endlessnet-client-` + c.InstanceID }

// Listen rejects remote pipe clients (go-winio's FILE_PIPE_REJECT_REMOTE_CLIENTS).
// Clients get data access, not FILE_CREATE_PIPE_INSTANCE. The service account
// retains full control. An existing first instance causes ListenPipe to fail.
func Listen(config PipeConfig, allowedSIDs []string) (net.Listener, error) {
	if err := config.validate(); err != nil || len(allowedSIDs) == 0 {
		return nil, errPipe
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil || user.User.Sid.String() != config.ServerSID {
		return nil, errPipe
	}
	sddl := "O:" + config.ServerSID + "D:P(A;;GA;;;" + config.ServerSID + ")"
	for _, value := range allowedSIDs {
		sid, err := windows.StringToSid(value)
		if err != nil || sid.String() != value {
			return nil, errPipe
		}
		sddl += "(A;;0x120183;;;" + value + ")"
	}
	return winio.ListenPipe(config.path(), &winio.PipeConfig{SecurityDescriptor: sddl, InputBufferSize: 65536, OutputBufferSize: 65536})
}

func Dial(ctx context.Context, config PipeConfig) (net.Conn, error) {
	if err := config.validate(); err != nil {
		return nil, err
	}
	return winio.DialPipeAccessImpLevel(ctx, config.path(), pipeAccess, winio.PipeImpLevelIdentification)
}

type pipeCredentials struct{ config PipeConfig }

func Credentials(config PipeConfig) (credentials.TransportCredentials, error) {
	if err := config.validate(); err != nil {
		return nil, err
	}
	return &pipeCredentials{config: config}, nil
}

func (c *pipeCredentials) Info() credentials.ProtocolInfo {
	return credentials.ProtocolInfo{SecurityProtocol: "windows-named-pipe"}
}

func (c *pipeCredentials) Clone() credentials.TransportCredentials {
	return &pipeCredentials{config: c.config}
}

func (c *pipeCredentials) OverrideServerName(name string) error {
	if name != c.config.InstanceID {
		return errPipe
	}
	return nil
}

func (c *pipeCredentials) ClientHandshake(ctx context.Context, _ string, conn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	fd, ok := conn.(interface{ Fd() uintptr })
	if !ok {
		return nil, nil, errPipe
	}
	sd, err := windows.GetSecurityInfo(windows.Handle(fd.Fd()), windows.SE_KERNEL_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return nil, nil, errPipe
	}
	owner, _, err := sd.Owner()
	if err != nil || owner.String() != c.config.ServerSID {
		return nil, nil, errPipe
	}
	// No additional wire preface: the published contract is ordinary gRPC/HTTP2.
	return conn, AuthInfo{CommonAuthInfo: credentials.CommonAuthInfo{SecurityLevel: credentials.PrivacyAndIntegrity}, principal: Principal{SID: c.config.ServerSID}}, nil
}

type bufferedPipe struct {
	net.Conn
	reader *bufio.Reader
}

func (p *bufferedPipe) Read(data []byte) (int, error) { return p.reader.Read(data) }

func (c *pipeCredentials) ServerHandshake(conn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	fd, ok := conn.(interface{ Fd() uintptr })
	if !ok {
		return nil, nil, errPipe
	}
	if err := conn.SetDeadline(time.Now().Add(c.config.HandshakeTimeout)); err != nil {
		return nil, nil, err
	}
	// Impersonation uses the context of the last message read from the pipe.
	// Peek the standard HTTP2 preface, authenticate, then replay those SAME bytes
	// to grpc-Go. This preserves the published gRPC wire and Dart interoperability.
	reader := bufio.NewReader(conn)
	preface, err := reader.Peek(len(http2.ClientPreface))
	if err != nil || !bytes.Equal(preface, []byte(http2.ClientPreface)) {
		return nil, nil, errPipe
	}
	principal, err := pipePrincipal(windows.Handle(fd.Fd()))
	if err != nil {
		return nil, nil, err
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return nil, nil, err
	}
	return &bufferedPipe{Conn: conn, reader: reader}, AuthInfo{CommonAuthInfo: credentials.CommonAuthInfo{SecurityLevel: credentials.PrivacyAndIntegrity}, principal: principal}, nil
}

func pipePrincipal(handle windows.Handle) (principal Principal, err error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	advapi := windows.NewLazySystemDLL("advapi32.dll")
	impersonate := advapi.NewProc("ImpersonateNamedPipeClient")
	revert := advapi.NewProc("RevertToSelf")
	ok, _, _ := impersonate.Call(uintptr(handle))
	if ok == 0 {
		return Principal{}, errPipe
	}
	defer func() {
		ok, _, _ := revert.Call()
		if ok == 0 {
			// Continuing a privileged service on an impersonated OS thread is
			// unsafe. Fail the process before this thread can execute more work.
			// A panic could be recovered above us and release an impersonated
			// thread back to the runtime. This security failure is unrecoverable.
			os.Exit(1)
		}
	}()
	var token windows.Token
	if err := windows.OpenThreadToken(windows.CurrentThread(), windows.TOKEN_QUERY, true, &token); err != nil {
		return Principal{}, errPipe
	}
	defer func() {
		if closeErr := token.Close(); closeErr != nil {
			principal, err = Principal{}, errPipe
		}
	}()
	user, err := token.GetTokenUser()
	if err != nil {
		return Principal{}, errPipe
	}
	adminSID, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return Principal{}, errPipe
	}
	admin, err := token.IsMember(adminSID)
	if err != nil {
		return Principal{}, errPipe
	}
	return Principal{SID: user.User.Sid.String(), Administrator: admin}, nil
}
