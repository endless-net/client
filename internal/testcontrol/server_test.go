package testcontrol_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"connectrpc.com/connect"
	api "github.com/endless-net/client-api/clientapi/v1"
	rpc "github.com/endless-net/client-api/clientapi/v1/clientrpc"
	bindings "github.com/endless-net/client-api/clientapi/v1/clientrpc/clientrpcconnect"
	wg "github.com/endless-net/client-api/clientapi/wireguard"
	"github.com/endless-net/client/internal/testcontrol"
)

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func request(t *testing.T, n api.Network, token string) (api.RegisterNodeRequest, ed25519.PrivateKey) {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	check(t, err)
	private, err := wg.GeneratePrivateKey()
	check(t, err)
	public, err := wg.PublicKey(private)
	check(t, err)
	id, err := api.NewRegistrationIdempotencyID()
	check(t, err)
	r := api.RegisterNodeRequest{SchemaVersion: api.SchemaVersion, IdempotencyID: id, NetworkID: n.ID, Hostname: "test-node", DeviceFingerprint: "test-device", PublicKey: public, JoinToken: token}
	check(t, api.SetRegisterNodeIdentityProof(&r, key))
	return r, key
}
func setup(t *testing.T) (*testcontrol.Server, *api.API, api.RegisterNodeRequest, api.RegisterNodeResponse, ed25519.PrivateKey) {
	t.Helper()
	s := testcontrol.New(t)
	n, token, err := s.AddNetwork("test", "100.80.0.0/24")
	check(t, err)
	req, key := request(t, n, token)
	a := api.NewAPI(s.URL(), "")
	result, err := a.RegisterNode(req)
	check(t, err)
	check(t, api.VerifyNetworkMapSignatureWithTrustBundle(result, s.Trust()))
	a.NodeCredential = result.NodeCredential
	return s, a, req, result, key
}

func TestRegistrationFaultPreservesCommittedReplay(t *testing.T) {
	for _, fault := range []string{"operation", "binding", "fingerprint", "credential-node", "credential-network", "map-signature"} {
		t.Run(fault, func(t *testing.T) {
			s, a, req, original, _ := setup(t)
			check(t, s.FaultNextRegistrationResponse(fault))
			requestBody, err := json.Marshal(req)
			check(t, err)
			wire, err := http.Post(s.URL()+"/nodes/register", "application/json", bytes.NewReader(requestBody))
			check(t, err)
			defer func() { _ = wire.Body.Close() }()
			if wire.StatusCode != http.StatusOK {
				t.Fatalf("fault response HTTP %d", wire.StatusCode)
			}
			var bad api.RegisterNodeResponse
			check(t, json.NewDecoder(wire.Body).Decode(&bad))
			if fault == "map-signature" {
				if api.VerifyNetworkMapSignatureWithTrustBundle(bad, s.Trust()) == nil {
					t.Fatal("signature fault was accepted")
				}
			} else {
				check(t, api.VerifyNetworkMapSignatureWithTrustBundle(bad, s.Trust()))
			}
			changed := false
			switch fault {
			case "operation":
				changed = bad.IdempotencyID != original.IdempotencyID
			case "binding":
				changed = bad.RegistrationBinding != original.RegistrationBinding
			case "fingerprint":
				changed = bad.Node.DeviceFingerprint != original.Node.DeviceFingerprint
			case "credential-node", "credential-network":
				claims, err := api.VerifyNodeCredentialWithTrustBundle(bad.NodeCredential, s.Trust(), "node:map", time.Now())
				check(t, err)
				changed = claims.NodeID != original.Node.ID || claims.NetworkID != original.Network.ID
			case "map-signature":
				changed = bad.MapSignature.Signature != original.MapSignature.Signature
			}
			if !changed {
				t.Fatal("fault did not alter wire response")
			}
			replayed, err := a.RegisterNode(req)
			check(t, err)
			want, err := json.Marshal(original)
			check(t, err)
			got, err := json.Marshal(replayed)
			check(t, err)
			if !bytes.Equal(got, want) {
				t.Fatal("fault modified committed registration")
			}
		})
	}
}

func TestCredentialRefreshDoesNotCreateAnotherNode(t *testing.T) {
	s, a, original, result, key := setup(t)
	req := original
	var err error
	req.IdempotencyID, err = api.NewRegistrationIdempotencyID()
	check(t, err)
	req.JoinToken = ""
	req.NodeCredential = result.NodeCredential
	req.RegistrationBinding = result.RegistrationBinding
	check(t, api.SetRegisterNodeIdentityProof(&req, key))
	refreshed, err := a.RegisterNode(req)
	check(t, err)
	if refreshed.Node.ID != result.Node.ID || refreshed.Node.PublicKey != result.Node.PublicKey || refreshed.Node.IdentityPublicKey != result.Node.IdentityPublicKey {
		t.Fatal("credential refresh changed node identity")
	}
	created, refreshes := 0, 0
	for _, event := range s.Events() {
		if event.Kind == "registered" {
			created++
		}
		if event.Kind == "registration-refreshed" {
			refreshes++
		}
	}
	if created != 1 || refreshes != 1 {
		t.Fatal("transcript conflates refresh with node creation")
	}
}

func TestTLSControlPeerRequiresItsExplicitTrust(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	check(t, err)
	s := testcontrol.NewWithListener(t, listener)
	a := api.NewAPI(s.URL(), "")
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: x509.NewCertPool()}}
	defer transport.CloseIdleConnections()
	a.HTTPClient = &http.Client{Transport: transport, Timeout: time.Second}
	if _, err := a.ServerKey(); err == nil {
		t.Fatal("untrusted test certificate accepted")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(s.TLSCertificatePEM()) {
		t.Fatal("invalid public test certificate")
	}
	trusted := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}}
	defer trusted.CloseIdleConnections()
	a.HTTPClient = &http.Client{Transport: trusted, Timeout: time.Second}
	_, err = a.ServerKey()
	check(t, err)
}

func TestPeerDeltaAndResync(t *testing.T) {
	s, a, _, registered, _ := setup(t)
	base := registered.Snapshot()
	key, err := wg.GeneratePrivateKey()
	check(t, err)
	pub, err := wg.PublicKey(key)
	check(t, err)
	peer := api.Peer{ID: "delta-peer", Hostname: "peer", PublicKey: pub, AllowedIPs: []string{"100.80.0.20/32"}}
	updated := peer
	updated.Hostname = "renamed-peer"
	current := base
	for _, peers := range [][]api.Peer{{peer}, {updated}, nil} {
		check(t, s.UpdatePeers(registered.Node.ID, peers))
		event, err := a.ReadMapStreamEvent(registered.Node.ID, api.MapCursor{Revision: current.Revision, MapHash: current.MapSignature.PayloadHash}, time.Second)
		check(t, err)
		if event.Type != "delta" || event.Snapshot != nil {
			t.Fatal("matching cursor did not receive delta")
		}
		current, err = api.ApplyMapStreamEvent(current, event, s.Trust(), time.Now())
		check(t, err)
		if len(current.Peers) != len(peers) {
			t.Fatal("delta did not change peers")
		}
		if len(peers) != 0 && current.Peers[0].Hostname != peers[0].Hostname {
			t.Fatal("delta did not replace peer fields")
		}
	}
	event, err := a.ReadMapStreamEvent(registered.Node.ID, api.MapCursor{Revision: base.Revision, MapHash: base.MapSignature.PayloadHash}, time.Second)
	check(t, err)
	if event.Type != "resync" {
		t.Fatal("expired cursor did not receive resync")
	}
	recovered, err := api.ApplyMapStreamEvent(base, event, s.Trust(), time.Now())
	check(t, err)
	if recovered.MapSignature.PayloadHash != current.MapSignature.PayloadHash {
		t.Fatal("resync did not recover current map")
	}
	for _, fault := range []string{"delta-base", "delta-revision"} {
		check(t, s.UpdatePeers(registered.Node.ID, []api.Peer{peer}))
		check(t, s.FaultNextMap(registered.Node.ID, fault))
		cursor := api.MapCursor{Revision: current.Revision, MapHash: current.MapSignature.PayloadHash}
		bad, err := a.ReadMapStreamEvent(registered.Node.ID, cursor, time.Second)
		if err == nil {
			if _, err := api.ApplyMapStreamEvent(current, bad, s.Trust(), time.Now()); err == nil {
				t.Fatal("invalid delta accepted")
			}
		}
		good, err := a.ReadMapStreamEvent(registered.Node.ID, cursor, time.Second)
		check(t, err)
		current, err = api.ApplyMapStreamEvent(current, good, s.Trust(), time.Now())
		check(t, err)
	}
}

func TestRegistrationBindingAndRevocation(t *testing.T) {
	s, a, req, result, key := setup(t)
	heartbeat, err := a.UpdateNodeEndpointState(result.Node.ID, api.UpdateNodeEndpointRequest{Status: api.NodeStatusOnline})
	check(t, err)
	if !heartbeat.Revision.Equal(result.Revision) || heartbeat.MapSignature.PayloadHash != result.MapSignature.PayloadHash {
		t.Fatal("heartbeat changed signed projection")
	}
	repeated, err := a.RegisterNode(req)
	check(t, err)
	if repeated.Node.ID != result.Node.ID || repeated.NodeCredential != result.NodeCredential {
		t.Fatal("retry changed durable registration")
	}
	conflict := req
	conflict.Hostname = "changed"
	check(t, api.SetRegisterNodeIdentityProof(&conflict, key))
	if _, err = a.RegisterNode(conflict); !api.IsControlPlaneStatus(err, 409) {
		t.Fatal("changed idempotent request was accepted")
	}
	renew := req
	renew.IdempotencyID, err = api.NewRegistrationIdempotencyID()
	check(t, err)
	renew.JoinToken = ""
	renew.NodeCredential = result.NodeCredential
	renew.RegistrationBinding = result.RegistrationBinding
	check(t, api.SetRegisterNodeIdentityProof(&renew, key))
	updated, err := a.RegisterNode(renew)
	check(t, err)
	if updated.Node.ID != result.Node.ID {
		t.Fatal("renewal created a new node")
	}
	a.NodeCredential = updated.NodeCredential
	if _, err = a.UpdateNodeEndpointState("another-node", api.UpdateNodeEndpointRequest{}); !api.IsControlPlaneStatus(err, 403) {
		t.Fatal("cross-node endpoint mutation accepted")
	}
	check(t, s.Revoke(result.Node.ID))
	if _, err = a.ReadMapStreamEvent(result.Node.ID, api.MapCursor{}, time.Second); !api.IsControlPlaneStatus(err, 401) {
		t.Fatal("revoked credential accepted")
	}
}

func TestRegistrationResponseLossPreservesOperation(t *testing.T) {
	s := testcontrol.New(t)
	n, token, err := s.AddNetwork("lost-response", "100.82.0.0/24")
	check(t, err)
	req, _ := request(t, n, token)
	a := api.NewAPI(s.URL(), "")
	s.DropNextRegistrationResponse()
	if _, err := a.RegisterNode(req); err == nil {
		t.Fatal("response loss was not observable by caller")
	}
	result, err := a.RegisterNode(req)
	check(t, err)
	check(t, api.VerifyNetworkMapSignatureWithTrustBundle(result, s.Trust()))
	var dropped string
	var attempts []testcontrol.Event
	commits := 0
	for _, event := range s.Events() {
		switch event.Kind {
		case "registration-request":
			attempts = append(attempts, event)
		case "registration-response-dropped":
			dropped = event.NodeID
		case "registered":
			commits++
		}
	}
	if dropped != result.Node.ID || commits != 1 || len(attempts) != 2 {
		t.Fatal("response loss/replay did not preserve one logical registration")
	}
	if attempts[0].OperationID != attempts[1].OperationID || attempts[0].RequestHash != attempts[1].RequestHash {
		t.Fatal("registration retry changed its wire input")
	}
}

func TestNegativeWireResponseIsScopedAndRecoverable(t *testing.T) {
	s, a, _, result, _ := setup(t)
	path := "/maps/" + result.Node.ID + "/stream"
	check(t, s.SetResponseFault("GET", path, 401, "text/plain", "node_credential_unknown"))
	for range 2 {
		if _, err := a.ReadMapStreamEvent(result.Node.ID, api.MapCursor{}, time.Second); err == nil {
			t.Fatal("negative wire response was ignored")
		}
	}
	_, err := a.ServerKey()
	check(t, err)
	s.ClearResponseFault("GET", path)
	event, err := a.ReadMapStreamEvent(result.Node.ID, api.MapCursor{}, time.Second)
	check(t, err)
	_, err = api.ApplyMapStreamEvent(api.NetworkMapSnapshot{}, event, s.Trust(), time.Now())
	check(t, err)
	if err := s.SetResponseFault("GET", path, 200, "text/plain", "success"); err == nil {
		t.Fatal("negative response API accepted a success status")
	}
}

func TestSignedMapUpdatesAndFailures(t *testing.T) {
	s, a, _, result, _ := setup(t)
	current := result.Snapshot()
	for _, fault := range []string{"signature", "unknown-key", "expired"} {
		t.Run(fault, func(t *testing.T) {
			check(t, s.FaultNextMap(result.Node.ID, fault))
			event, err := a.ReadMapStreamEvent(result.Node.ID, api.MapCursor{}, time.Second)
			check(t, err)
			if _, err = api.ApplyMapStreamEvent(api.NetworkMapSnapshot{}, event, s.Trust(), time.Now()); err == nil {
				t.Fatal("invalid map accepted")
			}
		})
	}
	private, err := wg.GeneratePrivateKey()
	check(t, err)
	public, err := wg.PublicKey(private)
	check(t, err)
	for step, count := range []int{1, 1, 0} {
		hostname, port := "peer", 443
		if step == 1 {
			hostname, port = "renamed-peer", 8443
		}
		check(t, s.UpdateMap(result.Node.ID, func(m *api.NetworkMapSnapshot) {
			m.Peers = nil
			if count != 0 {
				m.Peers = []api.Peer{{ID: "peer", Hostname: hostname, PublicKey: public, AllowedIPs: []string{"100.80.0.20/32"}, ACLRestricted: true, ACLGrants: []api.ACLGrant{{DestinationCIDRs: []string{"100.80.0.20/32"}, AllowedPorts: []api.ACLPort{{Protocol: "tcp", Port: port}}}}}}
			}
			m.Network.DNS = []string{"1.1.1.1"}
		}))
		event, err := a.ReadMapStreamEvent(result.Node.ID, api.MapCursor{Revision: current.Revision, MapHash: current.MapSignature.PayloadHash}, time.Second)
		check(t, err)
		current, err = api.ApplyMapStreamEvent(current, event, s.Trust(), time.Now())
		check(t, err)
		if len(current.Peers) != count || current.Network.DNS[0] != "1.1.1.1" {
			t.Fatal("map update was not projected")
		}
		if count != 0 {
			peer := current.Peers[0]
			if peer.Hostname != hostname || !peer.ACLRestricted || len(peer.ACLGrants) != 1 || len(peer.ACLGrants[0].AllowedPorts) != 1 || peer.ACLGrants[0].AllowedPorts[0].Port != port {
				t.Fatal("peer identity or ACL update was not projected")
			}
		}
	}
	check(t, s.FailNext("GET", "/maps/"+result.Node.ID+"/stream", api.ErrorCodeTemporarilyUnavailable))
	if _, err = a.ReadMapStreamEvent(result.Node.ID, api.MapCursor{}, time.Second); err == nil {
		t.Fatal("injected failure ignored")
	}
	_, err = a.ReadMapStreamEvent(result.Node.ID, api.MapCursor{}, time.Second)
	check(t, err)
}

func TestStreamCancellationAndConcurrentUpdates(t *testing.T) {
	s, a, _, result, _ := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := a.ReadMapStreamEvent(result.Node.ID, api.MapCursor{Revision: result.Revision, MapHash: result.MapSignature.PayloadHash}, 2*time.Second)
		done <- err
	}()
	_, err := s.Await(ctx, func(e testcontrol.Event) bool { return e.Kind == "stream" })
	check(t, err)
	s.BreakStreams()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("broken stream succeeded")
		}
	case <-ctx.Done():
		t.Fatal("broken stream did not terminate")
	}
	if s.ActiveStreams() != 0 {
		t.Fatal("stream subscription leaked")
	}
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() {
			if err := s.UpdateMap(result.Node.ID, func(m *api.NetworkMapSnapshot) { m.Network.DNS = []string{"1.1.1.1"} }); err != nil {
				t.Error(err)
			}
			_, err := s.Snapshot(result.Node.ID)
			if err != nil {
				t.Error(err)
			}
		})
	}
	group.Wait()
	s.Close()
	if s.ActiveStreams() != 0 {
		t.Fatal("closed server has subscriptions")
	}
}

func TestExpiredEnrollmentCannotCompleteOrBecomeNewByReplay(t *testing.T) {
	s := testcontrol.New(t)
	n, _, err := s.AddNetwork("expiry", "100.82.0.0/24")
	check(t, err)
	req, _ := request(t, n, "")
	a := api.NewAPI(s.URL(), "")
	e, err := a.CreateNodeEnrollmentRequest(req)
	check(t, err)
	check(t, s.ExpireEnrollment(e.Request.ID))
	status, err := a.NodeEnrollmentRequestStatus(e.Request.ID, e.PollToken)
	check(t, err)
	if status.Request.Status != api.NodeEnrollmentRequestExpired {
		t.Fatal("expired request did not expose terminal status")
	}
	completed, err := a.CompleteNodeEnrollmentRequest(e.Request.ID, e.PollToken)
	check(t, err)
	if completed.Registration != nil {
		t.Fatal("expired request issued credentials")
	}
	replay, err := a.CreateNodeEnrollmentRequest(req)
	check(t, err)
	if replay.Request.ID != e.Request.ID || replay.Request.Status != api.NodeEnrollmentRequestExpired {
		t.Fatal("exact retry resurrected expired enrollment")
	}
	if err := s.DecideEnrollment(e.Request.ID, true); err == nil {
		t.Fatal("expired request was approved")
	}
}

func TestBrowserEnrollment(t *testing.T) {
	for _, approve := range []bool{false, true} {
		t.Run(map[bool]string{false: "rejected", true: "approved"}[approve], func(t *testing.T) {
			s := testcontrol.New(t)
			n, _, err := s.AddNetwork("browser", "100.81.0.0/24")
			check(t, err)
			req, _ := request(t, n, "")
			a := api.NewAPI(s.URL(), "")
			e, err := a.CreateNodeEnrollmentRequest(req)
			check(t, err)
			if _, err = a.NodeEnrollmentRequestStatus(e.Request.ID, "wrong"); !api.IsControlPlaneStatus(err, 403) {
				t.Fatal("wrong poll token accepted")
			}
			check(t, s.DecideEnrollment(e.Request.ID, approve))
			status, err := a.NodeEnrollmentRequestStatus(e.Request.ID, e.PollToken)
			check(t, err)
			completed, err := a.CompleteNodeEnrollmentRequest(e.Request.ID, e.PollToken)
			check(t, err)
			if approve {
				if completed.Registration == nil {
					t.Fatal("approved enrollment did not complete")
				}
				check(t, api.VerifyNetworkMapSignatureWithTrustBundle(*completed.Registration, s.Trust()))
				again, err := a.CompleteNodeEnrollmentRequest(e.Request.ID, e.PollToken)
				check(t, err)
				if again.Registration.Node.ID != completed.Registration.Node.ID {
					t.Fatal("completion retry created a second node")
				}
			} else if status.Request.Status != api.NodeEnrollmentRequestRejected || completed.Registration != nil {
				t.Fatal("rejected enrollment registered a node")
			}
		})
	}
}

func TestRPCRequiresCallerAuthorization(t *testing.T) {
	s, a, _, result, _ := setup(t)
	user := bindings.NewUserServiceClient(http.DefaultClient, s.URL())
	req := connect.NewRequest(&rpc.ListAccountsRequest{})
	if _, err := user.ListAccounts(context.Background(), req); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatal("anonymous account read accepted")
	}
	req.Header().Set("Authorization", "Bearer "+s.SessionToken())
	accounts, err := user.ListAccounts(context.Background(), req)
	check(t, err)
	if len(accounts.Msg.Accounts) != 1 {
		t.Fatal("missing account")
	}
	flow := bindings.NewFlowLogServiceClient(http.DefaultClient, s.URL())
	policy := connect.NewRequest(&rpc.GetFlowLogPolicyRequest{NodeId: result.Node.ID})
	policy.Header().Set("Authorization", "Bearer "+a.NodeCredential)
	_, err = flow.GetFlowLogPolicy(context.Background(), policy)
	check(t, err)
	policy.Msg.NodeId = "other"
	if _, err = flow.GetFlowLogPolicy(context.Background(), policy); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatal("cross-node flow read accepted")
	}
	_, err = user.ListBillingPlans(context.Background(), connect.NewRequest(&rpc.ListBillingPlansRequest{}))
	if connect.CodeOf(err) != connect.CodeUnimplemented {
		t.Fatal("unsupported billing RPC must be explicit")
	}
}

func TestStrictWireAndIdentityProof(t *testing.T) {
	s, _, req, _, _ := setup(t)
	response, err := http.Post(s.URL()+"/nodes/register", "application/json", strings.NewReader(`{"legacy_field":true}`))
	check(t, err)
	check(t, response.Body.Close())
	if response.StatusCode != 400 {
		t.Fatal("unknown field accepted")
	}
	req.IdentitySignature = "invalid"
	raw, err := json.Marshal(req)
	check(t, err)
	response, err = http.Post(s.URL()+"/nodes/register", "application/json", bytes.NewReader(raw))
	check(t, err)
	check(t, response.Body.Close())
	if response.StatusCode != 400 {
		t.Fatal("server accepted invalid proof")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = s.Await(ctx, func(testcontrol.Event) bool { return false })
	if !errors.Is(err, context.Canceled) {
		t.Fatal("await ignored cancellation")
	}
}

func TestFlowAcknowledgementLossPreservesWindow(t *testing.T) {
	s, a, _, result, _ := setup(t)
	flow := bindings.NewFlowLogServiceClient(http.DefaultClient, s.URL())
	now := time.Now()
	check(t, s.SetFlowConsent(result.Node.ID, now.Add(-time.Minute), now.Add(time.Minute)))
	r := connect.NewRequest(&rpc.ReportFlowLogRequest{NodeId: result.Node.ID, ConsentVersion: 1, Window: &rpc.FlowWindow{WindowId: "lost-ack", WindowStart: timestamppb.New(now.Add(-time.Second)), WindowEnd: timestamppb.New(now), Bytes: 100}})
	r.Header().Set("Authorization", "Bearer "+a.NodeCredential)
	s.LoseNextFlowAcknowledgement()
	if _, err := flow.ReportFlowLog(context.Background(), r); connect.CodeOf(err) != connect.CodeUnavailable {
		t.Fatal("accepted flow did not lose its first acknowledgement")
	}
	response, err := flow.ReportFlowLog(context.Background(), r)
	check(t, err)
	if response.Msg.WindowId != "lost-ack" || len(s.FlowReports()) != 2 {
		t.Fatal("retry did not receive acknowledgement for the same window")
	}
	accepted, lost := 0, 0
	for _, event := range s.Events() {
		if event.Kind == "flow-accepted" && event.Path == "lost-ack" {
			accepted++
		}
		if event.Kind == "flow-ack-lost" && event.Path == "lost-ack" {
			lost++
		}
	}
	if accepted != 1 || lost != 1 {
		t.Fatal("flow acknowledgement loss changed acceptance cardinality")
	}
}

func TestFlowConsentAndIdempotency(t *testing.T) {
	s, a, _, result, _ := setup(t)
	flow := bindings.NewFlowLogServiceClient(http.DefaultClient, s.URL())
	now := time.Now()
	check(t, s.SetFlowConsent(result.Node.ID, now.Add(-time.Minute), now.Add(time.Minute)))
	r := connect.NewRequest(&rpc.ReportFlowLogRequest{NodeId: result.Node.ID, ConsentVersion: 1, Window: &rpc.FlowWindow{WindowId: "window-1", WindowStart: timestamppb.New(now.Add(-time.Second)), WindowEnd: timestamppb.New(now), Bytes: 100}})
	r.Header().Set("Authorization", "Bearer "+a.NodeCredential)
	for range 2 {
		response, err := flow.ReportFlowLog(context.Background(), r)
		check(t, err)
		if response.Msg.WindowId != "window-1" {
			t.Fatal("missing durable acknowledgement")
		}
	}
	r.Msg.Window.Bytes++
	observed := s.FlowReports()
	if len(observed) != 2 || observed[0].Window.Bytes != 100 || observed[1].Window.Bytes != 100 {
		t.Fatal("wire capture lost retry bodies or retained caller aliases")
	}
	observed[0].Window.Bytes = 0
	if s.FlowReports()[0].Window.Bytes != 100 {
		t.Fatal("wire observations are mutable through their accessor")
	}
	if _, err := flow.ReportFlowLog(context.Background(), r); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatal("changed retry accepted")
	}
	check(t, s.SetFlowConsent(result.Node.ID, now.Add(-time.Hour), now.Add(-time.Minute)))
	if _, err := flow.ReportFlowLog(context.Background(), r); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatal("expired consent accepted")
	}
	check(t, s.RevokeFlowConsent(result.Node.ID))
	policyRequest := connect.NewRequest(&rpc.GetFlowLogPolicyRequest{NodeId: result.Node.ID})
	policyRequest.Header().Set("Authorization", "Bearer "+a.NodeCredential)
	policy, err := flow.GetFlowLogPolicy(context.Background(), policyRequest)
	check(t, err)
	if policy.Msg.ConsentVersion != 0 {
		t.Fatal("revoked consent still enabled collection")
	}
}
