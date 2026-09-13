package tests

import (
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestLogoutReconfirmationRequiresUnchangedRegistration(t *testing.T) {
	before := &ipc.Status{NodeId: "node", ActiveProfileId: "profile", AccountId: "account",
		Metadata:    &ipc.SnapshotMetadata{InstanceId: "instance", Revision: 5},
		StoredState: &ipc.StoredStatePresence{NodeCredentialPresent: true}}
	for _, mode := range []string{"revision", "nil", "node", "profile", "account", "instance", "same-revision", "regression", "credential", "session", "intent", "disconnected", "network"} {
		t.Run(mode, func(t *testing.T) {
			after := proto.Clone(before).(*ipc.Status)
			after.Metadata.Revision++
			switch mode {
			case "nil":
				after = nil
			case "node":
				after.NodeId = "other"
			case "profile":
				after.ActiveProfileId = "other"
			case "account":
				after.AccountId = "other"
			case "instance":
				after.Metadata.InstanceId = "other"
			case "same-revision":
				after.Metadata.Revision--
			case "regression":
				after.Metadata.Revision = 1
			case "credential":
				after.StoredState.NodeCredentialPresent = false
			case "session":
				after.StoredState.TokenPresent = true
			case "intent":
				after.Intent = &ipc.ConnectionIntent{DesiredState: ipc.DesiredState_DESIRED_STATE_CONNECTED}
			case "disconnected":
				after.UserDisconnected = true
			case "network":
				after.Network = &ipc.Network{Id: "other"}
			}
			if nativeLogoutConsentUnchanged(before, after) != (mode == "revision") {
				t.Fatal("cleanup reconfirmation accepted a changed registration or intent")
			}
		})
	}
}
