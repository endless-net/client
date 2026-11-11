package connectivity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	transportprofile "github.com/endless-net/client/api/transport"

	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

func TestEndpointAdvertisementCannotChangeProtectedIntentGeneration(t *testing.T) {
	f := newMapFixture(t)
	store, _ := cursorStore(t, f)
	before, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ad := &coordinator.TransportAdvertisement{IntentGeneration: 2, Capabilities: transportprofile.Capabilities{Version: 1, Engines: []transportprofile.Capability{{Engine: transportprofile.WireGuard, ProtocolVersion: "1", ProfileVersion: 1}}}, Endpoints: []coordinator.TransportEndpoint{}}
	request := coordinator.UpdateNodeEndpointRequest{Endpoint: "192.0.2.1:51820", Generation: 1, Status: "online", TTL: "30s", Transport: ad}
	if err := PrepareEndpoint(t.Context(), store, f.profile.ID, 1, request); !errors.Is(err, ErrConfig) {
		t.Fatal("advertisement changed local intent", err)
	}
	after, err := store.Load(t.Context())
	if err != nil || before.Revision != after.Revision {
		t.Fatal("invalid advertisement changed protected state", err)
	}
}

func endpointACK(t *testing.T, f *mapFixture, request *http.Request, generation uint64) *http.Response {
	t.Helper()
	ack := coordinator.UpdateNodeEndpointResponse{SchemaVersion: 2, NodeID: "node", NetworkID: "network", Endpoint: "192.0.2.1:51820", Generation: generation, Online: true, UpdatedAt: f.now, ExpiresAt: f.now.Add(30 * time.Second), Revision: coordinator.MapRevision{Network: 9007199254740993}}
	raw, err := json.Marshal(ack)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Response{StatusCode: 200, Request: request, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(raw))}
}

func TestEndpointExecutorRecoversOriginalRequestAndNeverRenewsKnownACK(t *testing.T) {
	f := newMapFixture(t)
	store, b := cursorStore(t, f)
	request := coordinator.UpdateNodeEndpointRequest{Endpoint: "192.0.2.1:51820", Generation: 1, Status: "online", TTL: "30s"}
	if err := PrepareEndpoint(context.Background(), store, f.profile.ID, 1, request); err != nil {
		t.Fatal(err)
	}
	calls := 0
	var original []byte
	transport := transportFixture(t, func(r *http.Request) (*http.Response, error) {
		calls++
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if original == nil {
			original = append([]byte(nil), raw...)
		} else if !bytes.Equal(original, raw) {
			t.Fatal("recovery changed original announcement")
		}
		if calls == 1 {
			return nil, errors.New("lost endpoint ACK")
		}
		return endpointACK(t, f, r, 1), nil
	})
	// Lost local sent-marker ACK must not dispatch. A fresh read later recovers
	// that exact marker before the first HTTP operation.
	b.loseACK = true
	if _, err := SendEndpoint(context.Background(), store, transport, f.config, f.profile.ID, 1, 1); !errors.Is(err, state.ErrUncertain) || calls != 0 {
		t.Fatal("sent before confirmed marker or reported certain outcome", err)
	}
	b.loseACK = false
	if _, err := SendEndpoint(context.Background(), store, transport, f.config, f.profile.ID, 1, 1); !errors.Is(err, ErrEndpointUncertain) || calls != 1 {
		t.Fatal("lost network response did not remain uncertain", err)
	}
	ack, err := SendEndpoint(context.Background(), store, transport, f.config, f.profile.ID, 1, 1)
	if err != nil || calls != 2 || ack.Generation != 1 {
		t.Fatal("original generation recovery failed", err)
	}
	f.now = f.now.Add(time.Hour)
	replay, err := SendEndpoint(context.Background(), store, transport, f.config, f.profile.ID, 1, 1)
	if err != nil || replay != ack || calls != 2 {
		t.Fatal("known historical ACK renewed lease or caused I/O", err)
	}
}

func TestEndpointExecutorFencesIntentChangedWhileHTTPWasInFlight(t *testing.T) {
	f := newMapFixture(t)
	store, _ := cursorStore(t, f)
	request := coordinator.UpdateNodeEndpointRequest{Endpoint: "192.0.2.1:51820", Generation: 1, Status: "online", TTL: "30s"}
	if err := PrepareEndpoint(context.Background(), store, f.profile.ID, 1, request); err != nil {
		t.Fatal(err)
	}
	transport := transportFixture(t, func(r *http.Request) (*http.Response, error) {
		current, err := store.Load(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Update(context.Background(), current.Revision, func(value *state.State) error {
			p := value.Profiles[f.profile.ID]
			_, err := p.Connection.AcceptIntent(state.IntentDisconnected, f.now.Add(time.Second))
			value.Profiles[p.ID] = p
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return endpointACK(t, f, r, 1), nil
	})
	if _, err := SendEndpoint(context.Background(), store, transport, f.config, f.profile.ID, 1, 1); !errors.Is(err, state.ErrStale) {
		t.Fatal("late endpoint ACK crossed intent fence", err)
	}
	current, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	c := current.Profiles[f.profile.ID].Connection
	if c.Generation != 2 || c.Intent != state.IntentDisconnected || c.EndpointRequest.Receipt != nil || !c.EndpointRequest.Sent {
		t.Fatal("late result rewrote intent or implied rollback")
	}
	request.Generation = 2
	request.Status = "offline"
	if err := PrepareEndpoint(context.Background(), store, f.profile.ID, 2, request); err != nil {
		t.Fatal("explicit offline supersession failed", err)
	}
	if _, err := SendEndpoint(context.Background(), store, transport, f.config, f.profile.ID, 1, 1); !errors.Is(err, state.ErrStale) {
		t.Fatal("old request was rebound to offline generation", err)
	}
}
