package localipc

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/google/uuid"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

const linuxIPCProtocol = "unix-peer-credentials"
const LinuxRuntimeDirectory = "/run/endlessnet-client"
const linuxInstanceFile = "instance"

var errLinuxSocket = errors.New("invalid or inaccessible Client Unix socket")

type LinuxSocketConfig struct {
	Directory        string
	InstanceID       string
	ContractDigest   [32]byte
	HandshakeTimeout time.Duration
}

func (c LinuxSocketConfig) valid() bool {
	id, err := uuid.Parse(c.InstanceID)
	return err == nil && id != uuid.Nil && id.String() == c.InstanceID &&
		c.ContractDigest != [32]byte{} && c.HandshakeTimeout > 0 &&
		filepath.IsAbs(c.Directory) && filepath.Clean(c.Directory) == c.Directory && c.Directory != "/"
}

func linuxSocketPath(directory string) string { return filepath.Join(directory, "client.sock") }

func validLinuxDirectory(path string) bool {
	var metadata unix.Stat_t
	return unix.Lstat(path, &metadata) == nil && metadata.Uid == 0 &&
		metadata.Mode&unix.S_IFMT == unix.S_IFDIR && metadata.Mode&0777 == 0700
}

func ReadLinuxInstance(directory string) (string, error) {
	if !validLinuxDirectory(directory) {
		return "", errLinuxSocket
	}
	name := filepath.Join(directory, linuxInstanceFile)
	fd, err := unix.Openat2(unix.AT_FDCWD, name, &unix.OpenHow{
		Flags: unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NONBLOCK, Resolve: unix.RESOLVE_NO_SYMLINKS,
	})
	if err != nil {
		return "", errLinuxSocket
	}
	file := os.NewFile(uintptr(fd), name)
	defer func() { _ = file.Close() }()
	var metadata unix.Stat_t
	if unix.Fstat(fd, &metadata) != nil || metadata.Uid != 0 || metadata.Nlink != 1 ||
		metadata.Mode&unix.S_IFMT != unix.S_IFREG || metadata.Mode&07777 != 0600 ||
		metadata.Size != 36 {
		return "", errLinuxSocket
	}
	data, err := io.ReadAll(io.LimitReader(file, 37))
	if err != nil {
		return "", errLinuxSocket
	}
	value := string(data)
	id, err := uuid.Parse(value)
	if err != nil || id == uuid.Nil || id.String() != value {
		return "", errLinuxSocket
	}
	return value, nil
}

// PublishLinuxInstance makes the probed instance discoverable to the CLI.
// Never replace a hint that may belong to a different service instance.
func PublishLinuxInstance(config LinuxSocketConfig) (func() error, error) {
	if !config.valid() || !validLinuxDirectory(config.Directory) || os.Geteuid() != 0 {
		return nil, errLinuxSocket
	}
	tmp, err := os.CreateTemp(config.Directory, ".instance-")
	if err != nil {
		return nil, errLinuxSocket
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	_, writeErr := io.WriteString(tmp, config.InstanceID)
	if err := errors.Join(writeErr, tmp.Close()); err != nil {
		return nil, errLinuxSocket
	}
	path := filepath.Join(config.Directory, linuxInstanceFile)
	if err := unix.Renameat2(unix.AT_FDCWD, tmp.Name(), unix.AT_FDCWD, path, unix.RENAME_NOREPLACE); err != nil {
		return nil, errLinuxSocket
	}
	return func() error {
		if id, err := ReadLinuxInstance(config.Directory); err != nil || id != config.InstanceID {
			return errLinuxSocket
		}
		return os.Remove(path)
	}, nil
}

// ListenLinux creates the socket only inside the private service directory.
// systemd owns directory lifetime; an existing socket is never unlinked here.
func ListenLinux(config LinuxSocketConfig) (*net.UnixListener, error) {
	if !config.valid() || !validLinuxDirectory(config.Directory) || os.Geteuid() != 0 {
		return nil, errLinuxSocket
	}
	path := linuxSocketPath(config.Directory)
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, errLinuxSocket
	}
	if err := os.Chmod(path, 0600); err != nil {
		_ = listener.Close()
		return nil, errLinuxSocket
	}
	return listener, nil
}

func NewLinuxServer(config LinuxSocketConfig, owner OwnerReader, implementation clientv0.ClientServiceServer, maxMessageBytes int, maxConcurrent uint32, observation Observation) (*grpc.Server, error) {
	if !config.valid() || owner == nil || implementation == nil ||
		maxMessageBytes <= 0 || maxConcurrent == 0 || observation.Unary == nil || observation.Stream == nil {
		return nil, errLinuxSocket
	}
	server := grpc.NewServer(grpc.Creds(linuxCredentials{}),
		grpc.ChainUnaryInterceptor(observation.Unary, Unary(owner)),
		grpc.ChainStreamInterceptor(observation.Stream, Stream(owner)),
		grpc.ConnectionTimeout(config.HandshakeTimeout), grpc.MaxConcurrentStreams(maxConcurrent),
		grpc.MaxRecvMsgSize(maxMessageBytes), grpc.MaxSendMsgSize(maxMessageBytes))
	clientv0.RegisterClientServiceServer(server, implementation)
	return server, nil
}

func ConnectLinux(ctx context.Context, config LinuxSocketConfig) (*grpc.ClientConn, error) {
	if !config.valid() || !validLinuxDirectory(config.Directory) {
		return nil, errLinuxSocket
	}
	var dialer net.Dialer
	conn, err := grpc.NewClient("passthrough:///endlessnet-local", grpc.WithTransportCredentials(linuxCredentials{}),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", linuxSocketPath(config.Directory))
		}))
	if err != nil {
		return nil, err
	}
	operation, cancel := context.WithTimeout(ctx, config.HandshakeTimeout)
	defer cancel()
	info, err := clientv0.NewClientServiceClient(conn).GetRuntimeInfo(operation, &clientv0.GetRuntimeInfoRequest{})
	if err == nil {
		err = CheckRuntimeInfo(info.GetRuntime(), config.InstanceID, config.ContractDigest)
	}
	if err != nil {
		return nil, errors.Join(err, conn.Close())
	}
	return conn, nil
}

type linuxCredentials struct{}

func (linuxCredentials) Info() credentials.ProtocolInfo {
	return credentials.ProtocolInfo{SecurityProtocol: linuxIPCProtocol}
}

func (c linuxCredentials) Clone() credentials.TransportCredentials { return c }

func (linuxCredentials) OverrideServerName(string) error { return errLinuxSocket }

func (linuxCredentials) ClientHandshake(ctx context.Context, _ string, conn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return linuxPeer(conn)
}

func (linuxCredentials) ServerHandshake(conn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	return linuxPeer(conn)
}

func linuxPeer(conn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	socket, ok := conn.(syscall.Conn)
	if !ok || conn.LocalAddr().Network() != "unix" {
		return nil, nil, errLinuxSocket
	}
	raw, err := socket.SyscallConn()
	if err != nil {
		return nil, nil, errLinuxSocket
	}
	var peer *unix.Ucred
	var peerErr error
	if err := raw.Control(func(fd uintptr) {
		peer, peerErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil || peerErr != nil || peer == nil || peer.Uid != 0 {
		return nil, nil, errLinuxSocket
	}
	return conn, AuthInfo{
		CommonAuthInfo: credentials.CommonAuthInfo{SecurityLevel: credentials.PrivacyAndIntegrity},
		principal:      Principal{SID: "uid:0", Administrator: true}, transport: linuxIPCProtocol,
	}, nil
}
