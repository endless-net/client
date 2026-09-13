package testserver

import "errors"

func validGateName(name string) bool {
	if len(name) == 0 || len(name) > 64 {
		return false
	}
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// ReleaseGate is a parent-side test control, never a ClientService RPC. A
// named JSON response gate is one-shot. It may be released before the handler
// reaches it, but unknown/duplicate commands permanently fail verification.
func (s *Server) ReleaseGate(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	gate := s.gates[name]
	if !validGateName(name) || gate == nil {
		const failure = "invalid or already released response gate"
		s.failures = append(s.failures, failure)
		return errors.New(failure)
	}
	close(gate)
	s.gates[name] = nil
	return nil
}
