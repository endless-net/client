package tests

import (
	"encoding/json"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/ipc/v2"
)

// HC-021: explicit map-signing trust recovery preserves enrollment and intent;
// node credential trust remains an independent published server-key field.
func TestControlPlaneMapSigningRotation(t *testing.T) {
	for _, tc := range []struct {
		name         string
		disconnected bool
	}{{"connected", false}, {"disconnected", true}} {
		t.Run(tc.name, func(t *testing.T) { testMapSigningRotation(t, tc.disconnected) })
	}
}

func testMapSigningRotation(t *testing.T, disconnected bool) {
	t.Helper()
	s, n, id := controlScenario(t)
	oldKey := s.Trust().ActiveKeyID
	if disconnected {
		var response ipc.DisconnectResponse
		n.Service("disconnect", &response)
	}
	if err := s.RotateMapSigningKey(); err != nil {
		t.Fatal(err)
	}
	newKey := s.Trust().ActiveKeyID
	var identity ipc.ServerIdentityResponse
	n.Service("server-identity", &identity)
	if !identity.Changed || identity.TrustedKeyID != oldKey || identity.AnnouncedKeyID != newKey || identity.ControlOrigin != s.URL() {
		t.Fatal("client did not distinguish pinned and newly announced map identities")
	}
	if _, err := n.ServiceCommand("trust-server", "--yes", "--confirmed-control-origin", s.URL(), "--confirmed-key-id", oldKey); err == nil {
		t.Fatal("stale confirmation accepted a newly announced signing key")
	}
	output, err := n.ServiceCommand("trust-server", "--yes", "--confirmed-control-origin", s.URL(), "--confirmed-key-id", newKey)
	var recovered ipc.TrustServerResponse
	if err != nil || json.Unmarshal(output, &recovered) != nil || recovered.Outcome != ipc.RecoveryOutcomeAccepted || recovered.TrustedKeyID != newKey || recovered.OperationID == "" {
		t.Fatal("explicit signing identity recovery failed")
	}
	desired := ipc.DesiredConnected
	if disconnected {
		desired = ipc.DesiredDisconnected
	}
	awaitIntent := func() {
		t.Helper()
		n.AwaitStatus(func(v ipc.StatusResponse) bool {
			return v.NodeID == id && v.NodeCredentialPresent && v.CachedMapValid && v.UserDisconnected == disconnected && v.DesiredState == desired && (disconnected || (v.WireGuard != nil && v.WireGuard.OK))
		})
	}
	awaitIntent()
	output, err = n.ServiceCommand("trust-server", "--yes", "--confirmed-control-origin", s.URL(), "--confirmed-key-id", newKey)
	if err != nil || json.Unmarshal(output, &recovered) != nil || recovered.Outcome != ipc.RecoveryOutcomeAlreadyApplied || recovered.TrustedKeyID != newKey {
		t.Fatal("completed signing recovery was not repeatable")
	}
	awaitIntent()
	n.Stop()
	n.Start()
	awaitIntent()
	n.Service("server-identity", &identity)
	if identity.Changed || identity.TrustedKeyID != newKey || identity.AnnouncedKeyID != newKey {
		t.Fatal("confirmed map trust did not survive agent restart")
	}
	if disconnected {
		var connected ipc.ConnectResponse
		n.Service("connect", &connected)
	}
	if err := s.UpdateMap(id, func(m *api.NetworkMapSnapshot) { m.Network.Name = "after-map-rotation" }); err != nil {
		t.Fatal(err)
	}
	latest, err := s.Snapshot(id)
	if err != nil {
		t.Fatal(err)
	}
	n.AwaitStatus(func(v ipc.StatusResponse) bool {
		return v.NodeID == id && v.CachedMapValid && !v.UserDisconnected && v.DesiredState == ipc.DesiredConnected && v.MapRevision >= latest.Revision.Network
	})
	created, refreshed := 0, 0
	for _, event := range s.Events() {
		if event.Kind == "registered" {
			created++
		}
		if event.Kind == "registration-refreshed" {
			refreshed++
		}
		if (event.Kind == "registered" || event.Kind == "registration-refreshed") && event.NodeID != id {
			t.Fatal("trust recovery changed node identity")
		}
	}
	if created != 1 || refreshed == 0 {
		t.Fatal("trust recovery did not renew the existing enrollment")
	}
}
