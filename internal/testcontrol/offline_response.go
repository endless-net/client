package testcontrol

import (
	"net/http"
	"sync"

	api "github.com/endless-net/client-api/clientapi/v1"
)

type offlineResponseHold struct {
	entered chan struct{}
	release chan struct{}
}

// HoldNextOfflineResponse delays one authenticated offline response after its
// public request has been applied. Release is idempotent; cancellation also
// ends the wait. No payload or credential is exposed to the test driver.
func (s *Server) HoldNextOfflineResponse(id string) (<-chan struct{}, func()) {
	hold := &offlineResponseHold{entered: make(chan struct{}), release: make(chan struct{})}
	s.mu.Lock()
	if s.offlineResponses == nil {
		s.offlineResponses = make(map[string]*offlineResponseHold)
	}
	s.offlineResponses[id] = hold
	s.mu.Unlock()
	var once sync.Once
	return hold.entered, func() { once.Do(func() { close(hold.release) }) }
}

// The endpoint handler owns mu on entry and retains ownership on return.
// Release it while delaying the response so unrelated requests remain usable.
func (s *Server) writeEndpointResponseLocked(w http.ResponseWriter, r *http.Request, status string, snapshot api.RegisterNodeResponse) {
	if status == api.NodeStatusOffline {
		if hold := s.offlineResponses[snapshot.Node.ID]; hold != nil {
			delete(s.offlineResponses, snapshot.Node.ID)
			s.mu.Unlock()
			defer s.mu.Lock()
			close(hold.entered)
			select {
			case <-hold.release:
			case <-r.Context().Done():
				return
			case <-s.closed:
				return
			}
		}
	}
	writeJSON(w, snapshot)
}
