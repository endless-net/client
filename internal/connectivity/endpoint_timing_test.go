package connectivity

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/endless-net/client/internal/state"
	"github.com/endless-net/client/internal/testwire"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

func TestEndpointTransportAcceptsBoundReceiptWithServerClockAhead(t *testing.T) {
	for _, beyondCredential := range []bool{false, true} {
		f := newMapFixture(t)
		input := coordinator.UpdateNodeEndpointRequest{
			Endpoint: "192.0.2.1:51820", Generation: 1, Status: "online", TTL: "30s",
		}
		command, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		f.profile.Connection.EndpointGeneration = input.Generation
		f.profile.Connection.EndpointRequest = &state.EndpointRequest{
			IntentGeneration: f.profile.Connection.Generation, Request: command, Sent: true,
		}
		updatedAt := f.now.Add(1200 * time.Millisecond)
		if beyondCredential {
			updatedAt = f.profile.Attempt.CredentialEnds.Add(-time.Second)
		}
		ack := coordinator.UpdateNodeEndpointResponse{
			SchemaVersion: 2, NodeID: "node", NetworkID: "network", Endpoint: input.Endpoint,
			Generation: input.Generation, Online: true, UpdatedAt: updatedAt,
			ExpiresAt: updatedAt.Add(30 * time.Second), Revision: coordinator.MapRevision{Network: 2},
		}
		calls := 0
		transport := transportFixture(t, func(request *http.Request) (*http.Response, error) {
			calls++
			return testwire.UnaryResponse(request, testwire.EndpointReceipt(ack))
		})
		result, err := transport.UpdateEndpoint(t.Context(), f.profile, f.config)
		if calls != 1 {
			t.Fatal("endpoint acknowledgement changed dispatch count", calls)
		}
		if beyondCredential {
			if !errors.Is(err, ErrEndpointUncertain) || result.Generation != 0 {
				t.Fatal("acknowledgement exceeded credential expiry", err)
			}
		} else if err != nil || !result.UpdatedAt.Equal(updatedAt) || !result.UpdatedAt.After(f.config.Now()) {
			t.Fatal("matching acknowledgement was rejected because the server clock is ahead", err)
		}
	}
}
