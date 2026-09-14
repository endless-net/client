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
	status := n.AwaitNativeStatus(nativeControlSnapshotReady)
	var op *ipc.Operation
	err := retryNativeControlAdmission(command, status, func() (*ipc.Status, error) {
		response := &ipc.GetStatusResponse{}
		err := n.NativeService("status", response)
		return response.Status, err
	}, func(current *ipc.Status) error {
		args := testclient.NativeMutationArguments(id, current)
		switch command {
		case "connect":
			response := &ipc.ConnectResponse{}
			if err := n.NativeService(command, response, args...); err != nil {
				return err
			}
			op = response.Operation
		case "disconnect":
			response := &ipc.DisconnectResponse{}
			if err := n.NativeService(command, response, args...); err != nil {
				return err
			}
			op = response.Operation
		case "logout":
			response := &ipc.LogoutResponse{}
			if err := n.NativeService(command, response, args...); err != nil {
				return err
			}
			op = response.Operation
		default:
			t.Fatal("unexpected native control mutation")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if op == nil || op.Id == "" {
		t.Fatal("missing accepted native control mutation")
	}
	if n.AwaitNativeOperation(op.Id).State != ipc.OperationState_OPERATION_STATE_SUCCEEDED {
		t.Fatal("native control mutation failed")
	}
}

// A durable profile can be exposed before the first runtime observation. Wait
// for that observation before binding a scenario command to its node/network;
// the admission retry must still reject an actual identity or intent change.
func nativeControlSnapshotReady(status *ipc.Status) bool {
	return status.GetActiveProfileId() != "" && status.GetServiceState() != ipc.ServiceState_SERVICE_STATE_UNSPECIFIED &&
		status.GetMetadata().GetInstanceId() != "" && status.GetMetadata().GetRevision() != 0
}

func TestNativeControlSnapshotWaitsForRuntimeObservation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status *ipc.Status
		ready  bool
	}{
		{"absent", nil, false},
		{"profile_before_observation", &ipc.Status{ActiveProfileId: "profile", Metadata: &ipc.SnapshotMetadata{InstanceId: "instance", Revision: 1}}, false},
		{"missing_profile", &ipc.Status{ServiceState: ipc.ServiceState_SERVICE_STATE_DISCONNECTED, Metadata: &ipc.SnapshotMetadata{InstanceId: "instance", Revision: 1}}, false},
		{"missing_instance", &ipc.Status{ActiveProfileId: "profile", ServiceState: ipc.ServiceState_SERVICE_STATE_DISCONNECTED, Metadata: &ipc.SnapshotMetadata{Revision: 1}}, false},
		{"missing_revision", &ipc.Status{ActiveProfileId: "profile", ServiceState: ipc.ServiceState_SERVICE_STATE_DISCONNECTED, Metadata: &ipc.SnapshotMetadata{InstanceId: "instance"}}, false},
		{"disconnected", &ipc.Status{ActiveProfileId: "profile", ServiceState: ipc.ServiceState_SERVICE_STATE_DISCONNECTED, Metadata: &ipc.SnapshotMetadata{InstanceId: "instance", Revision: 1}}, true},
		{"unenrolled", &ipc.Status{ActiveProfileId: "profile", ServiceState: ipc.ServiceState_SERVICE_STATE_NEEDS_ENROLLMENT, Metadata: &ipc.SnapshotMetadata{InstanceId: "instance", Revision: 1}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if nativeControlSnapshotReady(tc.status) != tc.ready {
				t.Fatal("control scenario readiness confused a durable profile with a runtime observation")
			}
		})
	}
}

func TestNativeDNSMapRequiresCurrentConnectedObservation(t *testing.T) {
	for _, mode := range []string{"ready", "nil", "missing-node", "foreign-node", "old-revision", "missing-cache", "missing-agent", "previous-agent", "foreign-revision", "disconnected", "control-error", "control-unspecified"} {
		t.Run(mode, func(t *testing.T) {
			status := &ipc.Status{NodeId: "node", MapRevision: 2, StoredState: &ipc.StoredStatePresence{CachedMapValid: true},
				Agent:           &ipc.AgentStatus{SnapshotState: ipc.AgentSnapshotState_AGENT_SNAPSHOT_STATE_CURRENT, MapRevision: 2},
				ConnectionPhase: ipc.ConnectionPhase_CONNECTION_PHASE_CONNECTED, ControlState: ipc.ControlState_CONTROL_STATE_READY}
			switch mode {
			case "nil":
				status = nil
			case "missing-node":
				status.NodeId = ""
			case "foreign-node":
				status.NodeId = "other"
			case "old-revision":
				status.MapRevision = 1
				status.Agent.MapRevision = 1
			case "missing-cache":
				status.StoredState = nil
			case "missing-agent":
				status.Agent = nil
			case "previous-agent":
				status.Agent.SnapshotState = ipc.AgentSnapshotState_AGENT_SNAPSHOT_STATE_PREVIOUS
			case "foreign-revision":
				status.Agent.MapRevision = 1
			case "disconnected":
				status.ConnectionPhase = ipc.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED
			case "control-error":
				status.ControlState = ipc.ControlState_CONTROL_STATE_ERROR
			case "control-unspecified":
				status.ControlState = ipc.ControlState_CONTROL_STATE_UNSPECIFIED
			}
			if nativeDNSMapApplied(status, "node", 1) != (mode == "ready") {
				t.Fatal("DNS map readiness accepted an absent, stale or unhealthy observation")
			}
		})
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
