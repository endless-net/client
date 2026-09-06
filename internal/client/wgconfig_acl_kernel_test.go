package client

import (
	"io"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
)

// Runs only in a fresh CI network namespace; never modifies runner networking.
func TestACLKernelEstablishedRevocation(t *testing.T) {
	if testing.Short() || os.Getenv("ENDLESSNET_REQUIRE_KERNEL_ACL_TEST") != "1" {
		t.Skip("isolated privileged CI only")
	}
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		t.Fatal("kernel ACL test requires Linux root")
	}
	self, err := os.Readlink("/proc/self/ns/net")
	if err != nil {
		t.Fatal(err)
	}
	init, err := os.Readlink("/proc/1/ns/net")
	if err != nil || self == init {
		t.Fatal("refusing to alter the host network namespace")
	}
	command := func(name string, args ...string) {
		t.Helper()
		if output, err := exec.CommandContext(t.Context(), name, args...).CombinedOutput(); err != nil {
			t.Fatalf("isolated network command %s failed: %v: %s", name, err, output)
		}
	}
	command("ip", "link", "set", "lo", "up")
	for _, transport := range []string{"tcp", "udp"} {
		for _, address := range []string{"127.0.0.1:0", "[::1]:0"} {
			t.Run(transport+"/"+address, func(t *testing.T) {
				endpoint := aclKernelEchoEndpoint(t, transport, address)
				prefix := endpoint.IP.String() + "/32"
				if endpoint.IP.To4() == nil {
					prefix = endpoint.IP.String() + "/128"
				}
				peer := clientapi.Peer{AllowedIPs: []string{prefix}, ACLRestricted: true,
					ACLGrants: []clientapi.ACLGrant{{DestinationCIDRs: []string{prefix}, AllowedPorts: []clientapi.ACLPort{{Protocol: transport, Port: endpoint.Port}}}},
				}
				apply := func() {
					for _, hook := range renderACLFirewallHooks([]clientapi.Peer{peer}) {
						if script, ok := strings.CutPrefix(hook, "PostUp = "); ok {
							command("sh", "-ec", strings.ReplaceAll(script, "%i", "lo"))
						}
					}
				}
				apply()
				connection, err := net.DialTimeout(transport, endpoint.String(), 2*time.Second)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = connection.Close() }()
				exchange := func() error {
					if err := connection.SetDeadline(time.Now().Add(time.Second)); err != nil {
						return err
					}
					if _, err := connection.Write([]byte("x")); err != nil {
						return err
					}
					buffer := make([]byte, 1)
					_, err := io.ReadFull(connection, buffer)
					return err
				}
				if err := exchange(); err != nil {
					t.Fatalf("allowed established traffic/replies failed: %v", err)
				}
				peer.ACLGrants = nil
				apply()
				if err := exchange(); err == nil {
					t.Fatal("revoked established connection still delivered traffic")
				}
			})
		}
	}
}

func aclKernelEchoEndpoint(t *testing.T, transport, address string) *net.TCPAddr {
	t.Helper()
	if transport == "tcp" {
		listener, err := net.Listen("tcp", address)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = listener.Close() })
		go func() {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			defer func() { _ = connection.Close() }()
			_ = connection.SetDeadline(time.Now().Add(10 * time.Second))
			_, _ = io.Copy(connection, connection)
		}()
		return listener.Addr().(*net.TCPAddr)
	}
	listener, err := net.ListenPacket("udp", address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		buffer := make([]byte, 16)
		for {
			count, remote, err := listener.ReadFrom(buffer)
			if err != nil {
				return
			}
			if _, err := listener.WriteTo(buffer[:count], remote); err != nil {
				return
			}
		}
	}()
	endpoint := listener.LocalAddr().(*net.UDPAddr)
	return &net.TCPAddr{IP: endpoint.IP, Port: endpoint.Port}
}
