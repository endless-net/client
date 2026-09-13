package tests

import (
	"net"
	"runtime"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/testclient"
)

func nativeDNSListenerAddress(status *ipc.Status) string {
	if runtime.GOOS != "linux" {
		return "127.0.0.1:53"
	}
	for _, address := range status.GetOverlayAddresses() {
		if parsed := net.ParseIP(address); parsed != nil && parsed.To4() != nil {
			return net.JoinHostPort(parsed.String(), "53")
		}
	}
	return "missing-native-dns-listener"
}

func nativeDNSRecordAddressesMatch(addresses []string, ipv4, ipv6 string) bool {
	expected := map[string]bool{}
	for _, address := range []string{ipv4, ipv6} {
		if address != "" {
			expected[address] = true
		}
	}
	if len(addresses) != len(expected) {
		return false
	}
	for _, address := range addresses {
		if !expected[address] {
			return false
		}
		delete(expected, address)
	}
	return len(expected) == 0
}

func runNativeControlMutation(t *testing.T, n *testclient.Node, command, id string) {
	t.Helper()
	status := n.AwaitNativeStatus(func(status *ipc.Status) bool { return status.ActiveProfileId != "" })
	args := testclient.NativeMutationArguments(id, status)
	var op *ipc.Operation
	switch command {
	case "connect":
		response := &ipc.ConnectResponse{}
		if err := n.NativeService(command, response, args...); err != nil {
			t.Fatal(err)
		}
		op = response.Operation
	case "disconnect":
		response := &ipc.DisconnectResponse{}
		if err := n.NativeService(command, response, args...); err != nil {
			t.Fatal(err)
		}
		op = response.Operation
	case "logout":
		response := &ipc.LogoutResponse{}
		if err := n.NativeService(command, response, args...); err != nil {
			t.Fatal(err)
		}
		op = response.Operation
	default:
		t.Fatal("unexpected native control mutation")
	}
	if op == nil || op.Id == "" {
		t.Fatal("missing accepted native control mutation")
	}
	if n.AwaitNativeOperation(op.Id).State != ipc.OperationState_OPERATION_STATE_SUCCEEDED {
		t.Fatal("native control mutation failed")
	}
}

func TestNativeDNSAddressSetComparison(t *testing.T) {
	for _, test := range []struct {
		addresses []string
		want      bool
	}{
		{[]string{"192.0.2.1", "2001:db8::1"}, true},
		{[]string{"2001:db8::1", "192.0.2.1"}, true},
		{[]string{"192.0.2.1", "192.0.2.1"}, false},
		{[]string{"192.0.2.2", "2001:db8::1"}, false},
		{[]string{"192.0.2.1"}, false},
	} {
		if got := nativeDNSRecordAddressesMatch(test.addresses, "192.0.2.1", "2001:db8::1"); got != test.want {
			t.Fatal("address comparison lost missing, duplicate or obsolete record")
		}
	}
}
