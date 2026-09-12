// This is a generated file - do not edit.
//
// Generated from client/v0/common.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, prefer_relative_imports

import 'dart:core' as $core;

import 'package:fixnum/fixnum.dart' as $fixnum;
import 'package:protobuf/protobuf.dart' as $pb;
import 'package:protobuf/well_known_types/google/protobuf/duration.pb.dart'
    as $1;
import 'package:protobuf/well_known_types/google/protobuf/timestamp.pb.dart'
    as $0;

import 'common.pbenum.dart';

export 'package:protobuf/protobuf.dart' show GeneratedMessageGenericExtensions;

export 'common.pbenum.dart';

class Restriction extends $pb.GeneratedMessage {
  factory Restriction({
    Availability? availability,
    $core.String? reasonKey,
    ActionOwner? actionOwner,
  }) {
    final result = create();
    if (availability != null) result.availability = availability;
    if (reasonKey != null) result.reasonKey = reasonKey;
    if (actionOwner != null) result.actionOwner = actionOwner;
    return result;
  }

  Restriction._();

  factory Restriction.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory Restriction.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'Restriction',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aE<Availability>(1, _omitFieldNames ? '' : 'availability',
        enumValues: Availability.values)
    ..aOS(2, _omitFieldNames ? '' : 'reasonKey')
    ..aE<ActionOwner>(3, _omitFieldNames ? '' : 'actionOwner',
        enumValues: ActionOwner.values)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Restriction clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Restriction copyWith(void Function(Restriction) updates) =>
      super.copyWith((message) => updates(message as Restriction))
          as Restriction;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static Restriction create() => Restriction._();
  @$core.override
  Restriction createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static Restriction getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<Restriction>(create);
  static Restriction? _defaultInstance;

  @$pb.TagNumber(1)
  Availability get availability => $_getN(0);
  @$pb.TagNumber(1)
  set availability(Availability value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasAvailability() => $_has(0);
  @$pb.TagNumber(1)
  void clearAvailability() => $_clearField(1);

  /// Stable localization key; never arbitrary server text or a secret.
  @$pb.TagNumber(2)
  $core.String get reasonKey => $_getSZ(1);
  @$pb.TagNumber(2)
  set reasonKey($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasReasonKey() => $_has(1);
  @$pb.TagNumber(2)
  void clearReasonKey() => $_clearField(2);

  @$pb.TagNumber(3)
  ActionOwner get actionOwner => $_getN(2);
  @$pb.TagNumber(3)
  set actionOwner(ActionOwner value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasActionOwner() => $_has(2);
  @$pb.TagNumber(3)
  void clearActionOwner() => $_clearField(3);
}

class CapabilityStatus extends $pb.GeneratedMessage {
  factory CapabilityStatus({
    Capability? capability,
    Restriction? restriction,
    Platform? platform,
  }) {
    final result = create();
    if (capability != null) result.capability = capability;
    if (restriction != null) result.restriction = restriction;
    if (platform != null) result.platform = platform;
    return result;
  }

  CapabilityStatus._();

  factory CapabilityStatus.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory CapabilityStatus.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'CapabilityStatus',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aE<Capability>(1, _omitFieldNames ? '' : 'capability',
        enumValues: Capability.values)
    ..aOM<Restriction>(2, _omitFieldNames ? '' : 'restriction',
        subBuilder: Restriction.create)
    ..aE<Platform>(3, _omitFieldNames ? '' : 'platform',
        enumValues: Platform.values)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  CapabilityStatus clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  CapabilityStatus copyWith(void Function(CapabilityStatus) updates) =>
      super.copyWith((message) => updates(message as CapabilityStatus))
          as CapabilityStatus;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static CapabilityStatus create() => CapabilityStatus._();
  @$core.override
  CapabilityStatus createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static CapabilityStatus getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<CapabilityStatus>(create);
  static CapabilityStatus? _defaultInstance;

  @$pb.TagNumber(1)
  Capability get capability => $_getN(0);
  @$pb.TagNumber(1)
  set capability(Capability value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasCapability() => $_has(0);
  @$pb.TagNumber(1)
  void clearCapability() => $_clearField(1);

  @$pb.TagNumber(2)
  Restriction get restriction => $_getN(1);
  @$pb.TagNumber(2)
  set restriction(Restriction value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasRestriction() => $_has(1);
  @$pb.TagNumber(2)
  void clearRestriction() => $_clearField(2);
  @$pb.TagNumber(2)
  Restriction ensureRestriction() => $_ensure(1);

  /// Indicates applicability on this runtime, not a promise of release support.
  @$pb.TagNumber(3)
  Platform get platform => $_getN(2);
  @$pb.TagNumber(3)
  set platform(Platform value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasPlatform() => $_has(2);
  @$pb.TagNumber(3)
  void clearPlatform() => $_clearField(3);
}

class BuildIdentity extends $pb.GeneratedMessage {
  factory BuildIdentity({
    $core.String? version,
    $core.String? commit,
    $core.String? buildDate,
    Platform? platform,
    $core.String? architecture,
  }) {
    final result = create();
    if (version != null) result.version = version;
    if (commit != null) result.commit = commit;
    if (buildDate != null) result.buildDate = buildDate;
    if (platform != null) result.platform = platform;
    if (architecture != null) result.architecture = architecture;
    return result;
  }

  BuildIdentity._();

  factory BuildIdentity.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory BuildIdentity.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'BuildIdentity',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'version')
    ..aOS(2, _omitFieldNames ? '' : 'commit')
    ..aOS(3, _omitFieldNames ? '' : 'buildDate')
    ..aE<Platform>(4, _omitFieldNames ? '' : 'platform',
        enumValues: Platform.values)
    ..aOS(5, _omitFieldNames ? '' : 'architecture')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  BuildIdentity clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  BuildIdentity copyWith(void Function(BuildIdentity) updates) =>
      super.copyWith((message) => updates(message as BuildIdentity))
          as BuildIdentity;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static BuildIdentity create() => BuildIdentity._();
  @$core.override
  BuildIdentity createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static BuildIdentity getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<BuildIdentity>(create);
  static BuildIdentity? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get version => $_getSZ(0);
  @$pb.TagNumber(1)
  set version($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasVersion() => $_has(0);
  @$pb.TagNumber(1)
  void clearVersion() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get commit => $_getSZ(1);
  @$pb.TagNumber(2)
  set commit($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasCommit() => $_has(1);
  @$pb.TagNumber(2)
  void clearCommit() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get buildDate => $_getSZ(2);
  @$pb.TagNumber(3)
  set buildDate($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasBuildDate() => $_has(2);
  @$pb.TagNumber(3)
  void clearBuildDate() => $_clearField(3);

  @$pb.TagNumber(4)
  Platform get platform => $_getN(3);
  @$pb.TagNumber(4)
  set platform(Platform value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasPlatform() => $_has(3);
  @$pb.TagNumber(4)
  void clearPlatform() => $_clearField(4);

  @$pb.TagNumber(5)
  $core.String get architecture => $_getSZ(4);
  @$pb.TagNumber(5)
  set architecture($core.String value) => $_setString(4, value);
  @$pb.TagNumber(5)
  $core.bool hasArchitecture() => $_has(4);
  @$pb.TagNumber(5)
  void clearArchitecture() => $_clearField(5);
}

class RuntimeInfo extends $pb.GeneratedMessage {
  factory RuntimeInfo({
    BuildIdentity? build,
    $core.String? instanceId,
    $core.Iterable<CapabilityStatus>? capabilities,
    Access? callerAccess,
    $core.String? contractSha256,
    $core.String? protocol,
    $core.int? ipcVersion,
  }) {
    final result = create();
    if (build != null) result.build = build;
    if (instanceId != null) result.instanceId = instanceId;
    if (capabilities != null) result.capabilities.addAll(capabilities);
    if (callerAccess != null) result.callerAccess = callerAccess;
    if (contractSha256 != null) result.contractSha256 = contractSha256;
    if (protocol != null) result.protocol = protocol;
    if (ipcVersion != null) result.ipcVersion = ipcVersion;
    return result;
  }

  RuntimeInfo._();

  factory RuntimeInfo.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RuntimeInfo.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RuntimeInfo',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<BuildIdentity>(1, _omitFieldNames ? '' : 'build',
        subBuilder: BuildIdentity.create)
    ..aOS(2, _omitFieldNames ? '' : 'instanceId')
    ..pPM<CapabilityStatus>(3, _omitFieldNames ? '' : 'capabilities',
        subBuilder: CapabilityStatus.create)
    ..aE<Access>(4, _omitFieldNames ? '' : 'callerAccess',
        enumValues: Access.values)
    ..aOS(5, _omitFieldNames ? '' : 'contractSha256')
    ..aOS(6, _omitFieldNames ? '' : 'protocol')
    ..aI(7, _omitFieldNames ? '' : 'ipcVersion', fieldType: $pb.PbFieldType.OU3)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RuntimeInfo clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RuntimeInfo copyWith(void Function(RuntimeInfo) updates) =>
      super.copyWith((message) => updates(message as RuntimeInfo))
          as RuntimeInfo;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RuntimeInfo create() => RuntimeInfo._();
  @$core.override
  RuntimeInfo createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static RuntimeInfo getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RuntimeInfo>(create);
  static RuntimeInfo? _defaultInstance;

  @$pb.TagNumber(1)
  BuildIdentity get build => $_getN(0);
  @$pb.TagNumber(1)
  set build(BuildIdentity value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasBuild() => $_has(0);
  @$pb.TagNumber(1)
  void clearBuild() => $_clearField(1);
  @$pb.TagNumber(1)
  BuildIdentity ensureBuild() => $_ensure(0);

  /// Opaque identifier changes on every runtime start.
  @$pb.TagNumber(2)
  $core.String get instanceId => $_getSZ(1);
  @$pb.TagNumber(2)
  set instanceId($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasInstanceId() => $_has(1);
  @$pb.TagNumber(2)
  void clearInstanceId() => $_clearField(2);

  @$pb.TagNumber(3)
  $pb.PbList<CapabilityStatus> get capabilities => $_getList(2);

  @$pb.TagNumber(4)
  Access get callerAccess => $_getN(3);
  @$pb.TagNumber(4)
  set callerAccess(Access value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasCallerAccess() => $_has(3);
  @$pb.TagNumber(4)
  void clearCallerAccess() => $_clearField(4);

  /// Exact contract revision/hash. Version number alone cannot pair artifacts.
  @$pb.TagNumber(5)
  $core.String get contractSha256 => $_getSZ(4);
  @$pb.TagNumber(5)
  set contractSha256($core.String value) => $_setString(4, value);
  @$pb.TagNumber(5)
  $core.bool hasContractSha256() => $_has(4);
  @$pb.TagNumber(5)
  void clearContractSha256() => $_clearField(5);

  @$pb.TagNumber(6)
  $core.String get protocol => $_getSZ(5);
  @$pb.TagNumber(6)
  set protocol($core.String value) => $_setString(5, value);
  @$pb.TagNumber(6)
  $core.bool hasProtocol() => $_has(5);
  @$pb.TagNumber(6)
  void clearProtocol() => $_clearField(6);

  @$pb.TagNumber(7)
  $core.int get ipcVersion => $_getIZ(6);
  @$pb.TagNumber(7)
  set ipcVersion($core.int value) => $_setUnsignedInt32(6, value);
  @$pb.TagNumber(7)
  $core.bool hasIpcVersion() => $_has(6);
  @$pb.TagNumber(7)
  void clearIpcVersion() => $_clearField(7);
}

class SnapshotMetadata extends $pb.GeneratedMessage {
  factory SnapshotMetadata({
    $core.String? instanceId,
    $fixnum.Int64? revision,
    $0.Timestamp? generatedAt,
  }) {
    final result = create();
    if (instanceId != null) result.instanceId = instanceId;
    if (revision != null) result.revision = revision;
    if (generatedAt != null) result.generatedAt = generatedAt;
    return result;
  }

  SnapshotMetadata._();

  factory SnapshotMetadata.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory SnapshotMetadata.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SnapshotMetadata',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'instanceId')
    ..a<$fixnum.Int64>(
        2, _omitFieldNames ? '' : 'revision', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..aOM<$0.Timestamp>(3, _omitFieldNames ? '' : 'generatedAt',
        subBuilder: $0.Timestamp.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SnapshotMetadata clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SnapshotMetadata copyWith(void Function(SnapshotMetadata) updates) =>
      super.copyWith((message) => updates(message as SnapshotMetadata))
          as SnapshotMetadata;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static SnapshotMetadata create() => SnapshotMetadata._();
  @$core.override
  SnapshotMetadata createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static SnapshotMetadata getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SnapshotMetadata>(create);
  static SnapshotMetadata? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get instanceId => $_getSZ(0);
  @$pb.TagNumber(1)
  set instanceId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasInstanceId() => $_has(0);
  @$pb.TagNumber(1)
  void clearInstanceId() => $_clearField(1);

  @$pb.TagNumber(2)
  $fixnum.Int64 get revision => $_getI64(1);
  @$pb.TagNumber(2)
  set revision($fixnum.Int64 value) => $_setInt64(1, value);
  @$pb.TagNumber(2)
  $core.bool hasRevision() => $_has(1);
  @$pb.TagNumber(2)
  void clearRevision() => $_clearField(2);

  @$pb.TagNumber(3)
  $0.Timestamp get generatedAt => $_getN(2);
  @$pb.TagNumber(3)
  set generatedAt($0.Timestamp value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasGeneratedAt() => $_has(2);
  @$pb.TagNumber(3)
  void clearGeneratedAt() => $_clearField(3);
  @$pb.TagNumber(3)
  $0.Timestamp ensureGeneratedAt() => $_ensure(2);
}

class ProfileRef extends $pb.GeneratedMessage {
  factory ProfileRef({
    $core.String? profileId,
  }) {
    final result = create();
    if (profileId != null) result.profileId = profileId;
    return result;
  }

  ProfileRef._();

  factory ProfileRef.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ProfileRef.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ProfileRef',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'profileId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ProfileRef clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ProfileRef copyWith(void Function(ProfileRef) updates) =>
      super.copyWith((message) => updates(message as ProfileRef)) as ProfileRef;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ProfileRef create() => ProfileRef._();
  @$core.override
  ProfileRef createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ProfileRef getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ProfileRef>(create);
  static ProfileRef? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get profileId => $_getSZ(0);
  @$pb.TagNumber(1)
  set profileId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasProfileId() => $_has(0);
  @$pb.TagNumber(1)
  void clearProfileId() => $_clearField(1);
}

/// Mandatory on every mutation. Never contains credentials or claimed roles.
class MutationContext extends $pb.GeneratedMessage {
  factory MutationContext({
    $core.String? requestId,
    $core.String? expectedInstanceId,
    $fixnum.Int64? expectedRevision,
  }) {
    final result = create();
    if (requestId != null) result.requestId = requestId;
    if (expectedInstanceId != null)
      result.expectedInstanceId = expectedInstanceId;
    if (expectedRevision != null) result.expectedRevision = expectedRevision;
    return result;
  }

  MutationContext._();

  factory MutationContext.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory MutationContext.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'MutationContext',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'requestId')
    ..aOS(2, _omitFieldNames ? '' : 'expectedInstanceId')
    ..a<$fixnum.Int64>(
        3, _omitFieldNames ? '' : 'expectedRevision', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  MutationContext clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  MutationContext copyWith(void Function(MutationContext) updates) =>
      super.copyWith((message) => updates(message as MutationContext))
          as MutationContext;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static MutationContext create() => MutationContext._();
  @$core.override
  MutationContext createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static MutationContext getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<MutationContext>(create);
  static MutationContext? _defaultInstance;

  /// UUID, scoped to authenticated installation owner. Durable for at least 24h.
  @$pb.TagNumber(1)
  $core.String get requestId => $_getSZ(0);
  @$pb.TagNumber(1)
  set requestId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasRequestId() => $_has(0);
  @$pb.TagNumber(1)
  void clearRequestId() => $_clearField(1);

  /// Both required; stale values fail before side effects.
  @$pb.TagNumber(2)
  $core.String get expectedInstanceId => $_getSZ(1);
  @$pb.TagNumber(2)
  set expectedInstanceId($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasExpectedInstanceId() => $_has(1);
  @$pb.TagNumber(2)
  void clearExpectedInstanceId() => $_clearField(2);

  @$pb.TagNumber(3)
  $fixnum.Int64 get expectedRevision => $_getI64(2);
  @$pb.TagNumber(3)
  set expectedRevision($fixnum.Int64 value) => $_setInt64(2, value);
  @$pb.TagNumber(3)
  $core.bool hasExpectedRevision() => $_has(2);
  @$pb.TagNumber(3)
  void clearExpectedRevision() => $_clearField(3);
}

class Failure extends $pb.GeneratedMessage {
  factory Failure({
    ErrorCode? code,
    $core.String? reasonKey,
    $core.String? controlRequestId,
    $core.bool? retryable,
    $1.Duration? retryAfter,
    ActionOwner? actionOwner,
    $core.Iterable<$core.String>? fieldPaths,
  }) {
    final result = create();
    if (code != null) result.code = code;
    if (reasonKey != null) result.reasonKey = reasonKey;
    if (controlRequestId != null) result.controlRequestId = controlRequestId;
    if (retryable != null) result.retryable = retryable;
    if (retryAfter != null) result.retryAfter = retryAfter;
    if (actionOwner != null) result.actionOwner = actionOwner;
    if (fieldPaths != null) result.fieldPaths.addAll(fieldPaths);
    return result;
  }

  Failure._();

  factory Failure.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory Failure.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'Failure',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aE<ErrorCode>(1, _omitFieldNames ? '' : 'code',
        enumValues: ErrorCode.values)
    ..aOS(2, _omitFieldNames ? '' : 'reasonKey')
    ..aOS(3, _omitFieldNames ? '' : 'controlRequestId')
    ..aOB(4, _omitFieldNames ? '' : 'retryable')
    ..aOM<$1.Duration>(5, _omitFieldNames ? '' : 'retryAfter',
        subBuilder: $1.Duration.create)
    ..aE<ActionOwner>(6, _omitFieldNames ? '' : 'actionOwner',
        enumValues: ActionOwner.values)
    ..pPS(7, _omitFieldNames ? '' : 'fieldPaths')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Failure clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Failure copyWith(void Function(Failure) updates) =>
      super.copyWith((message) => updates(message as Failure)) as Failure;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static Failure create() => Failure._();
  @$core.override
  Failure createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static Failure getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<Failure>(create);
  static Failure? _defaultInstance;

  @$pb.TagNumber(1)
  ErrorCode get code => $_getN(0);
  @$pb.TagNumber(1)
  set code(ErrorCode value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasCode() => $_has(0);
  @$pb.TagNumber(1)
  void clearCode() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get reasonKey => $_getSZ(1);
  @$pb.TagNumber(2)
  set reasonKey($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasReasonKey() => $_has(1);
  @$pb.TagNumber(2)
  void clearReasonKey() => $_clearField(2);

  /// Correlation IDs only; no diagnostic payload, bearer token or credentials.
  @$pb.TagNumber(3)
  $core.String get controlRequestId => $_getSZ(2);
  @$pb.TagNumber(3)
  set controlRequestId($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasControlRequestId() => $_has(2);
  @$pb.TagNumber(3)
  void clearControlRequestId() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.bool get retryable => $_getBF(3);
  @$pb.TagNumber(4)
  set retryable($core.bool value) => $_setBool(3, value);
  @$pb.TagNumber(4)
  $core.bool hasRetryable() => $_has(3);
  @$pb.TagNumber(4)
  void clearRetryable() => $_clearField(4);

  @$pb.TagNumber(5)
  $1.Duration get retryAfter => $_getN(4);
  @$pb.TagNumber(5)
  set retryAfter($1.Duration value) => $_setField(5, value);
  @$pb.TagNumber(5)
  $core.bool hasRetryAfter() => $_has(4);
  @$pb.TagNumber(5)
  void clearRetryAfter() => $_clearField(5);
  @$pb.TagNumber(5)
  $1.Duration ensureRetryAfter() => $_ensure(4);

  @$pb.TagNumber(6)
  ActionOwner get actionOwner => $_getN(5);
  @$pb.TagNumber(6)
  set actionOwner(ActionOwner value) => $_setField(6, value);
  @$pb.TagNumber(6)
  $core.bool hasActionOwner() => $_has(5);
  @$pb.TagNumber(6)
  void clearActionOwner() => $_clearField(6);

  @$pb.TagNumber(7)
  $pb.PbList<$core.String> get fieldPaths => $_getList(6);
}

class UserAction extends $pb.GeneratedMessage {
  factory UserAction({
    UserAction_Kind? kind,
    $core.String? browserUrl,
    $0.Timestamp? expiresAt,
    $core.String? reasonKey,
  }) {
    final result = create();
    if (kind != null) result.kind = kind;
    if (browserUrl != null) result.browserUrl = browserUrl;
    if (expiresAt != null) result.expiresAt = expiresAt;
    if (reasonKey != null) result.reasonKey = reasonKey;
    return result;
  }

  UserAction._();

  factory UserAction.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory UserAction.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'UserAction',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aE<UserAction_Kind>(1, _omitFieldNames ? '' : 'kind',
        enumValues: UserAction_Kind.values)
    ..aOS(2, _omitFieldNames ? '' : 'browserUrl')
    ..aOM<$0.Timestamp>(3, _omitFieldNames ? '' : 'expiresAt',
        subBuilder: $0.Timestamp.create)
    ..aOS(4, _omitFieldNames ? '' : 'reasonKey')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  UserAction clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  UserAction copyWith(void Function(UserAction) updates) =>
      super.copyWith((message) => updates(message as UserAction)) as UserAction;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static UserAction create() => UserAction._();
  @$core.override
  UserAction createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static UserAction getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<UserAction>(create);
  static UserAction? _defaultInstance;

  @$pb.TagNumber(1)
  UserAction_Kind get kind => $_getN(0);
  @$pb.TagNumber(1)
  set kind(UserAction_Kind value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasKind() => $_has(0);
  @$pb.TagNumber(1)
  void clearKind() => $_clearField(1);

  /// Optional validated HTTPS browser URL; sensitive and excluded from logs.
  @$pb.TagNumber(2)
  $core.String get browserUrl => $_getSZ(1);
  @$pb.TagNumber(2)
  set browserUrl($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasBrowserUrl() => $_has(1);
  @$pb.TagNumber(2)
  void clearBrowserUrl() => $_clearField(2);

  @$pb.TagNumber(3)
  $0.Timestamp get expiresAt => $_getN(2);
  @$pb.TagNumber(3)
  set expiresAt($0.Timestamp value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasExpiresAt() => $_has(2);
  @$pb.TagNumber(3)
  void clearExpiresAt() => $_clearField(3);
  @$pb.TagNumber(3)
  $0.Timestamp ensureExpiresAt() => $_ensure(2);

  @$pb.TagNumber(4)
  $core.String get reasonKey => $_getSZ(3);
  @$pb.TagNumber(4)
  set reasonKey($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasReasonKey() => $_has(3);
  @$pb.TagNumber(4)
  void clearReasonKey() => $_clearField(4);
}

class ChangeResult extends $pb.GeneratedMessage {
  factory ChangeResult({
    $core.bool? changed,
  }) {
    final result = create();
    if (changed != null) result.changed = changed;
    return result;
  }

  ChangeResult._();

  factory ChangeResult.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ChangeResult.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ChangeResult',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOB(1, _omitFieldNames ? '' : 'changed')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ChangeResult clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ChangeResult copyWith(void Function(ChangeResult) updates) =>
      super.copyWith((message) => updates(message as ChangeResult))
          as ChangeResult;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ChangeResult create() => ChangeResult._();
  @$core.override
  ChangeResult createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ChangeResult getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ChangeResult>(create);
  static ChangeResult? _defaultInstance;

  @$pb.TagNumber(1)
  $core.bool get changed => $_getBF(0);
  @$pb.TagNumber(1)
  set changed($core.bool value) => $_setBool(0, value);
  @$pb.TagNumber(1)
  $core.bool hasChanged() => $_has(0);
  @$pb.TagNumber(1)
  void clearChanged() => $_clearField(1);
}

class EnrollmentResult extends $pb.GeneratedMessage {
  factory EnrollmentResult({
    $core.String? profileId,
    $core.String? nodeId,
  }) {
    final result = create();
    if (profileId != null) result.profileId = profileId;
    if (nodeId != null) result.nodeId = nodeId;
    return result;
  }

  EnrollmentResult._();

  factory EnrollmentResult.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory EnrollmentResult.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'EnrollmentResult',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'profileId')
    ..aOS(2, _omitFieldNames ? '' : 'nodeId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  EnrollmentResult clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  EnrollmentResult copyWith(void Function(EnrollmentResult) updates) =>
      super.copyWith((message) => updates(message as EnrollmentResult))
          as EnrollmentResult;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static EnrollmentResult create() => EnrollmentResult._();
  @$core.override
  EnrollmentResult createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static EnrollmentResult getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<EnrollmentResult>(create);
  static EnrollmentResult? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get profileId => $_getSZ(0);
  @$pb.TagNumber(1)
  set profileId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasProfileId() => $_has(0);
  @$pb.TagNumber(1)
  void clearProfileId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get nodeId => $_getSZ(1);
  @$pb.TagNumber(2)
  set nodeId($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasNodeId() => $_has(1);
  @$pb.TagNumber(2)
  void clearNodeId() => $_clearField(2);
}

class SelectionResult extends $pb.GeneratedMessage {
  factory SelectionResult({
    $core.String? selectedId,
  }) {
    final result = create();
    if (selectedId != null) result.selectedId = selectedId;
    return result;
  }

  SelectionResult._();

  factory SelectionResult.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory SelectionResult.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SelectionResult',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'selectedId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SelectionResult clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SelectionResult copyWith(void Function(SelectionResult) updates) =>
      super.copyWith((message) => updates(message as SelectionResult))
          as SelectionResult;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static SelectionResult create() => SelectionResult._();
  @$core.override
  SelectionResult createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static SelectionResult getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SelectionResult>(create);
  static SelectionResult? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get selectedId => $_getSZ(0);
  @$pb.TagNumber(1)
  set selectedId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasSelectedId() => $_has(0);
  @$pb.TagNumber(1)
  void clearSelectedId() => $_clearField(1);
}

class CleanupResult extends $pb.GeneratedMessage {
  factory CleanupResult({
    CleanupOutcome? outcome,
    $core.bool? localRegistrationRemoved,
    $core.String? controlRequestId,
  }) {
    final result = create();
    if (outcome != null) result.outcome = outcome;
    if (localRegistrationRemoved != null)
      result.localRegistrationRemoved = localRegistrationRemoved;
    if (controlRequestId != null) result.controlRequestId = controlRequestId;
    return result;
  }

  CleanupResult._();

  factory CleanupResult.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory CleanupResult.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'CleanupResult',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aE<CleanupOutcome>(1, _omitFieldNames ? '' : 'outcome',
        enumValues: CleanupOutcome.values)
    ..aOB(2, _omitFieldNames ? '' : 'localRegistrationRemoved')
    ..aOS(3, _omitFieldNames ? '' : 'controlRequestId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  CleanupResult clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  CleanupResult copyWith(void Function(CleanupResult) updates) =>
      super.copyWith((message) => updates(message as CleanupResult))
          as CleanupResult;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static CleanupResult create() => CleanupResult._();
  @$core.override
  CleanupResult createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static CleanupResult getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<CleanupResult>(create);
  static CleanupResult? _defaultInstance;

  @$pb.TagNumber(1)
  CleanupOutcome get outcome => $_getN(0);
  @$pb.TagNumber(1)
  set outcome(CleanupOutcome value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasOutcome() => $_has(0);
  @$pb.TagNumber(1)
  void clearOutcome() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.bool get localRegistrationRemoved => $_getBF(1);
  @$pb.TagNumber(2)
  set localRegistrationRemoved($core.bool value) => $_setBool(1, value);
  @$pb.TagNumber(2)
  $core.bool hasLocalRegistrationRemoved() => $_has(1);
  @$pb.TagNumber(2)
  void clearLocalRegistrationRemoved() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get controlRequestId => $_getSZ(2);
  @$pb.TagNumber(3)
  set controlRequestId($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasControlRequestId() => $_has(2);
  @$pb.TagNumber(3)
  void clearControlRequestId() => $_clearField(3);
}

class RenewalResult extends $pb.GeneratedMessage {
  factory RenewalResult({
    $0.Timestamp? expiresAt,
  }) {
    final result = create();
    if (expiresAt != null) result.expiresAt = expiresAt;
    return result;
  }

  RenewalResult._();

  factory RenewalResult.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RenewalResult.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RenewalResult',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$0.Timestamp>(1, _omitFieldNames ? '' : 'expiresAt',
        subBuilder: $0.Timestamp.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RenewalResult clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RenewalResult copyWith(void Function(RenewalResult) updates) =>
      super.copyWith((message) => updates(message as RenewalResult))
          as RenewalResult;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RenewalResult create() => RenewalResult._();
  @$core.override
  RenewalResult createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static RenewalResult getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RenewalResult>(create);
  static RenewalResult? _defaultInstance;

  @$pb.TagNumber(1)
  $0.Timestamp get expiresAt => $_getN(0);
  @$pb.TagNumber(1)
  set expiresAt($0.Timestamp value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasExpiresAt() => $_has(0);
  @$pb.TagNumber(1)
  void clearExpiresAt() => $_clearField(1);
  @$pb.TagNumber(1)
  $0.Timestamp ensureExpiresAt() => $_ensure(0);
}

class BundleResult extends $pb.GeneratedMessage {
  factory BundleResult({
    $core.String? bundleId,
    $0.Timestamp? createdAt,
    $0.Timestamp? expiresAt,
    $fixnum.Int64? sizeBytes,
    $core.String? sha256,
    $core.bool? reused,
  }) {
    final result = create();
    if (bundleId != null) result.bundleId = bundleId;
    if (createdAt != null) result.createdAt = createdAt;
    if (expiresAt != null) result.expiresAt = expiresAt;
    if (sizeBytes != null) result.sizeBytes = sizeBytes;
    if (sha256 != null) result.sha256 = sha256;
    if (reused != null) result.reused = reused;
    return result;
  }

  BundleResult._();

  factory BundleResult.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory BundleResult.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'BundleResult',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'bundleId')
    ..aOM<$0.Timestamp>(2, _omitFieldNames ? '' : 'createdAt',
        subBuilder: $0.Timestamp.create)
    ..aOM<$0.Timestamp>(3, _omitFieldNames ? '' : 'expiresAt',
        subBuilder: $0.Timestamp.create)
    ..a<$fixnum.Int64>(
        4, _omitFieldNames ? '' : 'sizeBytes', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..aOS(5, _omitFieldNames ? '' : 'sha256')
    ..aOB(6, _omitFieldNames ? '' : 'reused')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  BundleResult clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  BundleResult copyWith(void Function(BundleResult) updates) =>
      super.copyWith((message) => updates(message as BundleResult))
          as BundleResult;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static BundleResult create() => BundleResult._();
  @$core.override
  BundleResult createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static BundleResult getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<BundleResult>(create);
  static BundleResult? _defaultInstance;

  /// Opaque, caller-bound handle; never an arbitrary filesystem path.
  @$pb.TagNumber(1)
  $core.String get bundleId => $_getSZ(0);
  @$pb.TagNumber(1)
  set bundleId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasBundleId() => $_has(0);
  @$pb.TagNumber(1)
  void clearBundleId() => $_clearField(1);

  @$pb.TagNumber(2)
  $0.Timestamp get createdAt => $_getN(1);
  @$pb.TagNumber(2)
  set createdAt($0.Timestamp value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasCreatedAt() => $_has(1);
  @$pb.TagNumber(2)
  void clearCreatedAt() => $_clearField(2);
  @$pb.TagNumber(2)
  $0.Timestamp ensureCreatedAt() => $_ensure(1);

  @$pb.TagNumber(3)
  $0.Timestamp get expiresAt => $_getN(2);
  @$pb.TagNumber(3)
  set expiresAt($0.Timestamp value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasExpiresAt() => $_has(2);
  @$pb.TagNumber(3)
  void clearExpiresAt() => $_clearField(3);
  @$pb.TagNumber(3)
  $0.Timestamp ensureExpiresAt() => $_ensure(2);

  @$pb.TagNumber(4)
  $fixnum.Int64 get sizeBytes => $_getI64(3);
  @$pb.TagNumber(4)
  set sizeBytes($fixnum.Int64 value) => $_setInt64(3, value);
  @$pb.TagNumber(4)
  $core.bool hasSizeBytes() => $_has(3);
  @$pb.TagNumber(4)
  void clearSizeBytes() => $_clearField(4);

  @$pb.TagNumber(5)
  $core.String get sha256 => $_getSZ(4);
  @$pb.TagNumber(5)
  set sha256($core.String value) => $_setString(4, value);
  @$pb.TagNumber(5)
  $core.bool hasSha256() => $_has(4);
  @$pb.TagNumber(5)
  void clearSha256() => $_clearField(5);

  @$pb.TagNumber(6)
  $core.bool get reused => $_getBF(5);
  @$pb.TagNumber(6)
  set reused($core.bool value) => $_setBool(5, value);
  @$pb.TagNumber(6)
  $core.bool hasReused() => $_has(5);
  @$pb.TagNumber(6)
  void clearReused() => $_clearField(6);
}

enum Operation_Outcome {
  failure,
  change,
  enrollment,
  selection,
  cleanup,
  renewal,
  bundle,
  notSet
}

class Operation extends $pb.GeneratedMessage {
  factory Operation({
    $core.String? id,
    $core.String? requestId,
    $core.String? profileId,
    OperationState? state,
    SnapshotMetadata? metadata,
    UserAction? userAction,
    ConnectionContinuity? continuity,
    Failure? failure,
    ChangeResult? change,
    EnrollmentResult? enrollment,
    SelectionResult? selection,
    CleanupResult? cleanup,
    RenewalResult? renewal,
    BundleResult? bundle,
  }) {
    final result = create();
    if (id != null) result.id = id;
    if (requestId != null) result.requestId = requestId;
    if (profileId != null) result.profileId = profileId;
    if (state != null) result.state = state;
    if (metadata != null) result.metadata = metadata;
    if (userAction != null) result.userAction = userAction;
    if (continuity != null) result.continuity = continuity;
    if (failure != null) result.failure = failure;
    if (change != null) result.change = change;
    if (enrollment != null) result.enrollment = enrollment;
    if (selection != null) result.selection = selection;
    if (cleanup != null) result.cleanup = cleanup;
    if (renewal != null) result.renewal = renewal;
    if (bundle != null) result.bundle = bundle;
    return result;
  }

  Operation._();

  factory Operation.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory Operation.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static const $core.Map<$core.int, Operation_Outcome> _Operation_OutcomeByTag =
      {
    10: Operation_Outcome.failure,
    11: Operation_Outcome.change,
    12: Operation_Outcome.enrollment,
    13: Operation_Outcome.selection,
    14: Operation_Outcome.cleanup,
    15: Operation_Outcome.renewal,
    16: Operation_Outcome.bundle,
    0: Operation_Outcome.notSet
  };
  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'Operation',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..oo(0, [10, 11, 12, 13, 14, 15, 16])
    ..aOS(1, _omitFieldNames ? '' : 'id')
    ..aOS(2, _omitFieldNames ? '' : 'requestId')
    ..aOS(3, _omitFieldNames ? '' : 'profileId')
    ..aE<OperationState>(4, _omitFieldNames ? '' : 'state',
        enumValues: OperationState.values)
    ..aOM<SnapshotMetadata>(5, _omitFieldNames ? '' : 'metadata',
        subBuilder: SnapshotMetadata.create)
    ..aOM<UserAction>(6, _omitFieldNames ? '' : 'userAction',
        subBuilder: UserAction.create)
    ..aE<ConnectionContinuity>(7, _omitFieldNames ? '' : 'continuity',
        enumValues: ConnectionContinuity.values)
    ..aOM<Failure>(10, _omitFieldNames ? '' : 'failure',
        subBuilder: Failure.create)
    ..aOM<ChangeResult>(11, _omitFieldNames ? '' : 'change',
        subBuilder: ChangeResult.create)
    ..aOM<EnrollmentResult>(12, _omitFieldNames ? '' : 'enrollment',
        subBuilder: EnrollmentResult.create)
    ..aOM<SelectionResult>(13, _omitFieldNames ? '' : 'selection',
        subBuilder: SelectionResult.create)
    ..aOM<CleanupResult>(14, _omitFieldNames ? '' : 'cleanup',
        subBuilder: CleanupResult.create)
    ..aOM<RenewalResult>(15, _omitFieldNames ? '' : 'renewal',
        subBuilder: RenewalResult.create)
    ..aOM<BundleResult>(16, _omitFieldNames ? '' : 'bundle',
        subBuilder: BundleResult.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Operation clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Operation copyWith(void Function(Operation) updates) =>
      super.copyWith((message) => updates(message as Operation)) as Operation;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static Operation create() => Operation._();
  @$core.override
  Operation createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static Operation getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<Operation>(create);
  static Operation? _defaultInstance;

  @$pb.TagNumber(10)
  @$pb.TagNumber(11)
  @$pb.TagNumber(12)
  @$pb.TagNumber(13)
  @$pb.TagNumber(14)
  @$pb.TagNumber(15)
  @$pb.TagNumber(16)
  Operation_Outcome whichOutcome() => _Operation_OutcomeByTag[$_whichOneof(0)]!;
  @$pb.TagNumber(10)
  @$pb.TagNumber(11)
  @$pb.TagNumber(12)
  @$pb.TagNumber(13)
  @$pb.TagNumber(14)
  @$pb.TagNumber(15)
  @$pb.TagNumber(16)
  void clearOutcome() => $_clearField($_whichOneof(0));

  @$pb.TagNumber(1)
  $core.String get id => $_getSZ(0);
  @$pb.TagNumber(1)
  set id($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasId() => $_has(0);
  @$pb.TagNumber(1)
  void clearId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get requestId => $_getSZ(1);
  @$pb.TagNumber(2)
  set requestId($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasRequestId() => $_has(1);
  @$pb.TagNumber(2)
  void clearRequestId() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get profileId => $_getSZ(2);
  @$pb.TagNumber(3)
  set profileId($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasProfileId() => $_has(2);
  @$pb.TagNumber(3)
  void clearProfileId() => $_clearField(3);

  @$pb.TagNumber(4)
  OperationState get state => $_getN(3);
  @$pb.TagNumber(4)
  set state(OperationState value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasState() => $_has(3);
  @$pb.TagNumber(4)
  void clearState() => $_clearField(4);

  @$pb.TagNumber(5)
  SnapshotMetadata get metadata => $_getN(4);
  @$pb.TagNumber(5)
  set metadata(SnapshotMetadata value) => $_setField(5, value);
  @$pb.TagNumber(5)
  $core.bool hasMetadata() => $_has(4);
  @$pb.TagNumber(5)
  void clearMetadata() => $_clearField(5);
  @$pb.TagNumber(5)
  SnapshotMetadata ensureMetadata() => $_ensure(4);

  @$pb.TagNumber(6)
  UserAction get userAction => $_getN(5);
  @$pb.TagNumber(6)
  set userAction(UserAction value) => $_setField(6, value);
  @$pb.TagNumber(6)
  $core.bool hasUserAction() => $_has(5);
  @$pb.TagNumber(6)
  void clearUserAction() => $_clearField(6);
  @$pb.TagNumber(6)
  UserAction ensureUserAction() => $_ensure(5);

  @$pb.TagNumber(7)
  ConnectionContinuity get continuity => $_getN(6);
  @$pb.TagNumber(7)
  set continuity(ConnectionContinuity value) => $_setField(7, value);
  @$pb.TagNumber(7)
  $core.bool hasContinuity() => $_has(6);
  @$pb.TagNumber(7)
  void clearContinuity() => $_clearField(7);

  @$pb.TagNumber(10)
  Failure get failure => $_getN(7);
  @$pb.TagNumber(10)
  set failure(Failure value) => $_setField(10, value);
  @$pb.TagNumber(10)
  $core.bool hasFailure() => $_has(7);
  @$pb.TagNumber(10)
  void clearFailure() => $_clearField(10);
  @$pb.TagNumber(10)
  Failure ensureFailure() => $_ensure(7);

  @$pb.TagNumber(11)
  ChangeResult get change => $_getN(8);
  @$pb.TagNumber(11)
  set change(ChangeResult value) => $_setField(11, value);
  @$pb.TagNumber(11)
  $core.bool hasChange() => $_has(8);
  @$pb.TagNumber(11)
  void clearChange() => $_clearField(11);
  @$pb.TagNumber(11)
  ChangeResult ensureChange() => $_ensure(8);

  @$pb.TagNumber(12)
  EnrollmentResult get enrollment => $_getN(9);
  @$pb.TagNumber(12)
  set enrollment(EnrollmentResult value) => $_setField(12, value);
  @$pb.TagNumber(12)
  $core.bool hasEnrollment() => $_has(9);
  @$pb.TagNumber(12)
  void clearEnrollment() => $_clearField(12);
  @$pb.TagNumber(12)
  EnrollmentResult ensureEnrollment() => $_ensure(9);

  @$pb.TagNumber(13)
  SelectionResult get selection => $_getN(10);
  @$pb.TagNumber(13)
  set selection(SelectionResult value) => $_setField(13, value);
  @$pb.TagNumber(13)
  $core.bool hasSelection() => $_has(10);
  @$pb.TagNumber(13)
  void clearSelection() => $_clearField(13);
  @$pb.TagNumber(13)
  SelectionResult ensureSelection() => $_ensure(10);

  @$pb.TagNumber(14)
  CleanupResult get cleanup => $_getN(11);
  @$pb.TagNumber(14)
  set cleanup(CleanupResult value) => $_setField(14, value);
  @$pb.TagNumber(14)
  $core.bool hasCleanup() => $_has(11);
  @$pb.TagNumber(14)
  void clearCleanup() => $_clearField(14);
  @$pb.TagNumber(14)
  CleanupResult ensureCleanup() => $_ensure(11);

  @$pb.TagNumber(15)
  RenewalResult get renewal => $_getN(12);
  @$pb.TagNumber(15)
  set renewal(RenewalResult value) => $_setField(15, value);
  @$pb.TagNumber(15)
  $core.bool hasRenewal() => $_has(12);
  @$pb.TagNumber(15)
  void clearRenewal() => $_clearField(15);
  @$pb.TagNumber(15)
  RenewalResult ensureRenewal() => $_ensure(12);

  @$pb.TagNumber(16)
  BundleResult get bundle => $_getN(13);
  @$pb.TagNumber(16)
  set bundle(BundleResult value) => $_setField(16, value);
  @$pb.TagNumber(16)
  $core.bool hasBundle() => $_has(13);
  @$pb.TagNumber(16)
  void clearBundle() => $_clearField(16);
  @$pb.TagNumber(16)
  BundleResult ensureBundle() => $_ensure(13);
}

class PageRequest extends $pb.GeneratedMessage {
  factory PageRequest({
    $core.int? pageSize,
    $core.String? pageToken,
  }) {
    final result = create();
    if (pageSize != null) result.pageSize = pageSize;
    if (pageToken != null) result.pageToken = pageToken;
    return result;
  }

  PageRequest._();

  factory PageRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory PageRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'PageRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aI(1, _omitFieldNames ? '' : 'pageSize', fieldType: $pb.PbFieldType.OU3)
    ..aOS(2, _omitFieldNames ? '' : 'pageToken')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  PageRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  PageRequest copyWith(void Function(PageRequest) updates) =>
      super.copyWith((message) => updates(message as PageRequest))
          as PageRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static PageRequest create() => PageRequest._();
  @$core.override
  PageRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static PageRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<PageRequest>(create);
  static PageRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.int get pageSize => $_getIZ(0);
  @$pb.TagNumber(1)
  set pageSize($core.int value) => $_setUnsignedInt32(0, value);
  @$pb.TagNumber(1)
  $core.bool hasPageSize() => $_has(0);
  @$pb.TagNumber(1)
  void clearPageSize() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get pageToken => $_getSZ(1);
  @$pb.TagNumber(2)
  set pageToken($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasPageToken() => $_has(1);
  @$pb.TagNumber(2)
  void clearPageToken() => $_clearField(2);
}

class PageResponse extends $pb.GeneratedMessage {
  factory PageResponse({
    $core.String? nextPageToken,
    SnapshotMetadata? metadata,
  }) {
    final result = create();
    if (nextPageToken != null) result.nextPageToken = nextPageToken;
    if (metadata != null) result.metadata = metadata;
    return result;
  }

  PageResponse._();

  factory PageResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory PageResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'PageResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'nextPageToken')
    ..aOM<SnapshotMetadata>(2, _omitFieldNames ? '' : 'metadata',
        subBuilder: SnapshotMetadata.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  PageResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  PageResponse copyWith(void Function(PageResponse) updates) =>
      super.copyWith((message) => updates(message as PageResponse))
          as PageResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static PageResponse create() => PageResponse._();
  @$core.override
  PageResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static PageResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<PageResponse>(create);
  static PageResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get nextPageToken => $_getSZ(0);
  @$pb.TagNumber(1)
  set nextPageToken($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasNextPageToken() => $_has(0);
  @$pb.TagNumber(1)
  void clearNextPageToken() => $_clearField(1);

  @$pb.TagNumber(2)
  SnapshotMetadata get metadata => $_getN(1);
  @$pb.TagNumber(2)
  set metadata(SnapshotMetadata value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasMetadata() => $_has(1);
  @$pb.TagNumber(2)
  void clearMetadata() => $_clearField(2);
  @$pb.TagNumber(2)
  SnapshotMetadata ensureMetadata() => $_ensure(1);
}

class Common {
  static final access = $pb.Extension<Access>(
      _omitMessageNames ? '' : 'google.protobuf.MethodOptions',
      _omitFieldNames ? '' : 'access',
      51000,
      $pb.PbFieldType.OE,
      defaultOrMaker: Access.ACCESS_UNSPECIFIED,
      valueOf: Access.valueOf,
      enumValues: Access.values);
  static void registerAllExtensions($pb.ExtensionRegistry registry) {
    registry.add(access);
  }
}

const $core.bool _omitFieldNames =
    $core.bool.fromEnvironment('protobuf.omit_field_names');
const $core.bool _omitMessageNames =
    $core.bool.fromEnvironment('protobuf.omit_message_names');
