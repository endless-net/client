// This is a generated file - do not edit.
//
// Generated from client/v0/service.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, prefer_relative_imports

import 'dart:core' as $core;

import 'package:fixnum/fixnum.dart' as $fixnum;
import 'package:protobuf/protobuf.dart' as $pb;

import 'common.pb.dart' as $1;
import 'features.pb.dart' as $3;
import 'runtime.pb.dart' as $2;
import 'service.pbenum.dart';

export 'package:protobuf/protobuf.dart' show GeneratedMessageGenericExtensions;

export 'service.pbenum.dart';

class GetRuntimeInfoRequest extends $pb.GeneratedMessage {
  factory GetRuntimeInfoRequest() => create();

  GetRuntimeInfoRequest._();

  factory GetRuntimeInfoRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory GetRuntimeInfoRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'GetRuntimeInfoRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetRuntimeInfoRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetRuntimeInfoRequest copyWith(
          void Function(GetRuntimeInfoRequest) updates) =>
      super.copyWith((message) => updates(message as GetRuntimeInfoRequest))
          as GetRuntimeInfoRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static GetRuntimeInfoRequest create() => GetRuntimeInfoRequest._();
  @$core.override
  GetRuntimeInfoRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static GetRuntimeInfoRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<GetRuntimeInfoRequest>(create);
  static GetRuntimeInfoRequest? _defaultInstance;
}

class GetRuntimeInfoResponse extends $pb.GeneratedMessage {
  factory GetRuntimeInfoResponse({
    $1.RuntimeInfo? runtime,
  }) {
    final result = create();
    if (runtime != null) result.runtime = runtime;
    return result;
  }

  GetRuntimeInfoResponse._();

  factory GetRuntimeInfoResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory GetRuntimeInfoResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'GetRuntimeInfoResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.RuntimeInfo>(1, _omitFieldNames ? '' : 'runtime',
        subBuilder: $1.RuntimeInfo.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetRuntimeInfoResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetRuntimeInfoResponse copyWith(
          void Function(GetRuntimeInfoResponse) updates) =>
      super.copyWith((message) => updates(message as GetRuntimeInfoResponse))
          as GetRuntimeInfoResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static GetRuntimeInfoResponse create() => GetRuntimeInfoResponse._();
  @$core.override
  GetRuntimeInfoResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static GetRuntimeInfoResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<GetRuntimeInfoResponse>(create);
  static GetRuntimeInfoResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $1.RuntimeInfo get runtime => $_getN(0);
  @$pb.TagNumber(1)
  set runtime($1.RuntimeInfo value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasRuntime() => $_has(0);
  @$pb.TagNumber(1)
  void clearRuntime() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.RuntimeInfo ensureRuntime() => $_ensure(0);
}

class GetStatusRequest extends $pb.GeneratedMessage {
  factory GetStatusRequest() => create();

  GetStatusRequest._();

  factory GetStatusRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory GetStatusRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'GetStatusRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetStatusRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetStatusRequest copyWith(void Function(GetStatusRequest) updates) =>
      super.copyWith((message) => updates(message as GetStatusRequest))
          as GetStatusRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static GetStatusRequest create() => GetStatusRequest._();
  @$core.override
  GetStatusRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static GetStatusRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<GetStatusRequest>(create);
  static GetStatusRequest? _defaultInstance;
}

class GetStatusResponse extends $pb.GeneratedMessage {
  factory GetStatusResponse({
    $2.Status? status,
  }) {
    final result = create();
    if (status != null) result.status = status;
    return result;
  }

  GetStatusResponse._();

  factory GetStatusResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory GetStatusResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'GetStatusResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$2.Status>(1, _omitFieldNames ? '' : 'status',
        subBuilder: $2.Status.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetStatusResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetStatusResponse copyWith(void Function(GetStatusResponse) updates) =>
      super.copyWith((message) => updates(message as GetStatusResponse))
          as GetStatusResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static GetStatusResponse create() => GetStatusResponse._();
  @$core.override
  GetStatusResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static GetStatusResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<GetStatusResponse>(create);
  static GetStatusResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $2.Status get status => $_getN(0);
  @$pb.TagNumber(1)
  set status($2.Status value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasStatus() => $_has(0);
  @$pb.TagNumber(1)
  void clearStatus() => $_clearField(1);
  @$pb.TagNumber(1)
  $2.Status ensureStatus() => $_ensure(0);
}

class WatchEventsRequest extends $pb.GeneratedMessage {
  factory WatchEventsRequest() => create();

  WatchEventsRequest._();

  factory WatchEventsRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory WatchEventsRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'WatchEventsRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  WatchEventsRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  WatchEventsRequest copyWith(void Function(WatchEventsRequest) updates) =>
      super.copyWith((message) => updates(message as WatchEventsRequest))
          as WatchEventsRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static WatchEventsRequest create() => WatchEventsRequest._();
  @$core.override
  WatchEventsRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static WatchEventsRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<WatchEventsRequest>(create);
  static WatchEventsRequest? _defaultInstance;
}

enum GetOperationRequest_Lookup { operationId, requestId, notSet }

class GetOperationRequest extends $pb.GeneratedMessage {
  factory GetOperationRequest({
    $core.String? operationId,
    $core.String? requestId,
  }) {
    final result = create();
    if (operationId != null) result.operationId = operationId;
    if (requestId != null) result.requestId = requestId;
    return result;
  }

  GetOperationRequest._();

  factory GetOperationRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory GetOperationRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static const $core.Map<$core.int, GetOperationRequest_Lookup>
      _GetOperationRequest_LookupByTag = {
    1: GetOperationRequest_Lookup.operationId,
    2: GetOperationRequest_Lookup.requestId,
    0: GetOperationRequest_Lookup.notSet
  };
  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'GetOperationRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..oo(0, [1, 2])
    ..aOS(1, _omitFieldNames ? '' : 'operationId')
    ..aOS(2, _omitFieldNames ? '' : 'requestId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetOperationRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetOperationRequest copyWith(void Function(GetOperationRequest) updates) =>
      super.copyWith((message) => updates(message as GetOperationRequest))
          as GetOperationRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static GetOperationRequest create() => GetOperationRequest._();
  @$core.override
  GetOperationRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static GetOperationRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<GetOperationRequest>(create);
  static GetOperationRequest? _defaultInstance;

  @$pb.TagNumber(1)
  @$pb.TagNumber(2)
  GetOperationRequest_Lookup whichLookup() =>
      _GetOperationRequest_LookupByTag[$_whichOneof(0)]!;
  @$pb.TagNumber(1)
  @$pb.TagNumber(2)
  void clearLookup() => $_clearField($_whichOneof(0));

  @$pb.TagNumber(1)
  $core.String get operationId => $_getSZ(0);
  @$pb.TagNumber(1)
  set operationId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasOperationId() => $_has(0);
  @$pb.TagNumber(1)
  void clearOperationId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get requestId => $_getSZ(1);
  @$pb.TagNumber(2)
  set requestId($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasRequestId() => $_has(1);
  @$pb.TagNumber(2)
  void clearRequestId() => $_clearField(2);
}

class GetOperationResponse extends $pb.GeneratedMessage {
  factory GetOperationResponse({
    $1.Operation? operation,
  }) {
    final result = create();
    if (operation != null) result.operation = operation;
    return result;
  }

  GetOperationResponse._();

  factory GetOperationResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory GetOperationResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'GetOperationResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.Operation>(1, _omitFieldNames ? '' : 'operation',
        subBuilder: $1.Operation.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetOperationResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetOperationResponse copyWith(void Function(GetOperationResponse) updates) =>
      super.copyWith((message) => updates(message as GetOperationResponse))
          as GetOperationResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static GetOperationResponse create() => GetOperationResponse._();
  @$core.override
  GetOperationResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static GetOperationResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<GetOperationResponse>(create);
  static GetOperationResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $1.Operation get operation => $_getN(0);
  @$pb.TagNumber(1)
  set operation($1.Operation value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasOperation() => $_has(0);
  @$pb.TagNumber(1)
  void clearOperation() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.Operation ensureOperation() => $_ensure(0);
}

enum EnrollRequest_Authentication { enrollmentToken, browserLogin, notSet }

class EnrollRequest extends $pb.GeneratedMessage {
  factory EnrollRequest({
    $1.MutationContext? mutation,
    $1.ProfileRef? profile,
    $2.EnrollmentMode? mode,
    $core.String? hostname,
    $core.String? enrollmentToken,
    $core.bool? browserLogin,
  }) {
    final result = create();
    if (mutation != null) result.mutation = mutation;
    if (profile != null) result.profile = profile;
    if (mode != null) result.mode = mode;
    if (hostname != null) result.hostname = hostname;
    if (enrollmentToken != null) result.enrollmentToken = enrollmentToken;
    if (browserLogin != null) result.browserLogin = browserLogin;
    return result;
  }

  EnrollRequest._();

  factory EnrollRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory EnrollRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static const $core.Map<$core.int, EnrollRequest_Authentication>
      _EnrollRequest_AuthenticationByTag = {
    5: EnrollRequest_Authentication.enrollmentToken,
    6: EnrollRequest_Authentication.browserLogin,
    0: EnrollRequest_Authentication.notSet
  };
  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'EnrollRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..oo(0, [5, 6])
    ..aOM<$1.MutationContext>(1, _omitFieldNames ? '' : 'mutation',
        subBuilder: $1.MutationContext.create)
    ..aOM<$1.ProfileRef>(2, _omitFieldNames ? '' : 'profile',
        subBuilder: $1.ProfileRef.create)
    ..aE<$2.EnrollmentMode>(3, _omitFieldNames ? '' : 'mode',
        enumValues: $2.EnrollmentMode.values)
    ..aOS(4, _omitFieldNames ? '' : 'hostname')
    ..aOS(5, _omitFieldNames ? '' : 'enrollmentToken')
    ..aOB(6, _omitFieldNames ? '' : 'browserLogin')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  EnrollRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  EnrollRequest copyWith(void Function(EnrollRequest) updates) =>
      super.copyWith((message) => updates(message as EnrollRequest))
          as EnrollRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static EnrollRequest create() => EnrollRequest._();
  @$core.override
  EnrollRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static EnrollRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<EnrollRequest>(create);
  static EnrollRequest? _defaultInstance;

  @$pb.TagNumber(5)
  @$pb.TagNumber(6)
  EnrollRequest_Authentication whichAuthentication() =>
      _EnrollRequest_AuthenticationByTag[$_whichOneof(0)]!;
  @$pb.TagNumber(5)
  @$pb.TagNumber(6)
  void clearAuthentication() => $_clearField($_whichOneof(0));

  @$pb.TagNumber(1)
  $1.MutationContext get mutation => $_getN(0);
  @$pb.TagNumber(1)
  set mutation($1.MutationContext value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasMutation() => $_has(0);
  @$pb.TagNumber(1)
  void clearMutation() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.MutationContext ensureMutation() => $_ensure(0);

  @$pb.TagNumber(2)
  $1.ProfileRef get profile => $_getN(1);
  @$pb.TagNumber(2)
  set profile($1.ProfileRef value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasProfile() => $_has(1);
  @$pb.TagNumber(2)
  void clearProfile() => $_clearField(2);
  @$pb.TagNumber(2)
  $1.ProfileRef ensureProfile() => $_ensure(1);

  @$pb.TagNumber(3)
  $2.EnrollmentMode get mode => $_getN(2);
  @$pb.TagNumber(3)
  set mode($2.EnrollmentMode value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasMode() => $_has(2);
  @$pb.TagNumber(3)
  void clearMode() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.String get hostname => $_getSZ(3);
  @$pb.TagNumber(4)
  set hostname($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasHostname() => $_has(3);
  @$pb.TagNumber(4)
  void clearHostname() => $_clearField(4);

  @$pb.TagNumber(5)
  $core.String get enrollmentToken => $_getSZ(4);
  @$pb.TagNumber(5)
  set enrollmentToken($core.String value) => $_setString(4, value);
  @$pb.TagNumber(5)
  $core.bool hasEnrollmentToken() => $_has(4);
  @$pb.TagNumber(5)
  void clearEnrollmentToken() => $_clearField(5);

  @$pb.TagNumber(6)
  $core.bool get browserLogin => $_getBF(5);
  @$pb.TagNumber(6)
  set browserLogin($core.bool value) => $_setBool(5, value);
  @$pb.TagNumber(6)
  $core.bool hasBrowserLogin() => $_has(5);
  @$pb.TagNumber(6)
  void clearBrowserLogin() => $_clearField(6);
}

class EnrollResponse extends $pb.GeneratedMessage {
  factory EnrollResponse({
    $1.Operation? operation,
  }) {
    final result = create();
    if (operation != null) result.operation = operation;
    return result;
  }

  EnrollResponse._();

  factory EnrollResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory EnrollResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'EnrollResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.Operation>(1, _omitFieldNames ? '' : 'operation',
        subBuilder: $1.Operation.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  EnrollResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  EnrollResponse copyWith(void Function(EnrollResponse) updates) =>
      super.copyWith((message) => updates(message as EnrollResponse))
          as EnrollResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static EnrollResponse create() => EnrollResponse._();
  @$core.override
  EnrollResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static EnrollResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<EnrollResponse>(create);
  static EnrollResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $1.Operation get operation => $_getN(0);
  @$pb.TagNumber(1)
  set operation($1.Operation value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasOperation() => $_has(0);
  @$pb.TagNumber(1)
  void clearOperation() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.Operation ensureOperation() => $_ensure(0);
}

class ConnectRequest extends $pb.GeneratedMessage {
  factory ConnectRequest({
    $1.MutationContext? mutation,
    $1.ProfileRef? profile,
  }) {
    final result = create();
    if (mutation != null) result.mutation = mutation;
    if (profile != null) result.profile = profile;
    return result;
  }

  ConnectRequest._();

  factory ConnectRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ConnectRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ConnectRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.MutationContext>(1, _omitFieldNames ? '' : 'mutation',
        subBuilder: $1.MutationContext.create)
    ..aOM<$1.ProfileRef>(2, _omitFieldNames ? '' : 'profile',
        subBuilder: $1.ProfileRef.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ConnectRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ConnectRequest copyWith(void Function(ConnectRequest) updates) =>
      super.copyWith((message) => updates(message as ConnectRequest))
          as ConnectRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ConnectRequest create() => ConnectRequest._();
  @$core.override
  ConnectRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ConnectRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ConnectRequest>(create);
  static ConnectRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $1.MutationContext get mutation => $_getN(0);
  @$pb.TagNumber(1)
  set mutation($1.MutationContext value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasMutation() => $_has(0);
  @$pb.TagNumber(1)
  void clearMutation() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.MutationContext ensureMutation() => $_ensure(0);

  @$pb.TagNumber(2)
  $1.ProfileRef get profile => $_getN(1);
  @$pb.TagNumber(2)
  set profile($1.ProfileRef value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasProfile() => $_has(1);
  @$pb.TagNumber(2)
  void clearProfile() => $_clearField(2);
  @$pb.TagNumber(2)
  $1.ProfileRef ensureProfile() => $_ensure(1);
}

class ConnectResponse extends $pb.GeneratedMessage {
  factory ConnectResponse({
    $1.Operation? operation,
  }) {
    final result = create();
    if (operation != null) result.operation = operation;
    return result;
  }

  ConnectResponse._();

  factory ConnectResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ConnectResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ConnectResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.Operation>(1, _omitFieldNames ? '' : 'operation',
        subBuilder: $1.Operation.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ConnectResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ConnectResponse copyWith(void Function(ConnectResponse) updates) =>
      super.copyWith((message) => updates(message as ConnectResponse))
          as ConnectResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ConnectResponse create() => ConnectResponse._();
  @$core.override
  ConnectResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ConnectResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ConnectResponse>(create);
  static ConnectResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $1.Operation get operation => $_getN(0);
  @$pb.TagNumber(1)
  set operation($1.Operation value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasOperation() => $_has(0);
  @$pb.TagNumber(1)
  void clearOperation() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.Operation ensureOperation() => $_ensure(0);
}

class DisconnectRequest extends $pb.GeneratedMessage {
  factory DisconnectRequest({
    $1.MutationContext? mutation,
    $1.ProfileRef? profile,
  }) {
    final result = create();
    if (mutation != null) result.mutation = mutation;
    if (profile != null) result.profile = profile;
    return result;
  }

  DisconnectRequest._();

  factory DisconnectRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory DisconnectRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'DisconnectRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.MutationContext>(1, _omitFieldNames ? '' : 'mutation',
        subBuilder: $1.MutationContext.create)
    ..aOM<$1.ProfileRef>(2, _omitFieldNames ? '' : 'profile',
        subBuilder: $1.ProfileRef.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DisconnectRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DisconnectRequest copyWith(void Function(DisconnectRequest) updates) =>
      super.copyWith((message) => updates(message as DisconnectRequest))
          as DisconnectRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static DisconnectRequest create() => DisconnectRequest._();
  @$core.override
  DisconnectRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static DisconnectRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<DisconnectRequest>(create);
  static DisconnectRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $1.MutationContext get mutation => $_getN(0);
  @$pb.TagNumber(1)
  set mutation($1.MutationContext value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasMutation() => $_has(0);
  @$pb.TagNumber(1)
  void clearMutation() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.MutationContext ensureMutation() => $_ensure(0);

  @$pb.TagNumber(2)
  $1.ProfileRef get profile => $_getN(1);
  @$pb.TagNumber(2)
  set profile($1.ProfileRef value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasProfile() => $_has(1);
  @$pb.TagNumber(2)
  void clearProfile() => $_clearField(2);
  @$pb.TagNumber(2)
  $1.ProfileRef ensureProfile() => $_ensure(1);
}

class DisconnectResponse extends $pb.GeneratedMessage {
  factory DisconnectResponse({
    $1.Operation? operation,
  }) {
    final result = create();
    if (operation != null) result.operation = operation;
    return result;
  }

  DisconnectResponse._();

  factory DisconnectResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory DisconnectResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'DisconnectResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.Operation>(1, _omitFieldNames ? '' : 'operation',
        subBuilder: $1.Operation.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DisconnectResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DisconnectResponse copyWith(void Function(DisconnectResponse) updates) =>
      super.copyWith((message) => updates(message as DisconnectResponse))
          as DisconnectResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static DisconnectResponse create() => DisconnectResponse._();
  @$core.override
  DisconnectResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static DisconnectResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<DisconnectResponse>(create);
  static DisconnectResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $1.Operation get operation => $_getN(0);
  @$pb.TagNumber(1)
  set operation($1.Operation value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasOperation() => $_has(0);
  @$pb.TagNumber(1)
  void clearOperation() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.Operation ensureOperation() => $_ensure(0);
}

class GetServerIdentityRequest extends $pb.GeneratedMessage {
  factory GetServerIdentityRequest({
    $1.ProfileRef? profile,
  }) {
    final result = create();
    if (profile != null) result.profile = profile;
    return result;
  }

  GetServerIdentityRequest._();

  factory GetServerIdentityRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory GetServerIdentityRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'GetServerIdentityRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.ProfileRef>(1, _omitFieldNames ? '' : 'profile',
        subBuilder: $1.ProfileRef.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetServerIdentityRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetServerIdentityRequest copyWith(
          void Function(GetServerIdentityRequest) updates) =>
      super.copyWith((message) => updates(message as GetServerIdentityRequest))
          as GetServerIdentityRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static GetServerIdentityRequest create() => GetServerIdentityRequest._();
  @$core.override
  GetServerIdentityRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static GetServerIdentityRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<GetServerIdentityRequest>(create);
  static GetServerIdentityRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $1.ProfileRef get profile => $_getN(0);
  @$pb.TagNumber(1)
  set profile($1.ProfileRef value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasProfile() => $_has(0);
  @$pb.TagNumber(1)
  void clearProfile() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.ProfileRef ensureProfile() => $_ensure(0);
}

class GetServerIdentityResponse extends $pb.GeneratedMessage {
  factory GetServerIdentityResponse({
    $2.ServerIdentity? identity,
    $1.SnapshotMetadata? metadata,
  }) {
    final result = create();
    if (identity != null) result.identity = identity;
    if (metadata != null) result.metadata = metadata;
    return result;
  }

  GetServerIdentityResponse._();

  factory GetServerIdentityResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory GetServerIdentityResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'GetServerIdentityResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$2.ServerIdentity>(1, _omitFieldNames ? '' : 'identity',
        subBuilder: $2.ServerIdentity.create)
    ..aOM<$1.SnapshotMetadata>(2, _omitFieldNames ? '' : 'metadata',
        subBuilder: $1.SnapshotMetadata.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetServerIdentityResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetServerIdentityResponse copyWith(
          void Function(GetServerIdentityResponse) updates) =>
      super.copyWith((message) => updates(message as GetServerIdentityResponse))
          as GetServerIdentityResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static GetServerIdentityResponse create() => GetServerIdentityResponse._();
  @$core.override
  GetServerIdentityResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static GetServerIdentityResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<GetServerIdentityResponse>(create);
  static GetServerIdentityResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $2.ServerIdentity get identity => $_getN(0);
  @$pb.TagNumber(1)
  set identity($2.ServerIdentity value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasIdentity() => $_has(0);
  @$pb.TagNumber(1)
  void clearIdentity() => $_clearField(1);
  @$pb.TagNumber(1)
  $2.ServerIdentity ensureIdentity() => $_ensure(0);

  @$pb.TagNumber(2)
  $1.SnapshotMetadata get metadata => $_getN(1);
  @$pb.TagNumber(2)
  set metadata($1.SnapshotMetadata value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasMetadata() => $_has(1);
  @$pb.TagNumber(2)
  void clearMetadata() => $_clearField(2);
  @$pb.TagNumber(2)
  $1.SnapshotMetadata ensureMetadata() => $_ensure(1);
}

class TrustServerIdentityRequest extends $pb.GeneratedMessage {
  factory TrustServerIdentityRequest({
    $1.MutationContext? mutation,
    $1.ProfileRef? profile,
    $core.String? confirmedControlOrigin,
    $core.String? confirmedKeyId,
    $core.String? confirmedAnnouncementId,
  }) {
    final result = create();
    if (mutation != null) result.mutation = mutation;
    if (profile != null) result.profile = profile;
    if (confirmedControlOrigin != null)
      result.confirmedControlOrigin = confirmedControlOrigin;
    if (confirmedKeyId != null) result.confirmedKeyId = confirmedKeyId;
    if (confirmedAnnouncementId != null)
      result.confirmedAnnouncementId = confirmedAnnouncementId;
    return result;
  }

  TrustServerIdentityRequest._();

  factory TrustServerIdentityRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory TrustServerIdentityRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'TrustServerIdentityRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.MutationContext>(1, _omitFieldNames ? '' : 'mutation',
        subBuilder: $1.MutationContext.create)
    ..aOM<$1.ProfileRef>(2, _omitFieldNames ? '' : 'profile',
        subBuilder: $1.ProfileRef.create)
    ..aOS(3, _omitFieldNames ? '' : 'confirmedControlOrigin')
    ..aOS(4, _omitFieldNames ? '' : 'confirmedKeyId')
    ..aOS(5, _omitFieldNames ? '' : 'confirmedAnnouncementId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  TrustServerIdentityRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  TrustServerIdentityRequest copyWith(
          void Function(TrustServerIdentityRequest) updates) =>
      super.copyWith(
              (message) => updates(message as TrustServerIdentityRequest))
          as TrustServerIdentityRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static TrustServerIdentityRequest create() => TrustServerIdentityRequest._();
  @$core.override
  TrustServerIdentityRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static TrustServerIdentityRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<TrustServerIdentityRequest>(create);
  static TrustServerIdentityRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $1.MutationContext get mutation => $_getN(0);
  @$pb.TagNumber(1)
  set mutation($1.MutationContext value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasMutation() => $_has(0);
  @$pb.TagNumber(1)
  void clearMutation() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.MutationContext ensureMutation() => $_ensure(0);

  @$pb.TagNumber(2)
  $1.ProfileRef get profile => $_getN(1);
  @$pb.TagNumber(2)
  set profile($1.ProfileRef value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasProfile() => $_has(1);
  @$pb.TagNumber(2)
  void clearProfile() => $_clearField(2);
  @$pb.TagNumber(2)
  $1.ProfileRef ensureProfile() => $_ensure(1);

  @$pb.TagNumber(3)
  $core.String get confirmedControlOrigin => $_getSZ(2);
  @$pb.TagNumber(3)
  set confirmedControlOrigin($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasConfirmedControlOrigin() => $_has(2);
  @$pb.TagNumber(3)
  void clearConfirmedControlOrigin() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.String get confirmedKeyId => $_getSZ(3);
  @$pb.TagNumber(4)
  set confirmedKeyId($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasConfirmedKeyId() => $_has(3);
  @$pb.TagNumber(4)
  void clearConfirmedKeyId() => $_clearField(4);

  @$pb.TagNumber(5)
  $core.String get confirmedAnnouncementId => $_getSZ(4);
  @$pb.TagNumber(5)
  set confirmedAnnouncementId($core.String value) => $_setString(4, value);
  @$pb.TagNumber(5)
  $core.bool hasConfirmedAnnouncementId() => $_has(4);
  @$pb.TagNumber(5)
  void clearConfirmedAnnouncementId() => $_clearField(5);
}

class TrustServerIdentityResponse extends $pb.GeneratedMessage {
  factory TrustServerIdentityResponse({
    $1.Operation? operation,
  }) {
    final result = create();
    if (operation != null) result.operation = operation;
    return result;
  }

  TrustServerIdentityResponse._();

  factory TrustServerIdentityResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory TrustServerIdentityResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'TrustServerIdentityResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.Operation>(1, _omitFieldNames ? '' : 'operation',
        subBuilder: $1.Operation.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  TrustServerIdentityResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  TrustServerIdentityResponse copyWith(
          void Function(TrustServerIdentityResponse) updates) =>
      super.copyWith(
              (message) => updates(message as TrustServerIdentityResponse))
          as TrustServerIdentityResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static TrustServerIdentityResponse create() =>
      TrustServerIdentityResponse._();
  @$core.override
  TrustServerIdentityResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static TrustServerIdentityResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<TrustServerIdentityResponse>(create);
  static TrustServerIdentityResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $1.Operation get operation => $_getN(0);
  @$pb.TagNumber(1)
  set operation($1.Operation value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasOperation() => $_has(0);
  @$pb.TagNumber(1)
  void clearOperation() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.Operation ensureOperation() => $_ensure(0);
}

class LogoutRequest extends $pb.GeneratedMessage {
  factory LogoutRequest({
    $1.MutationContext? mutation,
    $1.ProfileRef? profile,
  }) {
    final result = create();
    if (mutation != null) result.mutation = mutation;
    if (profile != null) result.profile = profile;
    return result;
  }

  LogoutRequest._();

  factory LogoutRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory LogoutRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'LogoutRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.MutationContext>(1, _omitFieldNames ? '' : 'mutation',
        subBuilder: $1.MutationContext.create)
    ..aOM<$1.ProfileRef>(2, _omitFieldNames ? '' : 'profile',
        subBuilder: $1.ProfileRef.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  LogoutRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  LogoutRequest copyWith(void Function(LogoutRequest) updates) =>
      super.copyWith((message) => updates(message as LogoutRequest))
          as LogoutRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static LogoutRequest create() => LogoutRequest._();
  @$core.override
  LogoutRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static LogoutRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<LogoutRequest>(create);
  static LogoutRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $1.MutationContext get mutation => $_getN(0);
  @$pb.TagNumber(1)
  set mutation($1.MutationContext value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasMutation() => $_has(0);
  @$pb.TagNumber(1)
  void clearMutation() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.MutationContext ensureMutation() => $_ensure(0);

  @$pb.TagNumber(2)
  $1.ProfileRef get profile => $_getN(1);
  @$pb.TagNumber(2)
  set profile($1.ProfileRef value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasProfile() => $_has(1);
  @$pb.TagNumber(2)
  void clearProfile() => $_clearField(2);
  @$pb.TagNumber(2)
  $1.ProfileRef ensureProfile() => $_ensure(1);
}

class LogoutResponse extends $pb.GeneratedMessage {
  factory LogoutResponse({
    $1.Operation? operation,
  }) {
    final result = create();
    if (operation != null) result.operation = operation;
    return result;
  }

  LogoutResponse._();

  factory LogoutResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory LogoutResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'LogoutResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.Operation>(1, _omitFieldNames ? '' : 'operation',
        subBuilder: $1.Operation.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  LogoutResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  LogoutResponse copyWith(void Function(LogoutResponse) updates) =>
      super.copyWith((message) => updates(message as LogoutResponse))
          as LogoutResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static LogoutResponse create() => LogoutResponse._();
  @$core.override
  LogoutResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static LogoutResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<LogoutResponse>(create);
  static LogoutResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $1.Operation get operation => $_getN(0);
  @$pb.TagNumber(1)
  set operation($1.Operation value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasOperation() => $_has(0);
  @$pb.TagNumber(1)
  void clearOperation() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.Operation ensureOperation() => $_ensure(0);
}

class ForgetLocalEnrollmentRequest extends $pb.GeneratedMessage {
  factory ForgetLocalEnrollmentRequest({
    $1.MutationContext? mutation,
    $1.ProfileRef? profile,
    $core.bool? confirmed,
  }) {
    final result = create();
    if (mutation != null) result.mutation = mutation;
    if (profile != null) result.profile = profile;
    if (confirmed != null) result.confirmed = confirmed;
    return result;
  }

  ForgetLocalEnrollmentRequest._();

  factory ForgetLocalEnrollmentRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ForgetLocalEnrollmentRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ForgetLocalEnrollmentRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.MutationContext>(1, _omitFieldNames ? '' : 'mutation',
        subBuilder: $1.MutationContext.create)
    ..aOM<$1.ProfileRef>(2, _omitFieldNames ? '' : 'profile',
        subBuilder: $1.ProfileRef.create)
    ..aOB(3, _omitFieldNames ? '' : 'confirmed')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ForgetLocalEnrollmentRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ForgetLocalEnrollmentRequest copyWith(
          void Function(ForgetLocalEnrollmentRequest) updates) =>
      super.copyWith(
              (message) => updates(message as ForgetLocalEnrollmentRequest))
          as ForgetLocalEnrollmentRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ForgetLocalEnrollmentRequest create() =>
      ForgetLocalEnrollmentRequest._();
  @$core.override
  ForgetLocalEnrollmentRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ForgetLocalEnrollmentRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ForgetLocalEnrollmentRequest>(create);
  static ForgetLocalEnrollmentRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $1.MutationContext get mutation => $_getN(0);
  @$pb.TagNumber(1)
  set mutation($1.MutationContext value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasMutation() => $_has(0);
  @$pb.TagNumber(1)
  void clearMutation() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.MutationContext ensureMutation() => $_ensure(0);

  @$pb.TagNumber(2)
  $1.ProfileRef get profile => $_getN(1);
  @$pb.TagNumber(2)
  set profile($1.ProfileRef value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasProfile() => $_has(1);
  @$pb.TagNumber(2)
  void clearProfile() => $_clearField(2);
  @$pb.TagNumber(2)
  $1.ProfileRef ensureProfile() => $_ensure(1);

  @$pb.TagNumber(3)
  $core.bool get confirmed => $_getBF(2);
  @$pb.TagNumber(3)
  set confirmed($core.bool value) => $_setBool(2, value);
  @$pb.TagNumber(3)
  $core.bool hasConfirmed() => $_has(2);
  @$pb.TagNumber(3)
  void clearConfirmed() => $_clearField(3);
}

class ForgetLocalEnrollmentResponse extends $pb.GeneratedMessage {
  factory ForgetLocalEnrollmentResponse({
    $1.Operation? operation,
  }) {
    final result = create();
    if (operation != null) result.operation = operation;
    return result;
  }

  ForgetLocalEnrollmentResponse._();

  factory ForgetLocalEnrollmentResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ForgetLocalEnrollmentResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ForgetLocalEnrollmentResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.Operation>(1, _omitFieldNames ? '' : 'operation',
        subBuilder: $1.Operation.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ForgetLocalEnrollmentResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ForgetLocalEnrollmentResponse copyWith(
          void Function(ForgetLocalEnrollmentResponse) updates) =>
      super.copyWith(
              (message) => updates(message as ForgetLocalEnrollmentResponse))
          as ForgetLocalEnrollmentResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ForgetLocalEnrollmentResponse create() =>
      ForgetLocalEnrollmentResponse._();
  @$core.override
  ForgetLocalEnrollmentResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ForgetLocalEnrollmentResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ForgetLocalEnrollmentResponse>(create);
  static ForgetLocalEnrollmentResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $1.Operation get operation => $_getN(0);
  @$pb.TagNumber(1)
  set operation($1.Operation value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasOperation() => $_has(0);
  @$pb.TagNumber(1)
  void clearOperation() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.Operation ensureOperation() => $_ensure(0);
}

class ListNetworksRequest extends $pb.GeneratedMessage {
  factory ListNetworksRequest({
    $1.ProfileRef? profile,
    $1.PageRequest? page,
  }) {
    final result = create();
    if (profile != null) result.profile = profile;
    if (page != null) result.page = page;
    return result;
  }

  ListNetworksRequest._();

  factory ListNetworksRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListNetworksRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListNetworksRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.ProfileRef>(1, _omitFieldNames ? '' : 'profile',
        subBuilder: $1.ProfileRef.create)
    ..aOM<$1.PageRequest>(2, _omitFieldNames ? '' : 'page',
        subBuilder: $1.PageRequest.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListNetworksRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListNetworksRequest copyWith(void Function(ListNetworksRequest) updates) =>
      super.copyWith((message) => updates(message as ListNetworksRequest))
          as ListNetworksRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListNetworksRequest create() => ListNetworksRequest._();
  @$core.override
  ListNetworksRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ListNetworksRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListNetworksRequest>(create);
  static ListNetworksRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $1.ProfileRef get profile => $_getN(0);
  @$pb.TagNumber(1)
  set profile($1.ProfileRef value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasProfile() => $_has(0);
  @$pb.TagNumber(1)
  void clearProfile() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.ProfileRef ensureProfile() => $_ensure(0);

  @$pb.TagNumber(2)
  $1.PageRequest get page => $_getN(1);
  @$pb.TagNumber(2)
  set page($1.PageRequest value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasPage() => $_has(1);
  @$pb.TagNumber(2)
  void clearPage() => $_clearField(2);
  @$pb.TagNumber(2)
  $1.PageRequest ensurePage() => $_ensure(1);
}

class ListNetworksResponse extends $pb.GeneratedMessage {
  factory ListNetworksResponse({
    $core.Iterable<$2.Network>? networks,
    $core.String? selectedNetworkId,
    $1.PageResponse? page,
  }) {
    final result = create();
    if (networks != null) result.networks.addAll(networks);
    if (selectedNetworkId != null) result.selectedNetworkId = selectedNetworkId;
    if (page != null) result.page = page;
    return result;
  }

  ListNetworksResponse._();

  factory ListNetworksResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListNetworksResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListNetworksResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..pPM<$2.Network>(1, _omitFieldNames ? '' : 'networks',
        subBuilder: $2.Network.create)
    ..aOS(2, _omitFieldNames ? '' : 'selectedNetworkId')
    ..aOM<$1.PageResponse>(3, _omitFieldNames ? '' : 'page',
        subBuilder: $1.PageResponse.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListNetworksResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListNetworksResponse copyWith(void Function(ListNetworksResponse) updates) =>
      super.copyWith((message) => updates(message as ListNetworksResponse))
          as ListNetworksResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListNetworksResponse create() => ListNetworksResponse._();
  @$core.override
  ListNetworksResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ListNetworksResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListNetworksResponse>(create);
  static ListNetworksResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $pb.PbList<$2.Network> get networks => $_getList(0);

  @$pb.TagNumber(2)
  $core.String get selectedNetworkId => $_getSZ(1);
  @$pb.TagNumber(2)
  set selectedNetworkId($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasSelectedNetworkId() => $_has(1);
  @$pb.TagNumber(2)
  void clearSelectedNetworkId() => $_clearField(2);

  @$pb.TagNumber(3)
  $1.PageResponse get page => $_getN(2);
  @$pb.TagNumber(3)
  set page($1.PageResponse value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasPage() => $_has(2);
  @$pb.TagNumber(3)
  void clearPage() => $_clearField(3);
  @$pb.TagNumber(3)
  $1.PageResponse ensurePage() => $_ensure(2);
}

class SelectNetworkRequest extends $pb.GeneratedMessage {
  factory SelectNetworkRequest({
    $1.MutationContext? mutation,
    $1.ProfileRef? profile,
    $core.String? networkId,
  }) {
    final result = create();
    if (mutation != null) result.mutation = mutation;
    if (profile != null) result.profile = profile;
    if (networkId != null) result.networkId = networkId;
    return result;
  }

  SelectNetworkRequest._();

  factory SelectNetworkRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory SelectNetworkRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SelectNetworkRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.MutationContext>(1, _omitFieldNames ? '' : 'mutation',
        subBuilder: $1.MutationContext.create)
    ..aOM<$1.ProfileRef>(2, _omitFieldNames ? '' : 'profile',
        subBuilder: $1.ProfileRef.create)
    ..aOS(3, _omitFieldNames ? '' : 'networkId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SelectNetworkRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SelectNetworkRequest copyWith(void Function(SelectNetworkRequest) updates) =>
      super.copyWith((message) => updates(message as SelectNetworkRequest))
          as SelectNetworkRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static SelectNetworkRequest create() => SelectNetworkRequest._();
  @$core.override
  SelectNetworkRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static SelectNetworkRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SelectNetworkRequest>(create);
  static SelectNetworkRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $1.MutationContext get mutation => $_getN(0);
  @$pb.TagNumber(1)
  set mutation($1.MutationContext value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasMutation() => $_has(0);
  @$pb.TagNumber(1)
  void clearMutation() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.MutationContext ensureMutation() => $_ensure(0);

  @$pb.TagNumber(2)
  $1.ProfileRef get profile => $_getN(1);
  @$pb.TagNumber(2)
  set profile($1.ProfileRef value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasProfile() => $_has(1);
  @$pb.TagNumber(2)
  void clearProfile() => $_clearField(2);
  @$pb.TagNumber(2)
  $1.ProfileRef ensureProfile() => $_ensure(1);

  @$pb.TagNumber(3)
  $core.String get networkId => $_getSZ(2);
  @$pb.TagNumber(3)
  set networkId($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasNetworkId() => $_has(2);
  @$pb.TagNumber(3)
  void clearNetworkId() => $_clearField(3);
}

class SelectNetworkResponse extends $pb.GeneratedMessage {
  factory SelectNetworkResponse({
    $1.Operation? operation,
  }) {
    final result = create();
    if (operation != null) result.operation = operation;
    return result;
  }

  SelectNetworkResponse._();

  factory SelectNetworkResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory SelectNetworkResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SelectNetworkResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.Operation>(1, _omitFieldNames ? '' : 'operation',
        subBuilder: $1.Operation.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SelectNetworkResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SelectNetworkResponse copyWith(
          void Function(SelectNetworkResponse) updates) =>
      super.copyWith((message) => updates(message as SelectNetworkResponse))
          as SelectNetworkResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static SelectNetworkResponse create() => SelectNetworkResponse._();
  @$core.override
  SelectNetworkResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static SelectNetworkResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SelectNetworkResponse>(create);
  static SelectNetworkResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $1.Operation get operation => $_getN(0);
  @$pb.TagNumber(1)
  set operation($1.Operation value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasOperation() => $_has(0);
  @$pb.TagNumber(1)
  void clearOperation() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.Operation ensureOperation() => $_ensure(0);
}

class ListPeersRequest extends $pb.GeneratedMessage {
  factory ListPeersRequest({
    $1.ProfileRef? profile,
    $1.PageRequest? page,
    $core.String? search,
  }) {
    final result = create();
    if (profile != null) result.profile = profile;
    if (page != null) result.page = page;
    if (search != null) result.search = search;
    return result;
  }

  ListPeersRequest._();

  factory ListPeersRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListPeersRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListPeersRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.ProfileRef>(1, _omitFieldNames ? '' : 'profile',
        subBuilder: $1.ProfileRef.create)
    ..aOM<$1.PageRequest>(2, _omitFieldNames ? '' : 'page',
        subBuilder: $1.PageRequest.create)
    ..aOS(3, _omitFieldNames ? '' : 'search')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListPeersRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListPeersRequest copyWith(void Function(ListPeersRequest) updates) =>
      super.copyWith((message) => updates(message as ListPeersRequest))
          as ListPeersRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListPeersRequest create() => ListPeersRequest._();
  @$core.override
  ListPeersRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ListPeersRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListPeersRequest>(create);
  static ListPeersRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $1.ProfileRef get profile => $_getN(0);
  @$pb.TagNumber(1)
  set profile($1.ProfileRef value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasProfile() => $_has(0);
  @$pb.TagNumber(1)
  void clearProfile() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.ProfileRef ensureProfile() => $_ensure(0);

  @$pb.TagNumber(2)
  $1.PageRequest get page => $_getN(1);
  @$pb.TagNumber(2)
  set page($1.PageRequest value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasPage() => $_has(1);
  @$pb.TagNumber(2)
  void clearPage() => $_clearField(2);
  @$pb.TagNumber(2)
  $1.PageRequest ensurePage() => $_ensure(1);

  @$pb.TagNumber(3)
  $core.String get search => $_getSZ(2);
  @$pb.TagNumber(3)
  set search($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasSearch() => $_has(2);
  @$pb.TagNumber(3)
  void clearSearch() => $_clearField(3);
}

class ListPeersResponse extends $pb.GeneratedMessage {
  factory ListPeersResponse({
    $core.Iterable<$2.Peer>? peers,
    $1.PageResponse? page,
    $2.AgentSnapshotState? snapshotState,
    $fixnum.Int64? mapRevision,
    $fixnum.Int64? targetMapRevision,
  }) {
    final result = create();
    if (peers != null) result.peers.addAll(peers);
    if (page != null) result.page = page;
    if (snapshotState != null) result.snapshotState = snapshotState;
    if (mapRevision != null) result.mapRevision = mapRevision;
    if (targetMapRevision != null) result.targetMapRevision = targetMapRevision;
    return result;
  }

  ListPeersResponse._();

  factory ListPeersResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListPeersResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListPeersResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..pPM<$2.Peer>(1, _omitFieldNames ? '' : 'peers',
        subBuilder: $2.Peer.create)
    ..aOM<$1.PageResponse>(2, _omitFieldNames ? '' : 'page',
        subBuilder: $1.PageResponse.create)
    ..aE<$2.AgentSnapshotState>(3, _omitFieldNames ? '' : 'snapshotState',
        enumValues: $2.AgentSnapshotState.values)
    ..a<$fixnum.Int64>(
        4, _omitFieldNames ? '' : 'mapRevision', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..a<$fixnum.Int64>(
        5, _omitFieldNames ? '' : 'targetMapRevision', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListPeersResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListPeersResponse copyWith(void Function(ListPeersResponse) updates) =>
      super.copyWith((message) => updates(message as ListPeersResponse))
          as ListPeersResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListPeersResponse create() => ListPeersResponse._();
  @$core.override
  ListPeersResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ListPeersResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListPeersResponse>(create);
  static ListPeersResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $pb.PbList<$2.Peer> get peers => $_getList(0);

  @$pb.TagNumber(2)
  $1.PageResponse get page => $_getN(1);
  @$pb.TagNumber(2)
  set page($1.PageResponse value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasPage() => $_has(1);
  @$pb.TagNumber(2)
  void clearPage() => $_clearField(2);
  @$pb.TagNumber(2)
  $1.PageResponse ensurePage() => $_ensure(1);

  @$pb.TagNumber(3)
  $2.AgentSnapshotState get snapshotState => $_getN(2);
  @$pb.TagNumber(3)
  set snapshotState($2.AgentSnapshotState value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasSnapshotState() => $_has(2);
  @$pb.TagNumber(3)
  void clearSnapshotState() => $_clearField(3);

  @$pb.TagNumber(4)
  $fixnum.Int64 get mapRevision => $_getI64(3);
  @$pb.TagNumber(4)
  set mapRevision($fixnum.Int64 value) => $_setInt64(3, value);
  @$pb.TagNumber(4)
  $core.bool hasMapRevision() => $_has(3);
  @$pb.TagNumber(4)
  void clearMapRevision() => $_clearField(4);

  @$pb.TagNumber(5)
  $fixnum.Int64 get targetMapRevision => $_getI64(4);
  @$pb.TagNumber(5)
  set targetMapRevision($fixnum.Int64 value) => $_setInt64(4, value);
  @$pb.TagNumber(5)
  $core.bool hasTargetMapRevision() => $_has(4);
  @$pb.TagNumber(5)
  void clearTargetMapRevision() => $_clearField(5);
}

class GetDiagnosticsRequest extends $pb.GeneratedMessage {
  factory GetDiagnosticsRequest({
    $1.ProfileRef? profile,
  }) {
    final result = create();
    if (profile != null) result.profile = profile;
    return result;
  }

  GetDiagnosticsRequest._();

  factory GetDiagnosticsRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory GetDiagnosticsRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'GetDiagnosticsRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.ProfileRef>(1, _omitFieldNames ? '' : 'profile',
        subBuilder: $1.ProfileRef.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetDiagnosticsRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetDiagnosticsRequest copyWith(
          void Function(GetDiagnosticsRequest) updates) =>
      super.copyWith((message) => updates(message as GetDiagnosticsRequest))
          as GetDiagnosticsRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static GetDiagnosticsRequest create() => GetDiagnosticsRequest._();
  @$core.override
  GetDiagnosticsRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static GetDiagnosticsRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<GetDiagnosticsRequest>(create);
  static GetDiagnosticsRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $1.ProfileRef get profile => $_getN(0);
  @$pb.TagNumber(1)
  set profile($1.ProfileRef value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasProfile() => $_has(0);
  @$pb.TagNumber(1)
  void clearProfile() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.ProfileRef ensureProfile() => $_ensure(0);
}

class GetDiagnosticsResponse extends $pb.GeneratedMessage {
  factory GetDiagnosticsResponse({
    $2.Diagnostics? diagnostics,
  }) {
    final result = create();
    if (diagnostics != null) result.diagnostics = diagnostics;
    return result;
  }

  GetDiagnosticsResponse._();

  factory GetDiagnosticsResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory GetDiagnosticsResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'GetDiagnosticsResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$2.Diagnostics>(1, _omitFieldNames ? '' : 'diagnostics',
        subBuilder: $2.Diagnostics.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetDiagnosticsResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetDiagnosticsResponse copyWith(
          void Function(GetDiagnosticsResponse) updates) =>
      super.copyWith((message) => updates(message as GetDiagnosticsResponse))
          as GetDiagnosticsResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static GetDiagnosticsResponse create() => GetDiagnosticsResponse._();
  @$core.override
  GetDiagnosticsResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static GetDiagnosticsResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<GetDiagnosticsResponse>(create);
  static GetDiagnosticsResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $2.Diagnostics get diagnostics => $_getN(0);
  @$pb.TagNumber(1)
  set diagnostics($2.Diagnostics value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasDiagnostics() => $_has(0);
  @$pb.TagNumber(1)
  void clearDiagnostics() => $_clearField(1);
  @$pb.TagNumber(1)
  $2.Diagnostics ensureDiagnostics() => $_ensure(0);
}

class CreateDiagnosticsBundleRequest extends $pb.GeneratedMessage {
  factory CreateDiagnosticsBundleRequest({
    $1.MutationContext? mutation,
    $1.ProfileRef? profile,
  }) {
    final result = create();
    if (mutation != null) result.mutation = mutation;
    if (profile != null) result.profile = profile;
    return result;
  }

  CreateDiagnosticsBundleRequest._();

  factory CreateDiagnosticsBundleRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory CreateDiagnosticsBundleRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'CreateDiagnosticsBundleRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.MutationContext>(1, _omitFieldNames ? '' : 'mutation',
        subBuilder: $1.MutationContext.create)
    ..aOM<$1.ProfileRef>(2, _omitFieldNames ? '' : 'profile',
        subBuilder: $1.ProfileRef.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  CreateDiagnosticsBundleRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  CreateDiagnosticsBundleRequest copyWith(
          void Function(CreateDiagnosticsBundleRequest) updates) =>
      super.copyWith(
              (message) => updates(message as CreateDiagnosticsBundleRequest))
          as CreateDiagnosticsBundleRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static CreateDiagnosticsBundleRequest create() =>
      CreateDiagnosticsBundleRequest._();
  @$core.override
  CreateDiagnosticsBundleRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static CreateDiagnosticsBundleRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<CreateDiagnosticsBundleRequest>(create);
  static CreateDiagnosticsBundleRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $1.MutationContext get mutation => $_getN(0);
  @$pb.TagNumber(1)
  set mutation($1.MutationContext value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasMutation() => $_has(0);
  @$pb.TagNumber(1)
  void clearMutation() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.MutationContext ensureMutation() => $_ensure(0);

  @$pb.TagNumber(2)
  $1.ProfileRef get profile => $_getN(1);
  @$pb.TagNumber(2)
  set profile($1.ProfileRef value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasProfile() => $_has(1);
  @$pb.TagNumber(2)
  void clearProfile() => $_clearField(2);
  @$pb.TagNumber(2)
  $1.ProfileRef ensureProfile() => $_ensure(1);
}

class CreateDiagnosticsBundleResponse extends $pb.GeneratedMessage {
  factory CreateDiagnosticsBundleResponse({
    $1.Operation? operation,
  }) {
    final result = create();
    if (operation != null) result.operation = operation;
    return result;
  }

  CreateDiagnosticsBundleResponse._();

  factory CreateDiagnosticsBundleResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory CreateDiagnosticsBundleResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'CreateDiagnosticsBundleResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.Operation>(1, _omitFieldNames ? '' : 'operation',
        subBuilder: $1.Operation.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  CreateDiagnosticsBundleResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  CreateDiagnosticsBundleResponse copyWith(
          void Function(CreateDiagnosticsBundleResponse) updates) =>
      super.copyWith(
              (message) => updates(message as CreateDiagnosticsBundleResponse))
          as CreateDiagnosticsBundleResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static CreateDiagnosticsBundleResponse create() =>
      CreateDiagnosticsBundleResponse._();
  @$core.override
  CreateDiagnosticsBundleResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static CreateDiagnosticsBundleResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<CreateDiagnosticsBundleResponse>(
          create);
  static CreateDiagnosticsBundleResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $1.Operation get operation => $_getN(0);
  @$pb.TagNumber(1)
  set operation($1.Operation value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasOperation() => $_has(0);
  @$pb.TagNumber(1)
  void clearOperation() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.Operation ensureOperation() => $_ensure(0);
}

class ReadDiagnosticsBundleRequest extends $pb.GeneratedMessage {
  factory ReadDiagnosticsBundleRequest({
    $core.String? bundleId,
    $fixnum.Int64? offset,
    $core.int? maxBytes,
  }) {
    final result = create();
    if (bundleId != null) result.bundleId = bundleId;
    if (offset != null) result.offset = offset;
    if (maxBytes != null) result.maxBytes = maxBytes;
    return result;
  }

  ReadDiagnosticsBundleRequest._();

  factory ReadDiagnosticsBundleRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ReadDiagnosticsBundleRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ReadDiagnosticsBundleRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'bundleId')
    ..a<$fixnum.Int64>(2, _omitFieldNames ? '' : 'offset', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..aI(3, _omitFieldNames ? '' : 'maxBytes', fieldType: $pb.PbFieldType.OU3)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ReadDiagnosticsBundleRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ReadDiagnosticsBundleRequest copyWith(
          void Function(ReadDiagnosticsBundleRequest) updates) =>
      super.copyWith(
              (message) => updates(message as ReadDiagnosticsBundleRequest))
          as ReadDiagnosticsBundleRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ReadDiagnosticsBundleRequest create() =>
      ReadDiagnosticsBundleRequest._();
  @$core.override
  ReadDiagnosticsBundleRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ReadDiagnosticsBundleRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ReadDiagnosticsBundleRequest>(create);
  static ReadDiagnosticsBundleRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get bundleId => $_getSZ(0);
  @$pb.TagNumber(1)
  set bundleId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasBundleId() => $_has(0);
  @$pb.TagNumber(1)
  void clearBundleId() => $_clearField(1);

  @$pb.TagNumber(2)
  $fixnum.Int64 get offset => $_getI64(1);
  @$pb.TagNumber(2)
  set offset($fixnum.Int64 value) => $_setInt64(1, value);
  @$pb.TagNumber(2)
  $core.bool hasOffset() => $_has(1);
  @$pb.TagNumber(2)
  void clearOffset() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.int get maxBytes => $_getIZ(2);
  @$pb.TagNumber(3)
  set maxBytes($core.int value) => $_setUnsignedInt32(2, value);
  @$pb.TagNumber(3)
  $core.bool hasMaxBytes() => $_has(2);
  @$pb.TagNumber(3)
  void clearMaxBytes() => $_clearField(3);
}

class ReadDiagnosticsBundleResponse extends $pb.GeneratedMessage {
  factory ReadDiagnosticsBundleResponse({
    $core.List<$core.int>? data,
    $fixnum.Int64? nextOffset,
    $core.bool? eof,
  }) {
    final result = create();
    if (data != null) result.data = data;
    if (nextOffset != null) result.nextOffset = nextOffset;
    if (eof != null) result.eof = eof;
    return result;
  }

  ReadDiagnosticsBundleResponse._();

  factory ReadDiagnosticsBundleResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ReadDiagnosticsBundleResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ReadDiagnosticsBundleResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..a<$core.List<$core.int>>(
        1, _omitFieldNames ? '' : 'data', $pb.PbFieldType.OY)
    ..a<$fixnum.Int64>(
        2, _omitFieldNames ? '' : 'nextOffset', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..aOB(3, _omitFieldNames ? '' : 'eof')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ReadDiagnosticsBundleResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ReadDiagnosticsBundleResponse copyWith(
          void Function(ReadDiagnosticsBundleResponse) updates) =>
      super.copyWith(
              (message) => updates(message as ReadDiagnosticsBundleResponse))
          as ReadDiagnosticsBundleResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ReadDiagnosticsBundleResponse create() =>
      ReadDiagnosticsBundleResponse._();
  @$core.override
  ReadDiagnosticsBundleResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ReadDiagnosticsBundleResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ReadDiagnosticsBundleResponse>(create);
  static ReadDiagnosticsBundleResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $core.List<$core.int> get data => $_getN(0);
  @$pb.TagNumber(1)
  set data($core.List<$core.int> value) => $_setBytes(0, value);
  @$pb.TagNumber(1)
  $core.bool hasData() => $_has(0);
  @$pb.TagNumber(1)
  void clearData() => $_clearField(1);

  @$pb.TagNumber(2)
  $fixnum.Int64 get nextOffset => $_getI64(1);
  @$pb.TagNumber(2)
  set nextOffset($fixnum.Int64 value) => $_setInt64(1, value);
  @$pb.TagNumber(2)
  $core.bool hasNextOffset() => $_has(1);
  @$pb.TagNumber(2)
  void clearNextOffset() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.bool get eof => $_getBF(2);
  @$pb.TagNumber(3)
  set eof($core.bool value) => $_setBool(2, value);
  @$pb.TagNumber(3)
  $core.bool hasEof() => $_has(2);
  @$pb.TagNumber(3)
  void clearEof() => $_clearField(3);
}

class ListRecentLogsRequest extends $pb.GeneratedMessage {
  factory ListRecentLogsRequest({
    $1.ProfileRef? profile,
    $1.PageRequest? page,
  }) {
    final result = create();
    if (profile != null) result.profile = profile;
    if (page != null) result.page = page;
    return result;
  }

  ListRecentLogsRequest._();

  factory ListRecentLogsRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListRecentLogsRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListRecentLogsRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.ProfileRef>(1, _omitFieldNames ? '' : 'profile',
        subBuilder: $1.ProfileRef.create)
    ..aOM<$1.PageRequest>(2, _omitFieldNames ? '' : 'page',
        subBuilder: $1.PageRequest.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListRecentLogsRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListRecentLogsRequest copyWith(
          void Function(ListRecentLogsRequest) updates) =>
      super.copyWith((message) => updates(message as ListRecentLogsRequest))
          as ListRecentLogsRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListRecentLogsRequest create() => ListRecentLogsRequest._();
  @$core.override
  ListRecentLogsRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ListRecentLogsRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListRecentLogsRequest>(create);
  static ListRecentLogsRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $1.ProfileRef get profile => $_getN(0);
  @$pb.TagNumber(1)
  set profile($1.ProfileRef value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasProfile() => $_has(0);
  @$pb.TagNumber(1)
  void clearProfile() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.ProfileRef ensureProfile() => $_ensure(0);

  @$pb.TagNumber(2)
  $1.PageRequest get page => $_getN(1);
  @$pb.TagNumber(2)
  set page($1.PageRequest value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasPage() => $_has(1);
  @$pb.TagNumber(2)
  void clearPage() => $_clearField(2);
  @$pb.TagNumber(2)
  $1.PageRequest ensurePage() => $_ensure(1);
}

class ListRecentLogsResponse extends $pb.GeneratedMessage {
  factory ListRecentLogsResponse({
    $core.Iterable<$2.LogEntry>? logs,
    $1.PageResponse? page,
  }) {
    final result = create();
    if (logs != null) result.logs.addAll(logs);
    if (page != null) result.page = page;
    return result;
  }

  ListRecentLogsResponse._();

  factory ListRecentLogsResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListRecentLogsResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListRecentLogsResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..pPM<$2.LogEntry>(1, _omitFieldNames ? '' : 'logs',
        subBuilder: $2.LogEntry.create)
    ..aOM<$1.PageResponse>(2, _omitFieldNames ? '' : 'page',
        subBuilder: $1.PageResponse.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListRecentLogsResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListRecentLogsResponse copyWith(
          void Function(ListRecentLogsResponse) updates) =>
      super.copyWith((message) => updates(message as ListRecentLogsResponse))
          as ListRecentLogsResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListRecentLogsResponse create() => ListRecentLogsResponse._();
  @$core.override
  ListRecentLogsResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ListRecentLogsResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListRecentLogsResponse>(create);
  static ListRecentLogsResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $pb.PbList<$2.LogEntry> get logs => $_getList(0);

  @$pb.TagNumber(2)
  $1.PageResponse get page => $_getN(1);
  @$pb.TagNumber(2)
  set page($1.PageResponse value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasPage() => $_has(1);
  @$pb.TagNumber(2)
  void clearPage() => $_clearField(2);
  @$pb.TagNumber(2)
  $1.PageResponse ensurePage() => $_ensure(1);
}

class ListProfilesRequest extends $pb.GeneratedMessage {
  factory ListProfilesRequest({
    $1.PageRequest? page,
  }) {
    final result = create();
    if (page != null) result.page = page;
    return result;
  }

  ListProfilesRequest._();

  factory ListProfilesRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListProfilesRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListProfilesRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.PageRequest>(1, _omitFieldNames ? '' : 'page',
        subBuilder: $1.PageRequest.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListProfilesRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListProfilesRequest copyWith(void Function(ListProfilesRequest) updates) =>
      super.copyWith((message) => updates(message as ListProfilesRequest))
          as ListProfilesRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListProfilesRequest create() => ListProfilesRequest._();
  @$core.override
  ListProfilesRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ListProfilesRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListProfilesRequest>(create);
  static ListProfilesRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $1.PageRequest get page => $_getN(0);
  @$pb.TagNumber(1)
  set page($1.PageRequest value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasPage() => $_has(0);
  @$pb.TagNumber(1)
  void clearPage() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.PageRequest ensurePage() => $_ensure(0);
}

class ListProfilesResponse extends $pb.GeneratedMessage {
  factory ListProfilesResponse({
    $core.Iterable<$2.Profile>? profiles,
    $core.String? activeProfileId,
    $1.PageResponse? page,
  }) {
    final result = create();
    if (profiles != null) result.profiles.addAll(profiles);
    if (activeProfileId != null) result.activeProfileId = activeProfileId;
    if (page != null) result.page = page;
    return result;
  }

  ListProfilesResponse._();

  factory ListProfilesResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListProfilesResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListProfilesResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..pPM<$2.Profile>(1, _omitFieldNames ? '' : 'profiles',
        subBuilder: $2.Profile.create)
    ..aOS(2, _omitFieldNames ? '' : 'activeProfileId')
    ..aOM<$1.PageResponse>(3, _omitFieldNames ? '' : 'page',
        subBuilder: $1.PageResponse.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListProfilesResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListProfilesResponse copyWith(void Function(ListProfilesResponse) updates) =>
      super.copyWith((message) => updates(message as ListProfilesResponse))
          as ListProfilesResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListProfilesResponse create() => ListProfilesResponse._();
  @$core.override
  ListProfilesResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ListProfilesResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListProfilesResponse>(create);
  static ListProfilesResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $pb.PbList<$2.Profile> get profiles => $_getList(0);

  @$pb.TagNumber(2)
  $core.String get activeProfileId => $_getSZ(1);
  @$pb.TagNumber(2)
  set activeProfileId($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasActiveProfileId() => $_has(1);
  @$pb.TagNumber(2)
  void clearActiveProfileId() => $_clearField(2);

  @$pb.TagNumber(3)
  $1.PageResponse get page => $_getN(2);
  @$pb.TagNumber(3)
  set page($1.PageResponse value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasPage() => $_has(2);
  @$pb.TagNumber(3)
  void clearPage() => $_clearField(3);
  @$pb.TagNumber(3)
  $1.PageResponse ensurePage() => $_ensure(2);
}

class CreateProfileRequest extends $pb.GeneratedMessage {
  factory CreateProfileRequest({
    $1.MutationContext? mutation,
    $core.String? displayName,
    $core.String? controlOrigin,
  }) {
    final result = create();
    if (mutation != null) result.mutation = mutation;
    if (displayName != null) result.displayName = displayName;
    if (controlOrigin != null) result.controlOrigin = controlOrigin;
    return result;
  }

  CreateProfileRequest._();

  factory CreateProfileRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory CreateProfileRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'CreateProfileRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.MutationContext>(1, _omitFieldNames ? '' : 'mutation',
        subBuilder: $1.MutationContext.create)
    ..aOS(2, _omitFieldNames ? '' : 'displayName')
    ..aOS(3, _omitFieldNames ? '' : 'controlOrigin')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  CreateProfileRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  CreateProfileRequest copyWith(void Function(CreateProfileRequest) updates) =>
      super.copyWith((message) => updates(message as CreateProfileRequest))
          as CreateProfileRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static CreateProfileRequest create() => CreateProfileRequest._();
  @$core.override
  CreateProfileRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static CreateProfileRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<CreateProfileRequest>(create);
  static CreateProfileRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $1.MutationContext get mutation => $_getN(0);
  @$pb.TagNumber(1)
  set mutation($1.MutationContext value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasMutation() => $_has(0);
  @$pb.TagNumber(1)
  void clearMutation() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.MutationContext ensureMutation() => $_ensure(0);

  @$pb.TagNumber(2)
  $core.String get displayName => $_getSZ(1);
  @$pb.TagNumber(2)
  set displayName($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasDisplayName() => $_has(1);
  @$pb.TagNumber(2)
  void clearDisplayName() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get controlOrigin => $_getSZ(2);
  @$pb.TagNumber(3)
  set controlOrigin($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasControlOrigin() => $_has(2);
  @$pb.TagNumber(3)
  void clearControlOrigin() => $_clearField(3);
}

class CreateProfileResponse extends $pb.GeneratedMessage {
  factory CreateProfileResponse({
    $1.Operation? operation,
  }) {
    final result = create();
    if (operation != null) result.operation = operation;
    return result;
  }

  CreateProfileResponse._();

  factory CreateProfileResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory CreateProfileResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'CreateProfileResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.Operation>(1, _omitFieldNames ? '' : 'operation',
        subBuilder: $1.Operation.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  CreateProfileResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  CreateProfileResponse copyWith(
          void Function(CreateProfileResponse) updates) =>
      super.copyWith((message) => updates(message as CreateProfileResponse))
          as CreateProfileResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static CreateProfileResponse create() => CreateProfileResponse._();
  @$core.override
  CreateProfileResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static CreateProfileResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<CreateProfileResponse>(create);
  static CreateProfileResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $1.Operation get operation => $_getN(0);
  @$pb.TagNumber(1)
  set operation($1.Operation value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasOperation() => $_has(0);
  @$pb.TagNumber(1)
  void clearOperation() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.Operation ensureOperation() => $_ensure(0);
}

class SelectProfileRequest extends $pb.GeneratedMessage {
  factory SelectProfileRequest({
    $1.MutationContext? mutation,
    $1.ProfileRef? profile,
  }) {
    final result = create();
    if (mutation != null) result.mutation = mutation;
    if (profile != null) result.profile = profile;
    return result;
  }

  SelectProfileRequest._();

  factory SelectProfileRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory SelectProfileRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SelectProfileRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.MutationContext>(1, _omitFieldNames ? '' : 'mutation',
        subBuilder: $1.MutationContext.create)
    ..aOM<$1.ProfileRef>(2, _omitFieldNames ? '' : 'profile',
        subBuilder: $1.ProfileRef.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SelectProfileRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SelectProfileRequest copyWith(void Function(SelectProfileRequest) updates) =>
      super.copyWith((message) => updates(message as SelectProfileRequest))
          as SelectProfileRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static SelectProfileRequest create() => SelectProfileRequest._();
  @$core.override
  SelectProfileRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static SelectProfileRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SelectProfileRequest>(create);
  static SelectProfileRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $1.MutationContext get mutation => $_getN(0);
  @$pb.TagNumber(1)
  set mutation($1.MutationContext value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasMutation() => $_has(0);
  @$pb.TagNumber(1)
  void clearMutation() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.MutationContext ensureMutation() => $_ensure(0);

  @$pb.TagNumber(2)
  $1.ProfileRef get profile => $_getN(1);
  @$pb.TagNumber(2)
  set profile($1.ProfileRef value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasProfile() => $_has(1);
  @$pb.TagNumber(2)
  void clearProfile() => $_clearField(2);
  @$pb.TagNumber(2)
  $1.ProfileRef ensureProfile() => $_ensure(1);
}

class SelectProfileResponse extends $pb.GeneratedMessage {
  factory SelectProfileResponse({
    $1.Operation? operation,
  }) {
    final result = create();
    if (operation != null) result.operation = operation;
    return result;
  }

  SelectProfileResponse._();

  factory SelectProfileResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory SelectProfileResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SelectProfileResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.Operation>(1, _omitFieldNames ? '' : 'operation',
        subBuilder: $1.Operation.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SelectProfileResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SelectProfileResponse copyWith(
          void Function(SelectProfileResponse) updates) =>
      super.copyWith((message) => updates(message as SelectProfileResponse))
          as SelectProfileResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static SelectProfileResponse create() => SelectProfileResponse._();
  @$core.override
  SelectProfileResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static SelectProfileResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SelectProfileResponse>(create);
  static SelectProfileResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $1.Operation get operation => $_getN(0);
  @$pb.TagNumber(1)
  set operation($1.Operation value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasOperation() => $_has(0);
  @$pb.TagNumber(1)
  void clearOperation() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.Operation ensureOperation() => $_ensure(0);
}

class RenameProfileRequest extends $pb.GeneratedMessage {
  factory RenameProfileRequest({
    $1.MutationContext? mutation,
    $1.ProfileRef? profile,
    $core.String? displayName,
  }) {
    final result = create();
    if (mutation != null) result.mutation = mutation;
    if (profile != null) result.profile = profile;
    if (displayName != null) result.displayName = displayName;
    return result;
  }

  RenameProfileRequest._();

  factory RenameProfileRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RenameProfileRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RenameProfileRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.MutationContext>(1, _omitFieldNames ? '' : 'mutation',
        subBuilder: $1.MutationContext.create)
    ..aOM<$1.ProfileRef>(2, _omitFieldNames ? '' : 'profile',
        subBuilder: $1.ProfileRef.create)
    ..aOS(3, _omitFieldNames ? '' : 'displayName')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RenameProfileRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RenameProfileRequest copyWith(void Function(RenameProfileRequest) updates) =>
      super.copyWith((message) => updates(message as RenameProfileRequest))
          as RenameProfileRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RenameProfileRequest create() => RenameProfileRequest._();
  @$core.override
  RenameProfileRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static RenameProfileRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RenameProfileRequest>(create);
  static RenameProfileRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $1.MutationContext get mutation => $_getN(0);
  @$pb.TagNumber(1)
  set mutation($1.MutationContext value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasMutation() => $_has(0);
  @$pb.TagNumber(1)
  void clearMutation() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.MutationContext ensureMutation() => $_ensure(0);

  @$pb.TagNumber(2)
  $1.ProfileRef get profile => $_getN(1);
  @$pb.TagNumber(2)
  set profile($1.ProfileRef value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasProfile() => $_has(1);
  @$pb.TagNumber(2)
  void clearProfile() => $_clearField(2);
  @$pb.TagNumber(2)
  $1.ProfileRef ensureProfile() => $_ensure(1);

  @$pb.TagNumber(3)
  $core.String get displayName => $_getSZ(2);
  @$pb.TagNumber(3)
  set displayName($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasDisplayName() => $_has(2);
  @$pb.TagNumber(3)
  void clearDisplayName() => $_clearField(3);
}

class RenameProfileResponse extends $pb.GeneratedMessage {
  factory RenameProfileResponse({
    $1.Operation? operation,
  }) {
    final result = create();
    if (operation != null) result.operation = operation;
    return result;
  }

  RenameProfileResponse._();

  factory RenameProfileResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RenameProfileResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RenameProfileResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.Operation>(1, _omitFieldNames ? '' : 'operation',
        subBuilder: $1.Operation.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RenameProfileResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RenameProfileResponse copyWith(
          void Function(RenameProfileResponse) updates) =>
      super.copyWith((message) => updates(message as RenameProfileResponse))
          as RenameProfileResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RenameProfileResponse create() => RenameProfileResponse._();
  @$core.override
  RenameProfileResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static RenameProfileResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RenameProfileResponse>(create);
  static RenameProfileResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $1.Operation get operation => $_getN(0);
  @$pb.TagNumber(1)
  set operation($1.Operation value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasOperation() => $_has(0);
  @$pb.TagNumber(1)
  void clearOperation() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.Operation ensureOperation() => $_ensure(0);
}

class RemoveProfileRequest extends $pb.GeneratedMessage {
  factory RemoveProfileRequest({
    $1.MutationContext? mutation,
    $1.ProfileRef? profile,
  }) {
    final result = create();
    if (mutation != null) result.mutation = mutation;
    if (profile != null) result.profile = profile;
    return result;
  }

  RemoveProfileRequest._();

  factory RemoveProfileRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RemoveProfileRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RemoveProfileRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.MutationContext>(1, _omitFieldNames ? '' : 'mutation',
        subBuilder: $1.MutationContext.create)
    ..aOM<$1.ProfileRef>(2, _omitFieldNames ? '' : 'profile',
        subBuilder: $1.ProfileRef.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RemoveProfileRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RemoveProfileRequest copyWith(void Function(RemoveProfileRequest) updates) =>
      super.copyWith((message) => updates(message as RemoveProfileRequest))
          as RemoveProfileRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RemoveProfileRequest create() => RemoveProfileRequest._();
  @$core.override
  RemoveProfileRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static RemoveProfileRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RemoveProfileRequest>(create);
  static RemoveProfileRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $1.MutationContext get mutation => $_getN(0);
  @$pb.TagNumber(1)
  set mutation($1.MutationContext value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasMutation() => $_has(0);
  @$pb.TagNumber(1)
  void clearMutation() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.MutationContext ensureMutation() => $_ensure(0);

  @$pb.TagNumber(2)
  $1.ProfileRef get profile => $_getN(1);
  @$pb.TagNumber(2)
  set profile($1.ProfileRef value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasProfile() => $_has(1);
  @$pb.TagNumber(2)
  void clearProfile() => $_clearField(2);
  @$pb.TagNumber(2)
  $1.ProfileRef ensureProfile() => $_ensure(1);
}

class RemoveProfileResponse extends $pb.GeneratedMessage {
  factory RemoveProfileResponse({
    $1.Operation? operation,
  }) {
    final result = create();
    if (operation != null) result.operation = operation;
    return result;
  }

  RemoveProfileResponse._();

  factory RemoveProfileResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RemoveProfileResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RemoveProfileResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.Operation>(1, _omitFieldNames ? '' : 'operation',
        subBuilder: $1.Operation.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RemoveProfileResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RemoveProfileResponse copyWith(
          void Function(RemoveProfileResponse) updates) =>
      super.copyWith((message) => updates(message as RemoveProfileResponse))
          as RemoveProfileResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RemoveProfileResponse create() => RemoveProfileResponse._();
  @$core.override
  RemoveProfileResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static RemoveProfileResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RemoveProfileResponse>(create);
  static RemoveProfileResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $1.Operation get operation => $_getN(0);
  @$pb.TagNumber(1)
  set operation($1.Operation value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasOperation() => $_has(0);
  @$pb.TagNumber(1)
  void clearOperation() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.Operation ensureOperation() => $_ensure(0);
}

class GetSessionRequest extends $pb.GeneratedMessage {
  factory GetSessionRequest({
    $1.ProfileRef? profile,
  }) {
    final result = create();
    if (profile != null) result.profile = profile;
    return result;
  }

  GetSessionRequest._();

  factory GetSessionRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory GetSessionRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'GetSessionRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.ProfileRef>(1, _omitFieldNames ? '' : 'profile',
        subBuilder: $1.ProfileRef.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetSessionRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetSessionRequest copyWith(void Function(GetSessionRequest) updates) =>
      super.copyWith((message) => updates(message as GetSessionRequest))
          as GetSessionRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static GetSessionRequest create() => GetSessionRequest._();
  @$core.override
  GetSessionRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static GetSessionRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<GetSessionRequest>(create);
  static GetSessionRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $1.ProfileRef get profile => $_getN(0);
  @$pb.TagNumber(1)
  set profile($1.ProfileRef value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasProfile() => $_has(0);
  @$pb.TagNumber(1)
  void clearProfile() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.ProfileRef ensureProfile() => $_ensure(0);
}

class GetSessionResponse extends $pb.GeneratedMessage {
  factory GetSessionResponse({
    $2.Session? session,
    $1.SnapshotMetadata? metadata,
  }) {
    final result = create();
    if (session != null) result.session = session;
    if (metadata != null) result.metadata = metadata;
    return result;
  }

  GetSessionResponse._();

  factory GetSessionResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory GetSessionResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'GetSessionResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$2.Session>(1, _omitFieldNames ? '' : 'session',
        subBuilder: $2.Session.create)
    ..aOM<$1.SnapshotMetadata>(2, _omitFieldNames ? '' : 'metadata',
        subBuilder: $1.SnapshotMetadata.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetSessionResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetSessionResponse copyWith(void Function(GetSessionResponse) updates) =>
      super.copyWith((message) => updates(message as GetSessionResponse))
          as GetSessionResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static GetSessionResponse create() => GetSessionResponse._();
  @$core.override
  GetSessionResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static GetSessionResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<GetSessionResponse>(create);
  static GetSessionResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $2.Session get session => $_getN(0);
  @$pb.TagNumber(1)
  set session($2.Session value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasSession() => $_has(0);
  @$pb.TagNumber(1)
  void clearSession() => $_clearField(1);
  @$pb.TagNumber(1)
  $2.Session ensureSession() => $_ensure(0);

  @$pb.TagNumber(2)
  $1.SnapshotMetadata get metadata => $_getN(1);
  @$pb.TagNumber(2)
  set metadata($1.SnapshotMetadata value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasMetadata() => $_has(1);
  @$pb.TagNumber(2)
  void clearMetadata() => $_clearField(2);
  @$pb.TagNumber(2)
  $1.SnapshotMetadata ensureMetadata() => $_ensure(1);
}

class RenewSessionRequest extends $pb.GeneratedMessage {
  factory RenewSessionRequest({
    $1.MutationContext? mutation,
    $1.ProfileRef? profile,
  }) {
    final result = create();
    if (mutation != null) result.mutation = mutation;
    if (profile != null) result.profile = profile;
    return result;
  }

  RenewSessionRequest._();

  factory RenewSessionRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RenewSessionRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RenewSessionRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.MutationContext>(1, _omitFieldNames ? '' : 'mutation',
        subBuilder: $1.MutationContext.create)
    ..aOM<$1.ProfileRef>(2, _omitFieldNames ? '' : 'profile',
        subBuilder: $1.ProfileRef.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RenewSessionRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RenewSessionRequest copyWith(void Function(RenewSessionRequest) updates) =>
      super.copyWith((message) => updates(message as RenewSessionRequest))
          as RenewSessionRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RenewSessionRequest create() => RenewSessionRequest._();
  @$core.override
  RenewSessionRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static RenewSessionRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RenewSessionRequest>(create);
  static RenewSessionRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $1.MutationContext get mutation => $_getN(0);
  @$pb.TagNumber(1)
  set mutation($1.MutationContext value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasMutation() => $_has(0);
  @$pb.TagNumber(1)
  void clearMutation() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.MutationContext ensureMutation() => $_ensure(0);

  @$pb.TagNumber(2)
  $1.ProfileRef get profile => $_getN(1);
  @$pb.TagNumber(2)
  set profile($1.ProfileRef value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasProfile() => $_has(1);
  @$pb.TagNumber(2)
  void clearProfile() => $_clearField(2);
  @$pb.TagNumber(2)
  $1.ProfileRef ensureProfile() => $_ensure(1);
}

class RenewSessionResponse extends $pb.GeneratedMessage {
  factory RenewSessionResponse({
    $1.Operation? operation,
  }) {
    final result = create();
    if (operation != null) result.operation = operation;
    return result;
  }

  RenewSessionResponse._();

  factory RenewSessionResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RenewSessionResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RenewSessionResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.Operation>(1, _omitFieldNames ? '' : 'operation',
        subBuilder: $1.Operation.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RenewSessionResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RenewSessionResponse copyWith(void Function(RenewSessionResponse) updates) =>
      super.copyWith((message) => updates(message as RenewSessionResponse))
          as RenewSessionResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RenewSessionResponse create() => RenewSessionResponse._();
  @$core.override
  RenewSessionResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static RenewSessionResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RenewSessionResponse>(create);
  static RenewSessionResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $1.Operation get operation => $_getN(0);
  @$pb.TagNumber(1)
  set operation($1.Operation value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasOperation() => $_has(0);
  @$pb.TagNumber(1)
  void clearOperation() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.Operation ensureOperation() => $_ensure(0);
}

class ListExitNodesRequest extends $pb.GeneratedMessage {
  factory ListExitNodesRequest({
    $1.ProfileRef? profile,
    $1.PageRequest? page,
  }) {
    final result = create();
    if (profile != null) result.profile = profile;
    if (page != null) result.page = page;
    return result;
  }

  ListExitNodesRequest._();

  factory ListExitNodesRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListExitNodesRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListExitNodesRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.ProfileRef>(1, _omitFieldNames ? '' : 'profile',
        subBuilder: $1.ProfileRef.create)
    ..aOM<$1.PageRequest>(2, _omitFieldNames ? '' : 'page',
        subBuilder: $1.PageRequest.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListExitNodesRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListExitNodesRequest copyWith(void Function(ListExitNodesRequest) updates) =>
      super.copyWith((message) => updates(message as ListExitNodesRequest))
          as ListExitNodesRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListExitNodesRequest create() => ListExitNodesRequest._();
  @$core.override
  ListExitNodesRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ListExitNodesRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListExitNodesRequest>(create);
  static ListExitNodesRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $1.ProfileRef get profile => $_getN(0);
  @$pb.TagNumber(1)
  set profile($1.ProfileRef value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasProfile() => $_has(0);
  @$pb.TagNumber(1)
  void clearProfile() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.ProfileRef ensureProfile() => $_ensure(0);

  @$pb.TagNumber(2)
  $1.PageRequest get page => $_getN(1);
  @$pb.TagNumber(2)
  set page($1.PageRequest value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasPage() => $_has(1);
  @$pb.TagNumber(2)
  void clearPage() => $_clearField(2);
  @$pb.TagNumber(2)
  $1.PageRequest ensurePage() => $_ensure(1);
}

class ListExitNodesResponse extends $pb.GeneratedMessage {
  factory ListExitNodesResponse({
    $core.Iterable<$3.ExitNode>? exitNodes,
    $1.PageResponse? page,
  }) {
    final result = create();
    if (exitNodes != null) result.exitNodes.addAll(exitNodes);
    if (page != null) result.page = page;
    return result;
  }

  ListExitNodesResponse._();

  factory ListExitNodesResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListExitNodesResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListExitNodesResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..pPM<$3.ExitNode>(1, _omitFieldNames ? '' : 'exitNodes',
        subBuilder: $3.ExitNode.create)
    ..aOM<$1.PageResponse>(2, _omitFieldNames ? '' : 'page',
        subBuilder: $1.PageResponse.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListExitNodesResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListExitNodesResponse copyWith(
          void Function(ListExitNodesResponse) updates) =>
      super.copyWith((message) => updates(message as ListExitNodesResponse))
          as ListExitNodesResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListExitNodesResponse create() => ListExitNodesResponse._();
  @$core.override
  ListExitNodesResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ListExitNodesResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListExitNodesResponse>(create);
  static ListExitNodesResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $pb.PbList<$3.ExitNode> get exitNodes => $_getList(0);

  @$pb.TagNumber(2)
  $1.PageResponse get page => $_getN(1);
  @$pb.TagNumber(2)
  set page($1.PageResponse value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasPage() => $_has(1);
  @$pb.TagNumber(2)
  void clearPage() => $_clearField(2);
  @$pb.TagNumber(2)
  $1.PageResponse ensurePage() => $_ensure(1);
}

class GetExitNodeRequest extends $pb.GeneratedMessage {
  factory GetExitNodeRequest({
    $1.ProfileRef? profile,
  }) {
    final result = create();
    if (profile != null) result.profile = profile;
    return result;
  }

  GetExitNodeRequest._();

  factory GetExitNodeRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory GetExitNodeRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'GetExitNodeRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.ProfileRef>(1, _omitFieldNames ? '' : 'profile',
        subBuilder: $1.ProfileRef.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetExitNodeRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetExitNodeRequest copyWith(void Function(GetExitNodeRequest) updates) =>
      super.copyWith((message) => updates(message as GetExitNodeRequest))
          as GetExitNodeRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static GetExitNodeRequest create() => GetExitNodeRequest._();
  @$core.override
  GetExitNodeRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static GetExitNodeRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<GetExitNodeRequest>(create);
  static GetExitNodeRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $1.ProfileRef get profile => $_getN(0);
  @$pb.TagNumber(1)
  set profile($1.ProfileRef value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasProfile() => $_has(0);
  @$pb.TagNumber(1)
  void clearProfile() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.ProfileRef ensureProfile() => $_ensure(0);
}

class GetExitNodeResponse extends $pb.GeneratedMessage {
  factory GetExitNodeResponse({
    $3.ExitNodeStatus? status,
  }) {
    final result = create();
    if (status != null) result.status = status;
    return result;
  }

  GetExitNodeResponse._();

  factory GetExitNodeResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory GetExitNodeResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'GetExitNodeResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$3.ExitNodeStatus>(1, _omitFieldNames ? '' : 'status',
        subBuilder: $3.ExitNodeStatus.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetExitNodeResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetExitNodeResponse copyWith(void Function(GetExitNodeResponse) updates) =>
      super.copyWith((message) => updates(message as GetExitNodeResponse))
          as GetExitNodeResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static GetExitNodeResponse create() => GetExitNodeResponse._();
  @$core.override
  GetExitNodeResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static GetExitNodeResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<GetExitNodeResponse>(create);
  static GetExitNodeResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $3.ExitNodeStatus get status => $_getN(0);
  @$pb.TagNumber(1)
  set status($3.ExitNodeStatus value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasStatus() => $_has(0);
  @$pb.TagNumber(1)
  void clearStatus() => $_clearField(1);
  @$pb.TagNumber(1)
  $3.ExitNodeStatus ensureStatus() => $_ensure(0);
}

class SelectExitNodeRequest extends $pb.GeneratedMessage {
  factory SelectExitNodeRequest({
    $1.MutationContext? mutation,
    $1.ProfileRef? profile,
    $core.String? exitNodeId,
    $3.LanAccess? lanAccess,
    $3.ExitFamilyMode? familyMode,
  }) {
    final result = create();
    if (mutation != null) result.mutation = mutation;
    if (profile != null) result.profile = profile;
    if (exitNodeId != null) result.exitNodeId = exitNodeId;
    if (lanAccess != null) result.lanAccess = lanAccess;
    if (familyMode != null) result.familyMode = familyMode;
    return result;
  }

  SelectExitNodeRequest._();

  factory SelectExitNodeRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory SelectExitNodeRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SelectExitNodeRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.MutationContext>(1, _omitFieldNames ? '' : 'mutation',
        subBuilder: $1.MutationContext.create)
    ..aOM<$1.ProfileRef>(2, _omitFieldNames ? '' : 'profile',
        subBuilder: $1.ProfileRef.create)
    ..aOS(3, _omitFieldNames ? '' : 'exitNodeId')
    ..aE<$3.LanAccess>(4, _omitFieldNames ? '' : 'lanAccess',
        enumValues: $3.LanAccess.values)
    ..aE<$3.ExitFamilyMode>(5, _omitFieldNames ? '' : 'familyMode',
        enumValues: $3.ExitFamilyMode.values)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SelectExitNodeRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SelectExitNodeRequest copyWith(
          void Function(SelectExitNodeRequest) updates) =>
      super.copyWith((message) => updates(message as SelectExitNodeRequest))
          as SelectExitNodeRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static SelectExitNodeRequest create() => SelectExitNodeRequest._();
  @$core.override
  SelectExitNodeRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static SelectExitNodeRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SelectExitNodeRequest>(create);
  static SelectExitNodeRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $1.MutationContext get mutation => $_getN(0);
  @$pb.TagNumber(1)
  set mutation($1.MutationContext value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasMutation() => $_has(0);
  @$pb.TagNumber(1)
  void clearMutation() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.MutationContext ensureMutation() => $_ensure(0);

  @$pb.TagNumber(2)
  $1.ProfileRef get profile => $_getN(1);
  @$pb.TagNumber(2)
  set profile($1.ProfileRef value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasProfile() => $_has(1);
  @$pb.TagNumber(2)
  void clearProfile() => $_clearField(2);
  @$pb.TagNumber(2)
  $1.ProfileRef ensureProfile() => $_ensure(1);

  @$pb.TagNumber(3)
  $core.String get exitNodeId => $_getSZ(2);
  @$pb.TagNumber(3)
  set exitNodeId($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasExitNodeId() => $_has(2);
  @$pb.TagNumber(3)
  void clearExitNodeId() => $_clearField(3);

  @$pb.TagNumber(4)
  $3.LanAccess get lanAccess => $_getN(3);
  @$pb.TagNumber(4)
  set lanAccess($3.LanAccess value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasLanAccess() => $_has(3);
  @$pb.TagNumber(4)
  void clearLanAccess() => $_clearField(4);

  /// Must be explicitly listed in ExitNode.allowed_family_modes.
  /// UNSPECIFIED/NONE and unknown values fail INVALID_ARGUMENT; no downgrade.
  @$pb.TagNumber(5)
  $3.ExitFamilyMode get familyMode => $_getN(4);
  @$pb.TagNumber(5)
  set familyMode($3.ExitFamilyMode value) => $_setField(5, value);
  @$pb.TagNumber(5)
  $core.bool hasFamilyMode() => $_has(4);
  @$pb.TagNumber(5)
  void clearFamilyMode() => $_clearField(5);
}

class SelectExitNodeResponse extends $pb.GeneratedMessage {
  factory SelectExitNodeResponse({
    $1.Operation? operation,
  }) {
    final result = create();
    if (operation != null) result.operation = operation;
    return result;
  }

  SelectExitNodeResponse._();

  factory SelectExitNodeResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory SelectExitNodeResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SelectExitNodeResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.Operation>(1, _omitFieldNames ? '' : 'operation',
        subBuilder: $1.Operation.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SelectExitNodeResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SelectExitNodeResponse copyWith(
          void Function(SelectExitNodeResponse) updates) =>
      super.copyWith((message) => updates(message as SelectExitNodeResponse))
          as SelectExitNodeResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static SelectExitNodeResponse create() => SelectExitNodeResponse._();
  @$core.override
  SelectExitNodeResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static SelectExitNodeResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SelectExitNodeResponse>(create);
  static SelectExitNodeResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $1.Operation get operation => $_getN(0);
  @$pb.TagNumber(1)
  set operation($1.Operation value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasOperation() => $_has(0);
  @$pb.TagNumber(1)
  void clearOperation() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.Operation ensureOperation() => $_ensure(0);
}

class ClearExitNodeRequest extends $pb.GeneratedMessage {
  factory ClearExitNodeRequest({
    $1.MutationContext? mutation,
    $1.ProfileRef? profile,
  }) {
    final result = create();
    if (mutation != null) result.mutation = mutation;
    if (profile != null) result.profile = profile;
    return result;
  }

  ClearExitNodeRequest._();

  factory ClearExitNodeRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ClearExitNodeRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ClearExitNodeRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.MutationContext>(1, _omitFieldNames ? '' : 'mutation',
        subBuilder: $1.MutationContext.create)
    ..aOM<$1.ProfileRef>(2, _omitFieldNames ? '' : 'profile',
        subBuilder: $1.ProfileRef.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ClearExitNodeRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ClearExitNodeRequest copyWith(void Function(ClearExitNodeRequest) updates) =>
      super.copyWith((message) => updates(message as ClearExitNodeRequest))
          as ClearExitNodeRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ClearExitNodeRequest create() => ClearExitNodeRequest._();
  @$core.override
  ClearExitNodeRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ClearExitNodeRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ClearExitNodeRequest>(create);
  static ClearExitNodeRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $1.MutationContext get mutation => $_getN(0);
  @$pb.TagNumber(1)
  set mutation($1.MutationContext value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasMutation() => $_has(0);
  @$pb.TagNumber(1)
  void clearMutation() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.MutationContext ensureMutation() => $_ensure(0);

  @$pb.TagNumber(2)
  $1.ProfileRef get profile => $_getN(1);
  @$pb.TagNumber(2)
  set profile($1.ProfileRef value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasProfile() => $_has(1);
  @$pb.TagNumber(2)
  void clearProfile() => $_clearField(2);
  @$pb.TagNumber(2)
  $1.ProfileRef ensureProfile() => $_ensure(1);
}

class ClearExitNodeResponse extends $pb.GeneratedMessage {
  factory ClearExitNodeResponse({
    $1.Operation? operation,
  }) {
    final result = create();
    if (operation != null) result.operation = operation;
    return result;
  }

  ClearExitNodeResponse._();

  factory ClearExitNodeResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ClearExitNodeResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ClearExitNodeResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.Operation>(1, _omitFieldNames ? '' : 'operation',
        subBuilder: $1.Operation.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ClearExitNodeResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ClearExitNodeResponse copyWith(
          void Function(ClearExitNodeResponse) updates) =>
      super.copyWith((message) => updates(message as ClearExitNodeResponse))
          as ClearExitNodeResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ClearExitNodeResponse create() => ClearExitNodeResponse._();
  @$core.override
  ClearExitNodeResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ClearExitNodeResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ClearExitNodeResponse>(create);
  static ClearExitNodeResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $1.Operation get operation => $_getN(0);
  @$pb.TagNumber(1)
  set operation($1.Operation value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasOperation() => $_has(0);
  @$pb.TagNumber(1)
  void clearOperation() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.Operation ensureOperation() => $_ensure(0);
}

class GetPreferencesRequest extends $pb.GeneratedMessage {
  factory GetPreferencesRequest({
    $1.ProfileRef? profile,
  }) {
    final result = create();
    if (profile != null) result.profile = profile;
    return result;
  }

  GetPreferencesRequest._();

  factory GetPreferencesRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory GetPreferencesRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'GetPreferencesRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.ProfileRef>(1, _omitFieldNames ? '' : 'profile',
        subBuilder: $1.ProfileRef.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetPreferencesRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetPreferencesRequest copyWith(
          void Function(GetPreferencesRequest) updates) =>
      super.copyWith((message) => updates(message as GetPreferencesRequest))
          as GetPreferencesRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static GetPreferencesRequest create() => GetPreferencesRequest._();
  @$core.override
  GetPreferencesRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static GetPreferencesRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<GetPreferencesRequest>(create);
  static GetPreferencesRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $1.ProfileRef get profile => $_getN(0);
  @$pb.TagNumber(1)
  set profile($1.ProfileRef value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasProfile() => $_has(0);
  @$pb.TagNumber(1)
  void clearProfile() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.ProfileRef ensureProfile() => $_ensure(0);
}

class GetPreferencesResponse extends $pb.GeneratedMessage {
  factory GetPreferencesResponse({
    $3.Preferences? preferences,
  }) {
    final result = create();
    if (preferences != null) result.preferences = preferences;
    return result;
  }

  GetPreferencesResponse._();

  factory GetPreferencesResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory GetPreferencesResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'GetPreferencesResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$3.Preferences>(1, _omitFieldNames ? '' : 'preferences',
        subBuilder: $3.Preferences.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetPreferencesResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetPreferencesResponse copyWith(
          void Function(GetPreferencesResponse) updates) =>
      super.copyWith((message) => updates(message as GetPreferencesResponse))
          as GetPreferencesResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static GetPreferencesResponse create() => GetPreferencesResponse._();
  @$core.override
  GetPreferencesResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static GetPreferencesResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<GetPreferencesResponse>(create);
  static GetPreferencesResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $3.Preferences get preferences => $_getN(0);
  @$pb.TagNumber(1)
  set preferences($3.Preferences value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasPreferences() => $_has(0);
  @$pb.TagNumber(1)
  void clearPreferences() => $_clearField(1);
  @$pb.TagNumber(1)
  $3.Preferences ensurePreferences() => $_ensure(0);
}

class SetPreferencesRequest extends $pb.GeneratedMessage {
  factory SetPreferencesRequest({
    $1.MutationContext? mutation,
    $1.ProfileRef? profile,
    $3.PreferencesPatch? patch,
  }) {
    final result = create();
    if (mutation != null) result.mutation = mutation;
    if (profile != null) result.profile = profile;
    if (patch != null) result.patch = patch;
    return result;
  }

  SetPreferencesRequest._();

  factory SetPreferencesRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory SetPreferencesRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SetPreferencesRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.MutationContext>(1, _omitFieldNames ? '' : 'mutation',
        subBuilder: $1.MutationContext.create)
    ..aOM<$1.ProfileRef>(2, _omitFieldNames ? '' : 'profile',
        subBuilder: $1.ProfileRef.create)
    ..aOM<$3.PreferencesPatch>(3, _omitFieldNames ? '' : 'patch',
        subBuilder: $3.PreferencesPatch.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SetPreferencesRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SetPreferencesRequest copyWith(
          void Function(SetPreferencesRequest) updates) =>
      super.copyWith((message) => updates(message as SetPreferencesRequest))
          as SetPreferencesRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static SetPreferencesRequest create() => SetPreferencesRequest._();
  @$core.override
  SetPreferencesRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static SetPreferencesRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SetPreferencesRequest>(create);
  static SetPreferencesRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $1.MutationContext get mutation => $_getN(0);
  @$pb.TagNumber(1)
  set mutation($1.MutationContext value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasMutation() => $_has(0);
  @$pb.TagNumber(1)
  void clearMutation() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.MutationContext ensureMutation() => $_ensure(0);

  @$pb.TagNumber(2)
  $1.ProfileRef get profile => $_getN(1);
  @$pb.TagNumber(2)
  set profile($1.ProfileRef value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasProfile() => $_has(1);
  @$pb.TagNumber(2)
  void clearProfile() => $_clearField(2);
  @$pb.TagNumber(2)
  $1.ProfileRef ensureProfile() => $_ensure(1);

  @$pb.TagNumber(3)
  $3.PreferencesPatch get patch => $_getN(2);
  @$pb.TagNumber(3)
  set patch($3.PreferencesPatch value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasPatch() => $_has(2);
  @$pb.TagNumber(3)
  void clearPatch() => $_clearField(3);
  @$pb.TagNumber(3)
  $3.PreferencesPatch ensurePatch() => $_ensure(2);
}

class SetPreferencesResponse extends $pb.GeneratedMessage {
  factory SetPreferencesResponse({
    $1.Operation? operation,
  }) {
    final result = create();
    if (operation != null) result.operation = operation;
    return result;
  }

  SetPreferencesResponse._();

  factory SetPreferencesResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory SetPreferencesResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SetPreferencesResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.Operation>(1, _omitFieldNames ? '' : 'operation',
        subBuilder: $1.Operation.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SetPreferencesResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SetPreferencesResponse copyWith(
          void Function(SetPreferencesResponse) updates) =>
      super.copyWith((message) => updates(message as SetPreferencesResponse))
          as SetPreferencesResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static SetPreferencesResponse create() => SetPreferencesResponse._();
  @$core.override
  SetPreferencesResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static SetPreferencesResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SetPreferencesResponse>(create);
  static SetPreferencesResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $1.Operation get operation => $_getN(0);
  @$pb.TagNumber(1)
  set operation($1.Operation value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasOperation() => $_has(0);
  @$pb.TagNumber(1)
  void clearOperation() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.Operation ensureOperation() => $_ensure(0);
}

class ResetPreferencesRequest extends $pb.GeneratedMessage {
  factory ResetPreferencesRequest({
    $1.MutationContext? mutation,
    $1.ProfileRef? profile,
    $core.Iterable<$3.PreferenceKey>? keys,
  }) {
    final result = create();
    if (mutation != null) result.mutation = mutation;
    if (profile != null) result.profile = profile;
    if (keys != null) result.keys.addAll(keys);
    return result;
  }

  ResetPreferencesRequest._();

  factory ResetPreferencesRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ResetPreferencesRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ResetPreferencesRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.MutationContext>(1, _omitFieldNames ? '' : 'mutation',
        subBuilder: $1.MutationContext.create)
    ..aOM<$1.ProfileRef>(2, _omitFieldNames ? '' : 'profile',
        subBuilder: $1.ProfileRef.create)
    ..pc<$3.PreferenceKey>(3, _omitFieldNames ? '' : 'keys', $pb.PbFieldType.KE,
        valueOf: $3.PreferenceKey.valueOf,
        enumValues: $3.PreferenceKey.values,
        defaultEnumValue: $3.PreferenceKey.PREFERENCE_KEY_UNSPECIFIED)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ResetPreferencesRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ResetPreferencesRequest copyWith(
          void Function(ResetPreferencesRequest) updates) =>
      super.copyWith((message) => updates(message as ResetPreferencesRequest))
          as ResetPreferencesRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ResetPreferencesRequest create() => ResetPreferencesRequest._();
  @$core.override
  ResetPreferencesRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ResetPreferencesRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ResetPreferencesRequest>(create);
  static ResetPreferencesRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $1.MutationContext get mutation => $_getN(0);
  @$pb.TagNumber(1)
  set mutation($1.MutationContext value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasMutation() => $_has(0);
  @$pb.TagNumber(1)
  void clearMutation() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.MutationContext ensureMutation() => $_ensure(0);

  @$pb.TagNumber(2)
  $1.ProfileRef get profile => $_getN(1);
  @$pb.TagNumber(2)
  set profile($1.ProfileRef value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasProfile() => $_has(1);
  @$pb.TagNumber(2)
  void clearProfile() => $_clearField(2);
  @$pb.TagNumber(2)
  $1.ProfileRef ensureProfile() => $_ensure(1);

  @$pb.TagNumber(3)
  $pb.PbList<$3.PreferenceKey> get keys => $_getList(2);
}

class ResetPreferencesResponse extends $pb.GeneratedMessage {
  factory ResetPreferencesResponse({
    $1.Operation? operation,
  }) {
    final result = create();
    if (operation != null) result.operation = operation;
    return result;
  }

  ResetPreferencesResponse._();

  factory ResetPreferencesResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ResetPreferencesResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ResetPreferencesResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.Operation>(1, _omitFieldNames ? '' : 'operation',
        subBuilder: $1.Operation.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ResetPreferencesResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ResetPreferencesResponse copyWith(
          void Function(ResetPreferencesResponse) updates) =>
      super.copyWith((message) => updates(message as ResetPreferencesResponse))
          as ResetPreferencesResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ResetPreferencesResponse create() => ResetPreferencesResponse._();
  @$core.override
  ResetPreferencesResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ResetPreferencesResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ResetPreferencesResponse>(create);
  static ResetPreferencesResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $1.Operation get operation => $_getN(0);
  @$pb.TagNumber(1)
  set operation($1.Operation value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasOperation() => $_has(0);
  @$pb.TagNumber(1)
  void clearOperation() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.Operation ensureOperation() => $_ensure(0);
}

class ListManagedSettingsRequest extends $pb.GeneratedMessage {
  factory ListManagedSettingsRequest({
    $1.ProfileRef? profile,
  }) {
    final result = create();
    if (profile != null) result.profile = profile;
    return result;
  }

  ListManagedSettingsRequest._();

  factory ListManagedSettingsRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListManagedSettingsRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListManagedSettingsRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.ProfileRef>(1, _omitFieldNames ? '' : 'profile',
        subBuilder: $1.ProfileRef.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListManagedSettingsRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListManagedSettingsRequest copyWith(
          void Function(ListManagedSettingsRequest) updates) =>
      super.copyWith(
              (message) => updates(message as ListManagedSettingsRequest))
          as ListManagedSettingsRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListManagedSettingsRequest create() => ListManagedSettingsRequest._();
  @$core.override
  ListManagedSettingsRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ListManagedSettingsRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListManagedSettingsRequest>(create);
  static ListManagedSettingsRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $1.ProfileRef get profile => $_getN(0);
  @$pb.TagNumber(1)
  set profile($1.ProfileRef value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasProfile() => $_has(0);
  @$pb.TagNumber(1)
  void clearProfile() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.ProfileRef ensureProfile() => $_ensure(0);
}

class ListManagedSettingsResponse extends $pb.GeneratedMessage {
  factory ListManagedSettingsResponse({
    $core.Iterable<$3.ManagedSetting>? settings,
    $1.SnapshotMetadata? metadata,
  }) {
    final result = create();
    if (settings != null) result.settings.addAll(settings);
    if (metadata != null) result.metadata = metadata;
    return result;
  }

  ListManagedSettingsResponse._();

  factory ListManagedSettingsResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListManagedSettingsResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListManagedSettingsResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..pPM<$3.ManagedSetting>(1, _omitFieldNames ? '' : 'settings',
        subBuilder: $3.ManagedSetting.create)
    ..aOM<$1.SnapshotMetadata>(2, _omitFieldNames ? '' : 'metadata',
        subBuilder: $1.SnapshotMetadata.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListManagedSettingsResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListManagedSettingsResponse copyWith(
          void Function(ListManagedSettingsResponse) updates) =>
      super.copyWith(
              (message) => updates(message as ListManagedSettingsResponse))
          as ListManagedSettingsResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListManagedSettingsResponse create() =>
      ListManagedSettingsResponse._();
  @$core.override
  ListManagedSettingsResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ListManagedSettingsResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListManagedSettingsResponse>(create);
  static ListManagedSettingsResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $pb.PbList<$3.ManagedSetting> get settings => $_getList(0);

  @$pb.TagNumber(2)
  $1.SnapshotMetadata get metadata => $_getN(1);
  @$pb.TagNumber(2)
  set metadata($1.SnapshotMetadata value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasMetadata() => $_has(1);
  @$pb.TagNumber(2)
  void clearMetadata() => $_clearField(2);
  @$pb.TagNumber(2)
  $1.SnapshotMetadata ensureMetadata() => $_ensure(1);
}

class ListResourcesRequest extends $pb.GeneratedMessage {
  factory ListResourcesRequest({
    $1.ProfileRef? profile,
    $1.PageRequest? page,
    $core.String? search,
    $core.Iterable<$3.ResourceKind>? kinds,
  }) {
    final result = create();
    if (profile != null) result.profile = profile;
    if (page != null) result.page = page;
    if (search != null) result.search = search;
    if (kinds != null) result.kinds.addAll(kinds);
    return result;
  }

  ListResourcesRequest._();

  factory ListResourcesRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListResourcesRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListResourcesRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.ProfileRef>(1, _omitFieldNames ? '' : 'profile',
        subBuilder: $1.ProfileRef.create)
    ..aOM<$1.PageRequest>(2, _omitFieldNames ? '' : 'page',
        subBuilder: $1.PageRequest.create)
    ..aOS(3, _omitFieldNames ? '' : 'search')
    ..pc<$3.ResourceKind>(4, _omitFieldNames ? '' : 'kinds', $pb.PbFieldType.KE,
        valueOf: $3.ResourceKind.valueOf,
        enumValues: $3.ResourceKind.values,
        defaultEnumValue: $3.ResourceKind.RESOURCE_KIND_UNSPECIFIED)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListResourcesRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListResourcesRequest copyWith(void Function(ListResourcesRequest) updates) =>
      super.copyWith((message) => updates(message as ListResourcesRequest))
          as ListResourcesRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListResourcesRequest create() => ListResourcesRequest._();
  @$core.override
  ListResourcesRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ListResourcesRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListResourcesRequest>(create);
  static ListResourcesRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $1.ProfileRef get profile => $_getN(0);
  @$pb.TagNumber(1)
  set profile($1.ProfileRef value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasProfile() => $_has(0);
  @$pb.TagNumber(1)
  void clearProfile() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.ProfileRef ensureProfile() => $_ensure(0);

  @$pb.TagNumber(2)
  $1.PageRequest get page => $_getN(1);
  @$pb.TagNumber(2)
  set page($1.PageRequest value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasPage() => $_has(1);
  @$pb.TagNumber(2)
  void clearPage() => $_clearField(2);
  @$pb.TagNumber(2)
  $1.PageRequest ensurePage() => $_ensure(1);

  @$pb.TagNumber(3)
  $core.String get search => $_getSZ(2);
  @$pb.TagNumber(3)
  set search($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasSearch() => $_has(2);
  @$pb.TagNumber(3)
  void clearSearch() => $_clearField(3);

  @$pb.TagNumber(4)
  $pb.PbList<$3.ResourceKind> get kinds => $_getList(3);
}

class ListResourcesResponse extends $pb.GeneratedMessage {
  factory ListResourcesResponse({
    $core.Iterable<$3.Resource>? resources,
    $1.PageResponse? page,
  }) {
    final result = create();
    if (resources != null) result.resources.addAll(resources);
    if (page != null) result.page = page;
    return result;
  }

  ListResourcesResponse._();

  factory ListResourcesResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListResourcesResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListResourcesResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..pPM<$3.Resource>(1, _omitFieldNames ? '' : 'resources',
        subBuilder: $3.Resource.create)
    ..aOM<$1.PageResponse>(2, _omitFieldNames ? '' : 'page',
        subBuilder: $1.PageResponse.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListResourcesResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListResourcesResponse copyWith(
          void Function(ListResourcesResponse) updates) =>
      super.copyWith((message) => updates(message as ListResourcesResponse))
          as ListResourcesResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListResourcesResponse create() => ListResourcesResponse._();
  @$core.override
  ListResourcesResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ListResourcesResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListResourcesResponse>(create);
  static ListResourcesResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $pb.PbList<$3.Resource> get resources => $_getList(0);

  @$pb.TagNumber(2)
  $1.PageResponse get page => $_getN(1);
  @$pb.TagNumber(2)
  set page($1.PageResponse value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasPage() => $_has(1);
  @$pb.TagNumber(2)
  void clearPage() => $_clearField(2);
  @$pb.TagNumber(2)
  $1.PageResponse ensurePage() => $_ensure(1);
}

class SetResourceEnabledRequest extends $pb.GeneratedMessage {
  factory SetResourceEnabledRequest({
    $1.MutationContext? mutation,
    $1.ProfileRef? profile,
    $core.String? resourceId,
    $core.bool? enabled,
  }) {
    final result = create();
    if (mutation != null) result.mutation = mutation;
    if (profile != null) result.profile = profile;
    if (resourceId != null) result.resourceId = resourceId;
    if (enabled != null) result.enabled = enabled;
    return result;
  }

  SetResourceEnabledRequest._();

  factory SetResourceEnabledRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory SetResourceEnabledRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SetResourceEnabledRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.MutationContext>(1, _omitFieldNames ? '' : 'mutation',
        subBuilder: $1.MutationContext.create)
    ..aOM<$1.ProfileRef>(2, _omitFieldNames ? '' : 'profile',
        subBuilder: $1.ProfileRef.create)
    ..aOS(3, _omitFieldNames ? '' : 'resourceId')
    ..aOB(4, _omitFieldNames ? '' : 'enabled')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SetResourceEnabledRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SetResourceEnabledRequest copyWith(
          void Function(SetResourceEnabledRequest) updates) =>
      super.copyWith((message) => updates(message as SetResourceEnabledRequest))
          as SetResourceEnabledRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static SetResourceEnabledRequest create() => SetResourceEnabledRequest._();
  @$core.override
  SetResourceEnabledRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static SetResourceEnabledRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SetResourceEnabledRequest>(create);
  static SetResourceEnabledRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $1.MutationContext get mutation => $_getN(0);
  @$pb.TagNumber(1)
  set mutation($1.MutationContext value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasMutation() => $_has(0);
  @$pb.TagNumber(1)
  void clearMutation() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.MutationContext ensureMutation() => $_ensure(0);

  @$pb.TagNumber(2)
  $1.ProfileRef get profile => $_getN(1);
  @$pb.TagNumber(2)
  set profile($1.ProfileRef value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasProfile() => $_has(1);
  @$pb.TagNumber(2)
  void clearProfile() => $_clearField(2);
  @$pb.TagNumber(2)
  $1.ProfileRef ensureProfile() => $_ensure(1);

  @$pb.TagNumber(3)
  $core.String get resourceId => $_getSZ(2);
  @$pb.TagNumber(3)
  set resourceId($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasResourceId() => $_has(2);
  @$pb.TagNumber(3)
  void clearResourceId() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.bool get enabled => $_getBF(3);
  @$pb.TagNumber(4)
  set enabled($core.bool value) => $_setBool(3, value);
  @$pb.TagNumber(4)
  $core.bool hasEnabled() => $_has(3);
  @$pb.TagNumber(4)
  void clearEnabled() => $_clearField(4);
}

class SetResourceEnabledResponse extends $pb.GeneratedMessage {
  factory SetResourceEnabledResponse({
    $1.Operation? operation,
  }) {
    final result = create();
    if (operation != null) result.operation = operation;
    return result;
  }

  SetResourceEnabledResponse._();

  factory SetResourceEnabledResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory SetResourceEnabledResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SetResourceEnabledResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.Operation>(1, _omitFieldNames ? '' : 'operation',
        subBuilder: $1.Operation.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SetResourceEnabledResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SetResourceEnabledResponse copyWith(
          void Function(SetResourceEnabledResponse) updates) =>
      super.copyWith(
              (message) => updates(message as SetResourceEnabledResponse))
          as SetResourceEnabledResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static SetResourceEnabledResponse create() => SetResourceEnabledResponse._();
  @$core.override
  SetResourceEnabledResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static SetResourceEnabledResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SetResourceEnabledResponse>(create);
  static SetResourceEnabledResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $1.Operation get operation => $_getN(0);
  @$pb.TagNumber(1)
  set operation($1.Operation value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasOperation() => $_has(0);
  @$pb.TagNumber(1)
  void clearOperation() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.Operation ensureOperation() => $_ensure(0);
}

class NotifyLifecycleRequest extends $pb.GeneratedMessage {
  factory NotifyLifecycleRequest({
    $1.MutationContext? mutation,
    $1.ProfileRef? profile,
    $3.LifecycleEvent? event,
  }) {
    final result = create();
    if (mutation != null) result.mutation = mutation;
    if (profile != null) result.profile = profile;
    if (event != null) result.event = event;
    return result;
  }

  NotifyLifecycleRequest._();

  factory NotifyLifecycleRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory NotifyLifecycleRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'NotifyLifecycleRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.MutationContext>(1, _omitFieldNames ? '' : 'mutation',
        subBuilder: $1.MutationContext.create)
    ..aOM<$1.ProfileRef>(2, _omitFieldNames ? '' : 'profile',
        subBuilder: $1.ProfileRef.create)
    ..aE<$3.LifecycleEvent>(3, _omitFieldNames ? '' : 'event',
        enumValues: $3.LifecycleEvent.values)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  NotifyLifecycleRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  NotifyLifecycleRequest copyWith(
          void Function(NotifyLifecycleRequest) updates) =>
      super.copyWith((message) => updates(message as NotifyLifecycleRequest))
          as NotifyLifecycleRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static NotifyLifecycleRequest create() => NotifyLifecycleRequest._();
  @$core.override
  NotifyLifecycleRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static NotifyLifecycleRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<NotifyLifecycleRequest>(create);
  static NotifyLifecycleRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $1.MutationContext get mutation => $_getN(0);
  @$pb.TagNumber(1)
  set mutation($1.MutationContext value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasMutation() => $_has(0);
  @$pb.TagNumber(1)
  void clearMutation() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.MutationContext ensureMutation() => $_ensure(0);

  @$pb.TagNumber(2)
  $1.ProfileRef get profile => $_getN(1);
  @$pb.TagNumber(2)
  set profile($1.ProfileRef value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasProfile() => $_has(1);
  @$pb.TagNumber(2)
  void clearProfile() => $_clearField(2);
  @$pb.TagNumber(2)
  $1.ProfileRef ensureProfile() => $_ensure(1);

  @$pb.TagNumber(3)
  $3.LifecycleEvent get event => $_getN(2);
  @$pb.TagNumber(3)
  set event($3.LifecycleEvent value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasEvent() => $_has(2);
  @$pb.TagNumber(3)
  void clearEvent() => $_clearField(3);
}

class NotifyLifecycleResponse extends $pb.GeneratedMessage {
  factory NotifyLifecycleResponse({
    $1.Operation? operation,
  }) {
    final result = create();
    if (operation != null) result.operation = operation;
    return result;
  }

  NotifyLifecycleResponse._();

  factory NotifyLifecycleResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory NotifyLifecycleResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'NotifyLifecycleResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.Operation>(1, _omitFieldNames ? '' : 'operation',
        subBuilder: $1.Operation.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  NotifyLifecycleResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  NotifyLifecycleResponse copyWith(
          void Function(NotifyLifecycleResponse) updates) =>
      super.copyWith((message) => updates(message as NotifyLifecycleResponse))
          as NotifyLifecycleResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static NotifyLifecycleResponse create() => NotifyLifecycleResponse._();
  @$core.override
  NotifyLifecycleResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static NotifyLifecycleResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<NotifyLifecycleResponse>(create);
  static NotifyLifecycleResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $1.Operation get operation => $_getN(0);
  @$pb.TagNumber(1)
  set operation($1.Operation value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasOperation() => $_has(0);
  @$pb.TagNumber(1)
  void clearOperation() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.Operation ensureOperation() => $_ensure(0);
}

class GetUpdateInfoRequest extends $pb.GeneratedMessage {
  factory GetUpdateInfoRequest({
    $1.BuildIdentity? reportedUi,
  }) {
    final result = create();
    if (reportedUi != null) result.reportedUi = reportedUi;
    return result;
  }

  GetUpdateInfoRequest._();

  factory GetUpdateInfoRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory GetUpdateInfoRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'GetUpdateInfoRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.BuildIdentity>(1, _omitFieldNames ? '' : 'reportedUi',
        subBuilder: $1.BuildIdentity.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetUpdateInfoRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetUpdateInfoRequest copyWith(void Function(GetUpdateInfoRequest) updates) =>
      super.copyWith((message) => updates(message as GetUpdateInfoRequest))
          as GetUpdateInfoRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static GetUpdateInfoRequest create() => GetUpdateInfoRequest._();
  @$core.override
  GetUpdateInfoRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static GetUpdateInfoRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<GetUpdateInfoRequest>(create);
  static GetUpdateInfoRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $1.BuildIdentity get reportedUi => $_getN(0);
  @$pb.TagNumber(1)
  set reportedUi($1.BuildIdentity value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasReportedUi() => $_has(0);
  @$pb.TagNumber(1)
  void clearReportedUi() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.BuildIdentity ensureReportedUi() => $_ensure(0);
}

class GetUpdateInfoResponse extends $pb.GeneratedMessage {
  factory GetUpdateInfoResponse({
    $3.UpdateInfo? info,
  }) {
    final result = create();
    if (info != null) result.info = info;
    return result;
  }

  GetUpdateInfoResponse._();

  factory GetUpdateInfoResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory GetUpdateInfoResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'GetUpdateInfoResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$3.UpdateInfo>(1, _omitFieldNames ? '' : 'info',
        subBuilder: $3.UpdateInfo.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetUpdateInfoResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetUpdateInfoResponse copyWith(
          void Function(GetUpdateInfoResponse) updates) =>
      super.copyWith((message) => updates(message as GetUpdateInfoResponse))
          as GetUpdateInfoResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static GetUpdateInfoResponse create() => GetUpdateInfoResponse._();
  @$core.override
  GetUpdateInfoResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static GetUpdateInfoResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<GetUpdateInfoResponse>(create);
  static GetUpdateInfoResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $3.UpdateInfo get info => $_getN(0);
  @$pb.TagNumber(1)
  set info($3.UpdateInfo value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasInfo() => $_has(0);
  @$pb.TagNumber(1)
  void clearInfo() => $_clearField(1);
  @$pb.TagNumber(1)
  $3.UpdateInfo ensureInfo() => $_ensure(0);
}

class GetSupportInfoRequest extends $pb.GeneratedMessage {
  factory GetSupportInfoRequest() => create();

  GetSupportInfoRequest._();

  factory GetSupportInfoRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory GetSupportInfoRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'GetSupportInfoRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetSupportInfoRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetSupportInfoRequest copyWith(
          void Function(GetSupportInfoRequest) updates) =>
      super.copyWith((message) => updates(message as GetSupportInfoRequest))
          as GetSupportInfoRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static GetSupportInfoRequest create() => GetSupportInfoRequest._();
  @$core.override
  GetSupportInfoRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static GetSupportInfoRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<GetSupportInfoRequest>(create);
  static GetSupportInfoRequest? _defaultInstance;
}

class GetSupportInfoResponse extends $pb.GeneratedMessage {
  factory GetSupportInfoResponse({
    $3.SupportInfo? info,
  }) {
    final result = create();
    if (info != null) result.info = info;
    return result;
  }

  GetSupportInfoResponse._();

  factory GetSupportInfoResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory GetSupportInfoResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'GetSupportInfoResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$3.SupportInfo>(1, _omitFieldNames ? '' : 'info',
        subBuilder: $3.SupportInfo.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetSupportInfoResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetSupportInfoResponse copyWith(
          void Function(GetSupportInfoResponse) updates) =>
      super.copyWith((message) => updates(message as GetSupportInfoResponse))
          as GetSupportInfoResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static GetSupportInfoResponse create() => GetSupportInfoResponse._();
  @$core.override
  GetSupportInfoResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static GetSupportInfoResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<GetSupportInfoResponse>(create);
  static GetSupportInfoResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $3.SupportInfo get info => $_getN(0);
  @$pb.TagNumber(1)
  set info($3.SupportInfo value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasInfo() => $_has(0);
  @$pb.TagNumber(1)
  void clearInfo() => $_clearField(1);
  @$pb.TagNumber(1)
  $3.SupportInfo ensureInfo() => $_ensure(0);
}

enum WatchEventsResponse_Event {
  snapshot,
  statusChanged,
  invalidated,
  operationChanged,
  failure,
  sessionChanged,
  notSet
}

/// First event is always SnapshotEvent. Sequence starts at 1 per connection.
/// Invalidation means fetch the named domain again; no unordered delta merge.
class WatchEventsResponse extends $pb.GeneratedMessage {
  factory WatchEventsResponse({
    $fixnum.Int64? sequence,
    $1.SnapshotMetadata? metadata,
    SnapshotEvent? snapshot,
    $2.Status? statusChanged,
    DomainInvalidated? invalidated,
    $1.Operation? operationChanged,
    $1.Failure? failure,
    $2.Session? sessionChanged,
  }) {
    final result = create();
    if (sequence != null) result.sequence = sequence;
    if (metadata != null) result.metadata = metadata;
    if (snapshot != null) result.snapshot = snapshot;
    if (statusChanged != null) result.statusChanged = statusChanged;
    if (invalidated != null) result.invalidated = invalidated;
    if (operationChanged != null) result.operationChanged = operationChanged;
    if (failure != null) result.failure = failure;
    if (sessionChanged != null) result.sessionChanged = sessionChanged;
    return result;
  }

  WatchEventsResponse._();

  factory WatchEventsResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory WatchEventsResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static const $core.Map<$core.int, WatchEventsResponse_Event>
      _WatchEventsResponse_EventByTag = {
    3: WatchEventsResponse_Event.snapshot,
    4: WatchEventsResponse_Event.statusChanged,
    5: WatchEventsResponse_Event.invalidated,
    6: WatchEventsResponse_Event.operationChanged,
    7: WatchEventsResponse_Event.failure,
    8: WatchEventsResponse_Event.sessionChanged,
    0: WatchEventsResponse_Event.notSet
  };
  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'WatchEventsResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..oo(0, [3, 4, 5, 6, 7, 8])
    ..a<$fixnum.Int64>(
        1, _omitFieldNames ? '' : 'sequence', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..aOM<$1.SnapshotMetadata>(2, _omitFieldNames ? '' : 'metadata',
        subBuilder: $1.SnapshotMetadata.create)
    ..aOM<SnapshotEvent>(3, _omitFieldNames ? '' : 'snapshot',
        subBuilder: SnapshotEvent.create)
    ..aOM<$2.Status>(4, _omitFieldNames ? '' : 'statusChanged',
        subBuilder: $2.Status.create)
    ..aOM<DomainInvalidated>(5, _omitFieldNames ? '' : 'invalidated',
        subBuilder: DomainInvalidated.create)
    ..aOM<$1.Operation>(6, _omitFieldNames ? '' : 'operationChanged',
        subBuilder: $1.Operation.create)
    ..aOM<$1.Failure>(7, _omitFieldNames ? '' : 'failure',
        subBuilder: $1.Failure.create)
    ..aOM<$2.Session>(8, _omitFieldNames ? '' : 'sessionChanged',
        subBuilder: $2.Session.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  WatchEventsResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  WatchEventsResponse copyWith(void Function(WatchEventsResponse) updates) =>
      super.copyWith((message) => updates(message as WatchEventsResponse))
          as WatchEventsResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static WatchEventsResponse create() => WatchEventsResponse._();
  @$core.override
  WatchEventsResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static WatchEventsResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<WatchEventsResponse>(create);
  static WatchEventsResponse? _defaultInstance;

  @$pb.TagNumber(3)
  @$pb.TagNumber(4)
  @$pb.TagNumber(5)
  @$pb.TagNumber(6)
  @$pb.TagNumber(7)
  @$pb.TagNumber(8)
  WatchEventsResponse_Event whichEvent() =>
      _WatchEventsResponse_EventByTag[$_whichOneof(0)]!;
  @$pb.TagNumber(3)
  @$pb.TagNumber(4)
  @$pb.TagNumber(5)
  @$pb.TagNumber(6)
  @$pb.TagNumber(7)
  @$pb.TagNumber(8)
  void clearEvent() => $_clearField($_whichOneof(0));

  @$pb.TagNumber(1)
  $fixnum.Int64 get sequence => $_getI64(0);
  @$pb.TagNumber(1)
  set sequence($fixnum.Int64 value) => $_setInt64(0, value);
  @$pb.TagNumber(1)
  $core.bool hasSequence() => $_has(0);
  @$pb.TagNumber(1)
  void clearSequence() => $_clearField(1);

  @$pb.TagNumber(2)
  $1.SnapshotMetadata get metadata => $_getN(1);
  @$pb.TagNumber(2)
  set metadata($1.SnapshotMetadata value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasMetadata() => $_has(1);
  @$pb.TagNumber(2)
  void clearMetadata() => $_clearField(2);
  @$pb.TagNumber(2)
  $1.SnapshotMetadata ensureMetadata() => $_ensure(1);

  @$pb.TagNumber(3)
  SnapshotEvent get snapshot => $_getN(2);
  @$pb.TagNumber(3)
  set snapshot(SnapshotEvent value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasSnapshot() => $_has(2);
  @$pb.TagNumber(3)
  void clearSnapshot() => $_clearField(3);
  @$pb.TagNumber(3)
  SnapshotEvent ensureSnapshot() => $_ensure(2);

  @$pb.TagNumber(4)
  $2.Status get statusChanged => $_getN(3);
  @$pb.TagNumber(4)
  set statusChanged($2.Status value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasStatusChanged() => $_has(3);
  @$pb.TagNumber(4)
  void clearStatusChanged() => $_clearField(4);
  @$pb.TagNumber(4)
  $2.Status ensureStatusChanged() => $_ensure(3);

  @$pb.TagNumber(5)
  DomainInvalidated get invalidated => $_getN(4);
  @$pb.TagNumber(5)
  set invalidated(DomainInvalidated value) => $_setField(5, value);
  @$pb.TagNumber(5)
  $core.bool hasInvalidated() => $_has(4);
  @$pb.TagNumber(5)
  void clearInvalidated() => $_clearField(5);
  @$pb.TagNumber(5)
  DomainInvalidated ensureInvalidated() => $_ensure(4);

  @$pb.TagNumber(6)
  $1.Operation get operationChanged => $_getN(5);
  @$pb.TagNumber(6)
  set operationChanged($1.Operation value) => $_setField(6, value);
  @$pb.TagNumber(6)
  $core.bool hasOperationChanged() => $_has(5);
  @$pb.TagNumber(6)
  void clearOperationChanged() => $_clearField(6);
  @$pb.TagNumber(6)
  $1.Operation ensureOperationChanged() => $_ensure(5);

  @$pb.TagNumber(7)
  $1.Failure get failure => $_getN(6);
  @$pb.TagNumber(7)
  set failure($1.Failure value) => $_setField(7, value);
  @$pb.TagNumber(7)
  $core.bool hasFailure() => $_has(6);
  @$pb.TagNumber(7)
  void clearFailure() => $_clearField(7);
  @$pb.TagNumber(7)
  $1.Failure ensureFailure() => $_ensure(6);

  @$pb.TagNumber(8)
  $2.Session get sessionChanged => $_getN(7);
  @$pb.TagNumber(8)
  set sessionChanged($2.Session value) => $_setField(8, value);
  @$pb.TagNumber(8)
  $core.bool hasSessionChanged() => $_has(7);
  @$pb.TagNumber(8)
  void clearSessionChanged() => $_clearField(8);
  @$pb.TagNumber(8)
  $2.Session ensureSessionChanged() => $_ensure(7);
}

class SnapshotEvent extends $pb.GeneratedMessage {
  factory SnapshotEvent({
    $1.RuntimeInfo? runtime,
    $2.Status? status,
  }) {
    final result = create();
    if (runtime != null) result.runtime = runtime;
    if (status != null) result.status = status;
    return result;
  }

  SnapshotEvent._();

  factory SnapshotEvent.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory SnapshotEvent.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SnapshotEvent',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.RuntimeInfo>(1, _omitFieldNames ? '' : 'runtime',
        subBuilder: $1.RuntimeInfo.create)
    ..aOM<$2.Status>(2, _omitFieldNames ? '' : 'status',
        subBuilder: $2.Status.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SnapshotEvent clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SnapshotEvent copyWith(void Function(SnapshotEvent) updates) =>
      super.copyWith((message) => updates(message as SnapshotEvent))
          as SnapshotEvent;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static SnapshotEvent create() => SnapshotEvent._();
  @$core.override
  SnapshotEvent createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static SnapshotEvent getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SnapshotEvent>(create);
  static SnapshotEvent? _defaultInstance;

  @$pb.TagNumber(1)
  $1.RuntimeInfo get runtime => $_getN(0);
  @$pb.TagNumber(1)
  set runtime($1.RuntimeInfo value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasRuntime() => $_has(0);
  @$pb.TagNumber(1)
  void clearRuntime() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.RuntimeInfo ensureRuntime() => $_ensure(0);

  @$pb.TagNumber(2)
  $2.Status get status => $_getN(1);
  @$pb.TagNumber(2)
  set status($2.Status value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasStatus() => $_has(1);
  @$pb.TagNumber(2)
  void clearStatus() => $_clearField(2);
  @$pb.TagNumber(2)
  $2.Status ensureStatus() => $_ensure(1);
}

class DomainInvalidated extends $pb.GeneratedMessage {
  factory DomainInvalidated({
    Domain? domain,
    $core.String? profileId,
  }) {
    final result = create();
    if (domain != null) result.domain = domain;
    if (profileId != null) result.profileId = profileId;
    return result;
  }

  DomainInvalidated._();

  factory DomainInvalidated.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory DomainInvalidated.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'DomainInvalidated',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aE<Domain>(1, _omitFieldNames ? '' : 'domain', enumValues: Domain.values)
    ..aOS(2, _omitFieldNames ? '' : 'profileId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DomainInvalidated clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DomainInvalidated copyWith(void Function(DomainInvalidated) updates) =>
      super.copyWith((message) => updates(message as DomainInvalidated))
          as DomainInvalidated;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static DomainInvalidated create() => DomainInvalidated._();
  @$core.override
  DomainInvalidated createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static DomainInvalidated getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<DomainInvalidated>(create);
  static DomainInvalidated? _defaultInstance;

  @$pb.TagNumber(1)
  Domain get domain => $_getN(0);
  @$pb.TagNumber(1)
  set domain(Domain value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasDomain() => $_has(0);
  @$pb.TagNumber(1)
  void clearDomain() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get profileId => $_getSZ(1);
  @$pb.TagNumber(2)
  set profileId($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasProfileId() => $_has(1);
  @$pb.TagNumber(2)
  void clearProfileId() => $_clearField(2);
}

const $core.bool _omitFieldNames =
    $core.bool.fromEnvironment('protobuf.omit_field_names');
const $core.bool _omitMessageNames =
    $core.bool.fromEnvironment('protobuf.omit_message_names');
