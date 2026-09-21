package tests

import (
	"strings"
	"testing"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestInstalledNativeObservationWithholdsPrivateStatus(t *testing.T) {
	response := &ipc.GetStatusResponse{}
	if err := protojson.Unmarshal([]byte(`{"status":{"activeProfileId":"synthetic-private-profile","nodeId":"synthetic-private-node","accountId":"synthetic-private-account","hostname":"synthetic-private-host","mapRevision":"41","userDisconnected":true,"pendingAction":{"browserUrl":"https://private.invalid/?token=synthetic-private-token"}}}`), response); err != nil {
		t.Fatal(err)
	}
	got := installedNativeObservation(response)
	want := "service=0 control=0 connection=0 desired=0 disconnected=true map=41 agent_present=false agent_state=0 agent_map=0 agent_failure=false agent_generated_unix=0"
	if got != want || strings.Contains(got, "private") {
		t.Fatal("timeout evidence is not the numeric/boolean allowlist")
	}
	if installedNativeObservation(&ipc.GetRuntimeInfoResponse{}) != "status unavailable" || installedNativeObservation(&ipc.GetStatusResponse{}) != "status unavailable" {
		t.Fatal("absent status fabricated observation")
	}
}

func TestInstalledRecoveryCoversProductionBackoff(t *testing.T) {
	for name, maximum := range map[string]time.Duration{
		"windows": client.DefaultWindowsServiceOptions().ReconnectMaxDelay,
		"linux":   client.DefaultSystemdServiceOptions().ReconnectMaxDelay,
		"macos":   client.DefaultLaunchdServiceOptions().ReconnectMaxDelay,
	} {
		if installedControlRecoveryTimeout < maximum+time.Minute {
			t.Errorf("%s recovery budget cannot cover production backoff plus sync", name)
		}
	}
}

func TestInstalledObservationIncludesSnapshotTime(t *testing.T) {
	response := &ipc.GetStatusResponse{Status: &ipc.Status{Agent: &ipc.AgentStatus{
		GeneratedAt: timestamppb.New(time.Unix(1700000000, 0)),
	}}}
	if !strings.HasSuffix(installedNativeObservation(response), " agent_generated_unix=1700000000") {
		t.Fatal("timeout evidence omitted agent snapshot time")
	}
	response.Status.Agent.GeneratedAt.Seconds = 253402300800
	if !strings.HasSuffix(installedNativeObservation(response), " agent_generated_unix=0") {
		t.Fatal("invalid snapshot time was reported as valid")
	}
}
