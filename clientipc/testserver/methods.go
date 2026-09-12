package testserver

import (
	"context"

	"connectrpc.com/connect"
	pb "github.com/endless-net/client/clientipc/v0"
)

func (s *Server) GetRuntimeInfo(ctx context.Context, request *connect.Request[pb.GetRuntimeInfoRequest]) (*connect.Response[pb.GetRuntimeInfoResponse], error) {
	response, err := s.invoke(ctx, "GetRuntimeInfo", request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response.(*pb.GetRuntimeInfoResponse)), nil
}

func (s *Server) GetStatus(ctx context.Context, request *connect.Request[pb.GetStatusRequest]) (*connect.Response[pb.GetStatusResponse], error) {
	response, err := s.invoke(ctx, "GetStatus", request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response.(*pb.GetStatusResponse)), nil
}

func (s *Server) WatchEvents(ctx context.Context, request *connect.Request[pb.WatchEventsRequest], stream *connect.ServerStream[pb.WatchEventsResponse]) error {
	step, err := s.take("WatchEvents", request.Msg)
	if err != nil {
		return err
	}
	defer s.finish()
	if err := wait(ctx, step.Release); err != nil {
		return err
	}
	for _, response := range step.Responses {
		if err := stream.Send(response.(*pb.WatchEventsResponse)); err != nil {
			return err
		}
	}
	if step.HoldOpen {
		<-ctx.Done()
		return ctx.Err()
	}
	return step.Err
}

func (s *Server) GetOperation(ctx context.Context, request *connect.Request[pb.GetOperationRequest]) (*connect.Response[pb.GetOperationResponse], error) {
	response, err := s.invoke(ctx, "GetOperation", request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response.(*pb.GetOperationResponse)), nil
}

func (s *Server) Enroll(ctx context.Context, request *connect.Request[pb.EnrollRequest]) (*connect.Response[pb.EnrollResponse], error) {
	response, err := s.invoke(ctx, "Enroll", request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response.(*pb.EnrollResponse)), nil
}

func (s *Server) Connect(ctx context.Context, request *connect.Request[pb.ConnectRequest]) (*connect.Response[pb.ConnectResponse], error) {
	response, err := s.invoke(ctx, "Connect", request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response.(*pb.ConnectResponse)), nil
}

func (s *Server) Disconnect(ctx context.Context, request *connect.Request[pb.DisconnectRequest]) (*connect.Response[pb.DisconnectResponse], error) {
	response, err := s.invoke(ctx, "Disconnect", request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response.(*pb.DisconnectResponse)), nil
}

func (s *Server) GetServerIdentity(ctx context.Context, request *connect.Request[pb.GetServerIdentityRequest]) (*connect.Response[pb.GetServerIdentityResponse], error) {
	response, err := s.invoke(ctx, "GetServerIdentity", request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response.(*pb.GetServerIdentityResponse)), nil
}

func (s *Server) TrustServerIdentity(ctx context.Context, request *connect.Request[pb.TrustServerIdentityRequest]) (*connect.Response[pb.TrustServerIdentityResponse], error) {
	response, err := s.invoke(ctx, "TrustServerIdentity", request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response.(*pb.TrustServerIdentityResponse)), nil
}

func (s *Server) Logout(ctx context.Context, request *connect.Request[pb.LogoutRequest]) (*connect.Response[pb.LogoutResponse], error) {
	response, err := s.invoke(ctx, "Logout", request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response.(*pb.LogoutResponse)), nil
}

func (s *Server) ForgetLocalEnrollment(ctx context.Context, request *connect.Request[pb.ForgetLocalEnrollmentRequest]) (*connect.Response[pb.ForgetLocalEnrollmentResponse], error) {
	response, err := s.invoke(ctx, "ForgetLocalEnrollment", request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response.(*pb.ForgetLocalEnrollmentResponse)), nil
}

func (s *Server) ListNetworks(ctx context.Context, request *connect.Request[pb.ListNetworksRequest]) (*connect.Response[pb.ListNetworksResponse], error) {
	response, err := s.invoke(ctx, "ListNetworks", request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response.(*pb.ListNetworksResponse)), nil
}

func (s *Server) SelectNetwork(ctx context.Context, request *connect.Request[pb.SelectNetworkRequest]) (*connect.Response[pb.SelectNetworkResponse], error) {
	response, err := s.invoke(ctx, "SelectNetwork", request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response.(*pb.SelectNetworkResponse)), nil
}

func (s *Server) ListPeers(ctx context.Context, request *connect.Request[pb.ListPeersRequest]) (*connect.Response[pb.ListPeersResponse], error) {
	response, err := s.invoke(ctx, "ListPeers", request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response.(*pb.ListPeersResponse)), nil
}

func (s *Server) GetDiagnostics(ctx context.Context, request *connect.Request[pb.GetDiagnosticsRequest]) (*connect.Response[pb.GetDiagnosticsResponse], error) {
	response, err := s.invoke(ctx, "GetDiagnostics", request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response.(*pb.GetDiagnosticsResponse)), nil
}

func (s *Server) CreateDiagnosticsBundle(ctx context.Context, request *connect.Request[pb.CreateDiagnosticsBundleRequest]) (*connect.Response[pb.CreateDiagnosticsBundleResponse], error) {
	response, err := s.invoke(ctx, "CreateDiagnosticsBundle", request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response.(*pb.CreateDiagnosticsBundleResponse)), nil
}

func (s *Server) ReadDiagnosticsBundle(ctx context.Context, request *connect.Request[pb.ReadDiagnosticsBundleRequest]) (*connect.Response[pb.ReadDiagnosticsBundleResponse], error) {
	response, err := s.invoke(ctx, "ReadDiagnosticsBundle", request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response.(*pb.ReadDiagnosticsBundleResponse)), nil
}

func (s *Server) ListRecentLogs(ctx context.Context, request *connect.Request[pb.ListRecentLogsRequest]) (*connect.Response[pb.ListRecentLogsResponse], error) {
	response, err := s.invoke(ctx, "ListRecentLogs", request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response.(*pb.ListRecentLogsResponse)), nil
}

func (s *Server) ListProfiles(ctx context.Context, request *connect.Request[pb.ListProfilesRequest]) (*connect.Response[pb.ListProfilesResponse], error) {
	response, err := s.invoke(ctx, "ListProfiles", request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response.(*pb.ListProfilesResponse)), nil
}

func (s *Server) CreateProfile(ctx context.Context, request *connect.Request[pb.CreateProfileRequest]) (*connect.Response[pb.CreateProfileResponse], error) {
	response, err := s.invoke(ctx, "CreateProfile", request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response.(*pb.CreateProfileResponse)), nil
}

func (s *Server) SelectProfile(ctx context.Context, request *connect.Request[pb.SelectProfileRequest]) (*connect.Response[pb.SelectProfileResponse], error) {
	response, err := s.invoke(ctx, "SelectProfile", request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response.(*pb.SelectProfileResponse)), nil
}

func (s *Server) RenameProfile(ctx context.Context, request *connect.Request[pb.RenameProfileRequest]) (*connect.Response[pb.RenameProfileResponse], error) {
	response, err := s.invoke(ctx, "RenameProfile", request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response.(*pb.RenameProfileResponse)), nil
}

func (s *Server) RemoveProfile(ctx context.Context, request *connect.Request[pb.RemoveProfileRequest]) (*connect.Response[pb.RemoveProfileResponse], error) {
	response, err := s.invoke(ctx, "RemoveProfile", request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response.(*pb.RemoveProfileResponse)), nil
}

func (s *Server) GetSession(ctx context.Context, request *connect.Request[pb.GetSessionRequest]) (*connect.Response[pb.GetSessionResponse], error) {
	response, err := s.invoke(ctx, "GetSession", request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response.(*pb.GetSessionResponse)), nil
}

func (s *Server) RenewSession(ctx context.Context, request *connect.Request[pb.RenewSessionRequest]) (*connect.Response[pb.RenewSessionResponse], error) {
	response, err := s.invoke(ctx, "RenewSession", request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response.(*pb.RenewSessionResponse)), nil
}

func (s *Server) ListExitNodes(ctx context.Context, request *connect.Request[pb.ListExitNodesRequest]) (*connect.Response[pb.ListExitNodesResponse], error) {
	response, err := s.invoke(ctx, "ListExitNodes", request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response.(*pb.ListExitNodesResponse)), nil
}

func (s *Server) GetExitNode(ctx context.Context, request *connect.Request[pb.GetExitNodeRequest]) (*connect.Response[pb.GetExitNodeResponse], error) {
	response, err := s.invoke(ctx, "GetExitNode", request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response.(*pb.GetExitNodeResponse)), nil
}

func (s *Server) SelectExitNode(ctx context.Context, request *connect.Request[pb.SelectExitNodeRequest]) (*connect.Response[pb.SelectExitNodeResponse], error) {
	response, err := s.invoke(ctx, "SelectExitNode", request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response.(*pb.SelectExitNodeResponse)), nil
}

func (s *Server) ClearExitNode(ctx context.Context, request *connect.Request[pb.ClearExitNodeRequest]) (*connect.Response[pb.ClearExitNodeResponse], error) {
	response, err := s.invoke(ctx, "ClearExitNode", request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response.(*pb.ClearExitNodeResponse)), nil
}

func (s *Server) GetPreferences(ctx context.Context, request *connect.Request[pb.GetPreferencesRequest]) (*connect.Response[pb.GetPreferencesResponse], error) {
	response, err := s.invoke(ctx, "GetPreferences", request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response.(*pb.GetPreferencesResponse)), nil
}

func (s *Server) SetPreferences(ctx context.Context, request *connect.Request[pb.SetPreferencesRequest]) (*connect.Response[pb.SetPreferencesResponse], error) {
	response, err := s.invoke(ctx, "SetPreferences", request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response.(*pb.SetPreferencesResponse)), nil
}

func (s *Server) ResetPreferences(ctx context.Context, request *connect.Request[pb.ResetPreferencesRequest]) (*connect.Response[pb.ResetPreferencesResponse], error) {
	response, err := s.invoke(ctx, "ResetPreferences", request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response.(*pb.ResetPreferencesResponse)), nil
}

func (s *Server) ListManagedSettings(ctx context.Context, request *connect.Request[pb.ListManagedSettingsRequest]) (*connect.Response[pb.ListManagedSettingsResponse], error) {
	response, err := s.invoke(ctx, "ListManagedSettings", request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response.(*pb.ListManagedSettingsResponse)), nil
}

func (s *Server) ListResources(ctx context.Context, request *connect.Request[pb.ListResourcesRequest]) (*connect.Response[pb.ListResourcesResponse], error) {
	response, err := s.invoke(ctx, "ListResources", request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response.(*pb.ListResourcesResponse)), nil
}

func (s *Server) SetResourceEnabled(ctx context.Context, request *connect.Request[pb.SetResourceEnabledRequest]) (*connect.Response[pb.SetResourceEnabledResponse], error) {
	response, err := s.invoke(ctx, "SetResourceEnabled", request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response.(*pb.SetResourceEnabledResponse)), nil
}

func (s *Server) NotifyLifecycle(ctx context.Context, request *connect.Request[pb.NotifyLifecycleRequest]) (*connect.Response[pb.NotifyLifecycleResponse], error) {
	response, err := s.invoke(ctx, "NotifyLifecycle", request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response.(*pb.NotifyLifecycleResponse)), nil
}

func (s *Server) GetUpdateInfo(ctx context.Context, request *connect.Request[pb.GetUpdateInfoRequest]) (*connect.Response[pb.GetUpdateInfoResponse], error) {
	response, err := s.invoke(ctx, "GetUpdateInfo", request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response.(*pb.GetUpdateInfoResponse)), nil
}

func (s *Server) GetSupportInfo(ctx context.Context, request *connect.Request[pb.GetSupportInfoRequest]) (*connect.Response[pb.GetSupportInfoResponse], error) {
	response, err := s.invoke(ctx, "GetSupportInfo", request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response.(*pb.GetSupportInfoResponse)), nil
}
