// This is a generated file - do not edit.
//
// Generated from client/v0/dashboard.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, prefer_relative_imports

import 'dart:core' as $core;

import 'package:fixnum/fixnum.dart' as $fixnum;
import 'package:protobuf/protobuf.dart' as $pb;

import 'common.pb.dart' as $0;
import 'dashboard.pbenum.dart';

export 'package:protobuf/protobuf.dart' show GeneratedMessageGenericExtensions;

export 'dashboard.pbenum.dart';

class DashboardSpace extends $pb.GeneratedMessage {
  factory DashboardSpace({
    $core.String? accountId,
    $core.String? displayName,
  }) {
    final result = create();
    if (accountId != null) result.accountId = accountId;
    if (displayName != null) result.displayName = displayName;
    return result;
  }

  DashboardSpace._();

  factory DashboardSpace.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory DashboardSpace.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'DashboardSpace',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'accountId')
    ..aOS(2, _omitFieldNames ? '' : 'displayName')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DashboardSpace clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DashboardSpace copyWith(void Function(DashboardSpace) updates) =>
      super.copyWith((message) => updates(message as DashboardSpace))
          as DashboardSpace;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static DashboardSpace create() => DashboardSpace._();
  @$core.override
  DashboardSpace createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static DashboardSpace getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<DashboardSpace>(create);
  static DashboardSpace? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get accountId => $_getSZ(0);
  @$pb.TagNumber(1)
  set accountId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasAccountId() => $_has(0);
  @$pb.TagNumber(1)
  void clearAccountId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get displayName => $_getSZ(1);
  @$pb.TagNumber(2)
  set displayName($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasDisplayName() => $_has(1);
  @$pb.TagNumber(2)
  void clearDisplayName() => $_clearField(2);
}

class DashboardWidget extends $pb.GeneratedMessage {
  factory DashboardWidget({
    DashboardWidgetKind? kind,
    DashboardWidgetSize? size,
  }) {
    final result = create();
    if (kind != null) result.kind = kind;
    if (size != null) result.size = size;
    return result;
  }

  DashboardWidget._();

  factory DashboardWidget.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory DashboardWidget.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'DashboardWidget',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aE<DashboardWidgetKind>(1, _omitFieldNames ? '' : 'kind',
        enumValues: DashboardWidgetKind.values)
    ..aE<DashboardWidgetSize>(2, _omitFieldNames ? '' : 'size',
        enumValues: DashboardWidgetSize.values)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DashboardWidget clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DashboardWidget copyWith(void Function(DashboardWidget) updates) =>
      super.copyWith((message) => updates(message as DashboardWidget))
          as DashboardWidget;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static DashboardWidget create() => DashboardWidget._();
  @$core.override
  DashboardWidget createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static DashboardWidget getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<DashboardWidget>(create);
  static DashboardWidget? _defaultInstance;

  @$pb.TagNumber(1)
  DashboardWidgetKind get kind => $_getN(0);
  @$pb.TagNumber(1)
  set kind(DashboardWidgetKind value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasKind() => $_has(0);
  @$pb.TagNumber(1)
  void clearKind() => $_clearField(1);

  @$pb.TagNumber(2)
  DashboardWidgetSize get size => $_getN(1);
  @$pb.TagNumber(2)
  set size(DashboardWidgetSize value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasSize() => $_has(1);
  @$pb.TagNumber(2)
  void clearSize() => $_clearField(2);
}

class DashboardLayout extends $pb.GeneratedMessage {
  factory DashboardLayout({
    DashboardImage? image,
    $core.Iterable<DashboardWidget>? widgets,
  }) {
    final result = create();
    if (image != null) result.image = image;
    if (widgets != null) result.widgets.addAll(widgets);
    return result;
  }

  DashboardLayout._();

  factory DashboardLayout.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory DashboardLayout.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'DashboardLayout',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aE<DashboardImage>(1, _omitFieldNames ? '' : 'image',
        enumValues: DashboardImage.values)
    ..pPM<DashboardWidget>(2, _omitFieldNames ? '' : 'widgets',
        subBuilder: DashboardWidget.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DashboardLayout clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DashboardLayout copyWith(void Function(DashboardLayout) updates) =>
      super.copyWith((message) => updates(message as DashboardLayout))
          as DashboardLayout;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static DashboardLayout create() => DashboardLayout._();
  @$core.override
  DashboardLayout createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static DashboardLayout getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<DashboardLayout>(create);
  static DashboardLayout? _defaultInstance;

  @$pb.TagNumber(1)
  DashboardImage get image => $_getN(0);
  @$pb.TagNumber(1)
  set image(DashboardImage value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasImage() => $_has(0);
  @$pb.TagNumber(1)
  void clearImage() => $_clearField(1);

  @$pb.TagNumber(2)
  $pb.PbList<DashboardWidget> get widgets => $_getList(1);
}

class DashboardBoard extends $pb.GeneratedMessage {
  factory DashboardBoard({
    $core.String? accountId,
    $fixnum.Int64? revision,
    DashboardLayout? layout,
    $core.bool? canEdit,
  }) {
    final result = create();
    if (accountId != null) result.accountId = accountId;
    if (revision != null) result.revision = revision;
    if (layout != null) result.layout = layout;
    if (canEdit != null) result.canEdit = canEdit;
    return result;
  }

  DashboardBoard._();

  factory DashboardBoard.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory DashboardBoard.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'DashboardBoard',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'accountId')
    ..a<$fixnum.Int64>(
        2, _omitFieldNames ? '' : 'revision', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..aOM<DashboardLayout>(3, _omitFieldNames ? '' : 'layout',
        subBuilder: DashboardLayout.create)
    ..aOB(4, _omitFieldNames ? '' : 'canEdit')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DashboardBoard clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DashboardBoard copyWith(void Function(DashboardBoard) updates) =>
      super.copyWith((message) => updates(message as DashboardBoard))
          as DashboardBoard;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static DashboardBoard create() => DashboardBoard._();
  @$core.override
  DashboardBoard createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static DashboardBoard getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<DashboardBoard>(create);
  static DashboardBoard? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get accountId => $_getSZ(0);
  @$pb.TagNumber(1)
  set accountId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasAccountId() => $_has(0);
  @$pb.TagNumber(1)
  void clearAccountId() => $_clearField(1);

  @$pb.TagNumber(2)
  $fixnum.Int64 get revision => $_getI64(1);
  @$pb.TagNumber(2)
  set revision($fixnum.Int64 value) => $_setInt64(1, value);
  @$pb.TagNumber(2)
  $core.bool hasRevision() => $_has(1);
  @$pb.TagNumber(2)
  void clearRevision() => $_clearField(2);

  @$pb.TagNumber(3)
  DashboardLayout get layout => $_getN(2);
  @$pb.TagNumber(3)
  set layout(DashboardLayout value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasLayout() => $_has(2);
  @$pb.TagNumber(3)
  void clearLayout() => $_clearField(3);
  @$pb.TagNumber(3)
  DashboardLayout ensureLayout() => $_ensure(2);

  /// Current service result for this profile's native user. Not local authority.
  @$pb.TagNumber(4)
  $core.bool get canEdit => $_getBF(3);
  @$pb.TagNumber(4)
  set canEdit($core.bool value) => $_setBool(3, value);
  @$pb.TagNumber(4)
  $core.bool hasCanEdit() => $_has(3);
  @$pb.TagNumber(4)
  void clearCanEdit() => $_clearField(4);
}

class ListDashboardSpacesRequest extends $pb.GeneratedMessage {
  factory ListDashboardSpacesRequest({
    $0.ProfileRef? profile,
  }) {
    final result = create();
    if (profile != null) result.profile = profile;
    return result;
  }

  ListDashboardSpacesRequest._();

  factory ListDashboardSpacesRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListDashboardSpacesRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListDashboardSpacesRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$0.ProfileRef>(1, _omitFieldNames ? '' : 'profile',
        subBuilder: $0.ProfileRef.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListDashboardSpacesRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListDashboardSpacesRequest copyWith(
          void Function(ListDashboardSpacesRequest) updates) =>
      super.copyWith(
              (message) => updates(message as ListDashboardSpacesRequest))
          as ListDashboardSpacesRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListDashboardSpacesRequest create() => ListDashboardSpacesRequest._();
  @$core.override
  ListDashboardSpacesRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ListDashboardSpacesRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListDashboardSpacesRequest>(create);
  static ListDashboardSpacesRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $0.ProfileRef get profile => $_getN(0);
  @$pb.TagNumber(1)
  set profile($0.ProfileRef value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasProfile() => $_has(0);
  @$pb.TagNumber(1)
  void clearProfile() => $_clearField(1);
  @$pb.TagNumber(1)
  $0.ProfileRef ensureProfile() => $_ensure(0);
}

class ListDashboardSpacesResponse extends $pb.GeneratedMessage {
  factory ListDashboardSpacesResponse({
    $core.Iterable<DashboardSpace>? spaces,
    $core.String? primaryAccountId,
  }) {
    final result = create();
    if (spaces != null) result.spaces.addAll(spaces);
    if (primaryAccountId != null) result.primaryAccountId = primaryAccountId;
    return result;
  }

  ListDashboardSpacesResponse._();

  factory ListDashboardSpacesResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListDashboardSpacesResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListDashboardSpacesResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..pPM<DashboardSpace>(1, _omitFieldNames ? '' : 'spaces',
        subBuilder: DashboardSpace.create)
    ..aOS(2, _omitFieldNames ? '' : 'primaryAccountId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListDashboardSpacesResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListDashboardSpacesResponse copyWith(
          void Function(ListDashboardSpacesResponse) updates) =>
      super.copyWith(
              (message) => updates(message as ListDashboardSpacesResponse))
          as ListDashboardSpacesResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListDashboardSpacesResponse create() =>
      ListDashboardSpacesResponse._();
  @$core.override
  ListDashboardSpacesResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ListDashboardSpacesResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListDashboardSpacesResponse>(create);
  static ListDashboardSpacesResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $pb.PbList<DashboardSpace> get spaces => $_getList(0);

  /// The authoritative personal Account. Empty when not provisioned.
  @$pb.TagNumber(2)
  $core.String get primaryAccountId => $_getSZ(1);
  @$pb.TagNumber(2)
  set primaryAccountId($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasPrimaryAccountId() => $_has(1);
  @$pb.TagNumber(2)
  void clearPrimaryAccountId() => $_clearField(2);
}

class GetDashboardBoardRequest extends $pb.GeneratedMessage {
  factory GetDashboardBoardRequest({
    $0.ProfileRef? profile,
    $core.String? accountId,
  }) {
    final result = create();
    if (profile != null) result.profile = profile;
    if (accountId != null) result.accountId = accountId;
    return result;
  }

  GetDashboardBoardRequest._();

  factory GetDashboardBoardRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory GetDashboardBoardRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'GetDashboardBoardRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$0.ProfileRef>(1, _omitFieldNames ? '' : 'profile',
        subBuilder: $0.ProfileRef.create)
    ..aOS(2, _omitFieldNames ? '' : 'accountId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetDashboardBoardRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetDashboardBoardRequest copyWith(
          void Function(GetDashboardBoardRequest) updates) =>
      super.copyWith((message) => updates(message as GetDashboardBoardRequest))
          as GetDashboardBoardRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static GetDashboardBoardRequest create() => GetDashboardBoardRequest._();
  @$core.override
  GetDashboardBoardRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static GetDashboardBoardRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<GetDashboardBoardRequest>(create);
  static GetDashboardBoardRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $0.ProfileRef get profile => $_getN(0);
  @$pb.TagNumber(1)
  set profile($0.ProfileRef value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasProfile() => $_has(0);
  @$pb.TagNumber(1)
  void clearProfile() => $_clearField(1);
  @$pb.TagNumber(1)
  $0.ProfileRef ensureProfile() => $_ensure(0);

  @$pb.TagNumber(2)
  $core.String get accountId => $_getSZ(1);
  @$pb.TagNumber(2)
  set accountId($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasAccountId() => $_has(1);
  @$pb.TagNumber(2)
  void clearAccountId() => $_clearField(2);
}

class GetDashboardBoardResponse extends $pb.GeneratedMessage {
  factory GetDashboardBoardResponse({
    DashboardBoard? board,
  }) {
    final result = create();
    if (board != null) result.board = board;
    return result;
  }

  GetDashboardBoardResponse._();

  factory GetDashboardBoardResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory GetDashboardBoardResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'GetDashboardBoardResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<DashboardBoard>(1, _omitFieldNames ? '' : 'board',
        subBuilder: DashboardBoard.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetDashboardBoardResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetDashboardBoardResponse copyWith(
          void Function(GetDashboardBoardResponse) updates) =>
      super.copyWith((message) => updates(message as GetDashboardBoardResponse))
          as GetDashboardBoardResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static GetDashboardBoardResponse create() => GetDashboardBoardResponse._();
  @$core.override
  GetDashboardBoardResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static GetDashboardBoardResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<GetDashboardBoardResponse>(create);
  static GetDashboardBoardResponse? _defaultInstance;

  @$pb.TagNumber(1)
  DashboardBoard get board => $_getN(0);
  @$pb.TagNumber(1)
  set board(DashboardBoard value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasBoard() => $_has(0);
  @$pb.TagNumber(1)
  void clearBoard() => $_clearField(1);
  @$pb.TagNumber(1)
  DashboardBoard ensureBoard() => $_ensure(0);
}

class ReplaceDashboardBoardRequest extends $pb.GeneratedMessage {
  factory ReplaceDashboardBoardRequest({
    $0.ProfileRef? profile,
    $core.String? accountId,
    $fixnum.Int64? expectedRevision,
    DashboardLayout? layout,
  }) {
    final result = create();
    if (profile != null) result.profile = profile;
    if (accountId != null) result.accountId = accountId;
    if (expectedRevision != null) result.expectedRevision = expectedRevision;
    if (layout != null) result.layout = layout;
    return result;
  }

  ReplaceDashboardBoardRequest._();

  factory ReplaceDashboardBoardRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ReplaceDashboardBoardRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ReplaceDashboardBoardRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$0.ProfileRef>(1, _omitFieldNames ? '' : 'profile',
        subBuilder: $0.ProfileRef.create)
    ..aOS(2, _omitFieldNames ? '' : 'accountId')
    ..a<$fixnum.Int64>(
        3, _omitFieldNames ? '' : 'expectedRevision', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..aOM<DashboardLayout>(4, _omitFieldNames ? '' : 'layout',
        subBuilder: DashboardLayout.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ReplaceDashboardBoardRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ReplaceDashboardBoardRequest copyWith(
          void Function(ReplaceDashboardBoardRequest) updates) =>
      super.copyWith(
              (message) => updates(message as ReplaceDashboardBoardRequest))
          as ReplaceDashboardBoardRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ReplaceDashboardBoardRequest create() =>
      ReplaceDashboardBoardRequest._();
  @$core.override
  ReplaceDashboardBoardRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ReplaceDashboardBoardRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ReplaceDashboardBoardRequest>(create);
  static ReplaceDashboardBoardRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $0.ProfileRef get profile => $_getN(0);
  @$pb.TagNumber(1)
  set profile($0.ProfileRef value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasProfile() => $_has(0);
  @$pb.TagNumber(1)
  void clearProfile() => $_clearField(1);
  @$pb.TagNumber(1)
  $0.ProfileRef ensureProfile() => $_ensure(0);

  @$pb.TagNumber(2)
  $core.String get accountId => $_getSZ(1);
  @$pb.TagNumber(2)
  set accountId($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasAccountId() => $_has(1);
  @$pb.TagNumber(2)
  void clearAccountId() => $_clearField(2);

  @$pb.TagNumber(3)
  $fixnum.Int64 get expectedRevision => $_getI64(2);
  @$pb.TagNumber(3)
  set expectedRevision($fixnum.Int64 value) => $_setInt64(2, value);
  @$pb.TagNumber(3)
  $core.bool hasExpectedRevision() => $_has(2);
  @$pb.TagNumber(3)
  void clearExpectedRevision() => $_clearField(3);

  @$pb.TagNumber(4)
  DashboardLayout get layout => $_getN(3);
  @$pb.TagNumber(4)
  set layout(DashboardLayout value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasLayout() => $_has(3);
  @$pb.TagNumber(4)
  void clearLayout() => $_clearField(4);
  @$pb.TagNumber(4)
  DashboardLayout ensureLayout() => $_ensure(3);
}

class ReplaceDashboardBoardResponse extends $pb.GeneratedMessage {
  factory ReplaceDashboardBoardResponse({
    DashboardBoard? board,
  }) {
    final result = create();
    if (board != null) result.board = board;
    return result;
  }

  ReplaceDashboardBoardResponse._();

  factory ReplaceDashboardBoardResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ReplaceDashboardBoardResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ReplaceDashboardBoardResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<DashboardBoard>(1, _omitFieldNames ? '' : 'board',
        subBuilder: DashboardBoard.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ReplaceDashboardBoardResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ReplaceDashboardBoardResponse copyWith(
          void Function(ReplaceDashboardBoardResponse) updates) =>
      super.copyWith(
              (message) => updates(message as ReplaceDashboardBoardResponse))
          as ReplaceDashboardBoardResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ReplaceDashboardBoardResponse create() =>
      ReplaceDashboardBoardResponse._();
  @$core.override
  ReplaceDashboardBoardResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ReplaceDashboardBoardResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ReplaceDashboardBoardResponse>(create);
  static ReplaceDashboardBoardResponse? _defaultInstance;

  @$pb.TagNumber(1)
  DashboardBoard get board => $_getN(0);
  @$pb.TagNumber(1)
  set board(DashboardBoard value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasBoard() => $_has(0);
  @$pb.TagNumber(1)
  void clearBoard() => $_clearField(1);
  @$pb.TagNumber(1)
  DashboardBoard ensureBoard() => $_ensure(0);
}

const $core.bool _omitFieldNames =
    $core.bool.fromEnvironment('protobuf.omit_field_names');
const $core.bool _omitMessageNames =
    $core.bool.fromEnvironment('protobuf.omit_message_names');
