package testcontrol_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
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
	if _, err := flow.ReportFlowLog(context.Background(), r); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatal("changed retry accepted")
	}
	check(t, s.SetFlowConsent(result.Node.ID, now.Add(-time.Hour), now.Add(-time.Minute)))
	if _, err := flow.ReportFlowLog(context.Background(), r); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatal("expired consent accepted")
	}
}
