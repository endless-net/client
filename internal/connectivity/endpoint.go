package connectivity

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

func endpointProfile(value state.State, profileID string, intentGeneration, endpointGeneration uint64) (state.Profile, error) {
	p, exists := value.Profiles[profileID]
	if !exists || p.Connection == nil || p.Connection.Generation != intentGeneration || p.Connection.EndpointGeneration != endpointGeneration || p.Connection.EndpointRequest == nil || p.Connection.EndpointRequest.IntentGeneration != intentGeneration {
		return state.Profile{}, state.ErrStale
	}
	r, err := coordinator.DecodeUpdateNodeEndpointRequest(p.Connection.EndpointRequest.Request, len(p.Connection.EndpointRequest.Request))
	if err != nil || !validAnnouncement(p, r) {
		return state.Profile{}, state.ErrStale
	}
	return p, nil
}

// PrepareEndpoint is an explicit new announcement/renewal, not retry recovery.
// Its caller chooses the next generation from a current snapshot. Counter and
// exact producer request commit together; stale intent/revision prevents I/O.
func PrepareEndpoint(ctx context.Context, store *state.Store, profileID string, generation uint64, request coordinator.UpdateNodeEndpointRequest) error {
	if store == nil || generation == 0 {
		return ErrConfig
	}
	before, err := store.Load(ctx)
	if err != nil {
		return err
	}
	p, exists := before.Profiles[profileID]
	if !exists || p.Connection == nil || p.Connection.Generation != generation {
		return state.ErrStale
	}
	if p.Connection.EndpointGeneration == ^uint64(0) || request.Generation != p.Connection.EndpointGeneration+1 {
		return state.ErrStale
	}
	// Validate against the prospective counter without changing durable data.
	prospective := *p.Connection
	prospective.EndpointGeneration = request.Generation
	p.Connection = &prospective
	if !validAnnouncement(p, request) {
		return ErrConfig
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return ErrConfig
	}
	return store.Update(ctx, before.Revision, func(value *state.State) error {
		p := value.Profiles[profileID]
		if p.Connection == nil || p.Connection.Generation != generation {
			return state.ErrStale
		}
		next, err := p.Connection.NextEndpointGeneration()
		if err != nil {
			return err
		}
		if next != request.Generation {
			return state.ErrStale
		}
		p.Connection.EndpointRequest = &state.EndpointRequest{IntentGeneration: generation, Request: raw}
		value.Profiles[profileID] = p
		return nil
	})
}

// SendEndpoint marks dispatch durably, then makes one transport attempt. A
// subsequent explicit recovery call sends the same frozen request. Known ACKs
// are returned unchanged without network I/O and never count as fresh authority.
func SendEndpoint(ctx context.Context, store *state.Store, transport *Transport, config VerifierConfig, profileID string, intentGeneration, endpointGeneration uint64) (coordinator.UpdateNodeEndpointResponse, error) {
	var empty coordinator.UpdateNodeEndpointResponse
	if store == nil || transport == nil || intentGeneration == 0 || endpointGeneration == 0 {
		return empty, ErrConfig
	}
	before, err := store.Load(ctx)
	if err != nil {
		return empty, err
	}
	p, err := endpointProfile(before, profileID, intentGeneration, endpointGeneration)
	if err != nil {
		return empty, err
	}
	if p.Connection.EndpointRequest.Receipt != nil {
		return *p.Connection.EndpointRequest.Receipt, nil
	}
	if !p.Connection.EndpointRequest.Sent {
		err = store.Update(ctx, before.Revision, func(value *state.State) error {
			p, err := endpointProfile(*value, profileID, intentGeneration, endpointGeneration)
			if err != nil {
				return err
			}
			p.Connection.EndpointRequest.Sent = true
			value.Profiles[profileID] = p
			return nil
		})
		if err != nil {
			return empty, err
		}
	}
	current, err := store.Load(ctx)
	if err != nil {
		return empty, err
	}
	p, err = endpointProfile(current, profileID, intentGeneration, endpointGeneration)
	if err != nil {
		return empty, err
	}
	original := append([]byte(nil), p.Connection.EndpointRequest.Request...)
	result, err := transport.UpdateEndpoint(ctx, p, config)
	if err != nil {
		return empty, err
	}
	current, err = store.Load(ctx)
	if err != nil {
		return empty, err
	}
	err = store.Update(ctx, current.Revision, func(value *state.State) error {
		p, err := endpointProfile(*value, profileID, intentGeneration, endpointGeneration)
		if err != nil {
			return err
		}
		if !p.Connection.EndpointRequest.Sent || !bytes.Equal(original, p.Connection.EndpointRequest.Request) {
			return state.ErrStale
		}
		p.Connection.EndpointRequest.Receipt = &result
		value.Profiles[profileID] = p
		return nil
	})
	if err != nil {
		return empty, err
	}
	return result, nil
}
