package localipc

import (
	"context"
	"encoding/hex"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type linuxBootstrapFixture struct {
	clientv0.UnimplementedClientServiceServer
	config LinuxSocketConfig
}

func (s linuxBootstrapFixture) GetRuntimeInfo(ctx context.Context, _ *clientv0.GetRuntimeInfoRequest) (*clientv0.GetRuntimeInfoResponse, error) {
	who, err := Caller(ctx)
	if err != nil || who.SID != "uid:0" || !who.Administrator {
		return nil, status.Error(codes.Unauthenticated, "expected kernel root peer")
	}
	return &clientv0.GetRuntimeInfoResponse{Runtime: &clientv0.RuntimeInfo{
		InstanceId: s.config.InstanceID, Protocol: "endlessnet-client-ipc",
		ContractSha256: hex.EncodeToString(s.config.ContractDigest[:]),
	}}, nil
}

func TestLinuxIPCAuthenticatesAndChecksContract(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root in an isolated Linux test environment")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	config := LinuxSocketConfig{Directory: dir, InstanceID: uuid.NewString(), ContractDigest: [32]byte{1}, HandshakeTimeout: time.Second}
	listener, err := ListenLinux(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	server, err := NewLinuxServer(config, func(context.Context) (string, error) { return "", nil }, linuxBootstrapFixture{config: config}, 65536, 4, Observation{
		Unary: func(ctx context.Context, request any, _ *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
			return next(ctx, request)
		},
		Stream: func(service any, stream grpc.ServerStream, _ *grpc.StreamServerInfo, next grpc.StreamHandler) error {
			return next(service, stream)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); <-served })
	conn, err := ConnectLinux(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	remove, err := PublishLinuxInstance(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = remove() })
	instance, err := ReadLinuxInstance(dir)
	if err != nil || instance != config.InstanceID {
		t.Fatalf("published runtime hint not discoverable: %v", err)
	}
	wrongInstance := config
	wrongInstance.InstanceID = uuid.NewString()
	if _, err := PublishLinuxInstance(wrongInstance); err == nil {
		t.Fatal("replaced a live runtime hint")
	}
	if conn, err = ConnectLinux(t.Context(), wrongInstance); status.Code(err) != codes.FailedPrecondition {
		if conn != nil {
			_ = conn.Close()
		}
		t.Fatalf("wrong instance accepted: %v", err)
	}
	config.ContractDigest[0]++
	if conn, err = ConnectLinux(t.Context(), config); status.Code(err) != codes.FailedPrecondition {
		if conn != nil {
			_ = conn.Close()
		}
		t.Fatalf("wrong contract accepted: %v", err)
	}
}

func TestLinuxIPCDeniesUnprivilegedKernelPeer(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root in an isolated Linux test environment")
	}
	dir, err := os.MkdirTemp("", "en-ipc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(dir) })
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "socket")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	if err := os.Chmod(path, 0666); err != nil {
		t.Fatal(err)
	}
	if err := listener.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	childPath := filepath.Join(dir, "client")
	if err := os.WriteFile(childPath, binary, 0755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(childPath) })
	child := exec.CommandContext(t.Context(), childPath, "-test.run=^TestLinuxIPCUnprivilegedDialFixture$")
	child.Env = append(os.Environ(), "EN_TEST_SOCKET="+path)
	child.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 65534, Gid: 65534}}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Wait() }()
	c, err := listener.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	if _, auth, err := (linuxCredentials{}).ServerHandshake(c); err == nil || auth != nil {
		t.Fatal("kernel UID 65534 authenticated")
	}
}

func TestLinuxIPCUnprivilegedDialFixture(t *testing.T) {
	path := os.Getenv("EN_TEST_SOCKET")
	if path == "" {
		t.Skip("subprocess fixture")
	}
	conn, err := (&net.Dialer{Timeout: time.Second}).DialContext(t.Context(), "unix", path)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
}
