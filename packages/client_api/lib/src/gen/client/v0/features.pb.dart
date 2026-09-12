// This is a generated file - do not edit.
//
// Generated from client/v0/features.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, prefer_relative_imports

import 'dart:core' as $core;

import 'package:protobuf/protobuf.dart' as $pb;
import 'package:protobuf/well_known_types/google/protobuf/timestamp.pb.dart'
    as $1;

import 'common.pb.dart' as $0;
import 'features.pbenum.dart';

export 'package:protobuf/protobuf.dart' show GeneratedMessageGenericExtensions;

export 'features.pbenum.dart';

class SettingControl extends $pb.GeneratedMessage {
  factory SettingControl({
    SettingSource? source,
    $core.bool? locked,
    $0.Restriction? mutation,
    $core.String? policyId,
  }) {
    final result = create();
    if (source != null) result.source = source;
    if (locked != null) result.locked = locked;
    if (mutation != null) result.mutation = mutation;
    if (policyId != null) result.policyId = policyId;
    return result;
  }

  SettingControl._();

  factory SettingControl.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory SettingControl.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SettingControl',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aE<SettingSource>(1, _omitFieldNames ? '' : 'source',
        enumValues: SettingSource.values)
    ..aOB(2, _omitFieldNames ? '' : 'locked')
    ..aOM<$0.Restriction>(3, _omitFieldNames ? '' : 'mutation',
        subBuilder: $0.Restriction.create)
    ..aOS(4, _omitFieldNames ? '' : 'policyId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SettingControl clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SettingControl copyWith(void Function(SettingControl) updates) =>
      super.copyWith((message) => updates(message as SettingControl))
          as SettingControl;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static SettingControl create() => SettingControl._();
  @$core.override
  SettingControl createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static SettingControl getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SettingControl>(create);
  static SettingControl? _defaultInstance;

  @$pb.TagNumber(1)
  SettingSource get source => $_getN(0);
  @$pb.TagNumber(1)
  set source(SettingSource value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasSource() => $_has(0);
  @$pb.TagNumber(1)
  void clearSource() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.bool get locked => $_getBF(1);
  @$pb.TagNumber(2)
  set locked($core.bool value) => $_setBool(1, value);
  @$pb.TagNumber(2)
  $core.bool hasLocked() => $_has(1);
  @$pb.TagNumber(2)
  void clearLocked() => $_clearField(2);

  @$pb.TagNumber(3)
  $0.Restriction get mutation => $_getN(2);
  @$pb.TagNumber(3)
  set mutation($0.Restriction value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasMutation() => $_has(2);
  @$pb.TagNumber(3)
  void clearMutation() => $_clearField(3);
  @$pb.TagNumber(3)
  $0.Restriction ensureMutation() => $_ensure(2);

  @$pb.TagNumber(4)
  $core.String get policyId => $_getSZ(3);
  @$pb.TagNumber(4)
  set policyId($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasPolicyId() => $_has(3);
  @$pb.TagNumber(4)
  void clearPolicyId() => $_clearField(4);
}

class BooleanSetting extends $pb.GeneratedMessage {
  factory BooleanSetting({
    $core.bool? effective,
    $core.bool? requested,
    SettingControl? control,
  }) {
    final result = create();
    if (effective != null) result.effective = effective;
    if (requested != null) result.requested = requested;
    if (control != null) result.control = control;
    return result;
  }

  BooleanSetting._();

  factory BooleanSetting.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory BooleanSetting.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'BooleanSetting',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOB(1, _omitFieldNames ? '' : 'effective')
    ..aOB(2, _omitFieldNames ? '' : 'requested')
    ..aOM<SettingControl>(3, _omitFieldNames ? '' : 'control',
        subBuilder: SettingControl.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  BooleanSetting clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  BooleanSetting copyWith(void Function(BooleanSetting) updates) =>
      super.copyWith((message) => updates(message as BooleanSetting))
          as BooleanSetting;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static BooleanSetting create() => BooleanSetting._();
  @$core.override
  BooleanSetting createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static BooleanSetting getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<BooleanSetting>(create);
  static BooleanSetting? _defaultInstance;

  @$pb.TagNumber(1)
  $core.bool get effective => $_getBF(0);
  @$pb.TagNumber(1)
  set effective($core.bool value) => $_setBool(0, value);
  @$pb.TagNumber(1)
  $core.bool hasEffective() => $_has(0);
  @$pb.TagNumber(1)
  void clearEffective() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.bool get requested => $_getBF(1);
  @$pb.TagNumber(2)
  set requested($core.bool value) => $_setBool(1, value);
  @$pb.TagNumber(2)
  $core.bool hasRequested() => $_has(1);
  @$pb.TagNumber(2)
  void clearRequested() => $_clearField(2);

  @$pb.TagNumber(3)
  SettingControl get control => $_getN(2);
  @$pb.TagNumber(3)
  set control(SettingControl value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasControl() => $_has(2);
  @$pb.TagNumber(3)
  void clearControl() => $_clearField(3);
  @$pb.TagNumber(3)
  SettingControl ensureControl() => $_ensure(2);
}

class LifecycleSetting extends $pb.GeneratedMessage {
  factory LifecycleSetting({
    LifecycleBehavior? effective,
    LifecycleBehavior? requested,
    SettingControl? control,
    $core.Iterable<LifecycleBehavior>? allowedValues,
  }) {
    final result = create();
    if (effective != null) result.effective = effective;
    if (requested != null) result.requested = requested;
    if (control != null) result.control = control;
    if (allowedValues != null) result.allowedValues.addAll(allowedValues);
    return result;
  }

  LifecycleSetting._();

  factory LifecycleSetting.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory LifecycleSetting.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'LifecycleSetting',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aE<LifecycleBehavior>(1, _omitFieldNames ? '' : 'effective',
        enumValues: LifecycleBehavior.values)
    ..aE<LifecycleBehavior>(2, _omitFieldNames ? '' : 'requested',
        enumValues: LifecycleBehavior.values)
    ..aOM<SettingControl>(3, _omitFieldNames ? '' : 'control',
        subBuilder: SettingControl.create)
    ..pc<LifecycleBehavior>(
        4, _omitFieldNames ? '' : 'allowedValues', $pb.PbFieldType.KE,
        valueOf: LifecycleBehavior.valueOf,
        enumValues: LifecycleBehavior.values,
        defaultEnumValue: LifecycleBehavior.LIFECYCLE_BEHAVIOR_UNSPECIFIED)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  LifecycleSetting clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  LifecycleSetting copyWith(void Function(LifecycleSetting) updates) =>
      super.copyWith((message) => updates(message as LifecycleSetting))
          as LifecycleSetting;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static LifecycleSetting create() => LifecycleSetting._();
  @$core.override
  LifecycleSetting createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static LifecycleSetting getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<LifecycleSetting>(create);
  static LifecycleSetting? _defaultInstance;

  @$pb.TagNumber(1)
  LifecycleBehavior get effective => $_getN(0);
  @$pb.TagNumber(1)
  set effective(LifecycleBehavior value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasEffective() => $_has(0);
  @$pb.TagNumber(1)
  void clearEffective() => $_clearField(1);

  @$pb.TagNumber(2)
  LifecycleBehavior get requested => $_getN(1);
  @$pb.TagNumber(2)
  set requested(LifecycleBehavior value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasRequested() => $_has(1);
  @$pb.TagNumber(2)
  void clearRequested() => $_clearField(2);

  @$pb.TagNumber(3)
  SettingControl get control => $_getN(2);
  @$pb.TagNumber(3)
  set control(SettingControl value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasControl() => $_has(2);
  @$pb.TagNumber(3)
  void clearControl() => $_clearField(3);
  @$pb.TagNumber(3)
  SettingControl ensureControl() => $_ensure(2);

  @$pb.TagNumber(4)
  $pb.PbList<LifecycleBehavior> get allowedValues => $_getList(3);
}

class RuntimeLifecycle extends $pb.GeneratedMessage {
  factory RuntimeLifecycle({
    LifecycleSetting? runtimeStart,
    LifecycleSetting? uiQuit,
    LifecycleSetting? userLogoff,
    LifecycleSetting? suspend,
    LifecycleSetting? resume,
  }) {
    final result = create();
    if (runtimeStart != null) result.runtimeStart = runtimeStart;
    if (uiQuit != null) result.uiQuit = uiQuit;
    if (userLogoff != null) result.userLogoff = userLogoff;
    if (suspend != null) result.suspend = suspend;
    if (resume != null) result.resume = resume;
    return result;
  }

  RuntimeLifecycle._();

  factory RuntimeLifecycle.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RuntimeLifecycle.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RuntimeLifecycle',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<LifecycleSetting>(1, _omitFieldNames ? '' : 'runtimeStart',
        subBuilder: LifecycleSetting.create)
    ..aOM<LifecycleSetting>(2, _omitFieldNames ? '' : 'uiQuit',
        subBuilder: LifecycleSetting.create)
    ..aOM<LifecycleSetting>(3, _omitFieldNames ? '' : 'userLogoff',
        subBuilder: LifecycleSetting.create)
    ..aOM<LifecycleSetting>(4, _omitFieldNames ? '' : 'suspend',
        subBuilder: LifecycleSetting.create)
    ..aOM<LifecycleSetting>(5, _omitFieldNames ? '' : 'resume',
        subBuilder: LifecycleSetting.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RuntimeLifecycle clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RuntimeLifecycle copyWith(void Function(RuntimeLifecycle) updates) =>
      super.copyWith((message) => updates(message as RuntimeLifecycle))
          as RuntimeLifecycle;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RuntimeLifecycle create() => RuntimeLifecycle._();
  @$core.override
  RuntimeLifecycle createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static RuntimeLifecycle getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RuntimeLifecycle>(create);
  static RuntimeLifecycle? _defaultInstance;

  /// These concern tunnel intent, not whether the UI window starts or closes.
  @$pb.TagNumber(1)
  LifecycleSetting get runtimeStart => $_getN(0);
  @$pb.TagNumber(1)
  set runtimeStart(LifecycleSetting value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasRuntimeStart() => $_has(0);
  @$pb.TagNumber(1)
  void clearRuntimeStart() => $_clearField(1);
  @$pb.TagNumber(1)
  LifecycleSetting ensureRuntimeStart() => $_ensure(0);

  @$pb.TagNumber(2)
  LifecycleSetting get uiQuit => $_getN(1);
  @$pb.TagNumber(2)
  set uiQuit(LifecycleSetting value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasUiQuit() => $_has(1);
  @$pb.TagNumber(2)
  void clearUiQuit() => $_clearField(2);
  @$pb.TagNumber(2)
  LifecycleSetting ensureUiQuit() => $_ensure(1);

  @$pb.TagNumber(3)
  LifecycleSetting get userLogoff => $_getN(2);
  @$pb.TagNumber(3)
  set userLogoff(LifecycleSetting value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasUserLogoff() => $_has(2);
  @$pb.TagNumber(3)
  void clearUserLogoff() => $_clearField(3);
  @$pb.TagNumber(3)
  LifecycleSetting ensureUserLogoff() => $_ensure(2);

  @$pb.TagNumber(4)
  LifecycleSetting get suspend => $_getN(3);
  @$pb.TagNumber(4)
  set suspend(LifecycleSetting value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasSuspend() => $_has(3);
  @$pb.TagNumber(4)
  void clearSuspend() => $_clearField(4);
  @$pb.TagNumber(4)
  LifecycleSetting ensureSuspend() => $_ensure(3);

  @$pb.TagNumber(5)
  LifecycleSetting get resume => $_getN(4);
  @$pb.TagNumber(5)
  set resume(LifecycleSetting value) => $_setField(5, value);
  @$pb.TagNumber(5)
  $core.bool hasResume() => $_has(4);
  @$pb.TagNumber(5)
  void clearResume() => $_clearField(5);
  @$pb.TagNumber(5)
  LifecycleSetting ensureResume() => $_ensure(4);
}

class Preferences extends $pb.GeneratedMessage {
  factory Preferences({
    $0.SnapshotMetadata? metadata,
    $core.String? profileId,
    BooleanSetting? allowInbound,
    BooleanSetting? acceptDns,
    BooleanSetting? acceptRoutes,
    RuntimeLifecycle? lifecycle,
  }) {
    final result = create();
    if (metadata != null) result.metadata = metadata;
    if (profileId != null) result.profileId = profileId;
    if (allowInbound != null) result.allowInbound = allowInbound;
    if (acceptDns != null) result.acceptDns = acceptDns;
    if (acceptRoutes != null) result.acceptRoutes = acceptRoutes;
    if (lifecycle != null) result.lifecycle = lifecycle;
    return result;
  }

  Preferences._();

  factory Preferences.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory Preferences.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'Preferences',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$0.SnapshotMetadata>(1, _omitFieldNames ? '' : 'metadata',
        subBuilder: $0.SnapshotMetadata.create)
    ..aOS(2, _omitFieldNames ? '' : 'profileId')
    ..aOM<BooleanSetting>(3, _omitFieldNames ? '' : 'allowInbound',
        subBuilder: BooleanSetting.create)
    ..aOM<BooleanSetting>(4, _omitFieldNames ? '' : 'acceptDns',
        subBuilder: BooleanSetting.create)
    ..aOM<BooleanSetting>(5, _omitFieldNames ? '' : 'acceptRoutes',
        subBuilder: BooleanSetting.create)
    ..aOM<RuntimeLifecycle>(6, _omitFieldNames ? '' : 'lifecycle',
        subBuilder: RuntimeLifecycle.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Preferences clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Preferences copyWith(void Function(Preferences) updates) =>
      super.copyWith((message) => updates(message as Preferences))
          as Preferences;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static Preferences create() => Preferences._();
  @$core.override
  Preferences createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static Preferences getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<Preferences>(create);
  static Preferences? _defaultInstance;

  @$pb.TagNumber(1)
  $0.SnapshotMetadata get metadata => $_getN(0);
  @$pb.TagNumber(1)
  set metadata($0.SnapshotMetadata value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasMetadata() => $_has(0);
  @$pb.TagNumber(1)
  void clearMetadata() => $_clearField(1);
  @$pb.TagNumber(1)
  $0.SnapshotMetadata ensureMetadata() => $_ensure(0);

  @$pb.TagNumber(2)
  $core.String get profileId => $_getSZ(1);
  @$pb.TagNumber(2)
  set profileId($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasProfileId() => $_has(1);
  @$pb.TagNumber(2)
  void clearProfileId() => $_clearField(2);

  /// This switch may only restrict traffic already permitted by backend policy.
  @$pb.TagNumber(3)
  BooleanSetting get allowInbound => $_getN(2);
  @$pb.TagNumber(3)
  set allowInbound(BooleanSetting value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasAllowInbound() => $_has(2);
  @$pb.TagNumber(3)
  void clearAllowInbound() => $_clearField(3);
  @$pb.TagNumber(3)
  BooleanSetting ensureAllowInbound() => $_ensure(2);

  @$pb.TagNumber(4)
  BooleanSetting get acceptDns => $_getN(3);
  @$pb.TagNumber(4)
  set acceptDns(BooleanSetting value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasAcceptDns() => $_has(3);
  @$pb.TagNumber(4)
  void clearAcceptDns() => $_clearField(4);
  @$pb.TagNumber(4)
  BooleanSetting ensureAcceptDns() => $_ensure(3);

  @$pb.TagNumber(5)
  BooleanSetting get acceptRoutes => $_getN(4);
  @$pb.TagNumber(5)
  set acceptRoutes(BooleanSetting value) => $_setField(5, value);
  @$pb.TagNumber(5)
  $core.bool hasAcceptRoutes() => $_has(4);
  @$pb.TagNumber(5)
  void clearAcceptRoutes() => $_clearField(5);
  @$pb.TagNumber(5)
  BooleanSetting ensureAcceptRoutes() => $_ensure(4);

  @$pb.TagNumber(6)
  RuntimeLifecycle get lifecycle => $_getN(5);
  @$pb.TagNumber(6)
  set lifecycle(RuntimeLifecycle value) => $_setField(6, value);
  @$pb.TagNumber(6)
  $core.bool hasLifecycle() => $_has(5);
  @$pb.TagNumber(6)
  void clearLifecycle() => $_clearField(6);
  @$pb.TagNumber(6)
  RuntimeLifecycle ensureLifecycle() => $_ensure(5);
}

/// Presence means "set user preference", omission means "leave unchanged".
/// ResetPreferences removes user overrides instead of setting false/default.
class PreferencesPatch extends $pb.GeneratedMessage {
  factory PreferencesPatch({
    $core.bool? allowInbound,
    $core.bool? acceptDns,
    $core.bool? acceptRoutes,
    LifecycleBehavior? runtimeStart,
    LifecycleBehavior? uiQuit,
    LifecycleBehavior? userLogoff,
    LifecycleBehavior? suspend,
    LifecycleBehavior? resume,
  }) {
    final result = create();
    if (allowInbound != null) result.allowInbound = allowInbound;
    if (acceptDns != null) result.acceptDns = acceptDns;
    if (acceptRoutes != null) result.acceptRoutes = acceptRoutes;
    if (runtimeStart != null) result.runtimeStart = runtimeStart;
    if (uiQuit != null) result.uiQuit = uiQuit;
    if (userLogoff != null) result.userLogoff = userLogoff;
    if (suspend != null) result.suspend = suspend;
    if (resume != null) result.resume = resume;
    return result;
  }

  PreferencesPatch._();

  factory PreferencesPatch.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory PreferencesPatch.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'PreferencesPatch',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOB(1, _omitFieldNames ? '' : 'allowInbound')
    ..aOB(2, _omitFieldNames ? '' : 'acceptDns')
    ..aOB(3, _omitFieldNames ? '' : 'acceptRoutes')
    ..aE<LifecycleBehavior>(4, _omitFieldNames ? '' : 'runtimeStart',
        enumValues: LifecycleBehavior.values)
    ..aE<LifecycleBehavior>(5, _omitFieldNames ? '' : 'uiQuit',
        enumValues: LifecycleBehavior.values)
    ..aE<LifecycleBehavior>(6, _omitFieldNames ? '' : 'userLogoff',
        enumValues: LifecycleBehavior.values)
    ..aE<LifecycleBehavior>(7, _omitFieldNames ? '' : 'suspend',
        enumValues: LifecycleBehavior.values)
    ..aE<LifecycleBehavior>(8, _omitFieldNames ? '' : 'resume',
        enumValues: LifecycleBehavior.values)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  PreferencesPatch clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  PreferencesPatch copyWith(void Function(PreferencesPatch) updates) =>
      super.copyWith((message) => updates(message as PreferencesPatch))
          as PreferencesPatch;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static PreferencesPatch create() => PreferencesPatch._();
  @$core.override
  PreferencesPatch createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static PreferencesPatch getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<PreferencesPatch>(create);
  static PreferencesPatch? _defaultInstance;

  @$pb.TagNumber(1)
  $core.bool get allowInbound => $_getBF(0);
  @$pb.TagNumber(1)
  set allowInbound($core.bool value) => $_setBool(0, value);
  @$pb.TagNumber(1)
  $core.bool hasAllowInbound() => $_has(0);
  @$pb.TagNumber(1)
  void clearAllowInbound() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.bool get acceptDns => $_getBF(1);
  @$pb.TagNumber(2)
  set acceptDns($core.bool value) => $_setBool(1, value);
  @$pb.TagNumber(2)
  $core.bool hasAcceptDns() => $_has(1);
  @$pb.TagNumber(2)
  void clearAcceptDns() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.bool get acceptRoutes => $_getBF(2);
  @$pb.TagNumber(3)
  set acceptRoutes($core.bool value) => $_setBool(2, value);
  @$pb.TagNumber(3)
  $core.bool hasAcceptRoutes() => $_has(2);
  @$pb.TagNumber(3)
  void clearAcceptRoutes() => $_clearField(3);

  @$pb.TagNumber(4)
  LifecycleBehavior get runtimeStart => $_getN(3);
  @$pb.TagNumber(4)
  set runtimeStart(LifecycleBehavior value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasRuntimeStart() => $_has(3);
  @$pb.TagNumber(4)
  void clearRuntimeStart() => $_clearField(4);

  @$pb.TagNumber(5)
  LifecycleBehavior get uiQuit => $_getN(4);
  @$pb.TagNumber(5)
  set uiQuit(LifecycleBehavior value) => $_setField(5, value);
  @$pb.TagNumber(5)
  $core.bool hasUiQuit() => $_has(4);
  @$pb.TagNumber(5)
  void clearUiQuit() => $_clearField(5);

  @$pb.TagNumber(6)
  LifecycleBehavior get userLogoff => $_getN(5);
  @$pb.TagNumber(6)
  set userLogoff(LifecycleBehavior value) => $_setField(6, value);
  @$pb.TagNumber(6)
  $core.bool hasUserLogoff() => $_has(5);
  @$pb.TagNumber(6)
  void clearUserLogoff() => $_clearField(6);

  @$pb.TagNumber(7)
  LifecycleBehavior get suspend => $_getN(6);
  @$pb.TagNumber(7)
  set suspend(LifecycleBehavior value) => $_setField(7, value);
  @$pb.TagNumber(7)
  $core.bool hasSuspend() => $_has(6);
  @$pb.TagNumber(7)
  void clearSuspend() => $_clearField(7);

  @$pb.TagNumber(8)
  LifecycleBehavior get resume => $_getN(7);
  @$pb.TagNumber(8)
  set resume(LifecycleBehavior value) => $_setField(8, value);
  @$pb.TagNumber(8)
  $core.bool hasResume() => $_has(7);
  @$pb.TagNumber(8)
  void clearResume() => $_clearField(8);
}

enum ManagedSetting_EffectiveValue { booleanValue, lifecycleValue, notSet }

class ManagedSetting extends $pb.GeneratedMessage {
  factory ManagedSetting({
    PreferenceKey? key,
    SettingControl? control,
    $core.bool? booleanValue,
    LifecycleBehavior? lifecycleValue,
  }) {
    final result = create();
    if (key != null) result.key = key;
    if (control != null) result.control = control;
    if (booleanValue != null) result.booleanValue = booleanValue;
    if (lifecycleValue != null) result.lifecycleValue = lifecycleValue;
    return result;
  }

  ManagedSetting._();

  factory ManagedSetting.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ManagedSetting.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static const $core.Map<$core.int, ManagedSetting_EffectiveValue>
      _ManagedSetting_EffectiveValueByTag = {
    3: ManagedSetting_EffectiveValue.booleanValue,
    4: ManagedSetting_EffectiveValue.lifecycleValue,
    0: ManagedSetting_EffectiveValue.notSet
  };
  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ManagedSetting',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..oo(0, [3, 4])
    ..aE<PreferenceKey>(1, _omitFieldNames ? '' : 'key',
        enumValues: PreferenceKey.values)
    ..aOM<SettingControl>(2, _omitFieldNames ? '' : 'control',
        subBuilder: SettingControl.create)
    ..aOB(3, _omitFieldNames ? '' : 'booleanValue')
    ..aE<LifecycleBehavior>(4, _omitFieldNames ? '' : 'lifecycleValue',
        enumValues: LifecycleBehavior.values)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ManagedSetting clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ManagedSetting copyWith(void Function(ManagedSetting) updates) =>
      super.copyWith((message) => updates(message as ManagedSetting))
          as ManagedSetting;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ManagedSetting create() => ManagedSetting._();
  @$core.override
  ManagedSetting createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ManagedSetting getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ManagedSetting>(create);
  static ManagedSetting? _defaultInstance;

  @$pb.TagNumber(3)
  @$pb.TagNumber(4)
  ManagedSetting_EffectiveValue whichEffectiveValue() =>
      _ManagedSetting_EffectiveValueByTag[$_whichOneof(0)]!;
  @$pb.TagNumber(3)
  @$pb.TagNumber(4)
  void clearEffectiveValue() => $_clearField($_whichOneof(0));

  /// Closed keys for runtime-owned settings; UI-only preferences stay in UI.
  @$pb.TagNumber(1)
  PreferenceKey get key => $_getN(0);
  @$pb.TagNumber(1)
  set key(PreferenceKey value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasKey() => $_has(0);
  @$pb.TagNumber(1)
  void clearKey() => $_clearField(1);

  @$pb.TagNumber(2)
  SettingControl get control => $_getN(1);
  @$pb.TagNumber(2)
  set control(SettingControl value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasControl() => $_has(1);
  @$pb.TagNumber(2)
  void clearControl() => $_clearField(2);
  @$pb.TagNumber(2)
  SettingControl ensureControl() => $_ensure(1);

  @$pb.TagNumber(3)
  $core.bool get booleanValue => $_getBF(2);
  @$pb.TagNumber(3)
  set booleanValue($core.bool value) => $_setBool(2, value);
  @$pb.TagNumber(3)
  $core.bool hasBooleanValue() => $_has(2);
  @$pb.TagNumber(3)
  void clearBooleanValue() => $_clearField(3);

  @$pb.TagNumber(4)
  LifecycleBehavior get lifecycleValue => $_getN(3);
  @$pb.TagNumber(4)
  set lifecycleValue(LifecycleBehavior value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasLifecycleValue() => $_has(3);
  @$pb.TagNumber(4)
  void clearLifecycleValue() => $_clearField(4);
}

class ExitNode extends $pb.GeneratedMessage {
  factory ExitNode({
    $core.String? id,
    $core.String? displayName,
    $core.String? peerId,
    $0.Restriction? selection,
    $core.Iterable<LanAccess>? allowedLanAccess,
  }) {
    final result = create();
    if (id != null) result.id = id;
    if (displayName != null) result.displayName = displayName;
    if (peerId != null) result.peerId = peerId;
    if (selection != null) result.selection = selection;
    if (allowedLanAccess != null)
      result.allowedLanAccess.addAll(allowedLanAccess);
    return result;
  }

  ExitNode._();

  factory ExitNode.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ExitNode.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ExitNode',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'id')
    ..aOS(2, _omitFieldNames ? '' : 'displayName')
    ..aOS(3, _omitFieldNames ? '' : 'peerId')
    ..aOM<$0.Restriction>(4, _omitFieldNames ? '' : 'selection',
        subBuilder: $0.Restriction.create)
    ..pc<LanAccess>(
        5, _omitFieldNames ? '' : 'allowedLanAccess', $pb.PbFieldType.KE,
        valueOf: LanAccess.valueOf,
        enumValues: LanAccess.values,
        defaultEnumValue: LanAccess.LAN_ACCESS_UNSPECIFIED)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ExitNode clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ExitNode copyWith(void Function(ExitNode) updates) =>
      super.copyWith((message) => updates(message as ExitNode)) as ExitNode;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ExitNode create() => ExitNode._();
  @$core.override
  ExitNode createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ExitNode getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<ExitNode>(create);
  static ExitNode? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get id => $_getSZ(0);
  @$pb.TagNumber(1)
  set id($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasId() => $_has(0);
  @$pb.TagNumber(1)
  void clearId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get displayName => $_getSZ(1);
  @$pb.TagNumber(2)
  set displayName($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasDisplayName() => $_has(1);
  @$pb.TagNumber(2)
  void clearDisplayName() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get peerId => $_getSZ(2);
  @$pb.TagNumber(3)
  set peerId($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasPeerId() => $_has(2);
  @$pb.TagNumber(3)
  void clearPeerId() => $_clearField(3);

  @$pb.TagNumber(4)
  $0.Restriction get selection => $_getN(3);
  @$pb.TagNumber(4)
  set selection($0.Restriction value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasSelection() => $_has(3);
  @$pb.TagNumber(4)
  void clearSelection() => $_clearField(4);
  @$pb.TagNumber(4)
  $0.Restriction ensureSelection() => $_ensure(3);

  @$pb.TagNumber(5)
  $pb.PbList<LanAccess> get allowedLanAccess => $_getList(4);
}

class ExitNodeStatus extends $pb.GeneratedMessage {
  factory ExitNodeStatus({
    $0.SnapshotMetadata? metadata,
    $core.String? profileId,
    $core.String? requestedExitNodeId,
    $core.String? effectiveExitNodeId,
    LanAccess? requestedLanAccess,
    LanAccess? effectiveLanAccess,
    ApplyState? applyState,
    SettingControl? control,
    $0.Failure? failure,
    $core.bool? failClosed,
  }) {
    final result = create();
    if (metadata != null) result.metadata = metadata;
    if (profileId != null) result.profileId = profileId;
    if (requestedExitNodeId != null)
      result.requestedExitNodeId = requestedExitNodeId;
    if (effectiveExitNodeId != null)
      result.effectiveExitNodeId = effectiveExitNodeId;
    if (requestedLanAccess != null)
      result.requestedLanAccess = requestedLanAccess;
    if (effectiveLanAccess != null)
      result.effectiveLanAccess = effectiveLanAccess;
    if (applyState != null) result.applyState = applyState;
    if (control != null) result.control = control;
    if (failure != null) result.failure = failure;
    if (failClosed != null) result.failClosed = failClosed;
    return result;
  }

  ExitNodeStatus._();

  factory ExitNodeStatus.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ExitNodeStatus.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ExitNodeStatus',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$0.SnapshotMetadata>(1, _omitFieldNames ? '' : 'metadata',
        subBuilder: $0.SnapshotMetadata.create)
    ..aOS(2, _omitFieldNames ? '' : 'profileId')
    ..aOS(3, _omitFieldNames ? '' : 'requestedExitNodeId')
    ..aOS(4, _omitFieldNames ? '' : 'effectiveExitNodeId')
    ..aE<LanAccess>(5, _omitFieldNames ? '' : 'requestedLanAccess',
        enumValues: LanAccess.values)
    ..aE<LanAccess>(6, _omitFieldNames ? '' : 'effectiveLanAccess',
        enumValues: LanAccess.values)
    ..aE<ApplyState>(7, _omitFieldNames ? '' : 'applyState',
        enumValues: ApplyState.values)
    ..aOM<SettingControl>(8, _omitFieldNames ? '' : 'control',
        subBuilder: SettingControl.create)
    ..aOM<$0.Failure>(9, _omitFieldNames ? '' : 'failure',
        subBuilder: $0.Failure.create)
    ..aOB(10, _omitFieldNames ? '' : 'failClosed')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ExitNodeStatus clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ExitNodeStatus copyWith(void Function(ExitNodeStatus) updates) =>
      super.copyWith((message) => updates(message as ExitNodeStatus))
          as ExitNodeStatus;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ExitNodeStatus create() => ExitNodeStatus._();
  @$core.override
  ExitNodeStatus createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ExitNodeStatus getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ExitNodeStatus>(create);
  static ExitNodeStatus? _defaultInstance;

  @$pb.TagNumber(1)
  $0.SnapshotMetadata get metadata => $_getN(0);
  @$pb.TagNumber(1)
  set metadata($0.SnapshotMetadata value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasMetadata() => $_has(0);
  @$pb.TagNumber(1)
  void clearMetadata() => $_clearField(1);
  @$pb.TagNumber(1)
  $0.SnapshotMetadata ensureMetadata() => $_ensure(0);

  @$pb.TagNumber(2)
  $core.String get profileId => $_getSZ(1);
  @$pb.TagNumber(2)
  set profileId($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasProfileId() => $_has(1);
  @$pb.TagNumber(2)
  void clearProfileId() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get requestedExitNodeId => $_getSZ(2);
  @$pb.TagNumber(3)
  set requestedExitNodeId($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasRequestedExitNodeId() => $_has(2);
  @$pb.TagNumber(3)
  void clearRequestedExitNodeId() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.String get effectiveExitNodeId => $_getSZ(3);
  @$pb.TagNumber(4)
  set effectiveExitNodeId($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasEffectiveExitNodeId() => $_has(3);
  @$pb.TagNumber(4)
  void clearEffectiveExitNodeId() => $_clearField(4);

  @$pb.TagNumber(5)
  LanAccess get requestedLanAccess => $_getN(4);
  @$pb.TagNumber(5)
  set requestedLanAccess(LanAccess value) => $_setField(5, value);
  @$pb.TagNumber(5)
  $core.bool hasRequestedLanAccess() => $_has(4);
  @$pb.TagNumber(5)
  void clearRequestedLanAccess() => $_clearField(5);

  @$pb.TagNumber(6)
  LanAccess get effectiveLanAccess => $_getN(5);
  @$pb.TagNumber(6)
  set effectiveLanAccess(LanAccess value) => $_setField(6, value);
  @$pb.TagNumber(6)
  $core.bool hasEffectiveLanAccess() => $_has(5);
  @$pb.TagNumber(6)
  void clearEffectiveLanAccess() => $_clearField(6);

  @$pb.TagNumber(7)
  ApplyState get applyState => $_getN(6);
  @$pb.TagNumber(7)
  set applyState(ApplyState value) => $_setField(7, value);
  @$pb.TagNumber(7)
  $core.bool hasApplyState() => $_has(6);
  @$pb.TagNumber(7)
  void clearApplyState() => $_clearField(7);

  @$pb.TagNumber(8)
  SettingControl get control => $_getN(7);
  @$pb.TagNumber(8)
  set control(SettingControl value) => $_setField(8, value);
  @$pb.TagNumber(8)
  $core.bool hasControl() => $_has(7);
  @$pb.TagNumber(8)
  void clearControl() => $_clearField(8);
  @$pb.TagNumber(8)
  SettingControl ensureControl() => $_ensure(7);

  @$pb.TagNumber(9)
  $0.Failure get failure => $_getN(8);
  @$pb.TagNumber(9)
  set failure($0.Failure value) => $_setField(9, value);
  @$pb.TagNumber(9)
  $core.bool hasFailure() => $_has(8);
  @$pb.TagNumber(9)
  void clearFailure() => $_clearField(9);
  @$pb.TagNumber(9)
  $0.Failure ensureFailure() => $_ensure(8);

  /// On path loss, traffic cannot silently escape via a direct default route.
  @$pb.TagNumber(10)
  $core.bool get failClosed => $_getBF(9);
  @$pb.TagNumber(10)
  set failClosed($core.bool value) => $_setBool(9, value);
  @$pb.TagNumber(10)
  $core.bool hasFailClosed() => $_has(9);
  @$pb.TagNumber(10)
  void clearFailClosed() => $_clearField(10);
}

class HostTarget extends $pb.GeneratedMessage {
  factory HostTarget({
    $core.Iterable<$core.String>? addresses,
    $core.String? hostname,
  }) {
    final result = create();
    if (addresses != null) result.addresses.addAll(addresses);
    if (hostname != null) result.hostname = hostname;
    return result;
  }

  HostTarget._();

  factory HostTarget.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory HostTarget.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'HostTarget',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..pPS(1, _omitFieldNames ? '' : 'addresses')
    ..aOS(2, _omitFieldNames ? '' : 'hostname')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  HostTarget clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  HostTarget copyWith(void Function(HostTarget) updates) =>
      super.copyWith((message) => updates(message as HostTarget)) as HostTarget;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static HostTarget create() => HostTarget._();
  @$core.override
  HostTarget createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static HostTarget getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<HostTarget>(create);
  static HostTarget? _defaultInstance;

  @$pb.TagNumber(1)
  $pb.PbList<$core.String> get addresses => $_getList(0);

  @$pb.TagNumber(2)
  $core.String get hostname => $_getSZ(1);
  @$pb.TagNumber(2)
  set hostname($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasHostname() => $_has(1);
  @$pb.TagNumber(2)
  void clearHostname() => $_clearField(2);
}

class SubnetTarget extends $pb.GeneratedMessage {
  factory SubnetTarget({
    $core.String? cidr,
  }) {
    final result = create();
    if (cidr != null) result.cidr = cidr;
    return result;
  }

  SubnetTarget._();

  factory SubnetTarget.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory SubnetTarget.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SubnetTarget',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'cidr')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SubnetTarget clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SubnetTarget copyWith(void Function(SubnetTarget) updates) =>
      super.copyWith((message) => updates(message as SubnetTarget))
          as SubnetTarget;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static SubnetTarget create() => SubnetTarget._();
  @$core.override
  SubnetTarget createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static SubnetTarget getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SubnetTarget>(create);
  static SubnetTarget? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get cidr => $_getSZ(0);
  @$pb.TagNumber(1)
  set cidr($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasCidr() => $_has(0);
  @$pb.TagNumber(1)
  void clearCidr() => $_clearField(1);
}

class ServiceTarget extends $pb.GeneratedMessage {
  factory ServiceTarget({
    $core.String? hostname,
    $core.int? port,
    $core.String? protocol,
  }) {
    final result = create();
    if (hostname != null) result.hostname = hostname;
    if (port != null) result.port = port;
    if (protocol != null) result.protocol = protocol;
    return result;
  }

  ServiceTarget._();

  factory ServiceTarget.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ServiceTarget.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ServiceTarget',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'hostname')
    ..aI(2, _omitFieldNames ? '' : 'port', fieldType: $pb.PbFieldType.OU3)
    ..aOS(3, _omitFieldNames ? '' : 'protocol')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ServiceTarget clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ServiceTarget copyWith(void Function(ServiceTarget) updates) =>
      super.copyWith((message) => updates(message as ServiceTarget))
          as ServiceTarget;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ServiceTarget create() => ServiceTarget._();
  @$core.override
  ServiceTarget createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ServiceTarget getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ServiceTarget>(create);
  static ServiceTarget? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get hostname => $_getSZ(0);
  @$pb.TagNumber(1)
  set hostname($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasHostname() => $_has(0);
  @$pb.TagNumber(1)
  void clearHostname() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.int get port => $_getIZ(1);
  @$pb.TagNumber(2)
  set port($core.int value) => $_setUnsignedInt32(1, value);
  @$pb.TagNumber(2)
  $core.bool hasPort() => $_has(1);
  @$pb.TagNumber(2)
  void clearPort() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get protocol => $_getSZ(2);
  @$pb.TagNumber(3)
  set protocol($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasProtocol() => $_has(2);
  @$pb.TagNumber(3)
  void clearProtocol() => $_clearField(3);
}

class ApplicationTarget extends $pb.GeneratedMessage {
  factory ApplicationTarget({
    $core.String? displayAddress,
    $core.String? browserUrl,
  }) {
    final result = create();
    if (displayAddress != null) result.displayAddress = displayAddress;
    if (browserUrl != null) result.browserUrl = browserUrl;
    return result;
  }

  ApplicationTarget._();

  factory ApplicationTarget.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ApplicationTarget.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ApplicationTarget',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'displayAddress')
    ..aOS(2, _omitFieldNames ? '' : 'browserUrl')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ApplicationTarget clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ApplicationTarget copyWith(void Function(ApplicationTarget) updates) =>
      super.copyWith((message) => updates(message as ApplicationTarget))
          as ApplicationTarget;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ApplicationTarget create() => ApplicationTarget._();
  @$core.override
  ApplicationTarget createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ApplicationTarget getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ApplicationTarget>(create);
  static ApplicationTarget? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get displayAddress => $_getSZ(0);
  @$pb.TagNumber(1)
  set displayAddress($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasDisplayAddress() => $_has(0);
  @$pb.TagNumber(1)
  void clearDisplayAddress() => $_clearField(1);

  /// Validated HTTPS link when the authorized resource supports a browser.
  @$pb.TagNumber(2)
  $core.String get browserUrl => $_getSZ(1);
  @$pb.TagNumber(2)
  set browserUrl($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasBrowserUrl() => $_has(1);
  @$pb.TagNumber(2)
  void clearBrowserUrl() => $_clearField(2);
}

enum Resource_Target { host, subnet, service, application, notSet }

class Resource extends $pb.GeneratedMessage {
  factory Resource({
    $core.String? id,
    $core.String? displayName,
    ResourceKind? kind,
    $core.String? networkId,
    $0.Restriction? availability,
    BooleanSetting? enabled,
    $core.Iterable<$core.String>? overlappingResourceIds,
    $core.String? overlapReasonKey,
    HostTarget? host,
    SubnetTarget? subnet,
    ServiceTarget? service,
    ApplicationTarget? application,
  }) {
    final result = create();
    if (id != null) result.id = id;
    if (displayName != null) result.displayName = displayName;
    if (kind != null) result.kind = kind;
    if (networkId != null) result.networkId = networkId;
    if (availability != null) result.availability = availability;
    if (enabled != null) result.enabled = enabled;
    if (overlappingResourceIds != null)
      result.overlappingResourceIds.addAll(overlappingResourceIds);
    if (overlapReasonKey != null) result.overlapReasonKey = overlapReasonKey;
    if (host != null) result.host = host;
    if (subnet != null) result.subnet = subnet;
    if (service != null) result.service = service;
    if (application != null) result.application = application;
    return result;
  }

  Resource._();

  factory Resource.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory Resource.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static const $core.Map<$core.int, Resource_Target> _Resource_TargetByTag = {
    10: Resource_Target.host,
    11: Resource_Target.subnet,
    12: Resource_Target.service,
    13: Resource_Target.application,
    0: Resource_Target.notSet
  };
  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'Resource',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..oo(0, [10, 11, 12, 13])
    ..aOS(1, _omitFieldNames ? '' : 'id')
    ..aOS(2, _omitFieldNames ? '' : 'displayName')
    ..aE<ResourceKind>(3, _omitFieldNames ? '' : 'kind',
        enumValues: ResourceKind.values)
    ..aOS(4, _omitFieldNames ? '' : 'networkId')
    ..aOM<$0.Restriction>(5, _omitFieldNames ? '' : 'availability',
        subBuilder: $0.Restriction.create)
    ..aOM<BooleanSetting>(6, _omitFieldNames ? '' : 'enabled',
        subBuilder: BooleanSetting.create)
    ..pPS(7, _omitFieldNames ? '' : 'overlappingResourceIds')
    ..aOS(8, _omitFieldNames ? '' : 'overlapReasonKey')
    ..aOM<HostTarget>(10, _omitFieldNames ? '' : 'host',
        subBuilder: HostTarget.create)
    ..aOM<SubnetTarget>(11, _omitFieldNames ? '' : 'subnet',
        subBuilder: SubnetTarget.create)
    ..aOM<ServiceTarget>(12, _omitFieldNames ? '' : 'service',
        subBuilder: ServiceTarget.create)
    ..aOM<ApplicationTarget>(13, _omitFieldNames ? '' : 'application',
        subBuilder: ApplicationTarget.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Resource clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Resource copyWith(void Function(Resource) updates) =>
      super.copyWith((message) => updates(message as Resource)) as Resource;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static Resource create() => Resource._();
  @$core.override
  Resource createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static Resource getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<Resource>(create);
  static Resource? _defaultInstance;

  @$pb.TagNumber(10)
  @$pb.TagNumber(11)
  @$pb.TagNumber(12)
  @$pb.TagNumber(13)
  Resource_Target whichTarget() => _Resource_TargetByTag[$_whichOneof(0)]!;
  @$pb.TagNumber(10)
  @$pb.TagNumber(11)
  @$pb.TagNumber(12)
  @$pb.TagNumber(13)
  void clearTarget() => $_clearField($_whichOneof(0));

  @$pb.TagNumber(1)
  $core.String get id => $_getSZ(0);
  @$pb.TagNumber(1)
  set id($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasId() => $_has(0);
  @$pb.TagNumber(1)
  void clearId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get displayName => $_getSZ(1);
  @$pb.TagNumber(2)
  set displayName($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasDisplayName() => $_has(1);
  @$pb.TagNumber(2)
  void clearDisplayName() => $_clearField(2);

  @$pb.TagNumber(3)
  ResourceKind get kind => $_getN(2);
  @$pb.TagNumber(3)
  set kind(ResourceKind value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasKind() => $_has(2);
  @$pb.TagNumber(3)
  void clearKind() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.String get networkId => $_getSZ(3);
  @$pb.TagNumber(4)
  set networkId($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasNetworkId() => $_has(3);
  @$pb.TagNumber(4)
  void clearNetworkId() => $_clearField(4);

  @$pb.TagNumber(5)
  $0.Restriction get availability => $_getN(4);
  @$pb.TagNumber(5)
  set availability($0.Restriction value) => $_setField(5, value);
  @$pb.TagNumber(5)
  $core.bool hasAvailability() => $_has(4);
  @$pb.TagNumber(5)
  void clearAvailability() => $_clearField(5);
  @$pb.TagNumber(5)
  $0.Restriction ensureAvailability() => $_ensure(4);

  @$pb.TagNumber(6)
  BooleanSetting get enabled => $_getN(5);
  @$pb.TagNumber(6)
  set enabled(BooleanSetting value) => $_setField(6, value);
  @$pb.TagNumber(6)
  $core.bool hasEnabled() => $_has(5);
  @$pb.TagNumber(6)
  void clearEnabled() => $_clearField(6);
  @$pb.TagNumber(6)
  BooleanSetting ensureEnabled() => $_ensure(5);

  /// IDs refer only to resources disclosed to this caller; never hidden resources.
  @$pb.TagNumber(7)
  $pb.PbList<$core.String> get overlappingResourceIds => $_getList(6);

  @$pb.TagNumber(8)
  $core.String get overlapReasonKey => $_getSZ(7);
  @$pb.TagNumber(8)
  set overlapReasonKey($core.String value) => $_setString(7, value);
  @$pb.TagNumber(8)
  $core.bool hasOverlapReasonKey() => $_has(7);
  @$pb.TagNumber(8)
  void clearOverlapReasonKey() => $_clearField(8);

  @$pb.TagNumber(10)
  HostTarget get host => $_getN(8);
  @$pb.TagNumber(10)
  set host(HostTarget value) => $_setField(10, value);
  @$pb.TagNumber(10)
  $core.bool hasHost() => $_has(8);
  @$pb.TagNumber(10)
  void clearHost() => $_clearField(10);
  @$pb.TagNumber(10)
  HostTarget ensureHost() => $_ensure(8);

  @$pb.TagNumber(11)
  SubnetTarget get subnet => $_getN(9);
  @$pb.TagNumber(11)
  set subnet(SubnetTarget value) => $_setField(11, value);
  @$pb.TagNumber(11)
  $core.bool hasSubnet() => $_has(9);
  @$pb.TagNumber(11)
  void clearSubnet() => $_clearField(11);
  @$pb.TagNumber(11)
  SubnetTarget ensureSubnet() => $_ensure(9);

  @$pb.TagNumber(12)
  ServiceTarget get service => $_getN(10);
  @$pb.TagNumber(12)
  set service(ServiceTarget value) => $_setField(12, value);
  @$pb.TagNumber(12)
  $core.bool hasService() => $_has(10);
  @$pb.TagNumber(12)
  void clearService() => $_clearField(12);
  @$pb.TagNumber(12)
  ServiceTarget ensureService() => $_ensure(10);

  @$pb.TagNumber(13)
  ApplicationTarget get application => $_getN(11);
  @$pb.TagNumber(13)
  set application(ApplicationTarget value) => $_setField(13, value);
  @$pb.TagNumber(13)
  $core.bool hasApplication() => $_has(11);
  @$pb.TagNumber(13)
  void clearApplication() => $_clearField(13);
  @$pb.TagNumber(13)
  ApplicationTarget ensureApplication() => $_ensure(11);
}

class Compatibility extends $pb.GeneratedMessage {
  factory Compatibility({
    CompatibilityState? state,
    $core.String? reasonKey,
    $core.int? minimumIpcVersion,
    $core.int? maximumIpcVersion,
    $core.Iterable<$core.String>? acceptedContractSha256,
  }) {
    final result = create();
    if (state != null) result.state = state;
    if (reasonKey != null) result.reasonKey = reasonKey;
    if (minimumIpcVersion != null) result.minimumIpcVersion = minimumIpcVersion;
    if (maximumIpcVersion != null) result.maximumIpcVersion = maximumIpcVersion;
    if (acceptedContractSha256 != null)
      result.acceptedContractSha256.addAll(acceptedContractSha256);
    return result;
  }

  Compatibility._();

  factory Compatibility.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory Compatibility.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'Compatibility',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aE<CompatibilityState>(1, _omitFieldNames ? '' : 'state',
        enumValues: CompatibilityState.values)
    ..aOS(2, _omitFieldNames ? '' : 'reasonKey')
    ..aI(3, _omitFieldNames ? '' : 'minimumIpcVersion',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(4, _omitFieldNames ? '' : 'maximumIpcVersion',
        fieldType: $pb.PbFieldType.OU3)
    ..pPS(5, _omitFieldNames ? '' : 'acceptedContractSha256')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Compatibility clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Compatibility copyWith(void Function(Compatibility) updates) =>
      super.copyWith((message) => updates(message as Compatibility))
          as Compatibility;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static Compatibility create() => Compatibility._();
  @$core.override
  Compatibility createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static Compatibility getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<Compatibility>(create);
  static Compatibility? _defaultInstance;

  @$pb.TagNumber(1)
  CompatibilityState get state => $_getN(0);
  @$pb.TagNumber(1)
  set state(CompatibilityState value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasState() => $_has(0);
  @$pb.TagNumber(1)
  void clearState() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get reasonKey => $_getSZ(1);
  @$pb.TagNumber(2)
  set reasonKey($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasReasonKey() => $_has(1);
  @$pb.TagNumber(2)
  void clearReasonKey() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.int get minimumIpcVersion => $_getIZ(2);
  @$pb.TagNumber(3)
  set minimumIpcVersion($core.int value) => $_setUnsignedInt32(2, value);
  @$pb.TagNumber(3)
  $core.bool hasMinimumIpcVersion() => $_has(2);
  @$pb.TagNumber(3)
  void clearMinimumIpcVersion() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.int get maximumIpcVersion => $_getIZ(3);
  @$pb.TagNumber(4)
  set maximumIpcVersion($core.int value) => $_setUnsignedInt32(3, value);
  @$pb.TagNumber(4)
  $core.bool hasMaximumIpcVersion() => $_has(3);
  @$pb.TagNumber(4)
  void clearMaximumIpcVersion() => $_clearField(4);

  @$pb.TagNumber(5)
  $pb.PbList<$core.String> get acceptedContractSha256 => $_getList(4);
}

class VerifiedUpdate extends $pb.GeneratedMessage {
  factory VerifiedUpdate({
    $core.String? releaseId,
    $core.String? manifestSha256,
    $core.String? signingKeyId,
    $1.Timestamp? verifiedAt,
    $1.Timestamp? expiresAt,
    $0.BuildIdentity? runtime,
    $core.String? pairedUiVersion,
    Compatibility? compatibility,
    UpdateClassification? classification,
    DistributionChannel? channel,
    $1.Timestamp? mandatoryAfter,
    $core.String? actionUrl,
    $core.String? releaseNotesUrl,
  }) {
    final result = create();
    if (releaseId != null) result.releaseId = releaseId;
    if (manifestSha256 != null) result.manifestSha256 = manifestSha256;
    if (signingKeyId != null) result.signingKeyId = signingKeyId;
    if (verifiedAt != null) result.verifiedAt = verifiedAt;
    if (expiresAt != null) result.expiresAt = expiresAt;
    if (runtime != null) result.runtime = runtime;
    if (pairedUiVersion != null) result.pairedUiVersion = pairedUiVersion;
    if (compatibility != null) result.compatibility = compatibility;
    if (classification != null) result.classification = classification;
    if (channel != null) result.channel = channel;
    if (mandatoryAfter != null) result.mandatoryAfter = mandatoryAfter;
    if (actionUrl != null) result.actionUrl = actionUrl;
    if (releaseNotesUrl != null) result.releaseNotesUrl = releaseNotesUrl;
    return result;
  }

  VerifiedUpdate._();

  factory VerifiedUpdate.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory VerifiedUpdate.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'VerifiedUpdate',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'releaseId')
    ..aOS(2, _omitFieldNames ? '' : 'manifestSha256')
    ..aOS(3, _omitFieldNames ? '' : 'signingKeyId')
    ..aOM<$1.Timestamp>(4, _omitFieldNames ? '' : 'verifiedAt',
        subBuilder: $1.Timestamp.create)
    ..aOM<$1.Timestamp>(5, _omitFieldNames ? '' : 'expiresAt',
        subBuilder: $1.Timestamp.create)
    ..aOM<$0.BuildIdentity>(6, _omitFieldNames ? '' : 'runtime',
        subBuilder: $0.BuildIdentity.create)
    ..aOS(7, _omitFieldNames ? '' : 'pairedUiVersion')
    ..aOM<Compatibility>(8, _omitFieldNames ? '' : 'compatibility',
        subBuilder: Compatibility.create)
    ..aE<UpdateClassification>(9, _omitFieldNames ? '' : 'classification',
        enumValues: UpdateClassification.values)
    ..aE<DistributionChannel>(10, _omitFieldNames ? '' : 'channel',
        enumValues: DistributionChannel.values)
    ..aOM<$1.Timestamp>(11, _omitFieldNames ? '' : 'mandatoryAfter',
        subBuilder: $1.Timestamp.create)
    ..aOS(12, _omitFieldNames ? '' : 'actionUrl')
    ..aOS(13, _omitFieldNames ? '' : 'releaseNotesUrl')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  VerifiedUpdate clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  VerifiedUpdate copyWith(void Function(VerifiedUpdate) updates) =>
      super.copyWith((message) => updates(message as VerifiedUpdate))
          as VerifiedUpdate;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static VerifiedUpdate create() => VerifiedUpdate._();
  @$core.override
  VerifiedUpdate createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static VerifiedUpdate getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<VerifiedUpdate>(create);
  static VerifiedUpdate? _defaultInstance;

  /// Producer verifies signature, expiry, target and pairing before disclosure.
  @$pb.TagNumber(1)
  $core.String get releaseId => $_getSZ(0);
  @$pb.TagNumber(1)
  set releaseId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasReleaseId() => $_has(0);
  @$pb.TagNumber(1)
  void clearReleaseId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get manifestSha256 => $_getSZ(1);
  @$pb.TagNumber(2)
  set manifestSha256($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasManifestSha256() => $_has(1);
  @$pb.TagNumber(2)
  void clearManifestSha256() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get signingKeyId => $_getSZ(2);
  @$pb.TagNumber(3)
  set signingKeyId($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasSigningKeyId() => $_has(2);
  @$pb.TagNumber(3)
  void clearSigningKeyId() => $_clearField(3);

  @$pb.TagNumber(4)
  $1.Timestamp get verifiedAt => $_getN(3);
  @$pb.TagNumber(4)
  set verifiedAt($1.Timestamp value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasVerifiedAt() => $_has(3);
  @$pb.TagNumber(4)
  void clearVerifiedAt() => $_clearField(4);
  @$pb.TagNumber(4)
  $1.Timestamp ensureVerifiedAt() => $_ensure(3);

  @$pb.TagNumber(5)
  $1.Timestamp get expiresAt => $_getN(4);
  @$pb.TagNumber(5)
  set expiresAt($1.Timestamp value) => $_setField(5, value);
  @$pb.TagNumber(5)
  $core.bool hasExpiresAt() => $_has(4);
  @$pb.TagNumber(5)
  void clearExpiresAt() => $_clearField(5);
  @$pb.TagNumber(5)
  $1.Timestamp ensureExpiresAt() => $_ensure(4);

  @$pb.TagNumber(6)
  $0.BuildIdentity get runtime => $_getN(5);
  @$pb.TagNumber(6)
  set runtime($0.BuildIdentity value) => $_setField(6, value);
  @$pb.TagNumber(6)
  $core.bool hasRuntime() => $_has(5);
  @$pb.TagNumber(6)
  void clearRuntime() => $_clearField(6);
  @$pb.TagNumber(6)
  $0.BuildIdentity ensureRuntime() => $_ensure(5);

  @$pb.TagNumber(7)
  $core.String get pairedUiVersion => $_getSZ(6);
  @$pb.TagNumber(7)
  set pairedUiVersion($core.String value) => $_setString(6, value);
  @$pb.TagNumber(7)
  $core.bool hasPairedUiVersion() => $_has(6);
  @$pb.TagNumber(7)
  void clearPairedUiVersion() => $_clearField(7);

  @$pb.TagNumber(8)
  Compatibility get compatibility => $_getN(7);
  @$pb.TagNumber(8)
  set compatibility(Compatibility value) => $_setField(8, value);
  @$pb.TagNumber(8)
  $core.bool hasCompatibility() => $_has(7);
  @$pb.TagNumber(8)
  void clearCompatibility() => $_clearField(8);
  @$pb.TagNumber(8)
  Compatibility ensureCompatibility() => $_ensure(7);

  @$pb.TagNumber(9)
  UpdateClassification get classification => $_getN(8);
  @$pb.TagNumber(9)
  set classification(UpdateClassification value) => $_setField(9, value);
  @$pb.TagNumber(9)
  $core.bool hasClassification() => $_has(8);
  @$pb.TagNumber(9)
  void clearClassification() => $_clearField(9);

  @$pb.TagNumber(10)
  DistributionChannel get channel => $_getN(9);
  @$pb.TagNumber(10)
  set channel(DistributionChannel value) => $_setField(10, value);
  @$pb.TagNumber(10)
  $core.bool hasChannel() => $_has(9);
  @$pb.TagNumber(10)
  void clearChannel() => $_clearField(10);

  @$pb.TagNumber(11)
  $1.Timestamp get mandatoryAfter => $_getN(10);
  @$pb.TagNumber(11)
  set mandatoryAfter($1.Timestamp value) => $_setField(11, value);
  @$pb.TagNumber(11)
  $core.bool hasMandatoryAfter() => $_has(10);
  @$pb.TagNumber(11)
  void clearMandatoryAfter() => $_clearField(11);
  @$pb.TagNumber(11)
  $1.Timestamp ensureMandatoryAfter() => $_ensure(10);

  /// Validated destination for the platform package/store owner.
  @$pb.TagNumber(12)
  $core.String get actionUrl => $_getSZ(11);
  @$pb.TagNumber(12)
  set actionUrl($core.String value) => $_setString(11, value);
  @$pb.TagNumber(12)
  $core.bool hasActionUrl() => $_has(11);
  @$pb.TagNumber(12)
  void clearActionUrl() => $_clearField(12);

  @$pb.TagNumber(13)
  $core.String get releaseNotesUrl => $_getSZ(12);
  @$pb.TagNumber(13)
  set releaseNotesUrl($core.String value) => $_setString(12, value);
  @$pb.TagNumber(13)
  $core.bool hasReleaseNotesUrl() => $_has(12);
  @$pb.TagNumber(13)
  void clearReleaseNotesUrl() => $_clearField(13);
}

class UpdateInfo extends $pb.GeneratedMessage {
  factory UpdateInfo({
    $0.SnapshotMetadata? metadata,
    $0.BuildIdentity? installedRuntime,
    $0.BuildIdentity? reportedUi,
    Compatibility? installedPair,
    UpdateState? state,
    VerifiedUpdate? available,
    $0.Restriction? discovery,
  }) {
    final result = create();
    if (metadata != null) result.metadata = metadata;
    if (installedRuntime != null) result.installedRuntime = installedRuntime;
    if (reportedUi != null) result.reportedUi = reportedUi;
    if (installedPair != null) result.installedPair = installedPair;
    if (state != null) result.state = state;
    if (available != null) result.available = available;
    if (discovery != null) result.discovery = discovery;
    return result;
  }

  UpdateInfo._();

  factory UpdateInfo.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory UpdateInfo.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'UpdateInfo',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$0.SnapshotMetadata>(1, _omitFieldNames ? '' : 'metadata',
        subBuilder: $0.SnapshotMetadata.create)
    ..aOM<$0.BuildIdentity>(2, _omitFieldNames ? '' : 'installedRuntime',
        subBuilder: $0.BuildIdentity.create)
    ..aOM<$0.BuildIdentity>(3, _omitFieldNames ? '' : 'reportedUi',
        subBuilder: $0.BuildIdentity.create)
    ..aOM<Compatibility>(4, _omitFieldNames ? '' : 'installedPair',
        subBuilder: Compatibility.create)
    ..aE<UpdateState>(5, _omitFieldNames ? '' : 'state',
        enumValues: UpdateState.values)
    ..aOM<VerifiedUpdate>(6, _omitFieldNames ? '' : 'available',
        subBuilder: VerifiedUpdate.create)
    ..aOM<$0.Restriction>(7, _omitFieldNames ? '' : 'discovery',
        subBuilder: $0.Restriction.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  UpdateInfo clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  UpdateInfo copyWith(void Function(UpdateInfo) updates) =>
      super.copyWith((message) => updates(message as UpdateInfo)) as UpdateInfo;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static UpdateInfo create() => UpdateInfo._();
  @$core.override
  UpdateInfo createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static UpdateInfo getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<UpdateInfo>(create);
  static UpdateInfo? _defaultInstance;

  @$pb.TagNumber(1)
  $0.SnapshotMetadata get metadata => $_getN(0);
  @$pb.TagNumber(1)
  set metadata($0.SnapshotMetadata value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasMetadata() => $_has(0);
  @$pb.TagNumber(1)
  void clearMetadata() => $_clearField(1);
  @$pb.TagNumber(1)
  $0.SnapshotMetadata ensureMetadata() => $_ensure(0);

  @$pb.TagNumber(2)
  $0.BuildIdentity get installedRuntime => $_getN(1);
  @$pb.TagNumber(2)
  set installedRuntime($0.BuildIdentity value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasInstalledRuntime() => $_has(1);
  @$pb.TagNumber(2)
  void clearInstalledRuntime() => $_clearField(2);
  @$pb.TagNumber(2)
  $0.BuildIdentity ensureInstalledRuntime() => $_ensure(1);

  /// UI identity is a caller claim for pairing checks, never runtime attestation.
  @$pb.TagNumber(3)
  $0.BuildIdentity get reportedUi => $_getN(2);
  @$pb.TagNumber(3)
  set reportedUi($0.BuildIdentity value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasReportedUi() => $_has(2);
  @$pb.TagNumber(3)
  void clearReportedUi() => $_clearField(3);
  @$pb.TagNumber(3)
  $0.BuildIdentity ensureReportedUi() => $_ensure(2);

  @$pb.TagNumber(4)
  Compatibility get installedPair => $_getN(3);
  @$pb.TagNumber(4)
  set installedPair(Compatibility value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasInstalledPair() => $_has(3);
  @$pb.TagNumber(4)
  void clearInstalledPair() => $_clearField(4);
  @$pb.TagNumber(4)
  Compatibility ensureInstalledPair() => $_ensure(3);

  @$pb.TagNumber(5)
  UpdateState get state => $_getN(4);
  @$pb.TagNumber(5)
  set state(UpdateState value) => $_setField(5, value);
  @$pb.TagNumber(5)
  $core.bool hasState() => $_has(4);
  @$pb.TagNumber(5)
  void clearState() => $_clearField(5);

  @$pb.TagNumber(6)
  VerifiedUpdate get available => $_getN(5);
  @$pb.TagNumber(6)
  set available(VerifiedUpdate value) => $_setField(6, value);
  @$pb.TagNumber(6)
  $core.bool hasAvailable() => $_has(5);
  @$pb.TagNumber(6)
  void clearAvailable() => $_clearField(6);
  @$pb.TagNumber(6)
  VerifiedUpdate ensureAvailable() => $_ensure(5);

  @$pb.TagNumber(7)
  $0.Restriction get discovery => $_getN(6);
  @$pb.TagNumber(7)
  set discovery($0.Restriction value) => $_setField(7, value);
  @$pb.TagNumber(7)
  $core.bool hasDiscovery() => $_has(6);
  @$pb.TagNumber(7)
  void clearDiscovery() => $_clearField(7);
  @$pb.TagNumber(7)
  $0.Restriction ensureDiscovery() => $_ensure(6);
}

class SupportInfo extends $pb.GeneratedMessage {
  factory SupportInfo({
    $0.BuildIdentity? runtime,
    $core.String? productName,
    $core.String? documentationUrl,
    $core.String? supportUrl,
    $core.String? privacyUrl,
    $core.String? licenseUrl,
    $core.String? offlineHelpKey,
  }) {
    final result = create();
    if (runtime != null) result.runtime = runtime;
    if (productName != null) result.productName = productName;
    if (documentationUrl != null) result.documentationUrl = documentationUrl;
    if (supportUrl != null) result.supportUrl = supportUrl;
    if (privacyUrl != null) result.privacyUrl = privacyUrl;
    if (licenseUrl != null) result.licenseUrl = licenseUrl;
    if (offlineHelpKey != null) result.offlineHelpKey = offlineHelpKey;
    return result;
  }

  SupportInfo._();

  factory SupportInfo.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory SupportInfo.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SupportInfo',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$0.BuildIdentity>(1, _omitFieldNames ? '' : 'runtime',
        subBuilder: $0.BuildIdentity.create)
    ..aOS(2, _omitFieldNames ? '' : 'productName')
    ..aOS(3, _omitFieldNames ? '' : 'documentationUrl')
    ..aOS(4, _omitFieldNames ? '' : 'supportUrl')
    ..aOS(5, _omitFieldNames ? '' : 'privacyUrl')
    ..aOS(6, _omitFieldNames ? '' : 'licenseUrl')
    ..aOS(7, _omitFieldNames ? '' : 'offlineHelpKey')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SupportInfo clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SupportInfo copyWith(void Function(SupportInfo) updates) =>
      super.copyWith((message) => updates(message as SupportInfo))
          as SupportInfo;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static SupportInfo create() => SupportInfo._();
  @$core.override
  SupportInfo createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static SupportInfo getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SupportInfo>(create);
  static SupportInfo? _defaultInstance;

  @$pb.TagNumber(1)
  $0.BuildIdentity get runtime => $_getN(0);
  @$pb.TagNumber(1)
  set runtime($0.BuildIdentity value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasRuntime() => $_has(0);
  @$pb.TagNumber(1)
  void clearRuntime() => $_clearField(1);
  @$pb.TagNumber(1)
  $0.BuildIdentity ensureRuntime() => $_ensure(0);

  @$pb.TagNumber(2)
  $core.String get productName => $_getSZ(1);
  @$pb.TagNumber(2)
  set productName($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasProductName() => $_has(1);
  @$pb.TagNumber(2)
  void clearProductName() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get documentationUrl => $_getSZ(2);
  @$pb.TagNumber(3)
  set documentationUrl($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasDocumentationUrl() => $_has(2);
  @$pb.TagNumber(3)
  void clearDocumentationUrl() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.String get supportUrl => $_getSZ(3);
  @$pb.TagNumber(4)
  set supportUrl($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasSupportUrl() => $_has(3);
  @$pb.TagNumber(4)
  void clearSupportUrl() => $_clearField(4);

  @$pb.TagNumber(5)
  $core.String get privacyUrl => $_getSZ(4);
  @$pb.TagNumber(5)
  set privacyUrl($core.String value) => $_setString(4, value);
  @$pb.TagNumber(5)
  $core.bool hasPrivacyUrl() => $_has(4);
  @$pb.TagNumber(5)
  void clearPrivacyUrl() => $_clearField(5);

  @$pb.TagNumber(6)
  $core.String get licenseUrl => $_getSZ(5);
  @$pb.TagNumber(6)
  set licenseUrl($core.String value) => $_setString(5, value);
  @$pb.TagNumber(6)
  $core.bool hasLicenseUrl() => $_has(5);
  @$pb.TagNumber(6)
  void clearLicenseUrl() => $_clearField(6);

  /// Stable offline resource key, with translated content shipped in the UI.
  @$pb.TagNumber(7)
  $core.String get offlineHelpKey => $_getSZ(6);
  @$pb.TagNumber(7)
  set offlineHelpKey($core.String value) => $_setString(6, value);
  @$pb.TagNumber(7)
  $core.bool hasOfflineHelpKey() => $_has(6);
  @$pb.TagNumber(7)
  void clearOfflineHelpKey() => $_clearField(7);
}

const $core.bool _omitFieldNames =
    $core.bool.fromEnvironment('protobuf.omit_field_names');
const $core.bool _omitMessageNames =
    $core.bool.fromEnvironment('protobuf.omit_message_names');
