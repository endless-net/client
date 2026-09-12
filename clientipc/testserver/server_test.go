package testserver

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/rpc"
	pb "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/clientipc/v0/clientipcconnect"
	"google.golang.org/protobuf/proto"
)

func start(t *testing.T, server *Server) clientipcconnect.ClientServiceClient {
	t.Helper()
	host := httptest.NewUnstartedServer(server.Handler(func(context.Context, pb.Access, string) error { return nil }))
	host.EnableHTTP2 = true
	host.StartTLS()
	t.Cleanup(host.Close)
	return clientipcconnect.NewClientServiceClient(host.Client(), host.URL, connect.WithGRPC(), connect.WithInterceptors(rpc.ClientHeaders{}))
}

func expect(t *testing.T, server *Server, step Step) {
	t.Helper()
	if err := server.Expect(step); err != nil {
		t.Fatal(err)
	}
}

// Each typed method must dispatch through the script over real gRPC framing.
// Empty messages here test dispatch only, not domain validation.
func TestEveryUnaryMethodDispatches(t *testing.T) {

	t.Run("GetRuntimeInfo", func(t *testing.T) {
		server := New()
		expect(t, server, Step{Method: "GetRuntimeInfo", Request: &pb.GetRuntimeInfoRequest{}, Responses: []proto.Message{&pb.GetRuntimeInfoResponse{}}})
		client := start(t, server)
		if _, err := client.GetRuntimeInfo(t.Context(), connect.NewRequest(&pb.GetRuntimeInfoRequest{})); err != nil {
			t.Fatal(err)
		}
		if err := server.Verify(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("GetStatus", func(t *testing.T) {
		server := New()
		expect(t, server, Step{Method: "GetStatus", Request: &pb.GetStatusRequest{}, Responses: []proto.Message{&pb.GetStatusResponse{}}})
		client := start(t, server)
		if _, err := client.GetStatus(t.Context(), connect.NewRequest(&pb.GetStatusRequest{})); err != nil {
			t.Fatal(err)
		}
		if err := server.Verify(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("GetOperation", func(t *testing.T) {
		server := New()
		expect(t, server, Step{Method: "GetOperation", Request: &pb.GetOperationRequest{}, Responses: []proto.Message{&pb.GetOperationResponse{}}})
		client := start(t, server)
		if _, err := client.GetOperation(t.Context(), connect.NewRequest(&pb.GetOperationRequest{})); err != nil {
			t.Fatal(err)
		}
		if err := server.Verify(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Enroll", func(t *testing.T) {
		server := New()
		expect(t, server, Step{Method: "Enroll", Request: &pb.EnrollRequest{}, Responses: []proto.Message{&pb.EnrollResponse{}}})
		client := start(t, server)
		if _, err := client.Enroll(t.Context(), connect.NewRequest(&pb.EnrollRequest{})); err != nil {
			t.Fatal(err)
		}
		if err := server.Verify(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Connect", func(t *testing.T) {
		server := New()
		expect(t, server, Step{Method: "Connect", Request: &pb.ConnectRequest{}, Responses: []proto.Message{&pb.ConnectResponse{}}})
		client := start(t, server)
		if _, err := client.Connect(t.Context(), connect.NewRequest(&pb.ConnectRequest{})); err != nil {
			t.Fatal(err)
		}
		if err := server.Verify(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Disconnect", func(t *testing.T) {
		server := New()
		expect(t, server, Step{Method: "Disconnect", Request: &pb.DisconnectRequest{}, Responses: []proto.Message{&pb.DisconnectResponse{}}})
		client := start(t, server)
		if _, err := client.Disconnect(t.Context(), connect.NewRequest(&pb.DisconnectRequest{})); err != nil {
			t.Fatal(err)
		}
		if err := server.Verify(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("GetServerIdentity", func(t *testing.T) {
		server := New()
		expect(t, server, Step{Method: "GetServerIdentity", Request: &pb.GetServerIdentityRequest{}, Responses: []proto.Message{&pb.GetServerIdentityResponse{}}})
		client := start(t, server)
		if _, err := client.GetServerIdentity(t.Context(), connect.NewRequest(&pb.GetServerIdentityRequest{})); err != nil {
			t.Fatal(err)
		}
		if err := server.Verify(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("TrustServerIdentity", func(t *testing.T) {
		server := New()
		expect(t, server, Step{Method: "TrustServerIdentity", Request: &pb.TrustServerIdentityRequest{}, Responses: []proto.Message{&pb.TrustServerIdentityResponse{}}})
		client := start(t, server)
		if _, err := client.TrustServerIdentity(t.Context(), connect.NewRequest(&pb.TrustServerIdentityRequest{})); err != nil {
			t.Fatal(err)
		}
		if err := server.Verify(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Logout", func(t *testing.T) {
		server := New()
		expect(t, server, Step{Method: "Logout", Request: &pb.LogoutRequest{}, Responses: []proto.Message{&pb.LogoutResponse{}}})
		client := start(t, server)
		if _, err := client.Logout(t.Context(), connect.NewRequest(&pb.LogoutRequest{})); err != nil {
			t.Fatal(err)
		}
		if err := server.Verify(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("ForgetLocalEnrollment", func(t *testing.T) {
		server := New()
		expect(t, server, Step{Method: "ForgetLocalEnrollment", Request: &pb.ForgetLocalEnrollmentRequest{}, Responses: []proto.Message{&pb.ForgetLocalEnrollmentResponse{}}})
		client := start(t, server)
		if _, err := client.ForgetLocalEnrollment(t.Context(), connect.NewRequest(&pb.ForgetLocalEnrollmentRequest{})); err != nil {
			t.Fatal(err)
		}
		if err := server.Verify(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("ListNetworks", func(t *testing.T) {
		server := New()
		expect(t, server, Step{Method: "ListNetworks", Request: &pb.ListNetworksRequest{}, Responses: []proto.Message{&pb.ListNetworksResponse{}}})
		client := start(t, server)
		if _, err := client.ListNetworks(t.Context(), connect.NewRequest(&pb.ListNetworksRequest{})); err != nil {
			t.Fatal(err)
		}
		if err := server.Verify(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("SelectNetwork", func(t *testing.T) {
		server := New()
		expect(t, server, Step{Method: "SelectNetwork", Request: &pb.SelectNetworkRequest{}, Responses: []proto.Message{&pb.SelectNetworkResponse{}}})
		client := start(t, server)
		if _, err := client.SelectNetwork(t.Context(), connect.NewRequest(&pb.SelectNetworkRequest{})); err != nil {
			t.Fatal(err)
		}
		if err := server.Verify(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("ListPeers", func(t *testing.T) {
		server := New()
		expect(t, server, Step{Method: "ListPeers", Request: &pb.ListPeersRequest{}, Responses: []proto.Message{&pb.ListPeersResponse{}}})
		client := start(t, server)
		if _, err := client.ListPeers(t.Context(), connect.NewRequest(&pb.ListPeersRequest{})); err != nil {
			t.Fatal(err)
		}
		if err := server.Verify(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("GetDiagnostics", func(t *testing.T) {
		server := New()
		expect(t, server, Step{Method: "GetDiagnostics", Request: &pb.GetDiagnosticsRequest{}, Responses: []proto.Message{&pb.GetDiagnosticsResponse{}}})
		client := start(t, server)
		if _, err := client.GetDiagnostics(t.Context(), connect.NewRequest(&pb.GetDiagnosticsRequest{})); err != nil {
			t.Fatal(err)
		}
		if err := server.Verify(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("CreateDiagnosticsBundle", func(t *testing.T) {
		server := New()
		expect(t, server, Step{Method: "CreateDiagnosticsBundle", Request: &pb.CreateDiagnosticsBundleRequest{}, Responses: []proto.Message{&pb.CreateDiagnosticsBundleResponse{}}})
		client := start(t, server)
		if _, err := client.CreateDiagnosticsBundle(t.Context(), connect.NewRequest(&pb.CreateDiagnosticsBundleRequest{})); err != nil {
			t.Fatal(err)
		}
		if err := server.Verify(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("ReadDiagnosticsBundle", func(t *testing.T) {
		server := New()
		expect(t, server, Step{Method: "ReadDiagnosticsBundle", Request: &pb.ReadDiagnosticsBundleRequest{}, Responses: []proto.Message{&pb.ReadDiagnosticsBundleResponse{}}})
		client := start(t, server)
		if _, err := client.ReadDiagnosticsBundle(t.Context(), connect.NewRequest(&pb.ReadDiagnosticsBundleRequest{})); err != nil {
			t.Fatal(err)
		}
		if err := server.Verify(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("ListRecentLogs", func(t *testing.T) {
		server := New()
		expect(t, server, Step{Method: "ListRecentLogs", Request: &pb.ListRecentLogsRequest{}, Responses: []proto.Message{&pb.ListRecentLogsResponse{}}})
		client := start(t, server)
		if _, err := client.ListRecentLogs(t.Context(), connect.NewRequest(&pb.ListRecentLogsRequest{})); err != nil {
			t.Fatal(err)
		}
		if err := server.Verify(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("ListProfiles", func(t *testing.T) {
		server := New()
		expect(t, server, Step{Method: "ListProfiles", Request: &pb.ListProfilesRequest{}, Responses: []proto.Message{&pb.ListProfilesResponse{}}})
		client := start(t, server)
		if _, err := client.ListProfiles(t.Context(), connect.NewRequest(&pb.ListProfilesRequest{})); err != nil {
			t.Fatal(err)
		}
		if err := server.Verify(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("CreateProfile", func(t *testing.T) {
		server := New()
		expect(t, server, Step{Method: "CreateProfile", Request: &pb.CreateProfileRequest{}, Responses: []proto.Message{&pb.CreateProfileResponse{}}})
		client := start(t, server)
		if _, err := client.CreateProfile(t.Context(), connect.NewRequest(&pb.CreateProfileRequest{})); err != nil {
			t.Fatal(err)
		}
		if err := server.Verify(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("SelectProfile", func(t *testing.T) {
		server := New()
		expect(t, server, Step{Method: "SelectProfile", Request: &pb.SelectProfileRequest{}, Responses: []proto.Message{&pb.SelectProfileResponse{}}})
		client := start(t, server)
		if _, err := client.SelectProfile(t.Context(), connect.NewRequest(&pb.SelectProfileRequest{})); err != nil {
			t.Fatal(err)
		}
		if err := server.Verify(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("RenameProfile", func(t *testing.T) {
		server := New()
		expect(t, server, Step{Method: "RenameProfile", Request: &pb.RenameProfileRequest{}, Responses: []proto.Message{&pb.RenameProfileResponse{}}})
		client := start(t, server)
		if _, err := client.RenameProfile(t.Context(), connect.NewRequest(&pb.RenameProfileRequest{})); err != nil {
			t.Fatal(err)
		}
		if err := server.Verify(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("RemoveProfile", func(t *testing.T) {
		server := New()
		expect(t, server, Step{Method: "RemoveProfile", Request: &pb.RemoveProfileRequest{}, Responses: []proto.Message{&pb.RemoveProfileResponse{}}})
		client := start(t, server)
		if _, err := client.RemoveProfile(t.Context(), connect.NewRequest(&pb.RemoveProfileRequest{})); err != nil {
			t.Fatal(err)
		}
		if err := server.Verify(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("GetSession", func(t *testing.T) {
		server := New()
		expect(t, server, Step{Method: "GetSession", Request: &pb.GetSessionRequest{}, Responses: []proto.Message{&pb.GetSessionResponse{}}})
		client := start(t, server)
		if _, err := client.GetSession(t.Context(), connect.NewRequest(&pb.GetSessionRequest{})); err != nil {
			t.Fatal(err)
		}
		if err := server.Verify(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("RenewSession", func(t *testing.T) {
		server := New()
		expect(t, server, Step{Method: "RenewSession", Request: &pb.RenewSessionRequest{}, Responses: []proto.Message{&pb.RenewSessionResponse{}}})
		client := start(t, server)
		if _, err := client.RenewSession(t.Context(), connect.NewRequest(&pb.RenewSessionRequest{})); err != nil {
			t.Fatal(err)
		}
		if err := server.Verify(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("ListExitNodes", func(t *testing.T) {
		server := New()
		expect(t, server, Step{Method: "ListExitNodes", Request: &pb.ListExitNodesRequest{}, Responses: []proto.Message{&pb.ListExitNodesResponse{}}})
		client := start(t, server)
		if _, err := client.ListExitNodes(t.Context(), connect.NewRequest(&pb.ListExitNodesRequest{})); err != nil {
			t.Fatal(err)
		}
		if err := server.Verify(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("GetExitNode", func(t *testing.T) {
		server := New()
		expect(t, server, Step{Method: "GetExitNode", Request: &pb.GetExitNodeRequest{}, Responses: []proto.Message{&pb.GetExitNodeResponse{}}})
		client := start(t, server)
		if _, err := client.GetExitNode(t.Context(), connect.NewRequest(&pb.GetExitNodeRequest{})); err != nil {
			t.Fatal(err)
		}
		if err := server.Verify(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("SelectExitNode", func(t *testing.T) {
		server := New()
		expect(t, server, Step{Method: "SelectExitNode", Request: &pb.SelectExitNodeRequest{}, Responses: []proto.Message{&pb.SelectExitNodeResponse{}}})
		client := start(t, server)
		if _, err := client.SelectExitNode(t.Context(), connect.NewRequest(&pb.SelectExitNodeRequest{})); err != nil {
			t.Fatal(err)
		}
		if err := server.Verify(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("ClearExitNode", func(t *testing.T) {
		server := New()
		expect(t, server, Step{Method: "ClearExitNode", Request: &pb.ClearExitNodeRequest{}, Responses: []proto.Message{&pb.ClearExitNodeResponse{}}})
		client := start(t, server)
		if _, err := client.ClearExitNode(t.Context(), connect.NewRequest(&pb.ClearExitNodeRequest{})); err != nil {
			t.Fatal(err)
		}
		if err := server.Verify(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("GetPreferences", func(t *testing.T) {
		server := New()
		expect(t, server, Step{Method: "GetPreferences", Request: &pb.GetPreferencesRequest{}, Responses: []proto.Message{&pb.GetPreferencesResponse{}}})
		client := start(t, server)
		if _, err := client.GetPreferences(t.Context(), connect.NewRequest(&pb.GetPreferencesRequest{})); err != nil {
			t.Fatal(err)
		}
		if err := server.Verify(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("SetPreferences", func(t *testing.T) {
		server := New()
		expect(t, server, Step{Method: "SetPreferences", Request: &pb.SetPreferencesRequest{}, Responses: []proto.Message{&pb.SetPreferencesResponse{}}})
		client := start(t, server)
		if _, err := client.SetPreferences(t.Context(), connect.NewRequest(&pb.SetPreferencesRequest{})); err != nil {
			t.Fatal(err)
		}
		if err := server.Verify(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("ResetPreferences", func(t *testing.T) {
		server := New()
		expect(t, server, Step{Method: "ResetPreferences", Request: &pb.ResetPreferencesRequest{}, Responses: []proto.Message{&pb.ResetPreferencesResponse{}}})
		client := start(t, server)
		if _, err := client.ResetPreferences(t.Context(), connect.NewRequest(&pb.ResetPreferencesRequest{})); err != nil {
			t.Fatal(err)
		}
		if err := server.Verify(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("ListManagedSettings", func(t *testing.T) {
		server := New()
		expect(t, server, Step{Method: "ListManagedSettings", Request: &pb.ListManagedSettingsRequest{}, Responses: []proto.Message{&pb.ListManagedSettingsResponse{}}})
		client := start(t, server)
		if _, err := client.ListManagedSettings(t.Context(), connect.NewRequest(&pb.ListManagedSettingsRequest{})); err != nil {
			t.Fatal(err)
		}
		if err := server.Verify(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("ListResources", func(t *testing.T) {
		server := New()
		expect(t, server, Step{Method: "ListResources", Request: &pb.ListResourcesRequest{}, Responses: []proto.Message{&pb.ListResourcesResponse{}}})
		client := start(t, server)
		if _, err := client.ListResources(t.Context(), connect.NewRequest(&pb.ListResourcesRequest{})); err != nil {
			t.Fatal(err)
		}
		if err := server.Verify(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("SetResourceEnabled", func(t *testing.T) {
		server := New()
		expect(t, server, Step{Method: "SetResourceEnabled", Request: &pb.SetResourceEnabledRequest{}, Responses: []proto.Message{&pb.SetResourceEnabledResponse{}}})
		client := start(t, server)
		if _, err := client.SetResourceEnabled(t.Context(), connect.NewRequest(&pb.SetResourceEnabledRequest{})); err != nil {
			t.Fatal(err)
		}
		if err := server.Verify(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("NotifyLifecycle", func(t *testing.T) {
		server := New()
		expect(t, server, Step{Method: "NotifyLifecycle", Request: &pb.NotifyLifecycleRequest{}, Responses: []proto.Message{&pb.NotifyLifecycleResponse{}}})
		client := start(t, server)
		if _, err := client.NotifyLifecycle(t.Context(), connect.NewRequest(&pb.NotifyLifecycleRequest{})); err != nil {
			t.Fatal(err)
		}
		if err := server.Verify(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("GetUpdateInfo", func(t *testing.T) {
		server := New()
		expect(t, server, Step{Method: "GetUpdateInfo", Request: &pb.GetUpdateInfoRequest{}, Responses: []proto.Message{&pb.GetUpdateInfoResponse{}}})
		client := start(t, server)
		if _, err := client.GetUpdateInfo(t.Context(), connect.NewRequest(&pb.GetUpdateInfoRequest{})); err != nil {
			t.Fatal(err)
		}
		if err := server.Verify(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("GetSupportInfo", func(t *testing.T) {
		server := New()
		expect(t, server, Step{Method: "GetSupportInfo", Request: &pb.GetSupportInfoRequest{}, Responses: []proto.Message{&pb.GetSupportInfoResponse{}}})
		client := start(t, server)
		if _, err := client.GetSupportInfo(t.Context(), connect.NewRequest(&pb.GetSupportInfoRequest{})); err != nil {
			t.Fatal(err)
		}
		if err := server.Verify(); err != nil {
			t.Fatal(err)
		}
	})

}

func TestSnapshotThenStreamFailure(t *testing.T) {
	server := New()
	expect(t, server, Step{
		Method: "WatchEvents", Request: &pb.WatchEventsRequest{},
		Responses: []proto.Message{
			&pb.WatchEventsResponse{Sequence: 1, Event: &pb.WatchEventsResponse_Snapshot{Snapshot: &pb.SnapshotEvent{Status: &pb.Status{ConnectionPhase: pb.ConnectionPhase_CONNECTION_PHASE_CONNECTING}}}},
			&pb.WatchEventsResponse{Sequence: 2, Event: &pb.WatchEventsResponse_OperationChanged{OperationChanged: &pb.Operation{Kind: pb.OperationKind_OPERATION_KIND_CONNECT, State: pb.OperationState_OPERATION_STATE_RUNNING}}},
		},
		Err: rpc.Error(connect.CodeResourceExhausted, pb.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED),
	})
	stream, err := start(t, server).WatchEvents(t.Context(), connect.NewRequest(&pb.WatchEventsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	for _, sequence := range []uint64{1, 2} {
		if !stream.Receive() || stream.Msg().GetSequence() != sequence {
			t.Fatalf("missing sequence %d: %v", sequence, stream.Err())
		}
	}
	if stream.Receive() || rpc.FailureFromError(stream.Err()).GetCode() != pb.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED {
		t.Fatal("stream did not terminate with typed overflow")
	}
	if err := server.Verify(); err != nil {
		t.Fatal(err)
	}
}

func TestScriptIsClonedAndUnexpectedCallsFail(t *testing.T) {
	server := New()
	request := &pb.GetOperationRequest{Lookup: &pb.GetOperationRequest_RequestId{RequestId: "original"}}
	response := &pb.GetOperationResponse{Operation: &pb.Operation{Id: "original"}}
	expect(t, server, Step{Method: "GetOperation", Request: request, Responses: []proto.Message{response}})
	request.Lookup = &pb.GetOperationRequest_RequestId{RequestId: "mutated"}
	response.Operation.Id = "mutated"
	client := start(t, server)
	got, err := client.GetOperation(t.Context(), connect.NewRequest(&pb.GetOperationRequest{Lookup: &pb.GetOperationRequest_RequestId{RequestId: "original"}}))
	if err != nil || got.Msg.GetOperation().GetId() != "original" {
		t.Fatal("script aliased caller-owned messages")
	}
	if err := server.Verify(); err != nil {
		t.Fatal(err)
	}
	_, err = client.GetStatus(t.Context(), connect.NewRequest(&pb.GetStatusRequest{}))
	if rpc.FailureFromError(err).GetCode() != pb.ErrorCode_ERROR_CODE_UNSUPPORTED || server.Verify() == nil {
		t.Fatal("unexpected call was accepted")
	}
}

func TestMismatchNeverDisclosesPayload(t *testing.T) {
	server := New()
	expect(t, server, Step{Method: "Enroll", Request: &pb.EnrollRequest{}, Responses: []proto.Message{&pb.EnrollResponse{}}})
	_, err := start(t, server).Enroll(t.Context(), connect.NewRequest(&pb.EnrollRequest{Authentication: &pb.EnrollRequest_EnrollmentToken{EnrollmentToken: "synthetic-sensitive-marker"}}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatal("expected mismatch")
	}
	for _, failure := range []error{err, server.Verify()} {
		if failure == nil || strings.Contains(failure.Error(), "synthetic-sensitive-marker") {
			t.Fatal("mismatch disclosed payload or disappeared")
		}
	}
}

func TestDelayHonorsCancellation(t *testing.T) {
	server := New()
	release := make(chan struct{})
	expect(t, server, Step{Method: "GetStatus", Request: &pb.GetStatusRequest{}, Responses: []proto.Message{&pb.GetStatusResponse{}}, Release: release})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := server.GetStatus(ctx, connect.NewRequest(&pb.GetStatusRequest{})); err != context.Canceled {
		t.Fatalf("cancelled wait: %v", err)
	}
	if err := server.Verify(); err != nil {
		t.Fatal(err)
	}
}

func TestRejectInvalidScripts(t *testing.T) {
	for _, step := range []Step{
		{Method: "Unknown", Request: &pb.GetStatusRequest{}},
		{Method: "GetStatus", Request: &pb.ConnectRequest{}},
		{Method: "GetStatus", Request: &pb.GetStatusRequest{}},
		{Method: "GetStatus", Request: &pb.GetStatusRequest{}, Responses: []proto.Message{&pb.ConnectResponse{}}},
		{Method: "GetStatus", Request: &pb.GetStatusRequest{}, Responses: []proto.Message{&pb.GetStatusResponse{}}, Err: context.Canceled},
	} {
		if New().Expect(step) == nil {
			t.Fatalf("accepted invalid script for %s", step.Method)
		}
	}
}

func TestMissingAuthorizationDoesNotConsumeScript(t *testing.T) {
	server := New()
	expect(t, server, Step{Method: "GetRuntimeInfo", Request: &pb.GetRuntimeInfoRequest{}, Responses: []proto.Message{&pb.GetRuntimeInfoResponse{}}})
	host := httptest.NewUnstartedServer(server.Handler(nil))
	host.EnableHTTP2 = true
	host.StartTLS()
	defer host.Close()
	client := clientipcconnect.NewClientServiceClient(host.Client(), host.URL, connect.WithGRPC())
	_, err := client.GetRuntimeInfo(t.Context(), connect.NewRequest(&pb.GetRuntimeInfoRequest{}))
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatal("missing authorization accepted")
	}
	if server.Verify() == nil {
		t.Fatal("unauthorized request consumed script")
	}
}
