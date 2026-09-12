// This is a generated file - do not edit.
//
// Generated from client/v0/service.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, prefer_relative_imports

import 'dart:async' as $async;
import 'dart:core' as $core;

import 'package:grpc/service_api.dart' as $grpc;
import 'package:protobuf/protobuf.dart' as $pb;

import 'service.pb.dart' as $0;

export 'service.pb.dart';

@$pb.GrpcServiceName('client.v0.ClientService')
class ClientServiceClient extends $grpc.Client {
  /// The hostname for this service.
  static const $core.String defaultHost = '';

  /// OAuth scopes needed for the client.
  static const $core.List<$core.String> oauthScopes = [
    '',
  ];

  ClientServiceClient(super.channel, {super.options, super.interceptors});

  /// UF-04, UF-20, UF-22, UF-23: installed identity and caller-filtered capabilities.
  $grpc.ResponseFuture<$0.GetRuntimeInfoResponse> getRuntimeInfo(
    $0.GetRuntimeInfoRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$getRuntimeInfo, request, options: options);
  }

  /// UF-04, UF-09: authoritative active-context snapshot.
  $grpc.ResponseFuture<$0.GetStatusResponse> getStatus(
    $0.GetStatusRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$getStatus, request, options: options);
  }

  /// UF-04, UF-09, UF-15: snapshot first, then typed changes; no replay cursor.
  $grpc.ResponseStream<$0.WatchEventsResponse> watchEvents(
    $0.WatchEventsRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createStreamingCall(
        _$watchEvents, $async.Stream.fromIterable([request]),
        options: options);
  }

  /// Read a caller-owned operation after timeout or reconnection.
  $grpc.ResponseFuture<$0.GetOperationResponse> getOperation(
    $0.GetOperationRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$getOperation, request, options: options);
  }

  /// UF-03: initial unowned installation uses the ownership-claim exception.
  $grpc.ResponseFuture<$0.EnrollResponse> enroll(
    $0.EnrollRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$enroll, request, options: options);
  }

  /// UF-05: persist connected intent, then reconcile runtime.
  $grpc.ResponseFuture<$0.ConnectResponse> connect(
    $0.ConnectRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$connect, request, options: options);
  }

  /// UF-05: persist disconnected intent; retain registration.
  $grpc.ResponseFuture<$0.DisconnectResponse> disconnect(
    $0.DisconnectRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$disconnect, request, options: options);
  }

  /// UF-10: review announced and trusted identity for a profile.
  $grpc.ResponseFuture<$0.GetServerIdentityResponse> getServerIdentity(
    $0.GetServerIdentityRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$getServerIdentity, request, options: options);
  }

  /// UF-10: fixed-purpose privileged operation, matching the current announcement.
  $grpc.ResponseFuture<$0.TrustServerIdentityResponse> trustServerIdentity(
    $0.TrustServerIdentityRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$trustServerIdentity, request, options: options);
  }

  /// UF-12: remote confirmation is required before deleting local registration.
  $grpc.ResponseFuture<$0.LogoutResponse> logout(
    $0.LogoutRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$logout, request, options: options);
  }

  /// UF-12: explicit local-only cleanup; never claims remote revocation.
  $grpc.ResponseFuture<$0.ForgetLocalEnrollmentResponse> forgetLocalEnrollment(
    $0.ForgetLocalEnrollmentRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$forgetLocalEnrollment, request, options: options);
  }

  /// UF-07: only already granted networks for this profile.
  $grpc.ResponseFuture<$0.ListNetworksResponse> listNetworks(
    $0.ListNetworksRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$listNetworks, request, options: options);
  }

  /// UF-07: select an enrolled network by stable ID.
  $grpc.ResponseFuture<$0.SelectNetworkResponse> selectNetwork(
    $0.SelectNetworkRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$selectNetwork, request, options: options);
  }

  /// UF-06, UF-19: filtered peer search and path inspection.
  $grpc.ResponseFuture<$0.ListPeersResponse> listPeers(
    $0.ListPeersRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$listPeers, request, options: options);
  }

  /// UF-11: bounded producer-redacted snapshot.
  $grpc.ResponseFuture<$0.GetDiagnosticsResponse> getDiagnostics(
    $0.GetDiagnosticsRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$getDiagnostics, request, options: options);
  }

  /// UF-11: local-only redacted archive, no upload or packet capture.
  $grpc.ResponseFuture<$0.CreateDiagnosticsBundleResponse>
      createDiagnosticsBundle(
    $0.CreateDiagnosticsBundleRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$createDiagnosticsBundle, request,
        options: options);
  }

  /// UF-11: caller-bound handle; bounded chunks, no filesystem path input.
  $grpc.ResponseFuture<$0.ReadDiagnosticsBundleResponse> readDiagnosticsBundle(
    $0.ReadDiagnosticsBundleRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$readDiagnosticsBundle, request, options: options);
  }

  /// UF-11: bounded redacted log entries.
  $grpc.ResponseFuture<$0.ListRecentLogsResponse> listRecentLogs(
    $0.ListRecentLogsRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$listRecentLogs, request, options: options);
  }

  /// UF-16: local owner profiles, one active at most.
  $grpc.ResponseFuture<$0.ListProfilesResponse> listProfiles(
    $0.ListProfilesRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$listProfiles, request, options: options);
  }

  /// UF-16: create empty context; enrollment is a separate explicit action.
  $grpc.ResponseFuture<$0.CreateProfileResponse> createProfile(
    $0.CreateProfileRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$createProfile, request, options: options);
  }

  /// UF-16: atomic context switch, report actual connection interruption.
  $grpc.ResponseFuture<$0.SelectProfileResponse> selectProfile(
    $0.SelectProfileRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$selectProfile, request, options: options);
  }

  /// UF-16: change display name only; identity and origin are immutable.
  $grpc.ResponseFuture<$0.RenameProfileResponse> renameProfile(
    $0.RenameProfileRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$renameProfile, request, options: options);
  }

  /// UF-16: only inactive profiles with no remaining registration/session.
  $grpc.ResponseFuture<$0.RemoveProfileResponse> removeProfile(
    $0.RemoveProfileRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$removeProfile, request, options: options);
  }

  /// UF-17: expiry, warning deadline and renewal availability.
  $grpc.ResponseFuture<$0.GetSessionResponse> getSession(
    $0.GetSessionRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$getSession, request, options: options);
  }

  /// UF-17: no caller-supplied callback URL or browser credential.
  $grpc.ResponseFuture<$0.RenewSessionResponse> renewSession(
    $0.RenewSessionRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$renewSession, request, options: options);
  }

  /// UF-08: list authorized consumer-side egress choices.
  $grpc.ResponseFuture<$0.ListExitNodesResponse> listExitNodes(
    $0.ListExitNodesRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$listExitNodes, request, options: options);
  }

  /// UF-08: requested/effective selection, LAN policy and apply outcome.
  $grpc.ResponseFuture<$0.GetExitNodeResponse> getExitNode(
    $0.GetExitNodeRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$getExitNode, request, options: options);
  }

  /// UF-08: only an authorized catalog ID; never arbitrary routes.
  $grpc.ResponseFuture<$0.SelectExitNodeResponse> selectExitNode(
    $0.SelectExitNodeRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$selectExitNode, request, options: options);
  }

  /// UF-08: explicit removal of consumer-side egress selection.
  $grpc.ResponseFuture<$0.ClearExitNodeResponse> clearExitNode(
    $0.ClearExitNodeRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$clearExitNode, request, options: options);
  }

  /// UF-18, UF-21: effective values, user overrides and locks.
  $grpc.ResponseFuture<$0.GetPreferencesResponse> getPreferences(
    $0.GetPreferencesRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$getPreferences, request, options: options);
  }

  /// UF-18, UF-21: nonempty typed patch, all-or-nothing validation.
  $grpc.ResponseFuture<$0.SetPreferencesResponse> setPreferences(
    $0.SetPreferencesRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$setPreferences, request, options: options);
  }

  /// UF-18, UF-21: remove named user overrides; managed values remain.
  $grpc.ResponseFuture<$0.ResetPreferencesResponse> resetPreferences(
    $0.ResetPreferencesRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$resetPreferences, request, options: options);
  }

  /// UF-20: read-only effective runtime policy and next-action owner.
  $grpc.ResponseFuture<$0.ListManagedSettingsResponse> listManagedSettings(
    $0.ListManagedSettingsRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$listManagedSettings, request, options: options);
  }

  /// UF-19: granted catalog, availability, overlaps and local selection.
  $grpc.ResponseFuture<$0.ListResourcesResponse> listResources(
    $0.ListResourcesRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$listResources, request, options: options);
  }

  /// UF-19: local subset selection cannot expand backend authorization.
  $grpc.ResponseFuture<$0.SetResourceEnabledResponse> setResourceEnabled(
    $0.SetResourceEnabledRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$setResourceEnabled, request, options: options);
  }

  /// UF-21: explicit graceful UI quit only; OS lifecycle comes from runtime adapters.
  $grpc.ResponseFuture<$0.NotifyLifecycleResponse> notifyLifecycle(
    $0.NotifyLifecycleRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$notifyLifecycle, request, options: options);
  }

  /// UF-22: verified distribution projection and installed-pair compatibility.
  $grpc.ResponseFuture<$0.GetUpdateInfoResponse> getUpdateInfo(
    $0.GetUpdateInfoRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$getUpdateInfo, request, options: options);
  }

  /// UF-23: validated product links and offline-help key.
  $grpc.ResponseFuture<$0.GetSupportInfoResponse> getSupportInfo(
    $0.GetSupportInfoRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$getSupportInfo, request, options: options);
  }

  // method descriptors

  static final _$getRuntimeInfo =
      $grpc.ClientMethod<$0.GetRuntimeInfoRequest, $0.GetRuntimeInfoResponse>(
          '/client.v0.ClientService/GetRuntimeInfo',
          ($0.GetRuntimeInfoRequest value) => value.writeToBuffer(),
          $0.GetRuntimeInfoResponse.fromBuffer);
  static final _$getStatus =
      $grpc.ClientMethod<$0.GetStatusRequest, $0.GetStatusResponse>(
          '/client.v0.ClientService/GetStatus',
          ($0.GetStatusRequest value) => value.writeToBuffer(),
          $0.GetStatusResponse.fromBuffer);
  static final _$watchEvents =
      $grpc.ClientMethod<$0.WatchEventsRequest, $0.WatchEventsResponse>(
          '/client.v0.ClientService/WatchEvents',
          ($0.WatchEventsRequest value) => value.writeToBuffer(),
          $0.WatchEventsResponse.fromBuffer);
  static final _$getOperation =
      $grpc.ClientMethod<$0.GetOperationRequest, $0.GetOperationResponse>(
          '/client.v0.ClientService/GetOperation',
          ($0.GetOperationRequest value) => value.writeToBuffer(),
          $0.GetOperationResponse.fromBuffer);
  static final _$enroll =
      $grpc.ClientMethod<$0.EnrollRequest, $0.EnrollResponse>(
          '/client.v0.ClientService/Enroll',
          ($0.EnrollRequest value) => value.writeToBuffer(),
          $0.EnrollResponse.fromBuffer);
  static final _$connect =
      $grpc.ClientMethod<$0.ConnectRequest, $0.ConnectResponse>(
          '/client.v0.ClientService/Connect',
          ($0.ConnectRequest value) => value.writeToBuffer(),
          $0.ConnectResponse.fromBuffer);
  static final _$disconnect =
      $grpc.ClientMethod<$0.DisconnectRequest, $0.DisconnectResponse>(
          '/client.v0.ClientService/Disconnect',
          ($0.DisconnectRequest value) => value.writeToBuffer(),
          $0.DisconnectResponse.fromBuffer);
  static final _$getServerIdentity = $grpc.ClientMethod<
          $0.GetServerIdentityRequest, $0.GetServerIdentityResponse>(
      '/client.v0.ClientService/GetServerIdentity',
      ($0.GetServerIdentityRequest value) => value.writeToBuffer(),
      $0.GetServerIdentityResponse.fromBuffer);
  static final _$trustServerIdentity = $grpc.ClientMethod<
          $0.TrustServerIdentityRequest, $0.TrustServerIdentityResponse>(
      '/client.v0.ClientService/TrustServerIdentity',
      ($0.TrustServerIdentityRequest value) => value.writeToBuffer(),
      $0.TrustServerIdentityResponse.fromBuffer);
  static final _$logout =
      $grpc.ClientMethod<$0.LogoutRequest, $0.LogoutResponse>(
          '/client.v0.ClientService/Logout',
          ($0.LogoutRequest value) => value.writeToBuffer(),
          $0.LogoutResponse.fromBuffer);
  static final _$forgetLocalEnrollment = $grpc.ClientMethod<
          $0.ForgetLocalEnrollmentRequest, $0.ForgetLocalEnrollmentResponse>(
      '/client.v0.ClientService/ForgetLocalEnrollment',
      ($0.ForgetLocalEnrollmentRequest value) => value.writeToBuffer(),
      $0.ForgetLocalEnrollmentResponse.fromBuffer);
  static final _$listNetworks =
      $grpc.ClientMethod<$0.ListNetworksRequest, $0.ListNetworksResponse>(
          '/client.v0.ClientService/ListNetworks',
          ($0.ListNetworksRequest value) => value.writeToBuffer(),
          $0.ListNetworksResponse.fromBuffer);
  static final _$selectNetwork =
      $grpc.ClientMethod<$0.SelectNetworkRequest, $0.SelectNetworkResponse>(
          '/client.v0.ClientService/SelectNetwork',
          ($0.SelectNetworkRequest value) => value.writeToBuffer(),
          $0.SelectNetworkResponse.fromBuffer);
  static final _$listPeers =
      $grpc.ClientMethod<$0.ListPeersRequest, $0.ListPeersResponse>(
          '/client.v0.ClientService/ListPeers',
          ($0.ListPeersRequest value) => value.writeToBuffer(),
          $0.ListPeersResponse.fromBuffer);
  static final _$getDiagnostics =
      $grpc.ClientMethod<$0.GetDiagnosticsRequest, $0.GetDiagnosticsResponse>(
          '/client.v0.ClientService/GetDiagnostics',
          ($0.GetDiagnosticsRequest value) => value.writeToBuffer(),
          $0.GetDiagnosticsResponse.fromBuffer);
  static final _$createDiagnosticsBundle = $grpc.ClientMethod<
          $0.CreateDiagnosticsBundleRequest,
          $0.CreateDiagnosticsBundleResponse>(
      '/client.v0.ClientService/CreateDiagnosticsBundle',
      ($0.CreateDiagnosticsBundleRequest value) => value.writeToBuffer(),
      $0.CreateDiagnosticsBundleResponse.fromBuffer);
  static final _$readDiagnosticsBundle = $grpc.ClientMethod<
          $0.ReadDiagnosticsBundleRequest, $0.ReadDiagnosticsBundleResponse>(
      '/client.v0.ClientService/ReadDiagnosticsBundle',
      ($0.ReadDiagnosticsBundleRequest value) => value.writeToBuffer(),
      $0.ReadDiagnosticsBundleResponse.fromBuffer);
  static final _$listRecentLogs =
      $grpc.ClientMethod<$0.ListRecentLogsRequest, $0.ListRecentLogsResponse>(
          '/client.v0.ClientService/ListRecentLogs',
          ($0.ListRecentLogsRequest value) => value.writeToBuffer(),
          $0.ListRecentLogsResponse.fromBuffer);
  static final _$listProfiles =
      $grpc.ClientMethod<$0.ListProfilesRequest, $0.ListProfilesResponse>(
          '/client.v0.ClientService/ListProfiles',
          ($0.ListProfilesRequest value) => value.writeToBuffer(),
          $0.ListProfilesResponse.fromBuffer);
  static final _$createProfile =
      $grpc.ClientMethod<$0.CreateProfileRequest, $0.CreateProfileResponse>(
          '/client.v0.ClientService/CreateProfile',
          ($0.CreateProfileRequest value) => value.writeToBuffer(),
          $0.CreateProfileResponse.fromBuffer);
  static final _$selectProfile =
      $grpc.ClientMethod<$0.SelectProfileRequest, $0.SelectProfileResponse>(
          '/client.v0.ClientService/SelectProfile',
          ($0.SelectProfileRequest value) => value.writeToBuffer(),
          $0.SelectProfileResponse.fromBuffer);
  static final _$renameProfile =
      $grpc.ClientMethod<$0.RenameProfileRequest, $0.RenameProfileResponse>(
          '/client.v0.ClientService/RenameProfile',
          ($0.RenameProfileRequest value) => value.writeToBuffer(),
          $0.RenameProfileResponse.fromBuffer);
  static final _$removeProfile =
      $grpc.ClientMethod<$0.RemoveProfileRequest, $0.RemoveProfileResponse>(
          '/client.v0.ClientService/RemoveProfile',
          ($0.RemoveProfileRequest value) => value.writeToBuffer(),
          $0.RemoveProfileResponse.fromBuffer);
  static final _$getSession =
      $grpc.ClientMethod<$0.GetSessionRequest, $0.GetSessionResponse>(
          '/client.v0.ClientService/GetSession',
          ($0.GetSessionRequest value) => value.writeToBuffer(),
          $0.GetSessionResponse.fromBuffer);
  static final _$renewSession =
      $grpc.ClientMethod<$0.RenewSessionRequest, $0.RenewSessionResponse>(
          '/client.v0.ClientService/RenewSession',
          ($0.RenewSessionRequest value) => value.writeToBuffer(),
          $0.RenewSessionResponse.fromBuffer);
  static final _$listExitNodes =
      $grpc.ClientMethod<$0.ListExitNodesRequest, $0.ListExitNodesResponse>(
          '/client.v0.ClientService/ListExitNodes',
          ($0.ListExitNodesRequest value) => value.writeToBuffer(),
          $0.ListExitNodesResponse.fromBuffer);
  static final _$getExitNode =
      $grpc.ClientMethod<$0.GetExitNodeRequest, $0.GetExitNodeResponse>(
          '/client.v0.ClientService/GetExitNode',
          ($0.GetExitNodeRequest value) => value.writeToBuffer(),
          $0.GetExitNodeResponse.fromBuffer);
  static final _$selectExitNode =
      $grpc.ClientMethod<$0.SelectExitNodeRequest, $0.SelectExitNodeResponse>(
          '/client.v0.ClientService/SelectExitNode',
          ($0.SelectExitNodeRequest value) => value.writeToBuffer(),
          $0.SelectExitNodeResponse.fromBuffer);
  static final _$clearExitNode =
      $grpc.ClientMethod<$0.ClearExitNodeRequest, $0.ClearExitNodeResponse>(
          '/client.v0.ClientService/ClearExitNode',
          ($0.ClearExitNodeRequest value) => value.writeToBuffer(),
          $0.ClearExitNodeResponse.fromBuffer);
  static final _$getPreferences =
      $grpc.ClientMethod<$0.GetPreferencesRequest, $0.GetPreferencesResponse>(
          '/client.v0.ClientService/GetPreferences',
          ($0.GetPreferencesRequest value) => value.writeToBuffer(),
          $0.GetPreferencesResponse.fromBuffer);
  static final _$setPreferences =
      $grpc.ClientMethod<$0.SetPreferencesRequest, $0.SetPreferencesResponse>(
          '/client.v0.ClientService/SetPreferences',
          ($0.SetPreferencesRequest value) => value.writeToBuffer(),
          $0.SetPreferencesResponse.fromBuffer);
  static final _$resetPreferences = $grpc.ClientMethod<
          $0.ResetPreferencesRequest, $0.ResetPreferencesResponse>(
      '/client.v0.ClientService/ResetPreferences',
      ($0.ResetPreferencesRequest value) => value.writeToBuffer(),
      $0.ResetPreferencesResponse.fromBuffer);
  static final _$listManagedSettings = $grpc.ClientMethod<
          $0.ListManagedSettingsRequest, $0.ListManagedSettingsResponse>(
      '/client.v0.ClientService/ListManagedSettings',
      ($0.ListManagedSettingsRequest value) => value.writeToBuffer(),
      $0.ListManagedSettingsResponse.fromBuffer);
  static final _$listResources =
      $grpc.ClientMethod<$0.ListResourcesRequest, $0.ListResourcesResponse>(
          '/client.v0.ClientService/ListResources',
          ($0.ListResourcesRequest value) => value.writeToBuffer(),
          $0.ListResourcesResponse.fromBuffer);
  static final _$setResourceEnabled = $grpc.ClientMethod<
          $0.SetResourceEnabledRequest, $0.SetResourceEnabledResponse>(
      '/client.v0.ClientService/SetResourceEnabled',
      ($0.SetResourceEnabledRequest value) => value.writeToBuffer(),
      $0.SetResourceEnabledResponse.fromBuffer);
  static final _$notifyLifecycle =
      $grpc.ClientMethod<$0.NotifyLifecycleRequest, $0.NotifyLifecycleResponse>(
          '/client.v0.ClientService/NotifyLifecycle',
          ($0.NotifyLifecycleRequest value) => value.writeToBuffer(),
          $0.NotifyLifecycleResponse.fromBuffer);
  static final _$getUpdateInfo =
      $grpc.ClientMethod<$0.GetUpdateInfoRequest, $0.GetUpdateInfoResponse>(
          '/client.v0.ClientService/GetUpdateInfo',
          ($0.GetUpdateInfoRequest value) => value.writeToBuffer(),
          $0.GetUpdateInfoResponse.fromBuffer);
  static final _$getSupportInfo =
      $grpc.ClientMethod<$0.GetSupportInfoRequest, $0.GetSupportInfoResponse>(
          '/client.v0.ClientService/GetSupportInfo',
          ($0.GetSupportInfoRequest value) => value.writeToBuffer(),
          $0.GetSupportInfoResponse.fromBuffer);
}

@$pb.GrpcServiceName('client.v0.ClientService')
abstract class ClientServiceBase extends $grpc.Service {
  $core.String get $name => 'client.v0.ClientService';

  ClientServiceBase() {
    $addMethod($grpc.ServiceMethod<$0.GetRuntimeInfoRequest,
            $0.GetRuntimeInfoResponse>(
        'GetRuntimeInfo',
        getRuntimeInfo_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.GetRuntimeInfoRequest.fromBuffer(value),
        ($0.GetRuntimeInfoResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.GetStatusRequest, $0.GetStatusResponse>(
        'GetStatus',
        getStatus_Pre,
        false,
        false,
        ($core.List<$core.int> value) => $0.GetStatusRequest.fromBuffer(value),
        ($0.GetStatusResponse value) => value.writeToBuffer()));
    $addMethod(
        $grpc.ServiceMethod<$0.WatchEventsRequest, $0.WatchEventsResponse>(
            'WatchEvents',
            watchEvents_Pre,
            false,
            true,
            ($core.List<$core.int> value) =>
                $0.WatchEventsRequest.fromBuffer(value),
            ($0.WatchEventsResponse value) => value.writeToBuffer()));
    $addMethod(
        $grpc.ServiceMethod<$0.GetOperationRequest, $0.GetOperationResponse>(
            'GetOperation',
            getOperation_Pre,
            false,
            false,
            ($core.List<$core.int> value) =>
                $0.GetOperationRequest.fromBuffer(value),
            ($0.GetOperationResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.EnrollRequest, $0.EnrollResponse>(
        'Enroll',
        enroll_Pre,
        false,
        false,
        ($core.List<$core.int> value) => $0.EnrollRequest.fromBuffer(value),
        ($0.EnrollResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.ConnectRequest, $0.ConnectResponse>(
        'Connect',
        connect_Pre,
        false,
        false,
        ($core.List<$core.int> value) => $0.ConnectRequest.fromBuffer(value),
        ($0.ConnectResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.DisconnectRequest, $0.DisconnectResponse>(
        'Disconnect',
        disconnect_Pre,
        false,
        false,
        ($core.List<$core.int> value) => $0.DisconnectRequest.fromBuffer(value),
        ($0.DisconnectResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.GetServerIdentityRequest,
            $0.GetServerIdentityResponse>(
        'GetServerIdentity',
        getServerIdentity_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.GetServerIdentityRequest.fromBuffer(value),
        ($0.GetServerIdentityResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.TrustServerIdentityRequest,
            $0.TrustServerIdentityResponse>(
        'TrustServerIdentity',
        trustServerIdentity_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.TrustServerIdentityRequest.fromBuffer(value),
        ($0.TrustServerIdentityResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.LogoutRequest, $0.LogoutResponse>(
        'Logout',
        logout_Pre,
        false,
        false,
        ($core.List<$core.int> value) => $0.LogoutRequest.fromBuffer(value),
        ($0.LogoutResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.ForgetLocalEnrollmentRequest,
            $0.ForgetLocalEnrollmentResponse>(
        'ForgetLocalEnrollment',
        forgetLocalEnrollment_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.ForgetLocalEnrollmentRequest.fromBuffer(value),
        ($0.ForgetLocalEnrollmentResponse value) => value.writeToBuffer()));
    $addMethod(
        $grpc.ServiceMethod<$0.ListNetworksRequest, $0.ListNetworksResponse>(
            'ListNetworks',
            listNetworks_Pre,
            false,
            false,
            ($core.List<$core.int> value) =>
                $0.ListNetworksRequest.fromBuffer(value),
            ($0.ListNetworksResponse value) => value.writeToBuffer()));
    $addMethod(
        $grpc.ServiceMethod<$0.SelectNetworkRequest, $0.SelectNetworkResponse>(
            'SelectNetwork',
            selectNetwork_Pre,
            false,
            false,
            ($core.List<$core.int> value) =>
                $0.SelectNetworkRequest.fromBuffer(value),
            ($0.SelectNetworkResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.ListPeersRequest, $0.ListPeersResponse>(
        'ListPeers',
        listPeers_Pre,
        false,
        false,
        ($core.List<$core.int> value) => $0.ListPeersRequest.fromBuffer(value),
        ($0.ListPeersResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.GetDiagnosticsRequest,
            $0.GetDiagnosticsResponse>(
        'GetDiagnostics',
        getDiagnostics_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.GetDiagnosticsRequest.fromBuffer(value),
        ($0.GetDiagnosticsResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.CreateDiagnosticsBundleRequest,
            $0.CreateDiagnosticsBundleResponse>(
        'CreateDiagnosticsBundle',
        createDiagnosticsBundle_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.CreateDiagnosticsBundleRequest.fromBuffer(value),
        ($0.CreateDiagnosticsBundleResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.ReadDiagnosticsBundleRequest,
            $0.ReadDiagnosticsBundleResponse>(
        'ReadDiagnosticsBundle',
        readDiagnosticsBundle_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.ReadDiagnosticsBundleRequest.fromBuffer(value),
        ($0.ReadDiagnosticsBundleResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.ListRecentLogsRequest,
            $0.ListRecentLogsResponse>(
        'ListRecentLogs',
        listRecentLogs_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.ListRecentLogsRequest.fromBuffer(value),
        ($0.ListRecentLogsResponse value) => value.writeToBuffer()));
    $addMethod(
        $grpc.ServiceMethod<$0.ListProfilesRequest, $0.ListProfilesResponse>(
            'ListProfiles',
            listProfiles_Pre,
            false,
            false,
            ($core.List<$core.int> value) =>
                $0.ListProfilesRequest.fromBuffer(value),
            ($0.ListProfilesResponse value) => value.writeToBuffer()));
    $addMethod(
        $grpc.ServiceMethod<$0.CreateProfileRequest, $0.CreateProfileResponse>(
            'CreateProfile',
            createProfile_Pre,
            false,
            false,
            ($core.List<$core.int> value) =>
                $0.CreateProfileRequest.fromBuffer(value),
            ($0.CreateProfileResponse value) => value.writeToBuffer()));
    $addMethod(
        $grpc.ServiceMethod<$0.SelectProfileRequest, $0.SelectProfileResponse>(
            'SelectProfile',
            selectProfile_Pre,
            false,
            false,
            ($core.List<$core.int> value) =>
                $0.SelectProfileRequest.fromBuffer(value),
            ($0.SelectProfileResponse value) => value.writeToBuffer()));
    $addMethod(
        $grpc.ServiceMethod<$0.RenameProfileRequest, $0.RenameProfileResponse>(
            'RenameProfile',
            renameProfile_Pre,
            false,
            false,
            ($core.List<$core.int> value) =>
                $0.RenameProfileRequest.fromBuffer(value),
            ($0.RenameProfileResponse value) => value.writeToBuffer()));
    $addMethod(
        $grpc.ServiceMethod<$0.RemoveProfileRequest, $0.RemoveProfileResponse>(
            'RemoveProfile',
            removeProfile_Pre,
            false,
            false,
            ($core.List<$core.int> value) =>
                $0.RemoveProfileRequest.fromBuffer(value),
            ($0.RemoveProfileResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.GetSessionRequest, $0.GetSessionResponse>(
        'GetSession',
        getSession_Pre,
        false,
        false,
        ($core.List<$core.int> value) => $0.GetSessionRequest.fromBuffer(value),
        ($0.GetSessionResponse value) => value.writeToBuffer()));
    $addMethod(
        $grpc.ServiceMethod<$0.RenewSessionRequest, $0.RenewSessionResponse>(
            'RenewSession',
            renewSession_Pre,
            false,
            false,
            ($core.List<$core.int> value) =>
                $0.RenewSessionRequest.fromBuffer(value),
            ($0.RenewSessionResponse value) => value.writeToBuffer()));
    $addMethod(
        $grpc.ServiceMethod<$0.ListExitNodesRequest, $0.ListExitNodesResponse>(
            'ListExitNodes',
            listExitNodes_Pre,
            false,
            false,
            ($core.List<$core.int> value) =>
                $0.ListExitNodesRequest.fromBuffer(value),
            ($0.ListExitNodesResponse value) => value.writeToBuffer()));
    $addMethod(
        $grpc.ServiceMethod<$0.GetExitNodeRequest, $0.GetExitNodeResponse>(
            'GetExitNode',
            getExitNode_Pre,
            false,
            false,
            ($core.List<$core.int> value) =>
                $0.GetExitNodeRequest.fromBuffer(value),
            ($0.GetExitNodeResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.SelectExitNodeRequest,
            $0.SelectExitNodeResponse>(
        'SelectExitNode',
        selectExitNode_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.SelectExitNodeRequest.fromBuffer(value),
        ($0.SelectExitNodeResponse value) => value.writeToBuffer()));
    $addMethod(
        $grpc.ServiceMethod<$0.ClearExitNodeRequest, $0.ClearExitNodeResponse>(
            'ClearExitNode',
            clearExitNode_Pre,
            false,
            false,
            ($core.List<$core.int> value) =>
                $0.ClearExitNodeRequest.fromBuffer(value),
            ($0.ClearExitNodeResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.GetPreferencesRequest,
            $0.GetPreferencesResponse>(
        'GetPreferences',
        getPreferences_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.GetPreferencesRequest.fromBuffer(value),
        ($0.GetPreferencesResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.SetPreferencesRequest,
            $0.SetPreferencesResponse>(
        'SetPreferences',
        setPreferences_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.SetPreferencesRequest.fromBuffer(value),
        ($0.SetPreferencesResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.ResetPreferencesRequest,
            $0.ResetPreferencesResponse>(
        'ResetPreferences',
        resetPreferences_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.ResetPreferencesRequest.fromBuffer(value),
        ($0.ResetPreferencesResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.ListManagedSettingsRequest,
            $0.ListManagedSettingsResponse>(
        'ListManagedSettings',
        listManagedSettings_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.ListManagedSettingsRequest.fromBuffer(value),
        ($0.ListManagedSettingsResponse value) => value.writeToBuffer()));
    $addMethod(
        $grpc.ServiceMethod<$0.ListResourcesRequest, $0.ListResourcesResponse>(
            'ListResources',
            listResources_Pre,
            false,
            false,
            ($core.List<$core.int> value) =>
                $0.ListResourcesRequest.fromBuffer(value),
            ($0.ListResourcesResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.SetResourceEnabledRequest,
            $0.SetResourceEnabledResponse>(
        'SetResourceEnabled',
        setResourceEnabled_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.SetResourceEnabledRequest.fromBuffer(value),
        ($0.SetResourceEnabledResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.NotifyLifecycleRequest,
            $0.NotifyLifecycleResponse>(
        'NotifyLifecycle',
        notifyLifecycle_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.NotifyLifecycleRequest.fromBuffer(value),
        ($0.NotifyLifecycleResponse value) => value.writeToBuffer()));
    $addMethod(
        $grpc.ServiceMethod<$0.GetUpdateInfoRequest, $0.GetUpdateInfoResponse>(
            'GetUpdateInfo',
            getUpdateInfo_Pre,
            false,
            false,
            ($core.List<$core.int> value) =>
                $0.GetUpdateInfoRequest.fromBuffer(value),
            ($0.GetUpdateInfoResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.GetSupportInfoRequest,
            $0.GetSupportInfoResponse>(
        'GetSupportInfo',
        getSupportInfo_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.GetSupportInfoRequest.fromBuffer(value),
        ($0.GetSupportInfoResponse value) => value.writeToBuffer()));
  }

  $async.Future<$0.GetRuntimeInfoResponse> getRuntimeInfo_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.GetRuntimeInfoRequest> $request) async {
    return getRuntimeInfo($call, await $request);
  }

  $async.Future<$0.GetRuntimeInfoResponse> getRuntimeInfo(
      $grpc.ServiceCall call, $0.GetRuntimeInfoRequest request);

  $async.Future<$0.GetStatusResponse> getStatus_Pre($grpc.ServiceCall $call,
      $async.Future<$0.GetStatusRequest> $request) async {
    return getStatus($call, await $request);
  }

  $async.Future<$0.GetStatusResponse> getStatus(
      $grpc.ServiceCall call, $0.GetStatusRequest request);

  $async.Stream<$0.WatchEventsResponse> watchEvents_Pre($grpc.ServiceCall $call,
      $async.Future<$0.WatchEventsRequest> $request) async* {
    yield* watchEvents($call, await $request);
  }

  $async.Stream<$0.WatchEventsResponse> watchEvents(
      $grpc.ServiceCall call, $0.WatchEventsRequest request);

  $async.Future<$0.GetOperationResponse> getOperation_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.GetOperationRequest> $request) async {
    return getOperation($call, await $request);
  }

  $async.Future<$0.GetOperationResponse> getOperation(
      $grpc.ServiceCall call, $0.GetOperationRequest request);

  $async.Future<$0.EnrollResponse> enroll_Pre(
      $grpc.ServiceCall $call, $async.Future<$0.EnrollRequest> $request) async {
    return enroll($call, await $request);
  }

  $async.Future<$0.EnrollResponse> enroll(
      $grpc.ServiceCall call, $0.EnrollRequest request);

  $async.Future<$0.ConnectResponse> connect_Pre($grpc.ServiceCall $call,
      $async.Future<$0.ConnectRequest> $request) async {
    return connect($call, await $request);
  }

  $async.Future<$0.ConnectResponse> connect(
      $grpc.ServiceCall call, $0.ConnectRequest request);

  $async.Future<$0.DisconnectResponse> disconnect_Pre($grpc.ServiceCall $call,
      $async.Future<$0.DisconnectRequest> $request) async {
    return disconnect($call, await $request);
  }

  $async.Future<$0.DisconnectResponse> disconnect(
      $grpc.ServiceCall call, $0.DisconnectRequest request);

  $async.Future<$0.GetServerIdentityResponse> getServerIdentity_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.GetServerIdentityRequest> $request) async {
    return getServerIdentity($call, await $request);
  }

  $async.Future<$0.GetServerIdentityResponse> getServerIdentity(
      $grpc.ServiceCall call, $0.GetServerIdentityRequest request);

  $async.Future<$0.TrustServerIdentityResponse> trustServerIdentity_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.TrustServerIdentityRequest> $request) async {
    return trustServerIdentity($call, await $request);
  }

  $async.Future<$0.TrustServerIdentityResponse> trustServerIdentity(
      $grpc.ServiceCall call, $0.TrustServerIdentityRequest request);

  $async.Future<$0.LogoutResponse> logout_Pre(
      $grpc.ServiceCall $call, $async.Future<$0.LogoutRequest> $request) async {
    return logout($call, await $request);
  }

  $async.Future<$0.LogoutResponse> logout(
      $grpc.ServiceCall call, $0.LogoutRequest request);

  $async.Future<$0.ForgetLocalEnrollmentResponse> forgetLocalEnrollment_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.ForgetLocalEnrollmentRequest> $request) async {
    return forgetLocalEnrollment($call, await $request);
  }

  $async.Future<$0.ForgetLocalEnrollmentResponse> forgetLocalEnrollment(
      $grpc.ServiceCall call, $0.ForgetLocalEnrollmentRequest request);

  $async.Future<$0.ListNetworksResponse> listNetworks_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.ListNetworksRequest> $request) async {
    return listNetworks($call, await $request);
  }

  $async.Future<$0.ListNetworksResponse> listNetworks(
      $grpc.ServiceCall call, $0.ListNetworksRequest request);

  $async.Future<$0.SelectNetworkResponse> selectNetwork_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.SelectNetworkRequest> $request) async {
    return selectNetwork($call, await $request);
  }

  $async.Future<$0.SelectNetworkResponse> selectNetwork(
      $grpc.ServiceCall call, $0.SelectNetworkRequest request);

  $async.Future<$0.ListPeersResponse> listPeers_Pre($grpc.ServiceCall $call,
      $async.Future<$0.ListPeersRequest> $request) async {
    return listPeers($call, await $request);
  }

  $async.Future<$0.ListPeersResponse> listPeers(
      $grpc.ServiceCall call, $0.ListPeersRequest request);

  $async.Future<$0.GetDiagnosticsResponse> getDiagnostics_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.GetDiagnosticsRequest> $request) async {
    return getDiagnostics($call, await $request);
  }

  $async.Future<$0.GetDiagnosticsResponse> getDiagnostics(
      $grpc.ServiceCall call, $0.GetDiagnosticsRequest request);

  $async.Future<$0.CreateDiagnosticsBundleResponse> createDiagnosticsBundle_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.CreateDiagnosticsBundleRequest> $request) async {
    return createDiagnosticsBundle($call, await $request);
  }

  $async.Future<$0.CreateDiagnosticsBundleResponse> createDiagnosticsBundle(
      $grpc.ServiceCall call, $0.CreateDiagnosticsBundleRequest request);

  $async.Future<$0.ReadDiagnosticsBundleResponse> readDiagnosticsBundle_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.ReadDiagnosticsBundleRequest> $request) async {
    return readDiagnosticsBundle($call, await $request);
  }

  $async.Future<$0.ReadDiagnosticsBundleResponse> readDiagnosticsBundle(
      $grpc.ServiceCall call, $0.ReadDiagnosticsBundleRequest request);

  $async.Future<$0.ListRecentLogsResponse> listRecentLogs_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.ListRecentLogsRequest> $request) async {
    return listRecentLogs($call, await $request);
  }

  $async.Future<$0.ListRecentLogsResponse> listRecentLogs(
      $grpc.ServiceCall call, $0.ListRecentLogsRequest request);

  $async.Future<$0.ListProfilesResponse> listProfiles_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.ListProfilesRequest> $request) async {
    return listProfiles($call, await $request);
  }

  $async.Future<$0.ListProfilesResponse> listProfiles(
      $grpc.ServiceCall call, $0.ListProfilesRequest request);

  $async.Future<$0.CreateProfileResponse> createProfile_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.CreateProfileRequest> $request) async {
    return createProfile($call, await $request);
  }

  $async.Future<$0.CreateProfileResponse> createProfile(
      $grpc.ServiceCall call, $0.CreateProfileRequest request);

  $async.Future<$0.SelectProfileResponse> selectProfile_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.SelectProfileRequest> $request) async {
    return selectProfile($call, await $request);
  }

  $async.Future<$0.SelectProfileResponse> selectProfile(
      $grpc.ServiceCall call, $0.SelectProfileRequest request);

  $async.Future<$0.RenameProfileResponse> renameProfile_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.RenameProfileRequest> $request) async {
    return renameProfile($call, await $request);
  }

  $async.Future<$0.RenameProfileResponse> renameProfile(
      $grpc.ServiceCall call, $0.RenameProfileRequest request);

  $async.Future<$0.RemoveProfileResponse> removeProfile_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.RemoveProfileRequest> $request) async {
    return removeProfile($call, await $request);
  }

  $async.Future<$0.RemoveProfileResponse> removeProfile(
      $grpc.ServiceCall call, $0.RemoveProfileRequest request);

  $async.Future<$0.GetSessionResponse> getSession_Pre($grpc.ServiceCall $call,
      $async.Future<$0.GetSessionRequest> $request) async {
    return getSession($call, await $request);
  }

  $async.Future<$0.GetSessionResponse> getSession(
      $grpc.ServiceCall call, $0.GetSessionRequest request);

  $async.Future<$0.RenewSessionResponse> renewSession_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.RenewSessionRequest> $request) async {
    return renewSession($call, await $request);
  }

  $async.Future<$0.RenewSessionResponse> renewSession(
      $grpc.ServiceCall call, $0.RenewSessionRequest request);

  $async.Future<$0.ListExitNodesResponse> listExitNodes_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.ListExitNodesRequest> $request) async {
    return listExitNodes($call, await $request);
  }

  $async.Future<$0.ListExitNodesResponse> listExitNodes(
      $grpc.ServiceCall call, $0.ListExitNodesRequest request);

  $async.Future<$0.GetExitNodeResponse> getExitNode_Pre($grpc.ServiceCall $call,
      $async.Future<$0.GetExitNodeRequest> $request) async {
    return getExitNode($call, await $request);
  }

  $async.Future<$0.GetExitNodeResponse> getExitNode(
      $grpc.ServiceCall call, $0.GetExitNodeRequest request);

  $async.Future<$0.SelectExitNodeResponse> selectExitNode_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.SelectExitNodeRequest> $request) async {
    return selectExitNode($call, await $request);
  }

  $async.Future<$0.SelectExitNodeResponse> selectExitNode(
      $grpc.ServiceCall call, $0.SelectExitNodeRequest request);

  $async.Future<$0.ClearExitNodeResponse> clearExitNode_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.ClearExitNodeRequest> $request) async {
    return clearExitNode($call, await $request);
  }

  $async.Future<$0.ClearExitNodeResponse> clearExitNode(
      $grpc.ServiceCall call, $0.ClearExitNodeRequest request);

  $async.Future<$0.GetPreferencesResponse> getPreferences_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.GetPreferencesRequest> $request) async {
    return getPreferences($call, await $request);
  }

  $async.Future<$0.GetPreferencesResponse> getPreferences(
      $grpc.ServiceCall call, $0.GetPreferencesRequest request);

  $async.Future<$0.SetPreferencesResponse> setPreferences_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.SetPreferencesRequest> $request) async {
    return setPreferences($call, await $request);
  }

  $async.Future<$0.SetPreferencesResponse> setPreferences(
      $grpc.ServiceCall call, $0.SetPreferencesRequest request);

  $async.Future<$0.ResetPreferencesResponse> resetPreferences_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.ResetPreferencesRequest> $request) async {
    return resetPreferences($call, await $request);
  }

  $async.Future<$0.ResetPreferencesResponse> resetPreferences(
      $grpc.ServiceCall call, $0.ResetPreferencesRequest request);

  $async.Future<$0.ListManagedSettingsResponse> listManagedSettings_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.ListManagedSettingsRequest> $request) async {
    return listManagedSettings($call, await $request);
  }

  $async.Future<$0.ListManagedSettingsResponse> listManagedSettings(
      $grpc.ServiceCall call, $0.ListManagedSettingsRequest request);

  $async.Future<$0.ListResourcesResponse> listResources_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.ListResourcesRequest> $request) async {
    return listResources($call, await $request);
  }

  $async.Future<$0.ListResourcesResponse> listResources(
      $grpc.ServiceCall call, $0.ListResourcesRequest request);

  $async.Future<$0.SetResourceEnabledResponse> setResourceEnabled_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.SetResourceEnabledRequest> $request) async {
    return setResourceEnabled($call, await $request);
  }

  $async.Future<$0.SetResourceEnabledResponse> setResourceEnabled(
      $grpc.ServiceCall call, $0.SetResourceEnabledRequest request);

  $async.Future<$0.NotifyLifecycleResponse> notifyLifecycle_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.NotifyLifecycleRequest> $request) async {
    return notifyLifecycle($call, await $request);
  }

  $async.Future<$0.NotifyLifecycleResponse> notifyLifecycle(
      $grpc.ServiceCall call, $0.NotifyLifecycleRequest request);

  $async.Future<$0.GetUpdateInfoResponse> getUpdateInfo_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.GetUpdateInfoRequest> $request) async {
    return getUpdateInfo($call, await $request);
  }

  $async.Future<$0.GetUpdateInfoResponse> getUpdateInfo(
      $grpc.ServiceCall call, $0.GetUpdateInfoRequest request);

  $async.Future<$0.GetSupportInfoResponse> getSupportInfo_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.GetSupportInfoRequest> $request) async {
    return getSupportInfo($call, await $request);
  }

  $async.Future<$0.GetSupportInfoResponse> getSupportInfo(
      $grpc.ServiceCall call, $0.GetSupportInfoRequest request);
}
