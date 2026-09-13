package tests

import (
	"strings"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestInstalledNativeObservationWithholdsPrivateStatus(t *testing.T) {
	response := &ipc.GetStatusResponse{}
	if err := protojson.Unmarshal([]byte(`{"status":{"activeProfileId":"synthetic-private-profile","nodeId":"synthetic-private-node","accountId":"synthetic-private-account","hostname":"synthetic-private-host","mapRevision":"41","userDisconnected":true,"pendingAction":{"browserUrl":"https://private.invalid/?token=synthetic-private-token"}}}`), response); err != nil {
		t.Fatal(err)
	}
	got := installedNativeObservation(response)
	want := "service=0 control=0 connection=0 desired=0 disconnected=true map=41 agent_present=false agent_state=0 agent_map=0 agent_failure=false"
	if got != want || strings.Contains(got, "private") {
		t.Fatal("timeout evidence is not the numeric/boolean allowlist")
	}
	if installedNativeObservation(&ipc.GetRuntimeInfoResponse{}) != "status unavailable" || installedNativeObservation(&ipc.GetStatusResponse{}) != "status unavailable" {
		t.Fatal("absent status fabricated observation")
	}
}
