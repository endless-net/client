package client

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	stun "github.com/unng-lab/endlessnet-client/internal/stunclient"

	clientapi "github.com/unng-lab/endlessnet/clientapi/v1"
)

func TestCheckSTUNQueriesReachableEndpoint(t *testing.T) {
	addr := freeUDPAddrForNetcheckTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- serveNetcheckSTUNFixture(ctx, addr)
	}()
	waitUDPSTUNForNetcheckTest(t, addr)
	results := CheckSTUN(context.Background(), []clientapi.STUNEndpoint{{ID: "stun-1", Addr: addr}}, time.Second)
	if len(results) != 1 {
		t.Fatalf("results = %#v", results)
	}
	if !results[0].Reachable || results[0].MappedAddress == "" || results[0].Error != "" {
		t.Fatalf("STUN result = %#v", results[0])
	}
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("STUN server stopped with error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("STUN server did not stop")
	}
}

func TestCheckSTUNUsesSingleSocketForEndpointSet(t *testing.T) {
	addrA := freeUDPAddrForNetcheckTest(t)
	addrB := freeUDPAddrForNetcheckTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 2)
	for _, addr := range []string{addrA, addrB} {
		addr := addr
		go func() {
			errCh <- serveNetcheckSTUNFixture(ctx, addr)
		}()
		waitUDPSTUNForNetcheckTest(t, addr)
	}
	results := CheckSTUN(context.Background(), []clientapi.STUNEndpoint{
		{ID: "stun-a", Addr: addrA},
		{ID: "stun-b", Addr: addrB},
	}, time.Second)
	if len(results) != 2 {
		t.Fatalf("results = %#v", results)
	}
	if !results[0].Reachable || !results[1].Reachable {
		t.Fatalf("STUN results = %#v", results)
	}
	if results[0].MappedAddress == "" || results[0].MappedAddress != results[1].MappedAddress {
		t.Fatalf("STUN mapped addresses = %#v, want same mapped address from one local socket", results)
	}
	summary := ClassifySTUN(results)
	if summary.Classification != "consistent_mapping" || summary.ReachableEndpoints != 2 || len(summary.MappedAddresses) != 1 {
		t.Fatalf("STUN summary = %#v, want consistent mapping through shared socket", summary)
	}
	cancel()
	for i := 0; i < 2; i++ {
		select {
		case err := <-errCh:
			if err != nil {
				t.Fatalf("STUN server stopped with error: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("STUN server did not stop")
		}
	}
}

func TestCheckSTUNReportsFailure(t *testing.T) {
	addr := freeUDPAddrForNetcheckTest(t)
	results := CheckSTUN(context.Background(), []clientapi.STUNEndpoint{{ID: "stun-dead", Addr: addr}}, 20*time.Millisecond)
	if len(results) != 1 {
		t.Fatalf("results = %#v", results)
	}
	if results[0].Reachable || results[0].Error == "" {
		t.Fatalf("STUN failure result = %#v", results[0])
	}
}

func TestCheckSTUNQueriesExternalStandaloneService(t *testing.T) {
	exe := strings.TrimSpace(os.Getenv("ENDLESSNET_E2E_STUN_BINARY"))
	if exe == "" {
		t.Skip("ENDLESSNET_E2E_STUN_BINARY is not set")
	}
	if _, err := os.Stat(exe); err != nil {
		t.Fatalf("external STUN binary: %v", err)
	}
	stunAddr := freeUDPAddrForNetcheckTest(t)
	metricsAddr := freeTCPAddrForNetcheckTest(t)
	var logs bytes.Buffer
	cmd := exec.Command(exe,
		"--addr", stunAddr,
		"--metrics-addr", metricsAddr,
		"--rate-limit-per-second", "100",
		"--rate-limit-burst", "100",
	)
	cmd.Stdout = &logs
	cmd.Stderr = &logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil || !cmd.ProcessState.Exited() {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})
	waitExternalSTUNReady(t, "http://"+metricsAddr+"/readyz", &logs)
	results := CheckSTUN(context.Background(), []clientapi.STUNEndpoint{{ID: "external-stun", Addr: stunAddr}}, time.Second)
	if len(results) != 1 || !results[0].Reachable || results[0].MappedAddress == "" || results[0].Error != "" {
		t.Fatalf("external STUN result = %#v", results)
	}
}

func TestClassifySTUN(t *testing.T) {
	none := ClassifySTUN(nil)
	if none.Classification != "no_endpoints" || none.Error == "" {
		t.Fatalf("no endpoint classification = %#v", none)
	}
	unreachable := ClassifySTUN([]STUNCheckResult{{ID: "stun-1", Addr: "127.0.0.1:1", Error: "timeout"}})
	if unreachable.Classification != "unreachable" || unreachable.ReachableEndpoints != 0 || unreachable.Error == "" {
		t.Fatalf("unreachable classification = %#v", unreachable)
	}
	consistent := ClassifySTUN([]STUNCheckResult{
		{ID: "stun-1", Reachable: true, MappedAddress: "127.0.0.1:50000"},
		{ID: "stun-2", Reachable: true, MappedAddress: "127.0.0.1:50000"},
	})
	if consistent.Classification != "consistent_mapping" || consistent.ReachableEndpoints != 2 || len(consistent.MappedAddresses) != 1 {
		t.Fatalf("consistent classification = %#v", consistent)
	}
	varying := ClassifySTUN([]STUNCheckResult{
		{ID: "stun-1", Reachable: true, MappedAddress: "127.0.0.1:50000"},
		{ID: "stun-2", Reachable: true, MappedAddress: "127.0.0.1:50001"},
	})
	if varying.Classification != "varying_mapping" || varying.ReachableEndpoints != 2 || len(varying.MappedAddresses) != 2 {
		t.Fatalf("varying classification = %#v", varying)
	}
}

func TestLocalInterfaceStatusesReturnsInventory(t *testing.T) {
	statuses := LocalInterfaceStatuses()
	if len(statuses) == 0 {
		t.Fatal("LocalInterfaceStatuses returned no interfaces")
	}
	for _, status := range statuses {
		if status.Error != "" {
			continue
		}
		if status.Name == "" || status.Index <= 0 {
			t.Fatalf("interface status missing identity: %#v", status)
		}
		return
	}
	t.Fatalf("LocalInterfaceStatuses returned only errors: %#v", statuses)
}

func TestOverlayCIDRConflictsDetectsLocalLANPrefixes(t *testing.T) {
	conflicts := OverlayCIDRConflicts(clientapi.RegisterNodeResponse{
		Network: clientapi.Network{
			CIDR:     "100.64.0.0/24",
			IPv6CIDR: "fd7a:115c:a1e0::/120",
		},
	}, []NetworkInterfaceStatus{
		{Name: "eth0", Flags: []string{"up"}, Prefixes: []string{"100.64.0.0/16"}},
		{Name: "eth1", Flags: []string{"up"}, Prefixes: []string{"fd7a:115c:a1e0::/64"}},
		{Name: "wg0", Flags: []string{"up"}, Prefixes: []string{"100.64.0.0/24"}},
		{Name: "lo", Flags: []string{"up", "loopback"}, Prefixes: []string{"100.64.0.0/24"}},
		{Name: "down0", Prefixes: []string{"100.64.0.0/24"}},
		{Name: "host0", Flags: []string{"up"}, Prefixes: []string{"100.64.0.2/32", "fd7a:115c:a1e0::2/128"}},
	}, "wg0")

	if len(conflicts) != 2 {
		t.Fatalf("conflicts = %#v, want eth0 and eth1", conflicts)
	}
	if conflicts[0].Interface != "eth0" || conflicts[0].OverlayCIDR != "100.64.0.0/24" || conflicts[0].LocalPrefix != "100.64.0.0/16" || conflicts[0].AddressFamily != "ipv4" {
		t.Fatalf("ipv4 conflict = %#v", conflicts[0])
	}
	if conflicts[1].Interface != "eth1" || conflicts[1].OverlayCIDR != "fd7a:115c:a1e0::/120" || conflicts[1].LocalPrefix != "fd7a:115c:a1e0::/64" || conflicts[1].AddressFamily != "ipv6" {
		t.Fatalf("ipv6 conflict = %#v", conflicts[1])
	}
}

func TestOverlayCIDRConflictsReturnsEmptyForDisjointPrefixes(t *testing.T) {
	conflicts := OverlayCIDRConflicts(clientapi.RegisterNodeResponse{
		Network: clientapi.Network{CIDR: "100.64.0.0/24"},
	}, []NetworkInterfaceStatus{
		{Name: "eth0", Flags: []string{"up"}, Prefixes: []string{"192.168.1.0/24"}},
	})
	if len(conflicts) != 0 {
		t.Fatalf("conflicts = %#v, want none", conflicts)
	}
}

func freeUDPAddrForNetcheckTest(t *testing.T) string {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := conn.LocalAddr().String()
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

func freeTCPAddrForNetcheckTest(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

func serveNetcheckSTUNFixture(ctx context.Context, addr string) error {
	conn, err := net.ListenPacket("udp", addr)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()
	buffer := make([]byte, 1500)
	for {
		n, remoteAddr, err := conn.ReadFrom(buffer)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		remote, ok := remoteAddr.(*net.UDPAddr)
		if !ok {
			continue
		}
		response, err := buildSTUNBindingResponseForTest(buffer[:n], remote)
		if err != nil {
			continue
		}
		if _, err := conn.WriteTo(response, remote); err != nil {
			return err
		}
	}
}

func waitExternalSTUNReady(t *testing.T, url string, logs *bytes.Buffer) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		response, err := http.Get(url)
		if err == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		if time.Now().After(deadline) {
			text := logs.String()
			if len(text) > 2000 {
				text = text[len(text)-2000:]
			}
			t.Fatalf("external STUN did not become ready: %v; logs: %s", err, text)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func waitUDPSTUNForNetcheckTest(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		mapped, err := stun.Query(ctx, addr)
		cancel()
		if err == nil && mapped != nil {
			return
		}
		lastErr = err
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("STUN %s did not answer: %v", addr, lastErr)
}
