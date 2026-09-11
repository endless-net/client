// Package testcontrol implements a stateful Client API peer for client tests.
// It is not a backend implementation or a production authorization service.
package testcontrol

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
)

const credentialHeader = "X-EndlessNet-Node-Credential"

// Event deliberately excludes request bodies, credentials and identity keys.
type Event struct {
	Kind, NodeID, Path       string
	OperationID, RequestHash string
	Cursor                   api.MapCursor
}

type node struct {
	Map     api.RegisterNodeResponse
	Revoked bool
	Delta   *api.MapStreamEvent
}

type operation struct {
	Binding string
	Result  api.RegisterNodeResponse
}

type responseFault struct {
	Status            int
	ContentType, Body string
}

type enrollment struct {
	Request api.RegisterNodeRequest
	Public  api.NodeEnrollmentRequest
	Token   string
	Result  *api.RegisterNodeResponse
}

// Server owns all mutable state. Callers receive independent copies.
type Server struct {
	HTTP                     *httptest.Server
	mu                       sync.Mutex
	key                      ed25519.PrivateKey
	trust                    api.SigningTrustBundle
	session                  string
	networks                 map[string]api.Network
	joins                    map[string]string
	nodes                    map[string]*node
	operations               map[string]operation
	enrollments              map[string]*enrollment
	events                   []Event
	changed                  chan struct{}
	streams                  chan struct{}
	closed                   chan struct{}
	closeOnce                sync.Once
	unavailable              bool
	faults                   map[string]api.ErrorCode
	responseFaults           map[string]responseFault
	mapFaults                map[string]string
	active                   int
	dropRegistrationResponse bool
	registrationFault        string
	flows                    map[string]*flowState
}

func New(t testing.TB) *Server {
	return NewWithListener(t, nil)
}

// NewWithListener allows isolated namespace clients to reach the test peer.
// The caller supplies a listener on the CI-only underlay; nil uses loopback.
func NewWithListener(t testing.TB, listener net.Listener) *Server {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	trust, err := api.NewSigningTrustBundle(base64.RawURLEncoding.EncodeToString(pub))
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{key: key, trust: trust, session: rand.Text(), networks: map[string]api.Network{}, joins: map[string]string{}, nodes: map[string]*node{}, operations: map[string]operation{}, enrollments: map[string]*enrollment{}, changed: make(chan struct{}), streams: make(chan struct{}), closed: make(chan struct{}), faults: map[string]api.ErrorCode{}, mapFaults: map[string]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /client/readyz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("GET /server-key", s.serverKey)
	mux.HandleFunc("POST /nodes/register", s.register)
	mux.HandleFunc("POST /nodes/enrollment-requests", s.createEnrollment)
	mux.HandleFunc("GET /nodes/enrollment-requests/{id}", s.enrollmentStatus)
	mux.HandleFunc("POST /nodes/enrollment-requests/{id}/complete", s.completeEnrollment)
	mux.HandleFunc("PATCH /nodes/{id}/endpoint", s.endpoint)
	mux.HandleFunc("DELETE /nodes/{id}", s.deleteNode)
	mux.HandleFunc("GET /maps/{id}/stream", s.stream)
	mux.HandleFunc("POST /auth/logout", s.logout)
	s.mountRPC(mux)
	s.HTTP = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		code := s.faults[r.Method+" "+r.URL.Path]
		delete(s.faults, r.Method+" "+r.URL.Path)
		response, responseFaulted := s.responseFaults[r.Method+" "+r.URL.Path]
		if s.unavailable {
			code = api.ErrorCodeTemporarilyUnavailable
		}
		s.recordLocked(Event{Kind: "request", Path: r.Method + " " + r.URL.Path})
		s.mu.Unlock()
		if responseFaulted {
			w.Header().Set("Content-Type", response.ContentType)
			w.Header().Set("X-Request-ID", "test-request")
			w.WriteHeader(response.Status)
			_, _ = io.WriteString(w, response.Body)
			return
		}
		if code != "" {
			publicError(w, code)
			return
		}
		mux.ServeHTTP(w, r)
	}))
	if listener != nil {
		_ = s.HTTP.Listener.Close()
		s.HTTP.Listener = listener
	}
	s.HTTP.Start()
	t.Cleanup(s.Close)
	return s
}

func (s *Server) Close()                        { s.closeOnce.Do(func() { close(s.closed); s.HTTP.Close() }) }
func (s *Server) URL() string                   { return s.HTTP.URL }
func (s *Server) Trust() api.SigningTrustBundle { return api.CloneSigningTrustBundle(s.trust) }
func (s *Server) SessionToken() string          { s.mu.Lock(); defer s.mu.Unlock(); return s.session }

func clone[T any](v T) T {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	var out T
	if err := json.Unmarshal(b, &out); err != nil {
		panic(err)
	}
	return out
}

// AddNetwork creates an isolated IPv4 network and returns its join token.
func (s *Server) AddNetwork(name, cidr string) (api.Network, string, error) {
	p, err := netip.ParsePrefix(cidr)
	if err != nil || !p.Addr().Is4() || p.Bits() > 24 {
		return api.Network{}, "", errors.New("test network requires IPv4 /24 or larger")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, network := range s.networks {
		if network.Name == name {
			return api.Network{}, "", errors.New("duplicate network name")
		}
	}
	id := "net_" + rand.Text()
	n := api.Network{ID: id, Name: name, CIDR: p.Masked().String(), AccountID: "test-account", OwnerID: "test-user", Revision: 1, CreatedAt: time.Now().UTC()}
	token := rand.Text()
	s.networks[id] = n
	s.joins[token] = id
	return clone(n), token, nil
}

func (s *Server) Events() []Event    { s.mu.Lock(); defer s.mu.Unlock(); return clone(s.events) }
func (s *Server) ActiveStreams() int { s.mu.Lock(); defer s.mu.Unlock(); return s.active }
func (s *Server) recordLocked(e Event) {
	s.events = append(s.events, e)
	close(s.changed)
	s.changed = make(chan struct{})
}

func (s *Server) Await(ctx context.Context, match func(Event) bool) (Event, error) {
	for {
		s.mu.Lock()
		events := clone(s.events)
		changed := s.changed
		s.mu.Unlock()
		for _, e := range events {
			if match(e) {
				return e, nil
			}
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return Event{}, ctx.Err()
		case <-s.closed:
			return Event{}, errors.New("server closed")
		}
	}
}

func (s *Server) SetUnavailable(value bool) {
	s.mu.Lock()
	s.unavailable = value
	s.mu.Unlock()
	if value {
		s.BreakStreams()
	}
}
func (s *Server) FailNext(method, path string, code api.ErrorCode) error {
	if !code.Valid() {
		return errors.New("unknown public error code")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.faults[method+" "+path] = code
	return nil
}

// SetResponseFault persistently replaces a single wire route with an explicit
// negative response. Unlike FailNext it lets a process observe a stable failure.
// Bodies are test-supplied and never included in the event transcript.
func (s *Server) SetResponseFault(method, path string, status int, contentType, body string) error {
	if status < 400 || status > 599 || method == "" || !strings.HasPrefix(path, "/") || len(body) > 1<<20 {
		return errors.New("invalid negative response fault")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.responseFaults == nil {
		s.responseFaults = make(map[string]responseFault)
	}
	s.responseFaults[method+" "+path] = responseFault{Status: status, ContentType: contentType, Body: body}
	return nil
}

func (s *Server) ClearResponseFault(method, path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.responseFaults, method+" "+path)
}
func (s *Server) BreakStreams() {
	s.mu.Lock()
	defer s.mu.Unlock()
	close(s.streams)
	s.streams = make(chan struct{})
}

// DropNextRegistrationResponse closes the connection after successful registration
// has been recorded. Only the response is lost; a retry must reuse its operation.
func (s *Server) DropNextRegistrationResponse() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropRegistrationResponse = true
}

func (s *Server) Revoke(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.nodes[id]
	if n == nil {
		return errors.New("unknown node")
	}
	n.Revoked = true
	s.recordLocked(Event{Kind: "revoked", NodeID: id})
	return nil
}

func (s *Server) Snapshot(id string) (api.NetworkMapSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.nodes[id]
	if n == nil {
		return api.NetworkMapSnapshot{}, errors.New("unknown node")
	}
	return clone(n.Map.Snapshot()), nil
}

// UpdateMap validates and signs a new projection, then wakes subscribers.
func (s *Server) UpdateMap(id string, edit func(*api.NetworkMapSnapshot)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.updateMapLocked(id, edit)
}

// UpdatePeers publishes one retained delta. A client with any older cursor must
// receive a full resync; this double deliberately does not implement a history.
func (s *Server) UpdatePeers(id string, peers []api.Peer) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.nodes[id]
	if n == nil {
		return errors.New("unknown node")
	}
	base := clone(n.Map.Snapshot())
	if err := s.updateMapLocked(id, func(m *api.NetworkMapSnapshot) { m.Peers = clone(peers) }); err != nil {
		return err
	}
	result := clone(n.Map.Snapshot())
	delta := api.MapDelta{Network: &result.Network, PeerUpserts: clone(peers)}
	for _, old := range base.Peers {
		if !slices.ContainsFunc(peers, func(p api.Peer) bool { return p.ID == old.ID }) {
			delta.PeerRemoveIDs = append(delta.PeerRemoveIDs, old.ID)
		}
	}
	n.Delta = &api.MapStreamEvent{Type: "delta", ProtocolVersion: api.MapStreamProtocolVersion, Capabilities: api.MapStreamSupportedCapabilities(), EventID: fmt.Sprintf("%s-%d", id, result.Revision.Network), From: base.Revision, To: result.Revision, BaseHash: base.MapSignature.PayloadHash, Delta: &delta, ResultSignature: result.MapSignature}
	return nil
}

func (s *Server) updateMapLocked(id string, edit func(*api.NetworkMapSnapshot)) error {
	n := s.nodes[id]
	if n == nil {
		return errors.New("unknown node")
	}
	m := clone(n.Map.Snapshot())
	edit(&m)
	if m.Node.ID != id || m.Network.ID != n.Map.Network.ID {
		return errors.New("map identity cannot change")
	}
	m.Revision.Network = n.Map.Revision.Network + 1
	m.Network.Revision = m.Revision.Network
	if err := api.ValidateNetworkMapSnapshot(m); err != nil {
		return err
	}
	sig, err := api.SignNetworkMapSnapshot(s.key, m)
	if err != nil {
		return err
	}
	m.MapSignature = sig
	n.Map.Network = m.Network
	n.Map.Node = m.Node
	n.Map.Peers = m.Peers
	n.Map.Revision = m.Revision
	n.Map.STUNEndpoints = m.STUNEndpoints
	n.Map.Relays = m.Relays
	n.Map.RelayCredential = m.RelayCredential
	n.Map.MapSignature = m.MapSignature
	n.Delta = nil
	s.recordLocked(Event{Kind: "map-updated", NodeID: id})
	return nil
}

// FaultNextRegistrationResponse alters only the next successful wire response.
// The durable operation remains available for an unmodified retry.
func (s *Server) FaultNextRegistrationResponse(fault string) error {
	if !slices.Contains([]string{"operation", "binding", "fingerprint", "credential-node", "credential-network", "map-signature"}, fault) {
		return errors.New("unknown registration fault")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registrationFault = fault
	return nil
}

// FaultNextMap corrupts only the next wire response, leaving server state valid.
func (s *Server) FaultNextMap(id, fault string) error {
	if !slices.Contains([]string{"signature", "unknown-key", "expired", "delta-base", "delta-revision"}, fault) {
		return errors.New("unknown map fault")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.nodes[id] == nil {
		return errors.New("unknown node")
	}
	if strings.HasPrefix(fault, "delta-") && s.nodes[id].Delta == nil {
		return errors.New("delta fault requires a retained delta")
	}
	s.mapFaults[id] = fault
	return nil
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
func publicError(w http.ResponseWriter, code api.ErrorCode) {
	status, _ := code.HTTPStatus()
	w.Header().Set("X-Request-ID", "test-request")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	writeJSON(w, api.PublicError{SchemaVersion: api.SchemaVersion, ErrorCode: code, DiagnosticMessage: "test control request rejected", RequestID: "test-request"})
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		http.Error(w, "invalid request", 400)
		return false
	}
	if d.Decode(new(any)) != io.EOF {
		http.Error(w, "invalid trailing data", 400)
		return false
	}
	return true
}

func (s *Server) serverKey(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, api.ServerKeyResponse{TrustBundle: s.Trust(), NodeCredentialTrustBundle: s.Trust(), RelayTrustBundle: s.Trust()})
}

func (s *Server) credentialLocked(value, id, scope string) (*node, api.ErrorCode) {
	if value == "" {
		return nil, api.ErrorCodeAuthenticationRequired
	}
	claims, err := api.VerifyNodeCredentialWithTrustBundle(value, s.trust, scope, time.Now())
	if err != nil {
		if expired, decodeErr := api.DecodeNodeCredential(value); decodeErr == nil && time.Now().After(expired.ExpiresAt) {
			if _, verifyErr := api.VerifyNodeCredentialWithTrustBundle(value, s.trust, scope, expired.ExpiresAt.Add(-time.Nanosecond)); verifyErr == nil {
				return nil, api.ErrorCodeNodeCredentialExpired
			}
		}
		return nil, api.ErrorCodeNodeCredentialInvalid
	}
	n := s.nodes[claims.NodeID]
	if n == nil {
		return nil, api.ErrorCodeNodeCredentialUnknown
	}
	if n.Revoked {
		return nil, api.ErrorCodeNodeCredentialRevoked
	}
	if claims.NetworkID != n.Map.Network.ID || (id != "" && claims.NodeID != id) {
		return nil, api.ErrorCodeAuthorizationDenied
	}
	return n, ""
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var req api.RegisterNodeRequest
	if !decode(w, r, &req) {
		return
	}
	if req.Validate() != nil {
		http.Error(w, "invalid registration", 400)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := json.Marshal(req)
	if err != nil {
		http.Error(w, "invalid registration", 400)
		return
	}
	s.recordLocked(Event{Kind: "registration-request", OperationID: req.IdempotencyID, RequestHash: fmt.Sprintf("%x", sha256.Sum256(raw))})
	result, code, err := s.registerLocked(req, r.Header.Get("Authorization"), false)
	if code != "" {
		publicError(w, code)
		return
	}
	if err != nil {
		http.Error(w, "invalid registration", 400)
		return
	}
	if s.dropRegistrationResponse {
		s.dropRegistrationResponse = false
		s.recordLocked(Event{Kind: "registration-response-dropped", NodeID: result.Node.ID})
		// The test peer serves HTTP/1 over its own listener. Abort after commit,
		// before headers; ErrAbortHandler makes net/http close the connection.
		panic(http.ErrAbortHandler)
	}
	if fault := s.registrationFault; fault != "" {
		s.registrationFault = ""
		switch fault {
		case "operation":
			result.IdempotencyID, err = api.NewRegistrationIdempotencyID()
		case "binding":
			changed := req
			changed.Hostname += "-different"
			result.RegistrationBinding = api.RegistrationIdentityProofBinding(changed)
		case "fingerprint":
			result.Node.DeviceFingerprint += "-different"
		case "credential-node":
			result.NodeCredential, err = api.SignNodeCredential(s.key, result.Network.ID, "different-node", []string{"node:map"}, time.Now().Add(time.Hour))
		case "credential-network":
			result.NodeCredential, err = api.SignNodeCredential(s.key, "different-network", result.Node.ID, []string{"node:map"}, time.Now().Add(time.Hour))
		}
		if err == nil {
			result.MapSignature, err = api.SignNetworkMap(s.key, result)
		}
		if err != nil {
			http.Error(w, "test response signing failed", 500)
			return
		}
		if fault == "map-signature" {
			result.MapSignature.Signature = base64.RawURLEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))
		}
		s.recordLocked(Event{Kind: "registration-response-faulted", NodeID: result.Node.ID})
	}
	writeJSON(w, result)
}

func (s *Server) registerLocked(req api.RegisterNodeRequest, authorization string, approved bool) (api.RegisterNodeResponse, api.ErrorCode, error) {
	binding := api.RegistrationIdentityProofBinding(req)
	if old, ok := s.operations[req.IdempotencyID]; ok {
		if old.Binding != binding {
			return api.RegisterNodeResponse{}, api.ErrorCodeNodeIdentityBindingMismatch, nil
		}
		if s.nodes[old.Result.Node.ID].Revoked {
			return api.RegisterNodeResponse{}, api.ErrorCodeNodeCredentialRevoked, nil
		}
		return clone(old.Result), "", nil
	}
	var n *node
	networkID := req.NetworkID
	if req.NodeCredential != "" {
		var code api.ErrorCode
		n, code = s.credentialLocked(req.NodeCredential, "", "node:register")
		if code != "" {
			return api.RegisterNodeResponse{}, code, nil
		}
		if n.Map.Node.IdentityPublicKey != req.IdentityPublicKey || n.Map.Node.PublicKey != req.PublicKey || n.Map.Node.DeviceFingerprint != req.DeviceFingerprint || n.Map.RegistrationBinding != req.RegistrationBinding || n.Map.Network.ID != req.NetworkID {
			return api.RegisterNodeResponse{}, api.ErrorCodeNodeIdentityBindingMismatch, nil
		}
		networkID = n.Map.Network.ID
	} else if req.JoinToken != "" {
		var ok bool
		networkID, ok = s.joins[req.JoinToken]
		if !ok {
			return api.RegisterNodeResponse{}, api.ErrorCodeAuthorizationDenied, nil
		}
		if req.NetworkID != "" && req.NetworkID != networkID {
			return api.RegisterNodeResponse{}, api.ErrorCodeAuthorizationDenied, nil
		}
	} else if !approved && (s.session == "" || authorization != "Bearer "+s.session || req.SessionTokenBinding != api.RegistrationSessionTokenBinding(s.session)) {
		return api.RegisterNodeResponse{}, api.ErrorCodeAuthenticationRequired, nil
	}
	if networkID == "" {
		for id, network := range s.networks {
			if network.Name == req.NetworkName {
				networkID = id
				break
			}
		}
	}
	network, ok := s.networks[networkID]
	if !ok || (req.AccountID != "" && req.AccountID != network.AccountID) {
		return api.RegisterNodeResponse{}, api.ErrorCodeAuthorizationDenied, nil
	}
	if n == nil {
		p, _ := netip.ParsePrefix(network.CIDR)
		addr := p.Addr().Next()
		for range len(s.nodes) {
			addr = addr.Next()
		}
		if !p.Contains(addr) {
			return api.RegisterNodeResponse{}, "", errors.New("network full")
		}
		n = &node{Map: api.RegisterNodeResponse{Node: api.Node{ID: "node-" + rand.Text(), NetworkID: networkID, Hostname: req.Hostname, IdentityPublicKey: req.IdentityPublicKey, PublicKey: req.PublicKey, DeviceFingerprint: req.DeviceFingerprint, AssignedIP: addr.String(), ApprovalState: api.NodeApprovalApproved, Status: "online"}, Network: network, Revision: api.MapRevision{Network: 1}}}
	}
	result := clone(n.Map)
	result.Node.AdvertisedIPs = slices.Clone(req.AdvertisedIPs)
	result.SchemaVersion = api.SchemaVersion
	result.IdempotencyID = req.IdempotencyID
	result.RegistrationBinding = binding
	credential, err := api.SignNodeCredential(s.key, networkID, result.Node.ID, []string{"node:map", "node:register", "node:endpoint", "node:delete"}, time.Now().Add(time.Hour))
	if err != nil {
		return result, "", err
	}
	result.NodeCredential = credential
	result.MapSignature, err = api.SignNetworkMap(s.key, result)
	if err != nil {
		return result, "", err
	}
	n.Map = result
	s.nodes[result.Node.ID] = n
	s.operations[req.IdempotencyID] = operation{binding, clone(result)}
	s.recordLocked(Event{Kind: "registered", NodeID: result.Node.ID})
	return clone(result), "", nil
}

func (s *Server) endpoint(w http.ResponseWriter, r *http.Request) {
	var req api.UpdateNodeEndpointRequest
	if !decode(w, r, &req) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n, code := s.credentialLocked(r.Header.Get(credentialHeader), r.PathValue("id"), "node:endpoint")
	if code != "" {
		publicError(w, code)
		return
	}
	m := clone(n.Map)
	if req.Generation != 0 && req.Generation < m.Node.EndpointGeneration {
		http.Error(w, "stale endpoint generation", http.StatusConflict)
		return
	}
	if req.Endpoint != "" || req.Generation != 0 || len(req.Candidates) != 0 {
		m.Node.Endpoint = req.Endpoint
		m.Node.EndpointGeneration = req.Generation
		m.Node.EndpointCandidates = req.Candidates
	}
	if req.Status != "" {
		m.Node.Status = req.Status
	}
	if req.TTL != "" {
		ttl, err := time.ParseDuration(req.TTL)
		if err != nil || ttl <= 0 || ttl > 24*time.Hour {
			http.Error(w, "invalid ttl", 400)
			return
		}
		expires := time.Now().Add(ttl)
		m.Node.EndpointExpiresAt = &expires
	}
	if api.ValidateNetworkMap(m) != nil {
		http.Error(w, "invalid endpoint", 400)
		return
	}
	// A liveness heartbeat must not manufacture a new map revision and keep
	// the agent from ever entering its map stream.
	if reflect.DeepEqual(m.Node, n.Map.Node) {
		writeJSON(w, m)
		return
	}
	m.Revision.Network++
	m.Network.Revision = m.Revision.Network
	sig, err := api.SignNetworkMap(s.key, m)
	if err != nil {
		http.Error(w, "signing failed", 500)
		return
	}
	m.MapSignature = sig
	n.Map = m
	n.Delta = nil
	s.recordLocked(Event{Kind: "endpoint", NodeID: m.Node.ID})
	writeJSON(w, m)
}

func (s *Server) deleteNode(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, code := s.credentialLocked(r.Header.Get(credentialHeader), r.PathValue("id"), "node:delete")
	if code != "" {
		publicError(w, code)
		return
	}
	n.Revoked = true
	s.recordLocked(Event{Kind: "deleted", NodeID: n.Map.Node.ID})
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session == "" || r.Header.Get("Authorization") != "Bearer "+s.session {
		publicError(w, api.ErrorCodeAuthenticationRequired)
		return
	}
	s.session = ""
	s.recordLocked(Event{Kind: "logout"})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	capabilities := strings.Join(api.MapStreamSupportedCapabilities(), ",")
	if r.Header.Get("X-EndlessNet-Map-Protocol") != strconv.Itoa(api.MapStreamProtocolVersion) || r.Header.Get("X-EndlessNet-Map-Capabilities") != capabilities {
		http.Error(w, "unsupported stream negotiation", 400)
		return
	}
	id := r.PathValue("id")
	nr, e1 := strconv.ParseUint(r.URL.Query().Get("from_network_revision"), 10, 64)
	gr, e2 := strconv.ParseUint(r.URL.Query().Get("from_global_revision"), 10, 64)
	timeout, e3 := time.ParseDuration(r.URL.Query().Get("timeout"))
	if e1 != nil || e2 != nil || e3 != nil || timeout <= 0 {
		http.Error(w, "invalid cursor", 400)
		return
	}
	timeout = min(timeout, 30*time.Second)
	cursor := api.MapCursor{Revision: api.MapRevision{Network: nr, Global: gr}, MapHash: r.URL.Query().Get("from_map_hash")}
	s.mu.Lock()
	s.active++
	broken := s.streams
	s.recordLocked(Event{Kind: "stream", NodeID: id, Cursor: cursor})
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.active--
		s.recordLocked(Event{Kind: "stream-closed", NodeID: id})
		s.mu.Unlock()
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		s.mu.Lock()
		n, code := s.credentialLocked(r.Header.Get(credentialHeader), id, "node:map")
		if code != "" {
			s.mu.Unlock()
			publicError(w, code)
			return
		}
		m := clone(n.Map.Snapshot())
		changed := s.changed
		fault := s.mapFaults[id]
		if !m.Revision.Equal(cursor.Revision) || m.MapSignature.PayloadHash != cursor.MapHash || fault != "" {
			delete(s.mapFaults, id)
			var delta *api.MapStreamEvent
			if n.Delta != nil && n.Delta.From.Equal(cursor.Revision) && n.Delta.BaseHash == cursor.MapHash && (fault == "" || strings.HasPrefix(fault, "delta-")) {
				delta = clone(n.Delta)
				s.recordLocked(Event{Kind: "map-delta", NodeID: id, Cursor: cursor})
			} else if cursor.MapHash != "" {
				s.recordLocked(Event{Kind: "map-resync", NodeID: id, Cursor: cursor})
			}
			s.mu.Unlock()
			switch fault {
			case "signature":
				m.MapSignature.Signature = base64.RawURLEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))
			case "unknown-key":
				m.MapSignature.KeyID = "unknown-test-key"
			case "expired":
				sig, err := api.SignNetworkMapSnapshotAt(s.key, m, time.Now().Add(-2*time.Hour), time.Hour)
				if err != nil {
					http.Error(w, "signing failed", 500)
					return
				}
				m.MapSignature = sig
			}
			kind := "snapshot"
			if cursor.MapHash != "" {
				kind = "resync"
			}
			event := api.MapStreamEvent{Type: kind, ProtocolVersion: api.MapStreamProtocolVersion, Capabilities: api.MapStreamSupportedCapabilities(), EventID: fmt.Sprintf("%s-%d", id, m.Revision.Network), From: cursor.Revision, To: m.Revision, Snapshot: &m, ResultSignature: m.MapSignature}
			if delta != nil {
				event = *delta
				if fault == "delta-base" {
					event.BaseHash = strings.Repeat("0", 64)
				}
				if fault == "delta-revision" {
					event.From.Network++
				}
			}
			w.Header().Set("X-EndlessNet-Map-Protocol", strconv.Itoa(api.MapStreamProtocolVersion))
			w.Header().Set("X-EndlessNet-Map-Capabilities", capabilities)
			writeJSON(w, event)
			return
		}
		s.mu.Unlock()
		select {
		case <-changed:
		case <-timer.C:
			w.Header().Set("X-EndlessNet-Map-Protocol", strconv.Itoa(api.MapStreamProtocolVersion))
			w.Header().Set("X-EndlessNet-Map-Capabilities", capabilities)
			return
		case <-broken:
			return
		case <-s.closed:
			return
		case <-r.Context().Done():
			return
		}
	}
}
