package tests

import (
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

// Refreshing a rejected CAS must not refresh the user's trust consent.
func nativeTrustRetryContextMatches(before, after *ipc.Status, confirmed, observed *ipc.ServerIdentity) bool {
	return before != nil && after != nil && confirmed != nil && observed != nil &&
		before.NodeId != "" && before.NodeId == after.NodeId &&
		before.ActiveProfileId != "" && before.ActiveProfileId == after.ActiveProfileId &&
		before.UserDisconnected == after.UserDisconnected && proto.Equal(before.Intent, after.Intent) &&
		confirmed.ProfileId == before.ActiveProfileId &&
		before.GetMetadata().GetInstanceId() != "" && before.GetMetadata().GetInstanceId() == after.GetMetadata().GetInstanceId() &&
		after.GetMetadata().GetRevision() > before.GetMetadata().GetRevision() && proto.Equal(confirmed, observed)
}

func TestNativeTrustRetryPreservesContextAndConsent(t *testing.T) {
	before := &ipc.Status{NodeId: "node", ActiveProfileId: "profile", Metadata: &ipc.SnapshotMetadata{InstanceId: "instance", Revision: 10}}
	identity := &ipc.ServerIdentity{ProfileId: "profile", ControlOrigin: "https://control.test", TrustedKeyId: "old", AnnouncedKeyId: "new", AnnouncementId: "announcement"}
	for _, scenario := range []string{"revision-only", "same-revision", "instance", "node", "profile", "disconnected", "origin", "trusted", "announced", "announcement", "missing-status", "missing-identity"} {
		t.Run(scenario, func(t *testing.T) {
			after := proto.Clone(before).(*ipc.Status)
			after.Metadata.Revision++
			observed := proto.Clone(identity).(*ipc.ServerIdentity)
			switch scenario {
			case "same-revision":
				after.Metadata.Revision--
			case "instance":
				after.Metadata.InstanceId = "other"
			case "node":
				after.NodeId = "other"
			case "profile":
				after.ActiveProfileId = "other"
			case "disconnected":
				after.UserDisconnected = !before.UserDisconnected
			case "origin":
				observed.ControlOrigin = "https://other.test"
			case "trusted":
				observed.TrustedKeyId = "other"
			case "announced":
				observed.AnnouncedKeyId = "other"
			case "announcement":
				observed.AnnouncementId = "other"
			case "missing-status":
				after = nil
			case "missing-identity":
				observed = nil
			}
			if nativeTrustRetryContextMatches(before, after, identity, observed) != (scenario == "revision-only") {
				t.Fatal("CAS retry did not preserve identity, instance and confirmed announcement")
			}
		})
	}
}
