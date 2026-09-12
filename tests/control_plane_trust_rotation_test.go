package tests

import (
	"encoding/json"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/ipc/v2"
)

// HC-059: explicit map-signing trust recovery preserves enrollment and intent;
// node credential trust remains an independent published server-key field.
func TestControlPlaneMapSigningRotation(t *testing.T) {
	s, n, id := controlScenario(t)
	oldKey := s.Trust().ActiveKeyID
	var disconnected ipc.DisconnectResponse
	n.Service("disconnect", &disconnected)
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
	awaitDisconnected := func() {
		t.Helper()
		n.AwaitStatus(func(v ipc.StatusResponse) bool {
			return v.NodeID == id && v.NodeCredentialPresent && v.CachedMapValid && v.UserDisconnected && v.DesiredState == ipc.DesiredDisconnected
		})
	}
	awaitDisconnected()
	n.Stop()
	n.Start()
	awaitDisconnected()
	n.Service("server-identity", &identity)
	if identity.Changed || identity.TrustedKeyID != newKey || identity.AnnouncedKeyID != newKey {
		t.Fatal("confirmed map trust did not survive agent restart")
	}
	var connected ipc.ConnectResponse
	n.Service("connect", &connected)
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
