package client_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	client "github.com/unng-lab/endlessnet-client/internal/client"
	ipc "github.com/unng-lab/endlessnet-client/ipc/v1"
)

func TestClientIPCOpenAPICoversGoContractConstants(t *testing.T) {
	spec := readOpenAPISpec(t)
	requireOpenAPILine(t, spec, `openapi: 3.1.0`)
	requireOpenAPILine(t, spec, `title: EndlessNet Client Local Service IPC`)

	for _, path := range []string{
		ipc.PathStatus,
		ipc.PathEvents,
		ipc.PathEnroll,
		ipc.PathConnect,
		ipc.PathDisconnect,
		ipc.PathLogout,
		ipc.PathNetworks,
		ipc.PathSelectNetwork,
		ipc.PathDiagnostics,
		ipc.PathDiagnosticsBundle,
		ipc.PathRecentLogs,
	} {
		requireOpenAPIPath(t, spec, path)
	}

	for _, operation := range []string{
		ipc.OperationStatus,
		ipc.OperationEvents,
		ipc.OperationEnroll,
		ipc.OperationConnect,
		ipc.OperationDisconnect,
		ipc.OperationLogout,
		ipc.OperationNetworks,
		ipc.OperationSelectNetwork,
		ipc.OperationDiagnostics,
		ipc.OperationDiagnosticsBundle,
		ipc.OperationRecentLogs,
	} {
		requireOpenAPILine(t, spec, "operationId: "+operation)
	}

	for _, state := range []string{
		string(ipc.StateConnected),
		string(ipc.StateDisconnected),
		string(ipc.StateDegraded),
		string(ipc.StateError),
		string(ipc.StateNeedsEnrollment),
		string(ipc.StateNeedsApproval),
		string(ipc.StateServerIdentityChanged),
	} {
		requireOpenAPIEnumValue(t, spec, state)
	}

	for _, state := range []string{
		string(ipc.ControlStatePendingApproval),
		string(ipc.ControlStateDegraded),
		string(ipc.ControlStateOfflineCache),
		string(ipc.ControlStateReady),
		string(ipc.ControlStateRegistered),
		string(ipc.ControlStateCacheInvalid),
		string(ipc.ControlStateError),
		string(ipc.ControlStateNotRegistered),
		string(ipc.ControlStateDisconnected),
		string(ipc.ControlStateServerIdentityChanged),
	} {
		requireOpenAPIEnumValue(t, spec, state)
	}

	for _, state := range []string{
		string(ipc.AgentSnapshotAbsent),
		string(ipc.AgentSnapshotCurrent),
		string(ipc.AgentSnapshotPrevious),
	} {
		requireOpenAPIEnumValue(t, spec, state)
	}
}

func TestClientIPCOpenAPIDocumentsVersionedRealtimeContract(t *testing.T) {
	spec := readOpenAPISpec(t)

	for _, want := range []string{
		`x-endlessnet-transports:`,
		`kind: windows_named_pipe`,
		`endpoint: "\\\\.\\pipe\\endlessnet-service"`,
		`kind: ` + client.ServiceIPCTransportUnixSocket,
		`endpoint: "` + ipc.DefaultUnixSocket + `"`,
		`endpoint: "` + ipc.DefaultDarwinSocket + `"`,
		`application/x-ndjson:`,
		`x-ndjson: true`,
		`ipc_protocol:`,
		`const: ` + ipc.Protocol,
		fmt.Sprintf(`const: %d`, ipc.Version),
		`ipc_min_supported_version:`,
		`ipc_negotiated_version:`,
		`name: ` + ipc.ProtocolHeader,
		`name: ` + ipc.VersionHeader,
		`name: ` + ipc.MinVersionHeader,
		`service_version:`,
		`service_commit:`,
		`service_build_date:`,
		`desired_state:`,
		`wireguard_apply:`,
		string(ipc.DesiredConnected),
		string(ipc.DesiredDisconnected),
	} {
		requireOpenAPILine(t, spec, want)
	}

	for _, eventType := range []string{
		string(ipc.EventTypeHello),
		string(ipc.EventTypeStatusChanged),
		string(ipc.EventTypeError),
	} {
		requireOpenAPIEnumValue(t, spec, eventType)
	}

	for _, errorCode := range []string{
		ipc.ErrorInvalidJSON,
		ipc.ErrorMethodNotAllowed,
		ipc.ErrorNotImplemented,
		ipc.ErrorNotFound,
		ipc.ErrorRequestTooLarge,
		ipc.ErrorRequestFailed,
		ipc.ErrorUnauthorized,
		ipc.ErrorOwnerRequired,
		ipc.ErrorAdministratorRequired,
		ipc.ErrorVersionRequired,
		ipc.ErrorProtocolUnsupported,
		ipc.ErrorVersionUnsupported,
		ipc.ErrorInvalidVersionRange,
		ipc.ErrorResponseTooLarge,
	} {
		requireOpenAPIEnumValue(t, spec, errorCode)
	}
}

func TestClientIPCOpenAPIOmitsRemovedCompatibilitySurface(t *testing.T) {
	spec := readOpenAPISpec(t)
	for _, removed := range []string{
		"server_url:", "server_urls:", "enrollment_approval_url:", "enrollment_pending:",
		"requires_enrollment:", "stun_count:", "relay_count:", "- Connecting", "- Updating",
		"- connecting", "- updating", "- syncing", "- needs_approval",
	} {
		if strings.Contains(spec, removed) {
			t.Errorf("OpenAPI spec still contains removed compatibility token %q", removed)
		}
	}
}

func requireOpenAPIPath(t *testing.T, spec, path string) {
	t.Helper()
	pattern := regexp.MustCompile(`(?m)^  ` + regexp.QuoteMeta(path) + `:$`)
	if !pattern.MatchString(spec) {
		t.Fatalf("OpenAPI spec missing path %s", path)
	}
}

func requireOpenAPIEnumValue(t *testing.T, spec, value string) {
	t.Helper()
	requireOpenAPILine(t, spec, "- "+value)
}

func requireOpenAPILine(t *testing.T, spec, line string) {
	t.Helper()
	if !strings.Contains(spec, line) {
		t.Fatalf("OpenAPI spec missing %q", line)
	}
}

func readOpenAPISpec(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "client-ipc-v1.openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(raw), "\r\n", "\n")
}
