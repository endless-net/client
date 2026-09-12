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

import 'package:protobuf/protobuf.dart' as $pb;

class ServiceState extends $pb.ProtobufEnum {
  static const ServiceState SERVICE_STATE_UNSPECIFIED =
      ServiceState._(0, _omitEnumNames ? '' : 'SERVICE_STATE_UNSPECIFIED');
  static const ServiceState SERVICE_STATE_CONNECTED =
      ServiceState._(1, _omitEnumNames ? '' : 'SERVICE_STATE_CONNECTED');
  static const ServiceState SERVICE_STATE_DISCONNECTED =
      ServiceState._(2, _omitEnumNames ? '' : 'SERVICE_STATE_DISCONNECTED');
  static const ServiceState SERVICE_STATE_DEGRADED =
      ServiceState._(3, _omitEnumNames ? '' : 'SERVICE_STATE_DEGRADED');
  static const ServiceState SERVICE_STATE_ERROR =
      ServiceState._(4, _omitEnumNames ? '' : 'SERVICE_STATE_ERROR');
  static const ServiceState SERVICE_STATE_NEEDS_ENROLLMENT =
      ServiceState._(5, _omitEnumNames ? '' : 'SERVICE_STATE_NEEDS_ENROLLMENT');
  static const ServiceState SERVICE_STATE_NEEDS_APPROVAL =
      ServiceState._(6, _omitEnumNames ? '' : 'SERVICE_STATE_NEEDS_APPROVAL');
  static const ServiceState SERVICE_STATE_SERVER_IDENTITY_CHANGED =
      ServiceState._(
          7, _omitEnumNames ? '' : 'SERVICE_STATE_SERVER_IDENTITY_CHANGED');
  static const ServiceState SERVICE_STATE_RECOVERING =
      ServiceState._(8, _omitEnumNames ? '' : 'SERVICE_STATE_RECOVERING');
  static const ServiceState SERVICE_STATE_RECOVERY_BLOCKED =
      ServiceState._(9, _omitEnumNames ? '' : 'SERVICE_STATE_RECOVERY_BLOCKED');
  static const ServiceState SERVICE_STATE_POLICY_BLOCKED =
      ServiceState._(10, _omitEnumNames ? '' : 'SERVICE_STATE_POLICY_BLOCKED');
  static const ServiceState SERVICE_STATE_NEEDS_LOGIN =
      ServiceState._(11, _omitEnumNames ? '' : 'SERVICE_STATE_NEEDS_LOGIN');

  static const $core.List<ServiceState> values = <ServiceState>[
    SERVICE_STATE_UNSPECIFIED,
    SERVICE_STATE_CONNECTED,
    SERVICE_STATE_DISCONNECTED,
    SERVICE_STATE_DEGRADED,
    SERVICE_STATE_ERROR,
    SERVICE_STATE_NEEDS_ENROLLMENT,
    SERVICE_STATE_NEEDS_APPROVAL,
    SERVICE_STATE_SERVER_IDENTITY_CHANGED,
    SERVICE_STATE_RECOVERING,
    SERVICE_STATE_RECOVERY_BLOCKED,
    SERVICE_STATE_POLICY_BLOCKED,
    SERVICE_STATE_NEEDS_LOGIN,
  ];

  static final $core.List<ServiceState?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 11);
  static ServiceState? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ServiceState._(super.value, super.name);
}

class ControlState extends $pb.ProtobufEnum {
  static const ControlState CONTROL_STATE_UNSPECIFIED =
      ControlState._(0, _omitEnumNames ? '' : 'CONTROL_STATE_UNSPECIFIED');
  static const ControlState CONTROL_STATE_PENDING_APPROVAL =
      ControlState._(1, _omitEnumNames ? '' : 'CONTROL_STATE_PENDING_APPROVAL');
  static const ControlState CONTROL_STATE_DEGRADED =
      ControlState._(2, _omitEnumNames ? '' : 'CONTROL_STATE_DEGRADED');
  static const ControlState CONTROL_STATE_OFFLINE_CACHE =
      ControlState._(3, _omitEnumNames ? '' : 'CONTROL_STATE_OFFLINE_CACHE');
  static const ControlState CONTROL_STATE_READY =
      ControlState._(4, _omitEnumNames ? '' : 'CONTROL_STATE_READY');
  static const ControlState CONTROL_STATE_REGISTERED =
      ControlState._(5, _omitEnumNames ? '' : 'CONTROL_STATE_REGISTERED');
  static const ControlState CONTROL_STATE_CACHE_INVALID =
      ControlState._(6, _omitEnumNames ? '' : 'CONTROL_STATE_CACHE_INVALID');
  static const ControlState CONTROL_STATE_ERROR =
      ControlState._(7, _omitEnumNames ? '' : 'CONTROL_STATE_ERROR');
  static const ControlState CONTROL_STATE_NOT_REGISTERED =
      ControlState._(8, _omitEnumNames ? '' : 'CONTROL_STATE_NOT_REGISTERED');
  static const ControlState CONTROL_STATE_DISCONNECTED =
      ControlState._(9, _omitEnumNames ? '' : 'CONTROL_STATE_DISCONNECTED');
  static const ControlState CONTROL_STATE_SERVER_IDENTITY_CHANGED =
      ControlState._(
          10, _omitEnumNames ? '' : 'CONTROL_STATE_SERVER_IDENTITY_CHANGED');
  static const ControlState CONTROL_STATE_RECOVERING =
      ControlState._(11, _omitEnumNames ? '' : 'CONTROL_STATE_RECOVERING');
  static const ControlState CONTROL_STATE_RECOVERY_BLOCKED = ControlState._(
      12, _omitEnumNames ? '' : 'CONTROL_STATE_RECOVERY_BLOCKED');
  static const ControlState CONTROL_STATE_POLICY_BLOCKED =
      ControlState._(13, _omitEnumNames ? '' : 'CONTROL_STATE_POLICY_BLOCKED');
  static const ControlState CONTROL_STATE_NEEDS_LOGIN =
      ControlState._(14, _omitEnumNames ? '' : 'CONTROL_STATE_NEEDS_LOGIN');

  static const $core.List<ControlState> values = <ControlState>[
    CONTROL_STATE_UNSPECIFIED,
    CONTROL_STATE_PENDING_APPROVAL,
    CONTROL_STATE_DEGRADED,
    CONTROL_STATE_OFFLINE_CACHE,
    CONTROL_STATE_READY,
    CONTROL_STATE_REGISTERED,
    CONTROL_STATE_CACHE_INVALID,
    CONTROL_STATE_ERROR,
    CONTROL_STATE_NOT_REGISTERED,
    CONTROL_STATE_DISCONNECTED,
    CONTROL_STATE_SERVER_IDENTITY_CHANGED,
    CONTROL_STATE_RECOVERING,
    CONTROL_STATE_RECOVERY_BLOCKED,
    CONTROL_STATE_POLICY_BLOCKED,
    CONTROL_STATE_NEEDS_LOGIN,
  ];

  static final $core.List<ControlState?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 14);
  static ControlState? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ControlState._(super.value, super.name);
}

class DesiredState extends $pb.ProtobufEnum {
  static const DesiredState DESIRED_STATE_UNSPECIFIED =
      DesiredState._(0, _omitEnumNames ? '' : 'DESIRED_STATE_UNSPECIFIED');
  static const DesiredState DESIRED_STATE_CONNECTED =
      DesiredState._(1, _omitEnumNames ? '' : 'DESIRED_STATE_CONNECTED');
  static const DesiredState DESIRED_STATE_DISCONNECTED =
      DesiredState._(2, _omitEnumNames ? '' : 'DESIRED_STATE_DISCONNECTED');

  static const $core.List<DesiredState> values = <DesiredState>[
    DESIRED_STATE_UNSPECIFIED,
    DESIRED_STATE_CONNECTED,
    DESIRED_STATE_DISCONNECTED,
  ];

  static final $core.List<DesiredState?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static DesiredState? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const DesiredState._(super.value, super.name);
}

class AgentSnapshotState extends $pb.ProtobufEnum {
  static const AgentSnapshotState AGENT_SNAPSHOT_STATE_UNSPECIFIED =
      AgentSnapshotState._(
          0, _omitEnumNames ? '' : 'AGENT_SNAPSHOT_STATE_UNSPECIFIED');
  static const AgentSnapshotState AGENT_SNAPSHOT_STATE_ABSENT =
      AgentSnapshotState._(
          1, _omitEnumNames ? '' : 'AGENT_SNAPSHOT_STATE_ABSENT');
  static const AgentSnapshotState AGENT_SNAPSHOT_STATE_CURRENT =
      AgentSnapshotState._(
          2, _omitEnumNames ? '' : 'AGENT_SNAPSHOT_STATE_CURRENT');
  static const AgentSnapshotState AGENT_SNAPSHOT_STATE_PREVIOUS =
      AgentSnapshotState._(
          3, _omitEnumNames ? '' : 'AGENT_SNAPSHOT_STATE_PREVIOUS');

  static const $core.List<AgentSnapshotState> values = <AgentSnapshotState>[
    AGENT_SNAPSHOT_STATE_UNSPECIFIED,
    AGENT_SNAPSHOT_STATE_ABSENT,
    AGENT_SNAPSHOT_STATE_CURRENT,
    AGENT_SNAPSHOT_STATE_PREVIOUS,
  ];

  static final $core.List<AgentSnapshotState?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static AgentSnapshotState? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const AgentSnapshotState._(super.value, super.name);
}

class PathKind extends $pb.ProtobufEnum {
  static const PathKind PATH_KIND_UNSPECIFIED =
      PathKind._(0, _omitEnumNames ? '' : 'PATH_KIND_UNSPECIFIED');
  static const PathKind PATH_KIND_DIRECT =
      PathKind._(1, _omitEnumNames ? '' : 'PATH_KIND_DIRECT');
  static const PathKind PATH_KIND_RELAY =
      PathKind._(2, _omitEnumNames ? '' : 'PATH_KIND_RELAY');

  static const $core.List<PathKind> values = <PathKind>[
    PATH_KIND_UNSPECIFIED,
    PATH_KIND_DIRECT,
    PATH_KIND_RELAY,
  ];

  static final $core.List<PathKind?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static PathKind? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const PathKind._(super.value, super.name);
}

class PathHealth extends $pb.ProtobufEnum {
  static const PathHealth PATH_HEALTH_UNSPECIFIED =
      PathHealth._(0, _omitEnumNames ? '' : 'PATH_HEALTH_UNSPECIFIED');
  static const PathHealth PATH_HEALTH_UNKNOWN =
      PathHealth._(1, _omitEnumNames ? '' : 'PATH_HEALTH_UNKNOWN');
  static const PathHealth PATH_HEALTH_CHECKING =
      PathHealth._(2, _omitEnumNames ? '' : 'PATH_HEALTH_CHECKING');
  static const PathHealth PATH_HEALTH_REACHABLE =
      PathHealth._(3, _omitEnumNames ? '' : 'PATH_HEALTH_REACHABLE');
  static const PathHealth PATH_HEALTH_UNREACHABLE =
      PathHealth._(4, _omitEnumNames ? '' : 'PATH_HEALTH_UNREACHABLE');

  static const $core.List<PathHealth> values = <PathHealth>[
    PATH_HEALTH_UNSPECIFIED,
    PATH_HEALTH_UNKNOWN,
    PATH_HEALTH_CHECKING,
    PATH_HEALTH_REACHABLE,
    PATH_HEALTH_UNREACHABLE,
  ];

  static final $core.List<PathHealth?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 4);
  static PathHealth? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const PathHealth._(super.value, super.name);
}

class SessionState extends $pb.ProtobufEnum {
  static const SessionState SESSION_STATE_UNSPECIFIED =
      SessionState._(0, _omitEnumNames ? '' : 'SESSION_STATE_UNSPECIFIED');
  static const SessionState SESSION_STATE_NOT_AUTHENTICATED = SessionState._(
      1, _omitEnumNames ? '' : 'SESSION_STATE_NOT_AUTHENTICATED');
  static const SessionState SESSION_STATE_ACTIVE =
      SessionState._(2, _omitEnumNames ? '' : 'SESSION_STATE_ACTIVE');
  static const SessionState SESSION_STATE_EXPIRING =
      SessionState._(3, _omitEnumNames ? '' : 'SESSION_STATE_EXPIRING');
  static const SessionState SESSION_STATE_EXPIRED =
      SessionState._(4, _omitEnumNames ? '' : 'SESSION_STATE_EXPIRED');
  static const SessionState SESSION_STATE_RENEWING =
      SessionState._(5, _omitEnumNames ? '' : 'SESSION_STATE_RENEWING');

  static const $core.List<SessionState> values = <SessionState>[
    SESSION_STATE_UNSPECIFIED,
    SESSION_STATE_NOT_AUTHENTICATED,
    SESSION_STATE_ACTIVE,
    SESSION_STATE_EXPIRING,
    SESSION_STATE_EXPIRED,
    SESSION_STATE_RENEWING,
  ];

  static final $core.List<SessionState?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 5);
  static SessionState? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const SessionState._(super.value, super.name);
}

class EnrollmentMode extends $pb.ProtobufEnum {
  static const EnrollmentMode ENROLLMENT_MODE_UNSPECIFIED =
      EnrollmentMode._(0, _omitEnumNames ? '' : 'ENROLLMENT_MODE_UNSPECIFIED');
  static const EnrollmentMode ENROLLMENT_MODE_WORKSTATION =
      EnrollmentMode._(1, _omitEnumNames ? '' : 'ENROLLMENT_MODE_WORKSTATION');
  static const EnrollmentMode ENROLLMENT_MODE_SERVER =
      EnrollmentMode._(2, _omitEnumNames ? '' : 'ENROLLMENT_MODE_SERVER');
  static const EnrollmentMode ENROLLMENT_MODE_SUBNET_ROUTER = EnrollmentMode._(
      3, _omitEnumNames ? '' : 'ENROLLMENT_MODE_SUBNET_ROUTER');
  static const EnrollmentMode ENROLLMENT_MODE_INTERACTIVE =
      EnrollmentMode._(4, _omitEnumNames ? '' : 'ENROLLMENT_MODE_INTERACTIVE');

  static const $core.List<EnrollmentMode> values = <EnrollmentMode>[
    ENROLLMENT_MODE_UNSPECIFIED,
    ENROLLMENT_MODE_WORKSTATION,
    ENROLLMENT_MODE_SERVER,
    ENROLLMENT_MODE_SUBNET_ROUTER,
    ENROLLMENT_MODE_INTERACTIVE,
  ];

  static final $core.List<EnrollmentMode?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 4);
  static EnrollmentMode? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const EnrollmentMode._(super.value, super.name);
}

class ProfileState extends $pb.ProtobufEnum {
  static const ProfileState PROFILE_STATE_UNSPECIFIED =
      ProfileState._(0, _omitEnumNames ? '' : 'PROFILE_STATE_UNSPECIFIED');
  static const ProfileState PROFILE_STATE_EMPTY =
      ProfileState._(1, _omitEnumNames ? '' : 'PROFILE_STATE_EMPTY');
  static const ProfileState PROFILE_STATE_REGISTERED =
      ProfileState._(2, _omitEnumNames ? '' : 'PROFILE_STATE_REGISTERED');
  static const ProfileState PROFILE_STATE_NEEDS_LOGIN =
      ProfileState._(3, _omitEnumNames ? '' : 'PROFILE_STATE_NEEDS_LOGIN');
  static const ProfileState PROFILE_STATE_NEEDS_APPROVAL =
      ProfileState._(4, _omitEnumNames ? '' : 'PROFILE_STATE_NEEDS_APPROVAL');
  static const ProfileState PROFILE_STATE_BLOCKED =
      ProfileState._(5, _omitEnumNames ? '' : 'PROFILE_STATE_BLOCKED');

  static const $core.List<ProfileState> values = <ProfileState>[
    PROFILE_STATE_UNSPECIFIED,
    PROFILE_STATE_EMPTY,
    PROFILE_STATE_REGISTERED,
    PROFILE_STATE_NEEDS_LOGIN,
    PROFILE_STATE_NEEDS_APPROVAL,
    PROFILE_STATE_BLOCKED,
  ];

  static final $core.List<ProfileState?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 5);
  static ProfileState? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ProfileState._(super.value, super.name);
}

const $core.bool _omitEnumNames =
    $core.bool.fromEnvironment('protobuf.omit_enum_names');
