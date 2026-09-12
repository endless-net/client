package tests

import (
	"testing"
	"time"

	ipc "github.com/endless-net/client/ipc/v2"
)

// HC-018: disconnect intent must survive process death while the public offline
// notification is awaiting its response, before the CLI operation completes.
func TestControlPlaneInterruptedDisconnect(t *testing.T) {
	s, n, id := controlScenario(t)
	entered, release := s.HoldNextOfflineResponse(id)
	defer release()
	done := make(chan error, 1)
	go func() {
		_, err := n.ServiceCommand("disconnect")
		done <- err
	}()
	select {
	case <-entered:
	case <-done:
		t.Fatal("disconnect completed before the offline response boundary")
	case <-time.After(10 * time.Second):
		t.Fatal("disconnect did not reach the public offline request boundary")
	}
	select {
	case <-done:
		t.Fatal("disconnect completed while its response was held")
	default:
	}
	n.Crash()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("interrupted disconnect incorrectly reported CLI success")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("disconnect CLI did not end after agent termination")
	}
	release()
	n.Start()
	n.AwaitStatus(func(v ipc.StatusResponse) bool {
		return v.NodeID == id && v.NodeCredentialPresent && v.CachedMapValid && v.UserDisconnected && v.DesiredState == ipc.DesiredDisconnected
	})
	var connected ipc.ConnectResponse
	n.Service("connect", &connected)
	n.AwaitStatus(func(v ipc.StatusResponse) bool {
		return v.NodeID == id && v.NodeCredentialPresent && v.CachedMapValid && !v.UserDisconnected && v.DesiredState == ipc.DesiredConnected
	})
	created := 0
	for _, event := range s.Events() {
		if event.Kind == "registered" {
			created++
		}
	}
	if created != 1 {
		t.Fatal("interrupted disconnect recovery created another registration")
	}
}
