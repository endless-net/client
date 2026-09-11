package testcontrol

import (
	"crypto/rand"
	"errors"
	"net/http"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	wg "github.com/endless-net/client-api/clientapi/wireguard"
)

func (s *Server) createEnrollment(w http.ResponseWriter, r *http.Request) {
	var req api.RegisterNodeRequest
	if !decode(w, r, &req) {
		return
	}
	if req.SchemaVersion != api.SchemaVersion || len(req.IdempotencyID) < 16 || req.DeviceFingerprint == "" || (req.NetworkName == "" && req.NetworkID == "") || wg.ValidatePublicKey(req.PublicKey) != nil || wg.ValidateHostname(req.Hostname) != nil || api.VerifyRegisterNodeIdentityProof(req) != nil || req.NodeCredential != "" || req.JoinToken != "" || req.SessionTokenBinding != "" {
		http.Error(w, "invalid enrollment", 400)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.enrollments {
		if e.Request.IdempotencyID == req.IdempotencyID {
			if api.RegistrationIdentityProofBinding(e.Request) != api.RegistrationIdentityProofBinding(req) {
				publicError(w, api.ErrorCodeNodeIdentityBindingMismatch)
				return
			}
			writeJSON(w, api.CreateNodeEnrollmentRequestResponse{Request: e.Public, PollToken: e.Token, PollAfterSeconds: 1})
			return
		}
	}
	id := "enroll-" + rand.Text()
	e := &enrollment{Request: clone(req), Token: rand.Text(), Public: api.NodeEnrollmentRequest{ID: id, Status: api.NodeEnrollmentRequestPending, Hostname: req.Hostname, NetworkID: req.NetworkID, NetworkName: req.NetworkName, PublicKey: req.PublicKey, IdentityPublicKey: req.IdentityPublicKey, DeviceFingerprint: req.DeviceFingerprint, CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(time.Hour), ApprovalURL: s.URL() + "/approve/" + id}}
	s.enrollments[id] = e
	s.recordLocked(Event{Kind: "enrollment", Path: id})
	writeJSON(w, api.CreateNodeEnrollmentRequestResponse{Request: e.Public, PollToken: e.Token, PollAfterSeconds: 1})
}

// ExpireEnrollment advances only this request's lifetime in the contract model.
// The real client learns of expiry through the normal status response.
func (s *Server) ExpireEnrollment(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.enrollments[id]
	if e == nil || e.Public.Status != api.NodeEnrollmentRequestPending {
		return errors.New("enrollment is not pending")
	}
	e.Public.ExpiresAt = time.Now().Add(-time.Second)
	e.Public.Status = api.NodeEnrollmentRequestExpired
	s.recordLocked(Event{Kind: "enrollment-expired", Path: id})
	return nil
}

func (s *Server) DecideEnrollment(id string, approve bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.enrollments[id]
	if e == nil {
		return errors.New("unknown enrollment")
	}
	if e.Public.Status != api.NodeEnrollmentRequestPending {
		return errors.New("enrollment is not pending")
	}
	e.Public.Status = api.NodeEnrollmentRequestRejected
	if approve {
		e.Public.Status = api.NodeEnrollmentRequestApproved
	}
	s.recordLocked(Event{Kind: "enrollment-decided", Path: id})
	return nil
}

func (s *Server) enrollmentLocked(w http.ResponseWriter, r *http.Request) *enrollment {
	e := s.enrollments[r.PathValue("id")]
	if e == nil {
		http.NotFound(w, r)
		return nil
	}
	if r.Header.Get("Authorization") != "Bearer "+e.Token {
		publicError(w, api.ErrorCodeAuthorizationDenied)
		return nil
	}
	if time.Now().After(e.Public.ExpiresAt) && e.Result == nil {
		e.Public.Status = api.NodeEnrollmentRequestExpired
	}
	return e
}
func (s *Server) enrollmentStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.enrollmentLocked(w, r)
	if e != nil {
		writeJSON(w, api.NodeEnrollmentRequestStatusResponse{Request: e.Public, PollAfterSeconds: 1})
	}
}
func (s *Server) completeEnrollment(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.enrollmentLocked(w, r)
	if e == nil {
		return
	}
	if e.Result == nil && e.Public.Status == api.NodeEnrollmentRequestApproved {
		result, code, err := s.registerLocked(e.Request, "", true)
		if code != "" {
			publicError(w, code)
			return
		}
		if err != nil {
			http.Error(w, "registration failed", 400)
			return
		}
		e.Result = &result
		e.Public.Status = api.NodeEnrollmentRequestEnrolled
		e.Public.NodeID = result.Node.ID
	}
	writeJSON(w, api.CompleteNodeEnrollmentRequestResponse{Request: e.Public, Registration: e.Result})
}
