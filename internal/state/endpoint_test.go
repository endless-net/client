package state

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

func pendingEndpointFixture(t *testing.T, s *Store, now time.Time) coordinator.UpdateNodeEndpointRequest {
	t.Helper()
	if err := connectionChange(s, func(c *ConnectionState) { _, _ = c.AcceptIntent(IntentConnected, now) }); err != nil {
		t.Fatal(err)
	}
	request := coordinator.UpdateNodeEndpointRequest{Endpoint: "192.0.2.1:51820", Generation: 1, TTL: "30s", Status: "online"}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := connectionChange(s, func(c *ConnectionState) {
		_, _ = c.NextEndpointGeneration()
		c.EndpointRequest = &EndpointRequest{IntentGeneration: c.Generation, Request: raw}
	}); err != nil {
		t.Fatal(err)
	}
	return request
}

func TestEndpointRequestIsFrozenBeforeSendAndOriginalACKIsImmutable(t *testing.T) {
	s, _, now := registeredStateFixture(t)
	request := pendingEndpointFixture(t, s, now)
	ack := coordinator.UpdateNodeEndpointResponse{SchemaVersion: 2, NodeID: "node", NetworkID: "network", Endpoint: request.Endpoint, Generation: request.Generation, Online: true, UpdatedAt: now, ExpiresAt: now.Add(30 * time.Second), Revision: coordinator.MapRevision{Network: 9007199254740993}}
	for _, fn := range []func(*ConnectionState){
		func(c *ConnectionState) { c.EndpointRequest = nil },
		func(c *ConnectionState) {
			c.EndpointRequest.Request = append([]byte(" "), c.EndpointRequest.Request...)
		},
		func(c *ConnectionState) { c.EndpointRequest.Sent = true; c.EndpointRequest.Receipt = &ack },
	} {
		if err := connectionChange(s, fn); !errors.Is(err, ErrCorrupt) {
			t.Fatal("pending request could be erased/changed or ACKed before sent marker", err)
		}
	}
	if err := connectionChange(s, func(c *ConnectionState) { c.EndpointRequest.Sent = true }); err != nil {
		t.Fatal(err)
	}
	if err := connectionChange(s, func(c *ConnectionState) { c.EndpointRequest.Receipt = &ack }); err != nil {
		t.Fatal(err)
	}
	for _, fn := range []func(*ConnectionState){
		func(c *ConnectionState) { c.EndpointRequest.Sent = false },
		func(c *ConnectionState) { c.EndpointRequest.Receipt = nil },
		func(c *ConnectionState) { c.EndpointRequest.Receipt.Revision.Network++ },
		func(c *ConnectionState) { c.EndpointGeneration++ },
	} {
		if err := connectionChange(s, fn); !errors.Is(err, ErrCorrupt) {
			t.Fatal("original endpoint outcome changed", err)
		}
	}
	if stored := storedConnection(t, s); stored.EndpointRequest.Receipt.Generation != request.Generation || stored.EndpointRequest.Receipt.Revision.Network != 9007199254740993 {
		t.Fatal("original ACK/uint64 not preserved")
	}
}

func TestEndpointUnresolvedOutcomeSurvivesDisconnectAndOfflineSupersession(t *testing.T) {
	s, _, now := registeredStateFixture(t)
	request := pendingEndpointFixture(t, s, now)
	if err := connectionChange(s, func(c *ConnectionState) { c.EndpointRequest.Sent = true }); err != nil {
		t.Fatal(err)
	}
	if err := connectionChange(s, func(c *ConnectionState) { _, _ = c.AcceptIntent(IntentDisconnected, now.Add(time.Second)) }); err != nil {
		t.Fatal(err)
	}
	current := storedConnection(t, s)
	if current.EndpointRequest == nil || !current.EndpointRequest.Sent || current.EndpointRequest.IntentGeneration != 1 || current.Generation != 2 || current.EndpointRequest.Receipt != nil {
		t.Fatal("Disconnect invented rollback of endpoint send")
	}
	ack := coordinator.UpdateNodeEndpointResponse{SchemaVersion: 2, NodeID: "node", NetworkID: "network", Endpoint: request.Endpoint, Generation: request.Generation, Online: true, UpdatedAt: now, ExpiresAt: now.Add(30 * time.Second), Revision: coordinator.MapRevision{Network: 9007199254740993}}
	if err := connectionChange(s, func(c *ConnectionState) { c.EndpointRequest.Receipt = &ack }); !errors.Is(err, ErrCorrupt) {
		t.Fatal("late endpoint ACK crossed latest intent fence", err)
	}
	request.Generation = 2
	request.Status = "offline"
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := connectionChange(s, func(c *ConnectionState) {
		_, _ = c.NextEndpointGeneration()
		c.EndpointRequest = &EndpointRequest{IntentGeneration: c.Generation, Request: raw}
	}); err != nil {
		t.Fatal(err)
	}
	current = storedConnection(t, s)
	if current.Intent != IntentDisconnected || current.EndpointGeneration != 2 || current.EndpointRequest.Sent || current.EndpointRequest.IntentGeneration != 2 {
		t.Fatal("offline supersession reset intent/counter")
	}
}
