// This is a generated file - do not edit.
//
// Generated from client/v0/runtime.proto.

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
    as $2;
import 'package:protobuf/well_known_types/google/protobuf/timestamp.pb.dart'
    as $0;

import 'common.pb.dart' as $1;
import 'runtime.pbenum.dart';

export 'package:protobuf/protobuf.dart' show GeneratedMessageGenericExtensions;

export 'runtime.pbenum.dart';

class ConnectionIntent extends $pb.GeneratedMessage {
  factory ConnectionIntent({
    DesiredState? desiredState,
    $core.String? reasonKey,
    $0.Timestamp? updatedAt,
  }) {
    final result = create();
    if (desiredState != null) result.desiredState = desiredState;
    if (reasonKey != null) result.reasonKey = reasonKey;
    if (updatedAt != null) result.updatedAt = updatedAt;
    return result;
  }

  ConnectionIntent._();

  factory ConnectionIntent.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ConnectionIntent.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ConnectionIntent',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aE<DesiredState>(1, _omitFieldNames ? '' : 'desiredState',
        enumValues: DesiredState.values)
    ..aOS(2, _omitFieldNames ? '' : 'reasonKey')
    ..aOM<$0.Timestamp>(3, _omitFieldNames ? '' : 'updatedAt',
        subBuilder: $0.Timestamp.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ConnectionIntent clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ConnectionIntent copyWith(void Function(ConnectionIntent) updates) =>
      super.copyWith((message) => updates(message as ConnectionIntent))
          as ConnectionIntent;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ConnectionIntent create() => ConnectionIntent._();
  @$core.override
  ConnectionIntent createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ConnectionIntent getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ConnectionIntent>(create);
  static ConnectionIntent? _defaultInstance;

  @$pb.TagNumber(1)
  DesiredState get desiredState => $_getN(0);
  @$pb.TagNumber(1)
  set desiredState(DesiredState value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasDesiredState() => $_has(0);
  @$pb.TagNumber(1)
  void clearDesiredState() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get reasonKey => $_getSZ(1);
  @$pb.TagNumber(2)
  set reasonKey($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasReasonKey() => $_has(1);
  @$pb.TagNumber(2)
  void clearReasonKey() => $_clearField(2);

  @$pb.TagNumber(3)
  $0.Timestamp get updatedAt => $_getN(2);
  @$pb.TagNumber(3)
  set updatedAt($0.Timestamp value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasUpdatedAt() => $_has(2);
  @$pb.TagNumber(3)
  void clearUpdatedAt() => $_clearField(3);
  @$pb.TagNumber(3)
  $0.Timestamp ensureUpdatedAt() => $_ensure(2);
}

class StoredStatePresence extends $pb.GeneratedMessage {
  factory StoredStatePresence({
    $core.bool? cachedMapPresent,
    $core.bool? cachedMapValid,
    $core.bool? mapSigningTrustPresent,
    $core.bool? tokenPresent,
    $core.bool? nodeCredentialPresent,
    $core.bool? deviceFingerprintPresent,
    $core.bool? identityPrivateKeyPresent,
    $core.bool? tunnelPrivateKeyPresent,
  }) {
    final result = create();
    if (cachedMapPresent != null) result.cachedMapPresent = cachedMapPresent;
    if (cachedMapValid != null) result.cachedMapValid = cachedMapValid;
    if (mapSigningTrustPresent != null)
      result.mapSigningTrustPresent = mapSigningTrustPresent;
    if (tokenPresent != null) result.tokenPresent = tokenPresent;
    if (nodeCredentialPresent != null)
      result.nodeCredentialPresent = nodeCredentialPresent;
    if (deviceFingerprintPresent != null)
      result.deviceFingerprintPresent = deviceFingerprintPresent;
    if (identityPrivateKeyPresent != null)
      result.identityPrivateKeyPresent = identityPrivateKeyPresent;
    if (tunnelPrivateKeyPresent != null)
      result.tunnelPrivateKeyPresent = tunnelPrivateKeyPresent;
    return result;
  }

  StoredStatePresence._();

  factory StoredStatePresence.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory StoredStatePresence.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'StoredStatePresence',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOB(1, _omitFieldNames ? '' : 'cachedMapPresent')
    ..aOB(2, _omitFieldNames ? '' : 'cachedMapValid')
    ..aOB(3, _omitFieldNames ? '' : 'mapSigningTrustPresent')
    ..aOB(4, _omitFieldNames ? '' : 'tokenPresent')
    ..aOB(5, _omitFieldNames ? '' : 'nodeCredentialPresent')
    ..aOB(6, _omitFieldNames ? '' : 'deviceFingerprintPresent')
    ..aOB(7, _omitFieldNames ? '' : 'identityPrivateKeyPresent')
    ..aOB(8, _omitFieldNames ? '' : 'tunnelPrivateKeyPresent')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  StoredStatePresence clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  StoredStatePresence copyWith(void Function(StoredStatePresence) updates) =>
      super.copyWith((message) => updates(message as StoredStatePresence))
          as StoredStatePresence;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static StoredStatePresence create() => StoredStatePresence._();
  @$core.override
  StoredStatePresence createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static StoredStatePresence getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<StoredStatePresence>(create);
  static StoredStatePresence? _defaultInstance;

  @$pb.TagNumber(1)
  $core.bool get cachedMapPresent => $_getBF(0);
  @$pb.TagNumber(1)
  set cachedMapPresent($core.bool value) => $_setBool(0, value);
  @$pb.TagNumber(1)
  $core.bool hasCachedMapPresent() => $_has(0);
  @$pb.TagNumber(1)
  void clearCachedMapPresent() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.bool get cachedMapValid => $_getBF(1);
  @$pb.TagNumber(2)
  set cachedMapValid($core.bool value) => $_setBool(1, value);
  @$pb.TagNumber(2)
  $core.bool hasCachedMapValid() => $_has(1);
  @$pb.TagNumber(2)
  void clearCachedMapValid() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.bool get mapSigningTrustPresent => $_getBF(2);
  @$pb.TagNumber(3)
  set mapSigningTrustPresent($core.bool value) => $_setBool(2, value);
  @$pb.TagNumber(3)
  $core.bool hasMapSigningTrustPresent() => $_has(2);
  @$pb.TagNumber(3)
  void clearMapSigningTrustPresent() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.bool get tokenPresent => $_getBF(3);
  @$pb.TagNumber(4)
  set tokenPresent($core.bool value) => $_setBool(3, value);
  @$pb.TagNumber(4)
  $core.bool hasTokenPresent() => $_has(3);
  @$pb.TagNumber(4)
  void clearTokenPresent() => $_clearField(4);

  @$pb.TagNumber(5)
  $core.bool get nodeCredentialPresent => $_getBF(4);
  @$pb.TagNumber(5)
  set nodeCredentialPresent($core.bool value) => $_setBool(4, value);
  @$pb.TagNumber(5)
  $core.bool hasNodeCredentialPresent() => $_has(4);
  @$pb.TagNumber(5)
  void clearNodeCredentialPresent() => $_clearField(5);

  @$pb.TagNumber(6)
  $core.bool get deviceFingerprintPresent => $_getBF(5);
  @$pb.TagNumber(6)
  set deviceFingerprintPresent($core.bool value) => $_setBool(5, value);
  @$pb.TagNumber(6)
  $core.bool hasDeviceFingerprintPresent() => $_has(5);
  @$pb.TagNumber(6)
  void clearDeviceFingerprintPresent() => $_clearField(6);

  @$pb.TagNumber(7)
  $core.bool get identityPrivateKeyPresent => $_getBF(6);
  @$pb.TagNumber(7)
  set identityPrivateKeyPresent($core.bool value) => $_setBool(6, value);
  @$pb.TagNumber(7)
  $core.bool hasIdentityPrivateKeyPresent() => $_has(6);
  @$pb.TagNumber(7)
  void clearIdentityPrivateKeyPresent() => $_clearField(7);

  @$pb.TagNumber(8)
  $core.bool get tunnelPrivateKeyPresent => $_getBF(7);
  @$pb.TagNumber(8)
  set tunnelPrivateKeyPresent($core.bool value) => $_setBool(7, value);
  @$pb.TagNumber(8)
  $core.bool hasTunnelPrivateKeyPresent() => $_has(7);
  @$pb.TagNumber(8)
  void clearTunnelPrivateKeyPresent() => $_clearField(8);
}

class Network extends $pb.GeneratedMessage {
  factory Network({
    $core.String? id,
    $core.String? name,
    $core.String? accountId,
    $core.String? ipv4Cidr,
    $core.String? ipv6Cidr,
    $1.Restriction? selection,
  }) {
    final result = create();
    if (id != null) result.id = id;
    if (name != null) result.name = name;
    if (accountId != null) result.accountId = accountId;
    if (ipv4Cidr != null) result.ipv4Cidr = ipv4Cidr;
    if (ipv6Cidr != null) result.ipv6Cidr = ipv6Cidr;
    if (selection != null) result.selection = selection;
    return result;
  }

  Network._();

  factory Network.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory Network.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'Network',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'id')
    ..aOS(2, _omitFieldNames ? '' : 'name')
    ..aOS(3, _omitFieldNames ? '' : 'accountId')
    ..aOS(4, _omitFieldNames ? '' : 'ipv4Cidr')
    ..aOS(5, _omitFieldNames ? '' : 'ipv6Cidr')
    ..aOM<$1.Restriction>(6, _omitFieldNames ? '' : 'selection',
        subBuilder: $1.Restriction.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Network clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Network copyWith(void Function(Network) updates) =>
      super.copyWith((message) => updates(message as Network)) as Network;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static Network create() => Network._();
  @$core.override
  Network createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static Network getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<Network>(create);
  static Network? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get id => $_getSZ(0);
  @$pb.TagNumber(1)
  set id($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasId() => $_has(0);
  @$pb.TagNumber(1)
  void clearId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get name => $_getSZ(1);
  @$pb.TagNumber(2)
  set name($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasName() => $_has(1);
  @$pb.TagNumber(2)
  void clearName() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get accountId => $_getSZ(2);
  @$pb.TagNumber(3)
  set accountId($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasAccountId() => $_has(2);
  @$pb.TagNumber(3)
  void clearAccountId() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.String get ipv4Cidr => $_getSZ(3);
  @$pb.TagNumber(4)
  set ipv4Cidr($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasIpv4Cidr() => $_has(3);
  @$pb.TagNumber(4)
  void clearIpv4Cidr() => $_clearField(4);

  @$pb.TagNumber(5)
  $core.String get ipv6Cidr => $_getSZ(4);
  @$pb.TagNumber(5)
  set ipv6Cidr($core.String value) => $_setString(4, value);
  @$pb.TagNumber(5)
  $core.bool hasIpv6Cidr() => $_has(4);
  @$pb.TagNumber(5)
  void clearIpv6Cidr() => $_clearField(5);

  @$pb.TagNumber(6)
  $1.Restriction get selection => $_getN(5);
  @$pb.TagNumber(6)
  set selection($1.Restriction value) => $_setField(6, value);
  @$pb.TagNumber(6)
  $core.bool hasSelection() => $_has(5);
  @$pb.TagNumber(6)
  void clearSelection() => $_clearField(6);
  @$pb.TagNumber(6)
  $1.Restriction ensureSelection() => $_ensure(5);
}

class Profile extends $pb.GeneratedMessage {
  factory Profile({
    $core.String? id,
    $core.String? displayName,
    $core.String? controlOrigin,
    $core.String? accountId,
    $core.String? identityDisplayName,
    ProfileState? state,
    $core.bool? active,
    $core.String? selectedNetworkId,
    $1.Restriction? selection,
  }) {
    final result = create();
    if (id != null) result.id = id;
    if (displayName != null) result.displayName = displayName;
    if (controlOrigin != null) result.controlOrigin = controlOrigin;
    if (accountId != null) result.accountId = accountId;
    if (identityDisplayName != null)
      result.identityDisplayName = identityDisplayName;
    if (state != null) result.state = state;
    if (active != null) result.active = active;
    if (selectedNetworkId != null) result.selectedNetworkId = selectedNetworkId;
    if (selection != null) result.selection = selection;
    return result;
  }

  Profile._();

  factory Profile.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory Profile.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'Profile',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'id')
    ..aOS(2, _omitFieldNames ? '' : 'displayName')
    ..aOS(3, _omitFieldNames ? '' : 'controlOrigin')
    ..aOS(4, _omitFieldNames ? '' : 'accountId')
    ..aOS(5, _omitFieldNames ? '' : 'identityDisplayName')
    ..aE<ProfileState>(6, _omitFieldNames ? '' : 'state',
        enumValues: ProfileState.values)
    ..aOB(7, _omitFieldNames ? '' : 'active')
    ..aOS(8, _omitFieldNames ? '' : 'selectedNetworkId')
    ..aOM<$1.Restriction>(9, _omitFieldNames ? '' : 'selection',
        subBuilder: $1.Restriction.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Profile clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Profile copyWith(void Function(Profile) updates) =>
      super.copyWith((message) => updates(message as Profile)) as Profile;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static Profile create() => Profile._();
  @$core.override
  Profile createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static Profile getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<Profile>(create);
  static Profile? _defaultInstance;

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

  /// Canonical HTTPS origin. Immutable after creation.
  @$pb.TagNumber(3)
  $core.String get controlOrigin => $_getSZ(2);
  @$pb.TagNumber(3)
  set controlOrigin($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasControlOrigin() => $_has(2);
  @$pb.TagNumber(3)
  void clearControlOrigin() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.String get accountId => $_getSZ(3);
  @$pb.TagNumber(4)
  set accountId($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasAccountId() => $_has(3);
  @$pb.TagNumber(4)
  void clearAccountId() => $_clearField(4);

  @$pb.TagNumber(5)
  $core.String get identityDisplayName => $_getSZ(4);
  @$pb.TagNumber(5)
  set identityDisplayName($core.String value) => $_setString(4, value);
  @$pb.TagNumber(5)
  $core.bool hasIdentityDisplayName() => $_has(4);
  @$pb.TagNumber(5)
  void clearIdentityDisplayName() => $_clearField(5);

  @$pb.TagNumber(6)
  ProfileState get state => $_getN(5);
  @$pb.TagNumber(6)
  set state(ProfileState value) => $_setField(6, value);
  @$pb.TagNumber(6)
  $core.bool hasState() => $_has(5);
  @$pb.TagNumber(6)
  void clearState() => $_clearField(6);

  @$pb.TagNumber(7)
  $core.bool get active => $_getBF(6);
  @$pb.TagNumber(7)
  set active($core.bool value) => $_setBool(6, value);
  @$pb.TagNumber(7)
  $core.bool hasActive() => $_has(6);
  @$pb.TagNumber(7)
  void clearActive() => $_clearField(7);

  @$pb.TagNumber(8)
  $core.String get selectedNetworkId => $_getSZ(7);
  @$pb.TagNumber(8)
  set selectedNetworkId($core.String value) => $_setString(7, value);
  @$pb.TagNumber(8)
  $core.bool hasSelectedNetworkId() => $_has(7);
  @$pb.TagNumber(8)
  void clearSelectedNetworkId() => $_clearField(8);

  @$pb.TagNumber(9)
  $1.Restriction get selection => $_getN(8);
  @$pb.TagNumber(9)
  set selection($1.Restriction value) => $_setField(9, value);
  @$pb.TagNumber(9)
  $core.bool hasSelection() => $_has(8);
  @$pb.TagNumber(9)
  void clearSelection() => $_clearField(9);
  @$pb.TagNumber(9)
  $1.Restriction ensureSelection() => $_ensure(8);
}

class Session extends $pb.GeneratedMessage {
  factory Session({
    SessionState? state,
    $0.Timestamp? expiresAt,
    $0.Timestamp? warningAt,
    $1.Restriction? renewal,
    $core.bool? seamlessRenewalSupported,
    $core.String? renewalOperationId,
  }) {
    final result = create();
    if (state != null) result.state = state;
    if (expiresAt != null) result.expiresAt = expiresAt;
    if (warningAt != null) result.warningAt = warningAt;
    if (renewal != null) result.renewal = renewal;
    if (seamlessRenewalSupported != null)
      result.seamlessRenewalSupported = seamlessRenewalSupported;
    if (renewalOperationId != null)
      result.renewalOperationId = renewalOperationId;
    return result;
  }

  Session._();

  factory Session.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory Session.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'Session',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aE<SessionState>(1, _omitFieldNames ? '' : 'state',
        enumValues: SessionState.values)
    ..aOM<$0.Timestamp>(2, _omitFieldNames ? '' : 'expiresAt',
        subBuilder: $0.Timestamp.create)
    ..aOM<$0.Timestamp>(3, _omitFieldNames ? '' : 'warningAt',
        subBuilder: $0.Timestamp.create)
    ..aOM<$1.Restriction>(4, _omitFieldNames ? '' : 'renewal',
        subBuilder: $1.Restriction.create)
    ..aOB(5, _omitFieldNames ? '' : 'seamlessRenewalSupported')
    ..aOS(6, _omitFieldNames ? '' : 'renewalOperationId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Session clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Session copyWith(void Function(Session) updates) =>
      super.copyWith((message) => updates(message as Session)) as Session;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static Session create() => Session._();
  @$core.override
  Session createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static Session getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<Session>(create);
  static Session? _defaultInstance;

  @$pb.TagNumber(1)
  SessionState get state => $_getN(0);
  @$pb.TagNumber(1)
  set state(SessionState value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasState() => $_has(0);
  @$pb.TagNumber(1)
  void clearState() => $_clearField(1);

  /// Absence means no authoritative deadline, never "already expired".
  @$pb.TagNumber(2)
  $0.Timestamp get expiresAt => $_getN(1);
  @$pb.TagNumber(2)
  set expiresAt($0.Timestamp value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasExpiresAt() => $_has(1);
  @$pb.TagNumber(2)
  void clearExpiresAt() => $_clearField(2);
  @$pb.TagNumber(2)
  $0.Timestamp ensureExpiresAt() => $_ensure(1);

  @$pb.TagNumber(3)
  $0.Timestamp get warningAt => $_getN(2);
  @$pb.TagNumber(3)
  set warningAt($0.Timestamp value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasWarningAt() => $_has(2);
  @$pb.TagNumber(3)
  void clearWarningAt() => $_clearField(3);
  @$pb.TagNumber(3)
  $0.Timestamp ensureWarningAt() => $_ensure(2);

  @$pb.TagNumber(4)
  $1.Restriction get renewal => $_getN(3);
  @$pb.TagNumber(4)
  set renewal($1.Restriction value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasRenewal() => $_has(3);
  @$pb.TagNumber(4)
  void clearRenewal() => $_clearField(4);
  @$pb.TagNumber(4)
  $1.Restriction ensureRenewal() => $_ensure(3);

  /// Claim is supplied only when supported by the runtime/provider.
  @$pb.TagNumber(5)
  $core.bool get seamlessRenewalSupported => $_getBF(4);
  @$pb.TagNumber(5)
  set seamlessRenewalSupported($core.bool value) => $_setBool(4, value);
  @$pb.TagNumber(5)
  $core.bool hasSeamlessRenewalSupported() => $_has(4);
  @$pb.TagNumber(5)
  void clearSeamlessRenewalSupported() => $_clearField(5);

  @$pb.TagNumber(6)
  $core.String get renewalOperationId => $_getSZ(5);
  @$pb.TagNumber(6)
  set renewalOperationId($core.String value) => $_setString(5, value);
  @$pb.TagNumber(6)
  $core.bool hasRenewalOperationId() => $_has(5);
  @$pb.TagNumber(6)
  void clearRenewalOperationId() => $_clearField(6);
}

class Recovery extends $pb.GeneratedMessage {
  factory Recovery({
    $core.String? operationId,
    ServiceState? state,
    $1.Failure? failure,
  }) {
    final result = create();
    if (operationId != null) result.operationId = operationId;
    if (state != null) result.state = state;
    if (failure != null) result.failure = failure;
    return result;
  }

  Recovery._();

  factory Recovery.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory Recovery.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'Recovery',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'operationId')
    ..aE<ServiceState>(2, _omitFieldNames ? '' : 'state',
        enumValues: ServiceState.values)
    ..aOM<$1.Failure>(3, _omitFieldNames ? '' : 'failure',
        subBuilder: $1.Failure.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Recovery clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Recovery copyWith(void Function(Recovery) updates) =>
      super.copyWith((message) => updates(message as Recovery)) as Recovery;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static Recovery create() => Recovery._();
  @$core.override
  Recovery createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static Recovery getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<Recovery>(create);
  static Recovery? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get operationId => $_getSZ(0);
  @$pb.TagNumber(1)
  set operationId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasOperationId() => $_has(0);
  @$pb.TagNumber(1)
  void clearOperationId() => $_clearField(1);

  @$pb.TagNumber(2)
  ServiceState get state => $_getN(1);
  @$pb.TagNumber(2)
  set state(ServiceState value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasState() => $_has(1);
  @$pb.TagNumber(2)
  void clearState() => $_clearField(2);

  @$pb.TagNumber(3)
  $1.Failure get failure => $_getN(2);
  @$pb.TagNumber(3)
  set failure($1.Failure value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasFailure() => $_has(2);
  @$pb.TagNumber(3)
  void clearFailure() => $_clearField(3);
  @$pb.TagNumber(3)
  $1.Failure ensureFailure() => $_ensure(2);
}

class ServerIdentity extends $pb.GeneratedMessage {
  factory ServerIdentity({
    $core.String? profileId,
    $core.String? controlOrigin,
    $core.String? trustedKeyId,
    $core.String? announcedKeyId,
    $core.bool? changed,
    $core.String? announcementId,
  }) {
    final result = create();
    if (profileId != null) result.profileId = profileId;
    if (controlOrigin != null) result.controlOrigin = controlOrigin;
    if (trustedKeyId != null) result.trustedKeyId = trustedKeyId;
    if (announcedKeyId != null) result.announcedKeyId = announcedKeyId;
    if (changed != null) result.changed = changed;
    if (announcementId != null) result.announcementId = announcementId;
    return result;
  }

  ServerIdentity._();

  factory ServerIdentity.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ServerIdentity.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ServerIdentity',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'profileId')
    ..aOS(2, _omitFieldNames ? '' : 'controlOrigin')
    ..aOS(3, _omitFieldNames ? '' : 'trustedKeyId')
    ..aOS(4, _omitFieldNames ? '' : 'announcedKeyId')
    ..aOB(5, _omitFieldNames ? '' : 'changed')
    ..aOS(6, _omitFieldNames ? '' : 'announcementId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ServerIdentity clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ServerIdentity copyWith(void Function(ServerIdentity) updates) =>
      super.copyWith((message) => updates(message as ServerIdentity))
          as ServerIdentity;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ServerIdentity create() => ServerIdentity._();
  @$core.override
  ServerIdentity createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ServerIdentity getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ServerIdentity>(create);
  static ServerIdentity? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get profileId => $_getSZ(0);
  @$pb.TagNumber(1)
  set profileId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasProfileId() => $_has(0);
  @$pb.TagNumber(1)
  void clearProfileId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get controlOrigin => $_getSZ(1);
  @$pb.TagNumber(2)
  set controlOrigin($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasControlOrigin() => $_has(1);
  @$pb.TagNumber(2)
  void clearControlOrigin() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get trustedKeyId => $_getSZ(2);
  @$pb.TagNumber(3)
  set trustedKeyId($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasTrustedKeyId() => $_has(2);
  @$pb.TagNumber(3)
  void clearTrustedKeyId() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.String get announcedKeyId => $_getSZ(3);
  @$pb.TagNumber(4)
  set announcedKeyId($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasAnnouncedKeyId() => $_has(3);
  @$pb.TagNumber(4)
  void clearAnnouncedKeyId() => $_clearField(4);

  @$pb.TagNumber(5)
  $core.bool get changed => $_getBF(4);
  @$pb.TagNumber(5)
  set changed($core.bool value) => $_setBool(4, value);
  @$pb.TagNumber(5)
  $core.bool hasChanged() => $_has(4);
  @$pb.TagNumber(5)
  void clearChanged() => $_clearField(5);

  /// Opaque revision bound to the announcement; confirmation must match all fields.
  @$pb.TagNumber(6)
  $core.String get announcementId => $_getSZ(5);
  @$pb.TagNumber(6)
  set announcementId($core.String value) => $_setString(5, value);
  @$pb.TagNumber(6)
  $core.bool hasAnnouncementId() => $_has(5);
  @$pb.TagNumber(6)
  void clearAnnouncementId() => $_clearField(6);
}

class Endpoint extends $pb.GeneratedMessage {
  factory Endpoint({
    $core.String? id,
    $core.String? address,
    $core.String? protocol,
    $core.int? priority,
  }) {
    final result = create();
    if (id != null) result.id = id;
    if (address != null) result.address = address;
    if (protocol != null) result.protocol = protocol;
    if (priority != null) result.priority = priority;
    return result;
  }

  Endpoint._();

  factory Endpoint.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory Endpoint.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'Endpoint',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'id')
    ..aOS(2, _omitFieldNames ? '' : 'address')
    ..aOS(3, _omitFieldNames ? '' : 'protocol')
    ..aI(4, _omitFieldNames ? '' : 'priority', fieldType: $pb.PbFieldType.OU3)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Endpoint clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Endpoint copyWith(void Function(Endpoint) updates) =>
      super.copyWith((message) => updates(message as Endpoint)) as Endpoint;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static Endpoint create() => Endpoint._();
  @$core.override
  Endpoint createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static Endpoint getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<Endpoint>(create);
  static Endpoint? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get id => $_getSZ(0);
  @$pb.TagNumber(1)
  set id($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasId() => $_has(0);
  @$pb.TagNumber(1)
  void clearId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get address => $_getSZ(1);
  @$pb.TagNumber(2)
  set address($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasAddress() => $_has(1);
  @$pb.TagNumber(2)
  void clearAddress() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get protocol => $_getSZ(2);
  @$pb.TagNumber(3)
  set protocol($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasProtocol() => $_has(2);
  @$pb.TagNumber(3)
  void clearProtocol() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.int get priority => $_getIZ(3);
  @$pb.TagNumber(4)
  set priority($core.int value) => $_setUnsignedInt32(3, value);
  @$pb.TagNumber(4)
  $core.bool hasPriority() => $_has(3);
  @$pb.TagNumber(4)
  void clearPriority() => $_clearField(4);
}

class PathCandidate extends $pb.GeneratedMessage {
  factory PathCandidate({
    PathKind? kind,
    PathHealth? health,
    $core.String? endpoint,
    $core.String? relayId,
    $core.String? protocol,
    $core.int? priority,
    $2.Duration? rtt,
    $0.Timestamp? checkedAt,
    $0.Timestamp? lastReachableAt,
    $core.int? consecutiveFailures,
    $core.String? reasonKey,
    $core.String? tier,
  }) {
    final result = create();
    if (kind != null) result.kind = kind;
    if (health != null) result.health = health;
    if (endpoint != null) result.endpoint = endpoint;
    if (relayId != null) result.relayId = relayId;
    if (protocol != null) result.protocol = protocol;
    if (priority != null) result.priority = priority;
    if (rtt != null) result.rtt = rtt;
    if (checkedAt != null) result.checkedAt = checkedAt;
    if (lastReachableAt != null) result.lastReachableAt = lastReachableAt;
    if (consecutiveFailures != null)
      result.consecutiveFailures = consecutiveFailures;
    if (reasonKey != null) result.reasonKey = reasonKey;
    if (tier != null) result.tier = tier;
    return result;
  }

  PathCandidate._();

  factory PathCandidate.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory PathCandidate.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'PathCandidate',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aE<PathKind>(1, _omitFieldNames ? '' : 'kind',
        enumValues: PathKind.values)
    ..aE<PathHealth>(2, _omitFieldNames ? '' : 'health',
        enumValues: PathHealth.values)
    ..aOS(3, _omitFieldNames ? '' : 'endpoint')
    ..aOS(4, _omitFieldNames ? '' : 'relayId')
    ..aOS(5, _omitFieldNames ? '' : 'protocol')
    ..aI(6, _omitFieldNames ? '' : 'priority', fieldType: $pb.PbFieldType.OU3)
    ..aOM<$2.Duration>(7, _omitFieldNames ? '' : 'rtt',
        subBuilder: $2.Duration.create)
    ..aOM<$0.Timestamp>(8, _omitFieldNames ? '' : 'checkedAt',
        subBuilder: $0.Timestamp.create)
    ..aOM<$0.Timestamp>(9, _omitFieldNames ? '' : 'lastReachableAt',
        subBuilder: $0.Timestamp.create)
    ..aI(10, _omitFieldNames ? '' : 'consecutiveFailures',
        fieldType: $pb.PbFieldType.OU3)
    ..aOS(11, _omitFieldNames ? '' : 'reasonKey')
    ..aOS(12, _omitFieldNames ? '' : 'tier')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  PathCandidate clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  PathCandidate copyWith(void Function(PathCandidate) updates) =>
      super.copyWith((message) => updates(message as PathCandidate))
          as PathCandidate;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static PathCandidate create() => PathCandidate._();
  @$core.override
  PathCandidate createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static PathCandidate getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<PathCandidate>(create);
  static PathCandidate? _defaultInstance;

  @$pb.TagNumber(1)
  PathKind get kind => $_getN(0);
  @$pb.TagNumber(1)
  set kind(PathKind value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasKind() => $_has(0);
  @$pb.TagNumber(1)
  void clearKind() => $_clearField(1);

  @$pb.TagNumber(2)
  PathHealth get health => $_getN(1);
  @$pb.TagNumber(2)
  set health(PathHealth value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasHealth() => $_has(1);
  @$pb.TagNumber(2)
  void clearHealth() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get endpoint => $_getSZ(2);
  @$pb.TagNumber(3)
  set endpoint($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasEndpoint() => $_has(2);
  @$pb.TagNumber(3)
  void clearEndpoint() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.String get relayId => $_getSZ(3);
  @$pb.TagNumber(4)
  set relayId($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasRelayId() => $_has(3);
  @$pb.TagNumber(4)
  void clearRelayId() => $_clearField(4);

  @$pb.TagNumber(5)
  $core.String get protocol => $_getSZ(4);
  @$pb.TagNumber(5)
  set protocol($core.String value) => $_setString(4, value);
  @$pb.TagNumber(5)
  $core.bool hasProtocol() => $_has(4);
  @$pb.TagNumber(5)
  void clearProtocol() => $_clearField(5);

  @$pb.TagNumber(6)
  $core.int get priority => $_getIZ(5);
  @$pb.TagNumber(6)
  set priority($core.int value) => $_setUnsignedInt32(5, value);
  @$pb.TagNumber(6)
  $core.bool hasPriority() => $_has(5);
  @$pb.TagNumber(6)
  void clearPriority() => $_clearField(6);

  @$pb.TagNumber(7)
  $2.Duration get rtt => $_getN(6);
  @$pb.TagNumber(7)
  set rtt($2.Duration value) => $_setField(7, value);
  @$pb.TagNumber(7)
  $core.bool hasRtt() => $_has(6);
  @$pb.TagNumber(7)
  void clearRtt() => $_clearField(7);
  @$pb.TagNumber(7)
  $2.Duration ensureRtt() => $_ensure(6);

  @$pb.TagNumber(8)
  $0.Timestamp get checkedAt => $_getN(7);
  @$pb.TagNumber(8)
  set checkedAt($0.Timestamp value) => $_setField(8, value);
  @$pb.TagNumber(8)
  $core.bool hasCheckedAt() => $_has(7);
  @$pb.TagNumber(8)
  void clearCheckedAt() => $_clearField(8);
  @$pb.TagNumber(8)
  $0.Timestamp ensureCheckedAt() => $_ensure(7);

  @$pb.TagNumber(9)
  $0.Timestamp get lastReachableAt => $_getN(8);
  @$pb.TagNumber(9)
  set lastReachableAt($0.Timestamp value) => $_setField(9, value);
  @$pb.TagNumber(9)
  $core.bool hasLastReachableAt() => $_has(8);
  @$pb.TagNumber(9)
  void clearLastReachableAt() => $_clearField(9);
  @$pb.TagNumber(9)
  $0.Timestamp ensureLastReachableAt() => $_ensure(8);

  @$pb.TagNumber(10)
  $core.int get consecutiveFailures => $_getIZ(9);
  @$pb.TagNumber(10)
  set consecutiveFailures($core.int value) => $_setUnsignedInt32(9, value);
  @$pb.TagNumber(10)
  $core.bool hasConsecutiveFailures() => $_has(9);
  @$pb.TagNumber(10)
  void clearConsecutiveFailures() => $_clearField(10);

  @$pb.TagNumber(11)
  $core.String get reasonKey => $_getSZ(10);
  @$pb.TagNumber(11)
  set reasonKey($core.String value) => $_setString(10, value);
  @$pb.TagNumber(11)
  $core.bool hasReasonKey() => $_has(10);
  @$pb.TagNumber(11)
  void clearReasonKey() => $_clearField(11);

  @$pb.TagNumber(12)
  $core.String get tier => $_getSZ(11);
  @$pb.TagNumber(12)
  set tier($core.String value) => $_setString(11, value);
  @$pb.TagNumber(12)
  $core.bool hasTier() => $_has(11);
  @$pb.TagNumber(12)
  void clearTier() => $_clearField(12);
}

class Peer extends $pb.GeneratedMessage {
  factory Peer({
    $core.String? id,
    $core.String? hostname,
    $core.Iterable<$core.String>? overlayAddresses,
    $core.Iterable<PathCandidate>? candidates,
    PathKind? selectedPath,
    $core.String? selectedEndpoint,
    $0.Timestamp? lastTransitionAt,
    $core.String? selectionReasonKey,
  }) {
    final result = create();
    if (id != null) result.id = id;
    if (hostname != null) result.hostname = hostname;
    if (overlayAddresses != null)
      result.overlayAddresses.addAll(overlayAddresses);
    if (candidates != null) result.candidates.addAll(candidates);
    if (selectedPath != null) result.selectedPath = selectedPath;
    if (selectedEndpoint != null) result.selectedEndpoint = selectedEndpoint;
    if (lastTransitionAt != null) result.lastTransitionAt = lastTransitionAt;
    if (selectionReasonKey != null)
      result.selectionReasonKey = selectionReasonKey;
    return result;
  }

  Peer._();

  factory Peer.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory Peer.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'Peer',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'id')
    ..aOS(2, _omitFieldNames ? '' : 'hostname')
    ..pPS(3, _omitFieldNames ? '' : 'overlayAddresses')
    ..pPM<PathCandidate>(4, _omitFieldNames ? '' : 'candidates',
        subBuilder: PathCandidate.create)
    ..aE<PathKind>(5, _omitFieldNames ? '' : 'selectedPath',
        enumValues: PathKind.values)
    ..aOS(6, _omitFieldNames ? '' : 'selectedEndpoint')
    ..aOM<$0.Timestamp>(7, _omitFieldNames ? '' : 'lastTransitionAt',
        subBuilder: $0.Timestamp.create)
    ..aOS(8, _omitFieldNames ? '' : 'selectionReasonKey')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Peer clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Peer copyWith(void Function(Peer) updates) =>
      super.copyWith((message) => updates(message as Peer)) as Peer;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static Peer create() => Peer._();
  @$core.override
  Peer createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static Peer getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<Peer>(create);
  static Peer? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get id => $_getSZ(0);
  @$pb.TagNumber(1)
  set id($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasId() => $_has(0);
  @$pb.TagNumber(1)
  void clearId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get hostname => $_getSZ(1);
  @$pb.TagNumber(2)
  set hostname($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasHostname() => $_has(1);
  @$pb.TagNumber(2)
  void clearHostname() => $_clearField(2);

  @$pb.TagNumber(3)
  $pb.PbList<$core.String> get overlayAddresses => $_getList(2);

  @$pb.TagNumber(4)
  $pb.PbList<PathCandidate> get candidates => $_getList(3);

  @$pb.TagNumber(5)
  PathKind get selectedPath => $_getN(4);
  @$pb.TagNumber(5)
  set selectedPath(PathKind value) => $_setField(5, value);
  @$pb.TagNumber(5)
  $core.bool hasSelectedPath() => $_has(4);
  @$pb.TagNumber(5)
  void clearSelectedPath() => $_clearField(5);

  @$pb.TagNumber(6)
  $core.String get selectedEndpoint => $_getSZ(5);
  @$pb.TagNumber(6)
  set selectedEndpoint($core.String value) => $_setString(5, value);
  @$pb.TagNumber(6)
  $core.bool hasSelectedEndpoint() => $_has(5);
  @$pb.TagNumber(6)
  void clearSelectedEndpoint() => $_clearField(6);

  @$pb.TagNumber(7)
  $0.Timestamp get lastTransitionAt => $_getN(6);
  @$pb.TagNumber(7)
  set lastTransitionAt($0.Timestamp value) => $_setField(7, value);
  @$pb.TagNumber(7)
  $core.bool hasLastTransitionAt() => $_has(6);
  @$pb.TagNumber(7)
  void clearLastTransitionAt() => $_clearField(7);
  @$pb.TagNumber(7)
  $0.Timestamp ensureLastTransitionAt() => $_ensure(6);

  @$pb.TagNumber(8)
  $core.String get selectionReasonKey => $_getSZ(7);
  @$pb.TagNumber(8)
  set selectionReasonKey($core.String value) => $_setString(7, value);
  @$pb.TagNumber(8)
  $core.bool hasSelectionReasonKey() => $_has(7);
  @$pb.TagNumber(8)
  void clearSelectionReasonKey() => $_clearField(8);
}

class AgentStatus extends $pb.GeneratedMessage {
  factory AgentStatus({
    AgentSnapshotState? snapshotState,
    $fixnum.Int64? mapRevision,
    $fixnum.Int64? targetMapRevision,
    $0.Timestamp? generatedAt,
    $core.bool? connectionPaused,
    $core.String? nodeId,
    $core.String? networkId,
    $core.Iterable<$core.String>? overlayAddresses,
    $core.int? peerCount,
    $core.bool? stunOk,
    $core.bool? relayOk,
    Endpoint? selectedRelay,
    $core.int? relayAttemptCount,
    $core.int? directPathCount,
    $core.int? relayPathCount,
    $1.Failure? lastFailure,
  }) {
    final result = create();
    if (snapshotState != null) result.snapshotState = snapshotState;
    if (mapRevision != null) result.mapRevision = mapRevision;
    if (targetMapRevision != null) result.targetMapRevision = targetMapRevision;
    if (generatedAt != null) result.generatedAt = generatedAt;
    if (connectionPaused != null) result.connectionPaused = connectionPaused;
    if (nodeId != null) result.nodeId = nodeId;
    if (networkId != null) result.networkId = networkId;
    if (overlayAddresses != null)
      result.overlayAddresses.addAll(overlayAddresses);
    if (peerCount != null) result.peerCount = peerCount;
    if (stunOk != null) result.stunOk = stunOk;
    if (relayOk != null) result.relayOk = relayOk;
    if (selectedRelay != null) result.selectedRelay = selectedRelay;
    if (relayAttemptCount != null) result.relayAttemptCount = relayAttemptCount;
    if (directPathCount != null) result.directPathCount = directPathCount;
    if (relayPathCount != null) result.relayPathCount = relayPathCount;
    if (lastFailure != null) result.lastFailure = lastFailure;
    return result;
  }

  AgentStatus._();

  factory AgentStatus.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory AgentStatus.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'AgentStatus',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aE<AgentSnapshotState>(1, _omitFieldNames ? '' : 'snapshotState',
        enumValues: AgentSnapshotState.values)
    ..a<$fixnum.Int64>(
        2, _omitFieldNames ? '' : 'mapRevision', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..a<$fixnum.Int64>(
        3, _omitFieldNames ? '' : 'targetMapRevision', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..aOM<$0.Timestamp>(4, _omitFieldNames ? '' : 'generatedAt',
        subBuilder: $0.Timestamp.create)
    ..aOB(5, _omitFieldNames ? '' : 'connectionPaused')
    ..aOS(6, _omitFieldNames ? '' : 'nodeId')
    ..aOS(7, _omitFieldNames ? '' : 'networkId')
    ..pPS(8, _omitFieldNames ? '' : 'overlayAddresses')
    ..aI(9, _omitFieldNames ? '' : 'peerCount', fieldType: $pb.PbFieldType.OU3)
    ..aOB(10, _omitFieldNames ? '' : 'stunOk')
    ..aOB(11, _omitFieldNames ? '' : 'relayOk')
    ..aOM<Endpoint>(12, _omitFieldNames ? '' : 'selectedRelay',
        subBuilder: Endpoint.create)
    ..aI(13, _omitFieldNames ? '' : 'relayAttemptCount',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(14, _omitFieldNames ? '' : 'directPathCount',
        fieldType: $pb.PbFieldType.OU3)
    ..aI(15, _omitFieldNames ? '' : 'relayPathCount',
        fieldType: $pb.PbFieldType.OU3)
    ..aOM<$1.Failure>(16, _omitFieldNames ? '' : 'lastFailure',
        subBuilder: $1.Failure.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  AgentStatus clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  AgentStatus copyWith(void Function(AgentStatus) updates) =>
      super.copyWith((message) => updates(message as AgentStatus))
          as AgentStatus;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static AgentStatus create() => AgentStatus._();
  @$core.override
  AgentStatus createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static AgentStatus getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<AgentStatus>(create);
  static AgentStatus? _defaultInstance;

  @$pb.TagNumber(1)
  AgentSnapshotState get snapshotState => $_getN(0);
  @$pb.TagNumber(1)
  set snapshotState(AgentSnapshotState value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasSnapshotState() => $_has(0);
  @$pb.TagNumber(1)
  void clearSnapshotState() => $_clearField(1);

  @$pb.TagNumber(2)
  $fixnum.Int64 get mapRevision => $_getI64(1);
  @$pb.TagNumber(2)
  set mapRevision($fixnum.Int64 value) => $_setInt64(1, value);
  @$pb.TagNumber(2)
  $core.bool hasMapRevision() => $_has(1);
  @$pb.TagNumber(2)
  void clearMapRevision() => $_clearField(2);

  @$pb.TagNumber(3)
  $fixnum.Int64 get targetMapRevision => $_getI64(2);
  @$pb.TagNumber(3)
  set targetMapRevision($fixnum.Int64 value) => $_setInt64(2, value);
  @$pb.TagNumber(3)
  $core.bool hasTargetMapRevision() => $_has(2);
  @$pb.TagNumber(3)
  void clearTargetMapRevision() => $_clearField(3);

  @$pb.TagNumber(4)
  $0.Timestamp get generatedAt => $_getN(3);
  @$pb.TagNumber(4)
  set generatedAt($0.Timestamp value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasGeneratedAt() => $_has(3);
  @$pb.TagNumber(4)
  void clearGeneratedAt() => $_clearField(4);
  @$pb.TagNumber(4)
  $0.Timestamp ensureGeneratedAt() => $_ensure(3);

  @$pb.TagNumber(5)
  $core.bool get connectionPaused => $_getBF(4);
  @$pb.TagNumber(5)
  set connectionPaused($core.bool value) => $_setBool(4, value);
  @$pb.TagNumber(5)
  $core.bool hasConnectionPaused() => $_has(4);
  @$pb.TagNumber(5)
  void clearConnectionPaused() => $_clearField(5);

  @$pb.TagNumber(6)
  $core.String get nodeId => $_getSZ(5);
  @$pb.TagNumber(6)
  set nodeId($core.String value) => $_setString(5, value);
  @$pb.TagNumber(6)
  $core.bool hasNodeId() => $_has(5);
  @$pb.TagNumber(6)
  void clearNodeId() => $_clearField(6);

  @$pb.TagNumber(7)
  $core.String get networkId => $_getSZ(6);
  @$pb.TagNumber(7)
  set networkId($core.String value) => $_setString(6, value);
  @$pb.TagNumber(7)
  $core.bool hasNetworkId() => $_has(6);
  @$pb.TagNumber(7)
  void clearNetworkId() => $_clearField(7);

  @$pb.TagNumber(8)
  $pb.PbList<$core.String> get overlayAddresses => $_getList(7);

  @$pb.TagNumber(9)
  $core.int get peerCount => $_getIZ(8);
  @$pb.TagNumber(9)
  set peerCount($core.int value) => $_setUnsignedInt32(8, value);
  @$pb.TagNumber(9)
  $core.bool hasPeerCount() => $_has(8);
  @$pb.TagNumber(9)
  void clearPeerCount() => $_clearField(9);

  @$pb.TagNumber(10)
  $core.bool get stunOk => $_getBF(9);
  @$pb.TagNumber(10)
  set stunOk($core.bool value) => $_setBool(9, value);
  @$pb.TagNumber(10)
  $core.bool hasStunOk() => $_has(9);
  @$pb.TagNumber(10)
  void clearStunOk() => $_clearField(10);

  @$pb.TagNumber(11)
  $core.bool get relayOk => $_getBF(10);
  @$pb.TagNumber(11)
  set relayOk($core.bool value) => $_setBool(10, value);
  @$pb.TagNumber(11)
  $core.bool hasRelayOk() => $_has(10);
  @$pb.TagNumber(11)
  void clearRelayOk() => $_clearField(11);

  @$pb.TagNumber(12)
  Endpoint get selectedRelay => $_getN(11);
  @$pb.TagNumber(12)
  set selectedRelay(Endpoint value) => $_setField(12, value);
  @$pb.TagNumber(12)
  $core.bool hasSelectedRelay() => $_has(11);
  @$pb.TagNumber(12)
  void clearSelectedRelay() => $_clearField(12);
  @$pb.TagNumber(12)
  Endpoint ensureSelectedRelay() => $_ensure(11);

  @$pb.TagNumber(13)
  $core.int get relayAttemptCount => $_getIZ(12);
  @$pb.TagNumber(13)
  set relayAttemptCount($core.int value) => $_setUnsignedInt32(12, value);
  @$pb.TagNumber(13)
  $core.bool hasRelayAttemptCount() => $_has(12);
  @$pb.TagNumber(13)
  void clearRelayAttemptCount() => $_clearField(13);

  @$pb.TagNumber(14)
  $core.int get directPathCount => $_getIZ(13);
  @$pb.TagNumber(14)
  set directPathCount($core.int value) => $_setUnsignedInt32(13, value);
  @$pb.TagNumber(14)
  $core.bool hasDirectPathCount() => $_has(13);
  @$pb.TagNumber(14)
  void clearDirectPathCount() => $_clearField(14);

  @$pb.TagNumber(15)
  $core.int get relayPathCount => $_getIZ(14);
  @$pb.TagNumber(15)
  set relayPathCount($core.int value) => $_setUnsignedInt32(14, value);
  @$pb.TagNumber(15)
  $core.bool hasRelayPathCount() => $_has(14);
  @$pb.TagNumber(15)
  void clearRelayPathCount() => $_clearField(15);

  @$pb.TagNumber(16)
  $1.Failure get lastFailure => $_getN(15);
  @$pb.TagNumber(16)
  set lastFailure($1.Failure value) => $_setField(16, value);
  @$pb.TagNumber(16)
  $core.bool hasLastFailure() => $_has(15);
  @$pb.TagNumber(16)
  void clearLastFailure() => $_clearField(16);
  @$pb.TagNumber(16)
  $1.Failure ensureLastFailure() => $_ensure(15);
}

class ControlProbe extends $pb.GeneratedMessage {
  factory ControlProbe({
    $core.bool? ok,
    $core.String? origin,
    $core.int? httpStatus,
    $1.Failure? failure,
    $core.Iterable<ControlProbe>? attempts,
  }) {
    final result = create();
    if (ok != null) result.ok = ok;
    if (origin != null) result.origin = origin;
    if (httpStatus != null) result.httpStatus = httpStatus;
    if (failure != null) result.failure = failure;
    if (attempts != null) result.attempts.addAll(attempts);
    return result;
  }

  ControlProbe._();

  factory ControlProbe.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ControlProbe.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ControlProbe',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOB(1, _omitFieldNames ? '' : 'ok')
    ..aOS(2, _omitFieldNames ? '' : 'origin')
    ..aI(3, _omitFieldNames ? '' : 'httpStatus', fieldType: $pb.PbFieldType.OU3)
    ..aOM<$1.Failure>(4, _omitFieldNames ? '' : 'failure',
        subBuilder: $1.Failure.create)
    ..pPM<ControlProbe>(5, _omitFieldNames ? '' : 'attempts',
        subBuilder: ControlProbe.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ControlProbe clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ControlProbe copyWith(void Function(ControlProbe) updates) =>
      super.copyWith((message) => updates(message as ControlProbe))
          as ControlProbe;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ControlProbe create() => ControlProbe._();
  @$core.override
  ControlProbe createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static ControlProbe getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ControlProbe>(create);
  static ControlProbe? _defaultInstance;

  @$pb.TagNumber(1)
  $core.bool get ok => $_getBF(0);
  @$pb.TagNumber(1)
  set ok($core.bool value) => $_setBool(0, value);
  @$pb.TagNumber(1)
  $core.bool hasOk() => $_has(0);
  @$pb.TagNumber(1)
  void clearOk() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get origin => $_getSZ(1);
  @$pb.TagNumber(2)
  set origin($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasOrigin() => $_has(1);
  @$pb.TagNumber(2)
  void clearOrigin() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.int get httpStatus => $_getIZ(2);
  @$pb.TagNumber(3)
  set httpStatus($core.int value) => $_setUnsignedInt32(2, value);
  @$pb.TagNumber(3)
  $core.bool hasHttpStatus() => $_has(2);
  @$pb.TagNumber(3)
  void clearHttpStatus() => $_clearField(3);

  @$pb.TagNumber(4)
  $1.Failure get failure => $_getN(3);
  @$pb.TagNumber(4)
  set failure($1.Failure value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasFailure() => $_has(3);
  @$pb.TagNumber(4)
  void clearFailure() => $_clearField(4);
  @$pb.TagNumber(4)
  $1.Failure ensureFailure() => $_ensure(3);

  @$pb.TagNumber(5)
  $pb.PbList<ControlProbe> get attempts => $_getList(4);
}

class Status extends $pb.GeneratedMessage {
  factory Status({
    $1.SnapshotMetadata? metadata,
    ServiceState? serviceState,
    ControlState? controlState,
    ConnectionIntent? intent,
    $core.bool? userDisconnected,
    $core.String? activeProfileId,
    $core.String? accountId,
    $core.String? nodeId,
    $core.String? hostname,
    Network? network,
    $core.bool? ephemeral,
    $core.String? enrollmentRequestId,
    $1.UserAction? pendingAction,
    $core.Iterable<$core.String>? overlayAddresses,
    $fixnum.Int64? mapRevision,
    $core.int? peerCount,
    StoredStatePresence? storedState,
    Recovery? recovery,
    Session? session,
    AgentStatus? agent,
    ControlProbe? control,
    $core.Iterable<Endpoint>? stunEndpoints,
    $core.Iterable<Endpoint>? relayEndpoints,
    $core.Iterable<$1.Failure>? failures,
    $core.String? routeTable,
  }) {
    final result = create();
    if (metadata != null) result.metadata = metadata;
    if (serviceState != null) result.serviceState = serviceState;
    if (controlState != null) result.controlState = controlState;
    if (intent != null) result.intent = intent;
    if (userDisconnected != null) result.userDisconnected = userDisconnected;
    if (activeProfileId != null) result.activeProfileId = activeProfileId;
    if (accountId != null) result.accountId = accountId;
    if (nodeId != null) result.nodeId = nodeId;
    if (hostname != null) result.hostname = hostname;
    if (network != null) result.network = network;
    if (ephemeral != null) result.ephemeral = ephemeral;
    if (enrollmentRequestId != null)
      result.enrollmentRequestId = enrollmentRequestId;
    if (pendingAction != null) result.pendingAction = pendingAction;
    if (overlayAddresses != null)
      result.overlayAddresses.addAll(overlayAddresses);
    if (mapRevision != null) result.mapRevision = mapRevision;
    if (peerCount != null) result.peerCount = peerCount;
    if (storedState != null) result.storedState = storedState;
    if (recovery != null) result.recovery = recovery;
    if (session != null) result.session = session;
    if (agent != null) result.agent = agent;
    if (control != null) result.control = control;
    if (stunEndpoints != null) result.stunEndpoints.addAll(stunEndpoints);
    if (relayEndpoints != null) result.relayEndpoints.addAll(relayEndpoints);
    if (failures != null) result.failures.addAll(failures);
    if (routeTable != null) result.routeTable = routeTable;
    return result;
  }

  Status._();

  factory Status.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory Status.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'Status',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.SnapshotMetadata>(1, _omitFieldNames ? '' : 'metadata',
        subBuilder: $1.SnapshotMetadata.create)
    ..aE<ServiceState>(2, _omitFieldNames ? '' : 'serviceState',
        enumValues: ServiceState.values)
    ..aE<ControlState>(3, _omitFieldNames ? '' : 'controlState',
        enumValues: ControlState.values)
    ..aOM<ConnectionIntent>(4, _omitFieldNames ? '' : 'intent',
        subBuilder: ConnectionIntent.create)
    ..aOB(5, _omitFieldNames ? '' : 'userDisconnected')
    ..aOS(6, _omitFieldNames ? '' : 'activeProfileId')
    ..aOS(7, _omitFieldNames ? '' : 'accountId')
    ..aOS(8, _omitFieldNames ? '' : 'nodeId')
    ..aOS(9, _omitFieldNames ? '' : 'hostname')
    ..aOM<Network>(10, _omitFieldNames ? '' : 'network',
        subBuilder: Network.create)
    ..aOB(11, _omitFieldNames ? '' : 'ephemeral')
    ..aOS(12, _omitFieldNames ? '' : 'enrollmentRequestId')
    ..aOM<$1.UserAction>(13, _omitFieldNames ? '' : 'pendingAction',
        subBuilder: $1.UserAction.create)
    ..pPS(14, _omitFieldNames ? '' : 'overlayAddresses')
    ..a<$fixnum.Int64>(
        15, _omitFieldNames ? '' : 'mapRevision', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..aI(16, _omitFieldNames ? '' : 'peerCount', fieldType: $pb.PbFieldType.OU3)
    ..aOM<StoredStatePresence>(17, _omitFieldNames ? '' : 'storedState',
        subBuilder: StoredStatePresence.create)
    ..aOM<Recovery>(18, _omitFieldNames ? '' : 'recovery',
        subBuilder: Recovery.create)
    ..aOM<Session>(19, _omitFieldNames ? '' : 'session',
        subBuilder: Session.create)
    ..aOM<AgentStatus>(20, _omitFieldNames ? '' : 'agent',
        subBuilder: AgentStatus.create)
    ..aOM<ControlProbe>(21, _omitFieldNames ? '' : 'control',
        subBuilder: ControlProbe.create)
    ..pPM<Endpoint>(22, _omitFieldNames ? '' : 'stunEndpoints',
        subBuilder: Endpoint.create)
    ..pPM<Endpoint>(23, _omitFieldNames ? '' : 'relayEndpoints',
        subBuilder: Endpoint.create)
    ..pPM<$1.Failure>(24, _omitFieldNames ? '' : 'failures',
        subBuilder: $1.Failure.create)
    ..aOS(25, _omitFieldNames ? '' : 'routeTable')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Status clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Status copyWith(void Function(Status) updates) =>
      super.copyWith((message) => updates(message as Status)) as Status;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static Status create() => Status._();
  @$core.override
  Status createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static Status getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<Status>(create);
  static Status? _defaultInstance;

  @$pb.TagNumber(1)
  $1.SnapshotMetadata get metadata => $_getN(0);
  @$pb.TagNumber(1)
  set metadata($1.SnapshotMetadata value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasMetadata() => $_has(0);
  @$pb.TagNumber(1)
  void clearMetadata() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.SnapshotMetadata ensureMetadata() => $_ensure(0);

  @$pb.TagNumber(2)
  ServiceState get serviceState => $_getN(1);
  @$pb.TagNumber(2)
  set serviceState(ServiceState value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasServiceState() => $_has(1);
  @$pb.TagNumber(2)
  void clearServiceState() => $_clearField(2);

  @$pb.TagNumber(3)
  ControlState get controlState => $_getN(2);
  @$pb.TagNumber(3)
  set controlState(ControlState value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasControlState() => $_has(2);
  @$pb.TagNumber(3)
  void clearControlState() => $_clearField(3);

  @$pb.TagNumber(4)
  ConnectionIntent get intent => $_getN(3);
  @$pb.TagNumber(4)
  set intent(ConnectionIntent value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasIntent() => $_has(3);
  @$pb.TagNumber(4)
  void clearIntent() => $_clearField(4);
  @$pb.TagNumber(4)
  ConnectionIntent ensureIntent() => $_ensure(3);

  @$pb.TagNumber(5)
  $core.bool get userDisconnected => $_getBF(4);
  @$pb.TagNumber(5)
  set userDisconnected($core.bool value) => $_setBool(4, value);
  @$pb.TagNumber(5)
  $core.bool hasUserDisconnected() => $_has(4);
  @$pb.TagNumber(5)
  void clearUserDisconnected() => $_clearField(5);

  @$pb.TagNumber(6)
  $core.String get activeProfileId => $_getSZ(5);
  @$pb.TagNumber(6)
  set activeProfileId($core.String value) => $_setString(5, value);
  @$pb.TagNumber(6)
  $core.bool hasActiveProfileId() => $_has(5);
  @$pb.TagNumber(6)
  void clearActiveProfileId() => $_clearField(6);

  @$pb.TagNumber(7)
  $core.String get accountId => $_getSZ(6);
  @$pb.TagNumber(7)
  set accountId($core.String value) => $_setString(6, value);
  @$pb.TagNumber(7)
  $core.bool hasAccountId() => $_has(6);
  @$pb.TagNumber(7)
  void clearAccountId() => $_clearField(7);

  @$pb.TagNumber(8)
  $core.String get nodeId => $_getSZ(7);
  @$pb.TagNumber(8)
  set nodeId($core.String value) => $_setString(7, value);
  @$pb.TagNumber(8)
  $core.bool hasNodeId() => $_has(7);
  @$pb.TagNumber(8)
  void clearNodeId() => $_clearField(8);

  @$pb.TagNumber(9)
  $core.String get hostname => $_getSZ(8);
  @$pb.TagNumber(9)
  set hostname($core.String value) => $_setString(8, value);
  @$pb.TagNumber(9)
  $core.bool hasHostname() => $_has(8);
  @$pb.TagNumber(9)
  void clearHostname() => $_clearField(9);

  @$pb.TagNumber(10)
  Network get network => $_getN(9);
  @$pb.TagNumber(10)
  set network(Network value) => $_setField(10, value);
  @$pb.TagNumber(10)
  $core.bool hasNetwork() => $_has(9);
  @$pb.TagNumber(10)
  void clearNetwork() => $_clearField(10);
  @$pb.TagNumber(10)
  Network ensureNetwork() => $_ensure(9);

  @$pb.TagNumber(11)
  $core.bool get ephemeral => $_getBF(10);
  @$pb.TagNumber(11)
  set ephemeral($core.bool value) => $_setBool(10, value);
  @$pb.TagNumber(11)
  $core.bool hasEphemeral() => $_has(10);
  @$pb.TagNumber(11)
  void clearEphemeral() => $_clearField(11);

  @$pb.TagNumber(12)
  $core.String get enrollmentRequestId => $_getSZ(11);
  @$pb.TagNumber(12)
  set enrollmentRequestId($core.String value) => $_setString(11, value);
  @$pb.TagNumber(12)
  $core.bool hasEnrollmentRequestId() => $_has(11);
  @$pb.TagNumber(12)
  void clearEnrollmentRequestId() => $_clearField(12);

  @$pb.TagNumber(13)
  $1.UserAction get pendingAction => $_getN(12);
  @$pb.TagNumber(13)
  set pendingAction($1.UserAction value) => $_setField(13, value);
  @$pb.TagNumber(13)
  $core.bool hasPendingAction() => $_has(12);
  @$pb.TagNumber(13)
  void clearPendingAction() => $_clearField(13);
  @$pb.TagNumber(13)
  $1.UserAction ensurePendingAction() => $_ensure(12);

  @$pb.TagNumber(14)
  $pb.PbList<$core.String> get overlayAddresses => $_getList(13);

  @$pb.TagNumber(15)
  $fixnum.Int64 get mapRevision => $_getI64(14);
  @$pb.TagNumber(15)
  set mapRevision($fixnum.Int64 value) => $_setInt64(14, value);
  @$pb.TagNumber(15)
  $core.bool hasMapRevision() => $_has(14);
  @$pb.TagNumber(15)
  void clearMapRevision() => $_clearField(15);

  @$pb.TagNumber(16)
  $core.int get peerCount => $_getIZ(15);
  @$pb.TagNumber(16)
  set peerCount($core.int value) => $_setUnsignedInt32(15, value);
  @$pb.TagNumber(16)
  $core.bool hasPeerCount() => $_has(15);
  @$pb.TagNumber(16)
  void clearPeerCount() => $_clearField(16);

  @$pb.TagNumber(17)
  StoredStatePresence get storedState => $_getN(16);
  @$pb.TagNumber(17)
  set storedState(StoredStatePresence value) => $_setField(17, value);
  @$pb.TagNumber(17)
  $core.bool hasStoredState() => $_has(16);
  @$pb.TagNumber(17)
  void clearStoredState() => $_clearField(17);
  @$pb.TagNumber(17)
  StoredStatePresence ensureStoredState() => $_ensure(16);

  @$pb.TagNumber(18)
  Recovery get recovery => $_getN(17);
  @$pb.TagNumber(18)
  set recovery(Recovery value) => $_setField(18, value);
  @$pb.TagNumber(18)
  $core.bool hasRecovery() => $_has(17);
  @$pb.TagNumber(18)
  void clearRecovery() => $_clearField(18);
  @$pb.TagNumber(18)
  Recovery ensureRecovery() => $_ensure(17);

  @$pb.TagNumber(19)
  Session get session => $_getN(18);
  @$pb.TagNumber(19)
  set session(Session value) => $_setField(19, value);
  @$pb.TagNumber(19)
  $core.bool hasSession() => $_has(18);
  @$pb.TagNumber(19)
  void clearSession() => $_clearField(19);
  @$pb.TagNumber(19)
  Session ensureSession() => $_ensure(18);

  @$pb.TagNumber(20)
  AgentStatus get agent => $_getN(19);
  @$pb.TagNumber(20)
  set agent(AgentStatus value) => $_setField(20, value);
  @$pb.TagNumber(20)
  $core.bool hasAgent() => $_has(19);
  @$pb.TagNumber(20)
  void clearAgent() => $_clearField(20);
  @$pb.TagNumber(20)
  AgentStatus ensureAgent() => $_ensure(19);

  @$pb.TagNumber(21)
  ControlProbe get control => $_getN(20);
  @$pb.TagNumber(21)
  set control(ControlProbe value) => $_setField(21, value);
  @$pb.TagNumber(21)
  $core.bool hasControl() => $_has(20);
  @$pb.TagNumber(21)
  void clearControl() => $_clearField(21);
  @$pb.TagNumber(21)
  ControlProbe ensureControl() => $_ensure(20);

  @$pb.TagNumber(22)
  $pb.PbList<Endpoint> get stunEndpoints => $_getList(21);

  @$pb.TagNumber(23)
  $pb.PbList<Endpoint> get relayEndpoints => $_getList(22);

  @$pb.TagNumber(24)
  $pb.PbList<$1.Failure> get failures => $_getList(23);

  /// Read-only runtime route-table identifier, never a caller-selected path.
  @$pb.TagNumber(25)
  $core.String get routeTable => $_getSZ(24);
  @$pb.TagNumber(25)
  set routeTable($core.String value) => $_setString(24, value);
  @$pb.TagNumber(25)
  $core.bool hasRouteTable() => $_has(24);
  @$pb.TagNumber(25)
  void clearRouteTable() => $_clearField(25);
}

class TunnelPeer extends $pb.GeneratedMessage {
  factory TunnelPeer({
    $core.String? peerId,
    $core.String? publicKey,
    $core.String? endpoint,
    $core.Iterable<$core.String>? allowedIps,
    $0.Timestamp? latestHandshake,
    $fixnum.Int64? receivedBytes,
    $fixnum.Int64? transmittedBytes,
    $2.Duration? persistentKeepalive,
  }) {
    final result = create();
    if (peerId != null) result.peerId = peerId;
    if (publicKey != null) result.publicKey = publicKey;
    if (endpoint != null) result.endpoint = endpoint;
    if (allowedIps != null) result.allowedIps.addAll(allowedIps);
    if (latestHandshake != null) result.latestHandshake = latestHandshake;
    if (receivedBytes != null) result.receivedBytes = receivedBytes;
    if (transmittedBytes != null) result.transmittedBytes = transmittedBytes;
    if (persistentKeepalive != null)
      result.persistentKeepalive = persistentKeepalive;
    return result;
  }

  TunnelPeer._();

  factory TunnelPeer.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory TunnelPeer.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'TunnelPeer',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'peerId')
    ..aOS(2, _omitFieldNames ? '' : 'publicKey')
    ..aOS(3, _omitFieldNames ? '' : 'endpoint')
    ..pPS(4, _omitFieldNames ? '' : 'allowedIps')
    ..aOM<$0.Timestamp>(5, _omitFieldNames ? '' : 'latestHandshake',
        subBuilder: $0.Timestamp.create)
    ..a<$fixnum.Int64>(
        6, _omitFieldNames ? '' : 'receivedBytes', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..a<$fixnum.Int64>(
        7, _omitFieldNames ? '' : 'transmittedBytes', $pb.PbFieldType.OU6,
        defaultOrMaker: $fixnum.Int64.ZERO)
    ..aOM<$2.Duration>(8, _omitFieldNames ? '' : 'persistentKeepalive',
        subBuilder: $2.Duration.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  TunnelPeer clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  TunnelPeer copyWith(void Function(TunnelPeer) updates) =>
      super.copyWith((message) => updates(message as TunnelPeer)) as TunnelPeer;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static TunnelPeer create() => TunnelPeer._();
  @$core.override
  TunnelPeer createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static TunnelPeer getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<TunnelPeer>(create);
  static TunnelPeer? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get peerId => $_getSZ(0);
  @$pb.TagNumber(1)
  set peerId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasPeerId() => $_has(0);
  @$pb.TagNumber(1)
  void clearPeerId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get publicKey => $_getSZ(1);
  @$pb.TagNumber(2)
  set publicKey($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasPublicKey() => $_has(1);
  @$pb.TagNumber(2)
  void clearPublicKey() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get endpoint => $_getSZ(2);
  @$pb.TagNumber(3)
  set endpoint($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasEndpoint() => $_has(2);
  @$pb.TagNumber(3)
  void clearEndpoint() => $_clearField(3);

  @$pb.TagNumber(4)
  $pb.PbList<$core.String> get allowedIps => $_getList(3);

  @$pb.TagNumber(5)
  $0.Timestamp get latestHandshake => $_getN(4);
  @$pb.TagNumber(5)
  set latestHandshake($0.Timestamp value) => $_setField(5, value);
  @$pb.TagNumber(5)
  $core.bool hasLatestHandshake() => $_has(4);
  @$pb.TagNumber(5)
  void clearLatestHandshake() => $_clearField(5);
  @$pb.TagNumber(5)
  $0.Timestamp ensureLatestHandshake() => $_ensure(4);

  @$pb.TagNumber(6)
  $fixnum.Int64 get receivedBytes => $_getI64(5);
  @$pb.TagNumber(6)
  set receivedBytes($fixnum.Int64 value) => $_setInt64(5, value);
  @$pb.TagNumber(6)
  $core.bool hasReceivedBytes() => $_has(5);
  @$pb.TagNumber(6)
  void clearReceivedBytes() => $_clearField(6);

  @$pb.TagNumber(7)
  $fixnum.Int64 get transmittedBytes => $_getI64(6);
  @$pb.TagNumber(7)
  set transmittedBytes($fixnum.Int64 value) => $_setInt64(6, value);
  @$pb.TagNumber(7)
  $core.bool hasTransmittedBytes() => $_has(6);
  @$pb.TagNumber(7)
  void clearTransmittedBytes() => $_clearField(7);

  @$pb.TagNumber(8)
  $2.Duration get persistentKeepalive => $_getN(7);
  @$pb.TagNumber(8)
  set persistentKeepalive($2.Duration value) => $_setField(8, value);
  @$pb.TagNumber(8)
  $core.bool hasPersistentKeepalive() => $_has(7);
  @$pb.TagNumber(8)
  void clearPersistentKeepalive() => $_clearField(8);
  @$pb.TagNumber(8)
  $2.Duration ensurePersistentKeepalive() => $_ensure(7);
}

class TunnelInspection extends $pb.GeneratedMessage {
  factory TunnelInspection({
    $core.bool? ok,
    $core.String? interfaceName,
    $core.int? mtu,
    $core.int? listenPort,
    $core.Iterable<TunnelPeer>? peers,
    $1.Failure? failure,
  }) {
    final result = create();
    if (ok != null) result.ok = ok;
    if (interfaceName != null) result.interfaceName = interfaceName;
    if (mtu != null) result.mtu = mtu;
    if (listenPort != null) result.listenPort = listenPort;
    if (peers != null) result.peers.addAll(peers);
    if (failure != null) result.failure = failure;
    return result;
  }

  TunnelInspection._();

  factory TunnelInspection.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory TunnelInspection.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'TunnelInspection',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOB(1, _omitFieldNames ? '' : 'ok')
    ..aOS(2, _omitFieldNames ? '' : 'interfaceName')
    ..aI(3, _omitFieldNames ? '' : 'mtu', fieldType: $pb.PbFieldType.OU3)
    ..aI(4, _omitFieldNames ? '' : 'listenPort', fieldType: $pb.PbFieldType.OU3)
    ..pPM<TunnelPeer>(5, _omitFieldNames ? '' : 'peers',
        subBuilder: TunnelPeer.create)
    ..aOM<$1.Failure>(6, _omitFieldNames ? '' : 'failure',
        subBuilder: $1.Failure.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  TunnelInspection clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  TunnelInspection copyWith(void Function(TunnelInspection) updates) =>
      super.copyWith((message) => updates(message as TunnelInspection))
          as TunnelInspection;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static TunnelInspection create() => TunnelInspection._();
  @$core.override
  TunnelInspection createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static TunnelInspection getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<TunnelInspection>(create);
  static TunnelInspection? _defaultInstance;

  @$pb.TagNumber(1)
  $core.bool get ok => $_getBF(0);
  @$pb.TagNumber(1)
  set ok($core.bool value) => $_setBool(0, value);
  @$pb.TagNumber(1)
  $core.bool hasOk() => $_has(0);
  @$pb.TagNumber(1)
  void clearOk() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get interfaceName => $_getSZ(1);
  @$pb.TagNumber(2)
  set interfaceName($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasInterfaceName() => $_has(1);
  @$pb.TagNumber(2)
  void clearInterfaceName() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.int get mtu => $_getIZ(2);
  @$pb.TagNumber(3)
  set mtu($core.int value) => $_setUnsignedInt32(2, value);
  @$pb.TagNumber(3)
  $core.bool hasMtu() => $_has(2);
  @$pb.TagNumber(3)
  void clearMtu() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.int get listenPort => $_getIZ(3);
  @$pb.TagNumber(4)
  set listenPort($core.int value) => $_setUnsignedInt32(3, value);
  @$pb.TagNumber(4)
  $core.bool hasListenPort() => $_has(3);
  @$pb.TagNumber(4)
  void clearListenPort() => $_clearField(4);

  @$pb.TagNumber(5)
  $pb.PbList<TunnelPeer> get peers => $_getList(4);

  @$pb.TagNumber(6)
  $1.Failure get failure => $_getN(5);
  @$pb.TagNumber(6)
  set failure($1.Failure value) => $_setField(6, value);
  @$pb.TagNumber(6)
  $core.bool hasFailure() => $_has(5);
  @$pb.TagNumber(6)
  void clearFailure() => $_clearField(6);
  @$pb.TagNumber(6)
  $1.Failure ensureFailure() => $_ensure(5);
}

class Interface extends $pb.GeneratedMessage {
  factory Interface({
    $core.String? name,
    $core.int? index,
    $core.int? mtu,
    $core.Iterable<$core.String>? addresses,
    $core.Iterable<$core.String>? prefixes,
    $core.Iterable<$core.String>? flags,
    $1.Failure? failure,
  }) {
    final result = create();
    if (name != null) result.name = name;
    if (index != null) result.index = index;
    if (mtu != null) result.mtu = mtu;
    if (addresses != null) result.addresses.addAll(addresses);
    if (prefixes != null) result.prefixes.addAll(prefixes);
    if (flags != null) result.flags.addAll(flags);
    if (failure != null) result.failure = failure;
    return result;
  }

  Interface._();

  factory Interface.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory Interface.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'Interface',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'name')
    ..aI(2, _omitFieldNames ? '' : 'index', fieldType: $pb.PbFieldType.OU3)
    ..aI(3, _omitFieldNames ? '' : 'mtu', fieldType: $pb.PbFieldType.OU3)
    ..pPS(4, _omitFieldNames ? '' : 'addresses')
    ..pPS(5, _omitFieldNames ? '' : 'prefixes')
    ..pPS(6, _omitFieldNames ? '' : 'flags')
    ..aOM<$1.Failure>(7, _omitFieldNames ? '' : 'failure',
        subBuilder: $1.Failure.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Interface clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Interface copyWith(void Function(Interface) updates) =>
      super.copyWith((message) => updates(message as Interface)) as Interface;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static Interface create() => Interface._();
  @$core.override
  Interface createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static Interface getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<Interface>(create);
  static Interface? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get name => $_getSZ(0);
  @$pb.TagNumber(1)
  set name($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasName() => $_has(0);
  @$pb.TagNumber(1)
  void clearName() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.int get index => $_getIZ(1);
  @$pb.TagNumber(2)
  set index($core.int value) => $_setUnsignedInt32(1, value);
  @$pb.TagNumber(2)
  $core.bool hasIndex() => $_has(1);
  @$pb.TagNumber(2)
  void clearIndex() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.int get mtu => $_getIZ(2);
  @$pb.TagNumber(3)
  set mtu($core.int value) => $_setUnsignedInt32(2, value);
  @$pb.TagNumber(3)
  $core.bool hasMtu() => $_has(2);
  @$pb.TagNumber(3)
  void clearMtu() => $_clearField(3);

  @$pb.TagNumber(4)
  $pb.PbList<$core.String> get addresses => $_getList(3);

  @$pb.TagNumber(5)
  $pb.PbList<$core.String> get prefixes => $_getList(4);

  @$pb.TagNumber(6)
  $pb.PbList<$core.String> get flags => $_getList(5);

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
}

class RouteInspection extends $pb.GeneratedMessage {
  factory RouteInspection({
    $core.String? target,
    $core.String? interfaceName,
    $core.bool? usesInterface,
    $core.String? peerId,
    $1.Failure? failure,
  }) {
    final result = create();
    if (target != null) result.target = target;
    if (interfaceName != null) result.interfaceName = interfaceName;
    if (usesInterface != null) result.usesInterface = usesInterface;
    if (peerId != null) result.peerId = peerId;
    if (failure != null) result.failure = failure;
    return result;
  }

  RouteInspection._();

  factory RouteInspection.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RouteInspection.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RouteInspection',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'target')
    ..aOS(2, _omitFieldNames ? '' : 'interfaceName')
    ..aOB(3, _omitFieldNames ? '' : 'usesInterface')
    ..aOS(4, _omitFieldNames ? '' : 'peerId')
    ..aOM<$1.Failure>(5, _omitFieldNames ? '' : 'failure',
        subBuilder: $1.Failure.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RouteInspection clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RouteInspection copyWith(void Function(RouteInspection) updates) =>
      super.copyWith((message) => updates(message as RouteInspection))
          as RouteInspection;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RouteInspection create() => RouteInspection._();
  @$core.override
  RouteInspection createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static RouteInspection getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RouteInspection>(create);
  static RouteInspection? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get target => $_getSZ(0);
  @$pb.TagNumber(1)
  set target($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasTarget() => $_has(0);
  @$pb.TagNumber(1)
  void clearTarget() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get interfaceName => $_getSZ(1);
  @$pb.TagNumber(2)
  set interfaceName($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasInterfaceName() => $_has(1);
  @$pb.TagNumber(2)
  void clearInterfaceName() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.bool get usesInterface => $_getBF(2);
  @$pb.TagNumber(3)
  set usesInterface($core.bool value) => $_setBool(2, value);
  @$pb.TagNumber(3)
  $core.bool hasUsesInterface() => $_has(2);
  @$pb.TagNumber(3)
  void clearUsesInterface() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.String get peerId => $_getSZ(3);
  @$pb.TagNumber(4)
  set peerId($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasPeerId() => $_has(3);
  @$pb.TagNumber(4)
  void clearPeerId() => $_clearField(4);

  @$pb.TagNumber(5)
  $1.Failure get failure => $_getN(4);
  @$pb.TagNumber(5)
  set failure($1.Failure value) => $_setField(5, value);
  @$pb.TagNumber(5)
  $core.bool hasFailure() => $_has(4);
  @$pb.TagNumber(5)
  void clearFailure() => $_clearField(5);
  @$pb.TagNumber(5)
  $1.Failure ensureFailure() => $_ensure(4);
}

class RouteConflict extends $pb.GeneratedMessage {
  factory RouteConflict({
    $core.String? overlayCidr,
    $core.String? localPrefix,
    $core.String? interfaceName,
    $core.String? reasonKey,
  }) {
    final result = create();
    if (overlayCidr != null) result.overlayCidr = overlayCidr;
    if (localPrefix != null) result.localPrefix = localPrefix;
    if (interfaceName != null) result.interfaceName = interfaceName;
    if (reasonKey != null) result.reasonKey = reasonKey;
    return result;
  }

  RouteConflict._();

  factory RouteConflict.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RouteConflict.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RouteConflict',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'overlayCidr')
    ..aOS(2, _omitFieldNames ? '' : 'localPrefix')
    ..aOS(3, _omitFieldNames ? '' : 'interfaceName')
    ..aOS(4, _omitFieldNames ? '' : 'reasonKey')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RouteConflict clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RouteConflict copyWith(void Function(RouteConflict) updates) =>
      super.copyWith((message) => updates(message as RouteConflict))
          as RouteConflict;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RouteConflict create() => RouteConflict._();
  @$core.override
  RouteConflict createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static RouteConflict getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RouteConflict>(create);
  static RouteConflict? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get overlayCidr => $_getSZ(0);
  @$pb.TagNumber(1)
  set overlayCidr($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasOverlayCidr() => $_has(0);
  @$pb.TagNumber(1)
  void clearOverlayCidr() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get localPrefix => $_getSZ(1);
  @$pb.TagNumber(2)
  set localPrefix($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasLocalPrefix() => $_has(1);
  @$pb.TagNumber(2)
  void clearLocalPrefix() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get interfaceName => $_getSZ(2);
  @$pb.TagNumber(3)
  set interfaceName($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasInterfaceName() => $_has(2);
  @$pb.TagNumber(3)
  void clearInterfaceName() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.String get reasonKey => $_getSZ(3);
  @$pb.TagNumber(4)
  set reasonKey($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasReasonKey() => $_has(3);
  @$pb.TagNumber(4)
  void clearReasonKey() => $_clearField(4);
}

class DnsRecord extends $pb.GeneratedMessage {
  factory DnsRecord({
    $core.String? nodeId,
    $core.String? hostname,
    $core.String? label,
    $core.String? fqdn,
    $core.Iterable<$core.String>? addresses,
  }) {
    final result = create();
    if (nodeId != null) result.nodeId = nodeId;
    if (hostname != null) result.hostname = hostname;
    if (label != null) result.label = label;
    if (fqdn != null) result.fqdn = fqdn;
    if (addresses != null) result.addresses.addAll(addresses);
    return result;
  }

  DnsRecord._();

  factory DnsRecord.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory DnsRecord.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'DnsRecord',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'nodeId')
    ..aOS(2, _omitFieldNames ? '' : 'hostname')
    ..aOS(3, _omitFieldNames ? '' : 'label')
    ..aOS(4, _omitFieldNames ? '' : 'fqdn')
    ..pPS(5, _omitFieldNames ? '' : 'addresses')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DnsRecord clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DnsRecord copyWith(void Function(DnsRecord) updates) =>
      super.copyWith((message) => updates(message as DnsRecord)) as DnsRecord;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static DnsRecord create() => DnsRecord._();
  @$core.override
  DnsRecord createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static DnsRecord getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<DnsRecord>(create);
  static DnsRecord? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get nodeId => $_getSZ(0);
  @$pb.TagNumber(1)
  set nodeId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasNodeId() => $_has(0);
  @$pb.TagNumber(1)
  void clearNodeId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get hostname => $_getSZ(1);
  @$pb.TagNumber(2)
  set hostname($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasHostname() => $_has(1);
  @$pb.TagNumber(2)
  void clearHostname() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get label => $_getSZ(2);
  @$pb.TagNumber(3)
  set label($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasLabel() => $_has(2);
  @$pb.TagNumber(3)
  void clearLabel() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.String get fqdn => $_getSZ(3);
  @$pb.TagNumber(4)
  set fqdn($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasFqdn() => $_has(3);
  @$pb.TagNumber(4)
  void clearFqdn() => $_clearField(4);

  @$pb.TagNumber(5)
  $pb.PbList<$core.String> get addresses => $_getList(4);
}

class DnsDiagnostics extends $pb.GeneratedMessage {
  factory DnsDiagnostics({
    $core.String? searchDomain,
    $2.Duration? ttl,
    $core.Iterable<$core.String>? servers,
    $core.Iterable<DnsRecord>? records,
  }) {
    final result = create();
    if (searchDomain != null) result.searchDomain = searchDomain;
    if (ttl != null) result.ttl = ttl;
    if (servers != null) result.servers.addAll(servers);
    if (records != null) result.records.addAll(records);
    return result;
  }

  DnsDiagnostics._();

  factory DnsDiagnostics.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory DnsDiagnostics.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'DnsDiagnostics',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'searchDomain')
    ..aOM<$2.Duration>(2, _omitFieldNames ? '' : 'ttl',
        subBuilder: $2.Duration.create)
    ..pPS(3, _omitFieldNames ? '' : 'servers')
    ..pPM<DnsRecord>(4, _omitFieldNames ? '' : 'records',
        subBuilder: DnsRecord.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DnsDiagnostics clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DnsDiagnostics copyWith(void Function(DnsDiagnostics) updates) =>
      super.copyWith((message) => updates(message as DnsDiagnostics))
          as DnsDiagnostics;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static DnsDiagnostics create() => DnsDiagnostics._();
  @$core.override
  DnsDiagnostics createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static DnsDiagnostics getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<DnsDiagnostics>(create);
  static DnsDiagnostics? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get searchDomain => $_getSZ(0);
  @$pb.TagNumber(1)
  set searchDomain($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasSearchDomain() => $_has(0);
  @$pb.TagNumber(1)
  void clearSearchDomain() => $_clearField(1);

  @$pb.TagNumber(2)
  $2.Duration get ttl => $_getN(1);
  @$pb.TagNumber(2)
  set ttl($2.Duration value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasTtl() => $_has(1);
  @$pb.TagNumber(2)
  void clearTtl() => $_clearField(2);
  @$pb.TagNumber(2)
  $2.Duration ensureTtl() => $_ensure(1);

  @$pb.TagNumber(3)
  $pb.PbList<$core.String> get servers => $_getList(2);

  @$pb.TagNumber(4)
  $pb.PbList<DnsRecord> get records => $_getList(3);
}

class LogEntry extends $pb.GeneratedMessage {
  factory LogEntry({
    $0.Timestamp? timestamp,
    $core.String? message,
  }) {
    final result = create();
    if (timestamp != null) result.timestamp = timestamp;
    if (message != null) result.message = message;
    return result;
  }

  LogEntry._();

  factory LogEntry.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory LogEntry.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'LogEntry',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$0.Timestamp>(1, _omitFieldNames ? '' : 'timestamp',
        subBuilder: $0.Timestamp.create)
    ..aOS(2, _omitFieldNames ? '' : 'message')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  LogEntry clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  LogEntry copyWith(void Function(LogEntry) updates) =>
      super.copyWith((message) => updates(message as LogEntry)) as LogEntry;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static LogEntry create() => LogEntry._();
  @$core.override
  LogEntry createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static LogEntry getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<LogEntry>(create);
  static LogEntry? _defaultInstance;

  @$pb.TagNumber(1)
  $0.Timestamp get timestamp => $_getN(0);
  @$pb.TagNumber(1)
  set timestamp($0.Timestamp value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasTimestamp() => $_has(0);
  @$pb.TagNumber(1)
  void clearTimestamp() => $_clearField(1);
  @$pb.TagNumber(1)
  $0.Timestamp ensureTimestamp() => $_ensure(0);

  /// Bounded producer-redacted diagnostic; never localized as business state.
  @$pb.TagNumber(2)
  $core.String get message => $_getSZ(1);
  @$pb.TagNumber(2)
  set message($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasMessage() => $_has(1);
  @$pb.TagNumber(2)
  void clearMessage() => $_clearField(2);
}

class Diagnostics extends $pb.GeneratedMessage {
  factory Diagnostics({
    $1.SnapshotMetadata? metadata,
    $1.BuildIdentity? client,
    $core.String? osName,
    $core.String? osVersion,
    $core.String? goVersion,
    Status? status,
    TunnelInspection? tunnel,
    $core.Iterable<Interface>? interfaces,
    $core.Iterable<RouteInspection>? routes,
    $core.Iterable<RouteConflict>? routeConflicts,
    DnsDiagnostics? dns,
    $core.Iterable<LogEntry>? recentLogs,
    $core.Iterable<$1.Failure>? failures,
    $core.bool? truncated,
    $core.Iterable<Peer>? peers,
    $core.bool? defaultRoutePresent,
  }) {
    final result = create();
    if (metadata != null) result.metadata = metadata;
    if (client != null) result.client = client;
    if (osName != null) result.osName = osName;
    if (osVersion != null) result.osVersion = osVersion;
    if (goVersion != null) result.goVersion = goVersion;
    if (status != null) result.status = status;
    if (tunnel != null) result.tunnel = tunnel;
    if (interfaces != null) result.interfaces.addAll(interfaces);
    if (routes != null) result.routes.addAll(routes);
    if (routeConflicts != null) result.routeConflicts.addAll(routeConflicts);
    if (dns != null) result.dns = dns;
    if (recentLogs != null) result.recentLogs.addAll(recentLogs);
    if (failures != null) result.failures.addAll(failures);
    if (truncated != null) result.truncated = truncated;
    if (peers != null) result.peers.addAll(peers);
    if (defaultRoutePresent != null)
      result.defaultRoutePresent = defaultRoutePresent;
    return result;
  }

  Diagnostics._();

  factory Diagnostics.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory Diagnostics.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'Diagnostics',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'client.v0'),
      createEmptyInstance: create)
    ..aOM<$1.SnapshotMetadata>(1, _omitFieldNames ? '' : 'metadata',
        subBuilder: $1.SnapshotMetadata.create)
    ..aOM<$1.BuildIdentity>(2, _omitFieldNames ? '' : 'client',
        subBuilder: $1.BuildIdentity.create)
    ..aOS(3, _omitFieldNames ? '' : 'osName')
    ..aOS(4, _omitFieldNames ? '' : 'osVersion')
    ..aOS(5, _omitFieldNames ? '' : 'goVersion')
    ..aOM<Status>(6, _omitFieldNames ? '' : 'status', subBuilder: Status.create)
    ..aOM<TunnelInspection>(7, _omitFieldNames ? '' : 'tunnel',
        subBuilder: TunnelInspection.create)
    ..pPM<Interface>(8, _omitFieldNames ? '' : 'interfaces',
        subBuilder: Interface.create)
    ..pPM<RouteInspection>(9, _omitFieldNames ? '' : 'routes',
        subBuilder: RouteInspection.create)
    ..pPM<RouteConflict>(10, _omitFieldNames ? '' : 'routeConflicts',
        subBuilder: RouteConflict.create)
    ..aOM<DnsDiagnostics>(11, _omitFieldNames ? '' : 'dns',
        subBuilder: DnsDiagnostics.create)
    ..pPM<LogEntry>(12, _omitFieldNames ? '' : 'recentLogs',
        subBuilder: LogEntry.create)
    ..pPM<$1.Failure>(13, _omitFieldNames ? '' : 'failures',
        subBuilder: $1.Failure.create)
    ..aOB(14, _omitFieldNames ? '' : 'truncated')
    ..pPM<Peer>(15, _omitFieldNames ? '' : 'peers', subBuilder: Peer.create)
    ..aOB(16, _omitFieldNames ? '' : 'defaultRoutePresent')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Diagnostics clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Diagnostics copyWith(void Function(Diagnostics) updates) =>
      super.copyWith((message) => updates(message as Diagnostics))
          as Diagnostics;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static Diagnostics create() => Diagnostics._();
  @$core.override
  Diagnostics createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static Diagnostics getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<Diagnostics>(create);
  static Diagnostics? _defaultInstance;

  @$pb.TagNumber(1)
  $1.SnapshotMetadata get metadata => $_getN(0);
  @$pb.TagNumber(1)
  set metadata($1.SnapshotMetadata value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasMetadata() => $_has(0);
  @$pb.TagNumber(1)
  void clearMetadata() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.SnapshotMetadata ensureMetadata() => $_ensure(0);

  @$pb.TagNumber(2)
  $1.BuildIdentity get client => $_getN(1);
  @$pb.TagNumber(2)
  set client($1.BuildIdentity value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasClient() => $_has(1);
  @$pb.TagNumber(2)
  void clearClient() => $_clearField(2);
  @$pb.TagNumber(2)
  $1.BuildIdentity ensureClient() => $_ensure(1);

  @$pb.TagNumber(3)
  $core.String get osName => $_getSZ(2);
  @$pb.TagNumber(3)
  set osName($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasOsName() => $_has(2);
  @$pb.TagNumber(3)
  void clearOsName() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.String get osVersion => $_getSZ(3);
  @$pb.TagNumber(4)
  set osVersion($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasOsVersion() => $_has(3);
  @$pb.TagNumber(4)
  void clearOsVersion() => $_clearField(4);

  @$pb.TagNumber(5)
  $core.String get goVersion => $_getSZ(4);
  @$pb.TagNumber(5)
  set goVersion($core.String value) => $_setString(4, value);
  @$pb.TagNumber(5)
  $core.bool hasGoVersion() => $_has(4);
  @$pb.TagNumber(5)
  void clearGoVersion() => $_clearField(5);

  @$pb.TagNumber(6)
  Status get status => $_getN(5);
  @$pb.TagNumber(6)
  set status(Status value) => $_setField(6, value);
  @$pb.TagNumber(6)
  $core.bool hasStatus() => $_has(5);
  @$pb.TagNumber(6)
  void clearStatus() => $_clearField(6);
  @$pb.TagNumber(6)
  Status ensureStatus() => $_ensure(5);

  @$pb.TagNumber(7)
  TunnelInspection get tunnel => $_getN(6);
  @$pb.TagNumber(7)
  set tunnel(TunnelInspection value) => $_setField(7, value);
  @$pb.TagNumber(7)
  $core.bool hasTunnel() => $_has(6);
  @$pb.TagNumber(7)
  void clearTunnel() => $_clearField(7);
  @$pb.TagNumber(7)
  TunnelInspection ensureTunnel() => $_ensure(6);

  @$pb.TagNumber(8)
  $pb.PbList<Interface> get interfaces => $_getList(7);

  @$pb.TagNumber(9)
  $pb.PbList<RouteInspection> get routes => $_getList(8);

  @$pb.TagNumber(10)
  $pb.PbList<RouteConflict> get routeConflicts => $_getList(9);

  @$pb.TagNumber(11)
  DnsDiagnostics get dns => $_getN(10);
  @$pb.TagNumber(11)
  set dns(DnsDiagnostics value) => $_setField(11, value);
  @$pb.TagNumber(11)
  $core.bool hasDns() => $_has(10);
  @$pb.TagNumber(11)
  void clearDns() => $_clearField(11);
  @$pb.TagNumber(11)
  DnsDiagnostics ensureDns() => $_ensure(10);

  @$pb.TagNumber(12)
  $pb.PbList<LogEntry> get recentLogs => $_getList(11);

  @$pb.TagNumber(13)
  $pb.PbList<$1.Failure> get failures => $_getList(12);

  @$pb.TagNumber(14)
  $core.bool get truncated => $_getBF(13);
  @$pb.TagNumber(14)
  set truncated($core.bool value) => $_setBool(13, value);
  @$pb.TagNumber(14)
  $core.bool hasTruncated() => $_has(13);
  @$pb.TagNumber(14)
  void clearTruncated() => $_clearField(14);

  @$pb.TagNumber(15)
  $pb.PbList<Peer> get peers => $_getList(14);

  @$pb.TagNumber(16)
  $core.bool get defaultRoutePresent => $_getBF(15);
  @$pb.TagNumber(16)
  set defaultRoutePresent($core.bool value) => $_setBool(15, value);
  @$pb.TagNumber(16)
  $core.bool hasDefaultRoutePresent() => $_has(15);
  @$pb.TagNumber(16)
  void clearDefaultRoutePresent() => $_clearField(16);
}

const $core.bool _omitFieldNames =
    $core.bool.fromEnvironment('protobuf.omit_field_names');
const $core.bool _omitMessageNames =
    $core.bool.fromEnvironment('protobuf.omit_message_names');
