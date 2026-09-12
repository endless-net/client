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

import 'package:protobuf/protobuf.dart' as $pb;

class Access extends $pb.ProtobufEnum {
  static const Access ACCESS_UNSPECIFIED =
      Access._(0, _omitEnumNames ? '' : 'ACCESS_UNSPECIFIED');
  static const Access ACCESS_OBSERVER =
      Access._(1, _omitEnumNames ? '' : 'ACCESS_OBSERVER');
  static const Access ACCESS_OWNER =
      Access._(2, _omitEnumNames ? '' : 'ACCESS_OWNER');
  static const Access ACCESS_ADMINISTRATOR =
      Access._(3, _omitEnumNames ? '' : 'ACCESS_ADMINISTRATOR');

  static const $core.List<Access> values = <Access>[
    ACCESS_UNSPECIFIED,
    ACCESS_OBSERVER,
    ACCESS_OWNER,
    ACCESS_ADMINISTRATOR,
  ];

  static final $core.List<Access?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static Access? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const Access._(super.value, super.name);
}

class Platform extends $pb.ProtobufEnum {
  static const Platform PLATFORM_UNSPECIFIED =
      Platform._(0, _omitEnumNames ? '' : 'PLATFORM_UNSPECIFIED');
  static const Platform PLATFORM_WINDOWS =
      Platform._(1, _omitEnumNames ? '' : 'PLATFORM_WINDOWS');
  static const Platform PLATFORM_MACOS =
      Platform._(2, _omitEnumNames ? '' : 'PLATFORM_MACOS');
  static const Platform PLATFORM_LINUX =
      Platform._(3, _omitEnumNames ? '' : 'PLATFORM_LINUX');
  static const Platform PLATFORM_ANDROID =
      Platform._(4, _omitEnumNames ? '' : 'PLATFORM_ANDROID');
  static const Platform PLATFORM_IOS =
      Platform._(5, _omitEnumNames ? '' : 'PLATFORM_IOS');

  static const $core.List<Platform> values = <Platform>[
    PLATFORM_UNSPECIFIED,
    PLATFORM_WINDOWS,
    PLATFORM_MACOS,
    PLATFORM_LINUX,
    PLATFORM_ANDROID,
    PLATFORM_IOS,
  ];

  static final $core.List<Platform?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 5);
  static Platform? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const Platform._(super.value, super.name);
}

class Capability extends $pb.ProtobufEnum {
  static const Capability CAPABILITY_UNSPECIFIED =
      Capability._(0, _omitEnumNames ? '' : 'CAPABILITY_UNSPECIFIED');
  static const Capability CAPABILITY_ENROLLMENT =
      Capability._(1, _omitEnumNames ? '' : 'CAPABILITY_ENROLLMENT');
  static const Capability CAPABILITY_CONNECTION =
      Capability._(2, _omitEnumNames ? '' : 'CAPABILITY_CONNECTION');
  static const Capability CAPABILITY_PEERS =
      Capability._(3, _omitEnumNames ? '' : 'CAPABILITY_PEERS');
  static const Capability CAPABILITY_NETWORK_SELECTION =
      Capability._(4, _omitEnumNames ? '' : 'CAPABILITY_NETWORK_SELECTION');
  static const Capability CAPABILITY_EXIT_NODE =
      Capability._(5, _omitEnumNames ? '' : 'CAPABILITY_EXIT_NODE');
  static const Capability CAPABILITY_IDENTITY_RECOVERY =
      Capability._(6, _omitEnumNames ? '' : 'CAPABILITY_IDENTITY_RECOVERY');
  static const Capability CAPABILITY_DIAGNOSTICS =
      Capability._(7, _omitEnumNames ? '' : 'CAPABILITY_DIAGNOSTICS');
  static const Capability CAPABILITY_LOGOUT =
      Capability._(8, _omitEnumNames ? '' : 'CAPABILITY_LOGOUT');
  static const Capability CAPABILITY_LOCAL_FORGET =
      Capability._(9, _omitEnumNames ? '' : 'CAPABILITY_LOCAL_FORGET');
  static const Capability CAPABILITY_PROFILES =
      Capability._(10, _omitEnumNames ? '' : 'CAPABILITY_PROFILES');
  static const Capability CAPABILITY_SESSION_RENEWAL =
      Capability._(11, _omitEnumNames ? '' : 'CAPABILITY_SESSION_RENEWAL');
  static const Capability CAPABILITY_PREFERENCES =
      Capability._(12, _omitEnumNames ? '' : 'CAPABILITY_PREFERENCES');
  static const Capability CAPABILITY_RESOURCES =
      Capability._(13, _omitEnumNames ? '' : 'CAPABILITY_RESOURCES');
  static const Capability CAPABILITY_MANAGED_SETTINGS =
      Capability._(14, _omitEnumNames ? '' : 'CAPABILITY_MANAGED_SETTINGS');
  static const Capability CAPABILITY_RUNTIME_LIFECYCLE =
      Capability._(15, _omitEnumNames ? '' : 'CAPABILITY_RUNTIME_LIFECYCLE');
  static const Capability CAPABILITY_UPDATE_DISCOVERY =
      Capability._(16, _omitEnumNames ? '' : 'CAPABILITY_UPDATE_DISCOVERY');
  static const Capability CAPABILITY_SUPPORT_INFO =
      Capability._(17, _omitEnumNames ? '' : 'CAPABILITY_SUPPORT_INFO');

  static const $core.List<Capability> values = <Capability>[
    CAPABILITY_UNSPECIFIED,
    CAPABILITY_ENROLLMENT,
    CAPABILITY_CONNECTION,
    CAPABILITY_PEERS,
    CAPABILITY_NETWORK_SELECTION,
    CAPABILITY_EXIT_NODE,
    CAPABILITY_IDENTITY_RECOVERY,
    CAPABILITY_DIAGNOSTICS,
    CAPABILITY_LOGOUT,
    CAPABILITY_LOCAL_FORGET,
    CAPABILITY_PROFILES,
    CAPABILITY_SESSION_RENEWAL,
    CAPABILITY_PREFERENCES,
    CAPABILITY_RESOURCES,
    CAPABILITY_MANAGED_SETTINGS,
    CAPABILITY_RUNTIME_LIFECYCLE,
    CAPABILITY_UPDATE_DISCOVERY,
    CAPABILITY_SUPPORT_INFO,
  ];

  static final $core.List<Capability?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 17);
  static Capability? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const Capability._(super.value, super.name);
}

class Availability extends $pb.ProtobufEnum {
  static const Availability AVAILABILITY_UNSPECIFIED =
      Availability._(0, _omitEnumNames ? '' : 'AVAILABILITY_UNSPECIFIED');
  static const Availability AVAILABILITY_AVAILABLE =
      Availability._(1, _omitEnumNames ? '' : 'AVAILABILITY_AVAILABLE');
  static const Availability AVAILABILITY_UNSUPPORTED =
      Availability._(2, _omitEnumNames ? '' : 'AVAILABILITY_UNSUPPORTED');
  static const Availability AVAILABILITY_POLICY_BLOCKED =
      Availability._(3, _omitEnumNames ? '' : 'AVAILABILITY_POLICY_BLOCKED');
  static const Availability AVAILABILITY_PERMISSION_REQUIRED = Availability._(
      4, _omitEnumNames ? '' : 'AVAILABILITY_PERMISSION_REQUIRED');
  static const Availability AVAILABILITY_TEMPORARILY_UNAVAILABLE =
      Availability._(
          5, _omitEnumNames ? '' : 'AVAILABILITY_TEMPORARILY_UNAVAILABLE');

  static const $core.List<Availability> values = <Availability>[
    AVAILABILITY_UNSPECIFIED,
    AVAILABILITY_AVAILABLE,
    AVAILABILITY_UNSUPPORTED,
    AVAILABILITY_POLICY_BLOCKED,
    AVAILABILITY_PERMISSION_REQUIRED,
    AVAILABILITY_TEMPORARILY_UNAVAILABLE,
  ];

  static final $core.List<Availability?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 5);
  static Availability? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const Availability._(super.value, super.name);
}

class ActionOwner extends $pb.ProtobufEnum {
  static const ActionOwner ACTION_OWNER_UNSPECIFIED =
      ActionOwner._(0, _omitEnumNames ? '' : 'ACTION_OWNER_UNSPECIFIED');
  static const ActionOwner ACTION_OWNER_USER =
      ActionOwner._(1, _omitEnumNames ? '' : 'ACTION_OWNER_USER');
  static const ActionOwner ACTION_OWNER_DEVICE_ADMINISTRATOR = ActionOwner._(
      2, _omitEnumNames ? '' : 'ACTION_OWNER_DEVICE_ADMINISTRATOR');
  static const ActionOwner ACTION_OWNER_ACCESS_ADMINISTRATOR = ActionOwner._(
      3, _omitEnumNames ? '' : 'ACTION_OWNER_ACCESS_ADMINISTRATOR');
  static const ActionOwner ACTION_OWNER_SUPPORT =
      ActionOwner._(4, _omitEnumNames ? '' : 'ACTION_OWNER_SUPPORT');

  static const $core.List<ActionOwner> values = <ActionOwner>[
    ACTION_OWNER_UNSPECIFIED,
    ACTION_OWNER_USER,
    ACTION_OWNER_DEVICE_ADMINISTRATOR,
    ACTION_OWNER_ACCESS_ADMINISTRATOR,
    ACTION_OWNER_SUPPORT,
  ];

  static final $core.List<ActionOwner?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 4);
  static ActionOwner? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ActionOwner._(super.value, super.name);
}

class ErrorCode extends $pb.ProtobufEnum {
  static const ErrorCode ERROR_CODE_UNSPECIFIED =
      ErrorCode._(0, _omitEnumNames ? '' : 'ERROR_CODE_UNSPECIFIED');
  static const ErrorCode ERROR_CODE_INVALID_ARGUMENT =
      ErrorCode._(1, _omitEnumNames ? '' : 'ERROR_CODE_INVALID_ARGUMENT');
  static const ErrorCode ERROR_CODE_UNAUTHENTICATED =
      ErrorCode._(2, _omitEnumNames ? '' : 'ERROR_CODE_UNAUTHENTICATED');
  static const ErrorCode ERROR_CODE_OWNER_REQUIRED =
      ErrorCode._(3, _omitEnumNames ? '' : 'ERROR_CODE_OWNER_REQUIRED');
  static const ErrorCode ERROR_CODE_ADMINISTRATOR_REQUIRED =
      ErrorCode._(4, _omitEnumNames ? '' : 'ERROR_CODE_ADMINISTRATOR_REQUIRED');
  static const ErrorCode ERROR_CODE_UNSUPPORTED =
      ErrorCode._(5, _omitEnumNames ? '' : 'ERROR_CODE_UNSUPPORTED');
  static const ErrorCode ERROR_CODE_NOT_FOUND =
      ErrorCode._(6, _omitEnumNames ? '' : 'ERROR_CODE_NOT_FOUND');
  static const ErrorCode ERROR_CODE_STALE_STATE =
      ErrorCode._(7, _omitEnumNames ? '' : 'ERROR_CODE_STALE_STATE');
  static const ErrorCode ERROR_CODE_POLICY_BLOCKED =
      ErrorCode._(8, _omitEnumNames ? '' : 'ERROR_CODE_POLICY_BLOCKED');
  static const ErrorCode ERROR_CODE_PERMISSION_REQUIRED =
      ErrorCode._(9, _omitEnumNames ? '' : 'ERROR_CODE_PERMISSION_REQUIRED');
  static const ErrorCode ERROR_CODE_BUSY =
      ErrorCode._(10, _omitEnumNames ? '' : 'ERROR_CODE_BUSY');
  static const ErrorCode ERROR_CODE_LIMIT_EXCEEDED =
      ErrorCode._(11, _omitEnumNames ? '' : 'ERROR_CODE_LIMIT_EXCEEDED');
  static const ErrorCode ERROR_CODE_UNAVAILABLE =
      ErrorCode._(12, _omitEnumNames ? '' : 'ERROR_CODE_UNAVAILABLE');
  static const ErrorCode ERROR_CODE_DEADLINE_EXCEEDED =
      ErrorCode._(13, _omitEnumNames ? '' : 'ERROR_CODE_DEADLINE_EXCEEDED');
  static const ErrorCode ERROR_CODE_CANCELLED =
      ErrorCode._(14, _omitEnumNames ? '' : 'ERROR_CODE_CANCELLED');
  static const ErrorCode ERROR_CODE_INTERNAL =
      ErrorCode._(15, _omitEnumNames ? '' : 'ERROR_CODE_INTERNAL');
  static const ErrorCode ERROR_CODE_NEEDS_ENROLLMENT =
      ErrorCode._(16, _omitEnumNames ? '' : 'ERROR_CODE_NEEDS_ENROLLMENT');
  static const ErrorCode ERROR_CODE_NEEDS_LOGIN =
      ErrorCode._(17, _omitEnumNames ? '' : 'ERROR_CODE_NEEDS_LOGIN');
  static const ErrorCode ERROR_CODE_APPROVAL_REQUIRED =
      ErrorCode._(18, _omitEnumNames ? '' : 'ERROR_CODE_APPROVAL_REQUIRED');
  static const ErrorCode ERROR_CODE_APPROVAL_REJECTED =
      ErrorCode._(19, _omitEnumNames ? '' : 'ERROR_CODE_APPROVAL_REJECTED');
  static const ErrorCode ERROR_CODE_SERVER_IDENTITY_CHANGED = ErrorCode._(
      20, _omitEnumNames ? '' : 'ERROR_CODE_SERVER_IDENTITY_CHANGED');
  static const ErrorCode ERROR_CODE_IDENTITY_CONFIRMATION_MISMATCH = ErrorCode
      ._(21, _omitEnumNames ? '' : 'ERROR_CODE_IDENTITY_CONFIRMATION_MISMATCH');
  static const ErrorCode ERROR_CODE_REMOTE_CLEANUP_REQUIRED = ErrorCode._(
      22, _omitEnumNames ? '' : 'ERROR_CODE_REMOTE_CLEANUP_REQUIRED');
  static const ErrorCode ERROR_CODE_LOCAL_FORGET_CONFIRMATION_REQUIRED =
      ErrorCode._(
          23,
          _omitEnumNames
              ? ''
              : 'ERROR_CODE_LOCAL_FORGET_CONFIRMATION_REQUIRED');
  static const ErrorCode ERROR_CODE_APPLY_FAILED =
      ErrorCode._(24, _omitEnumNames ? '' : 'ERROR_CODE_APPLY_FAILED');
  static const ErrorCode ERROR_CODE_PROFILE_ACTIVE =
      ErrorCode._(25, _omitEnumNames ? '' : 'ERROR_CODE_PROFILE_ACTIVE');
  static const ErrorCode ERROR_CODE_RESOURCE_CONFLICT =
      ErrorCode._(26, _omitEnumNames ? '' : 'ERROR_CODE_RESOURCE_CONFLICT');
  static const ErrorCode ERROR_CODE_CONTRACT_MISMATCH =
      ErrorCode._(27, _omitEnumNames ? '' : 'ERROR_CODE_CONTRACT_MISMATCH');

  static const $core.List<ErrorCode> values = <ErrorCode>[
    ERROR_CODE_UNSPECIFIED,
    ERROR_CODE_INVALID_ARGUMENT,
    ERROR_CODE_UNAUTHENTICATED,
    ERROR_CODE_OWNER_REQUIRED,
    ERROR_CODE_ADMINISTRATOR_REQUIRED,
    ERROR_CODE_UNSUPPORTED,
    ERROR_CODE_NOT_FOUND,
    ERROR_CODE_STALE_STATE,
    ERROR_CODE_POLICY_BLOCKED,
    ERROR_CODE_PERMISSION_REQUIRED,
    ERROR_CODE_BUSY,
    ERROR_CODE_LIMIT_EXCEEDED,
    ERROR_CODE_UNAVAILABLE,
    ERROR_CODE_DEADLINE_EXCEEDED,
    ERROR_CODE_CANCELLED,
    ERROR_CODE_INTERNAL,
    ERROR_CODE_NEEDS_ENROLLMENT,
    ERROR_CODE_NEEDS_LOGIN,
    ERROR_CODE_APPROVAL_REQUIRED,
    ERROR_CODE_APPROVAL_REJECTED,
    ERROR_CODE_SERVER_IDENTITY_CHANGED,
    ERROR_CODE_IDENTITY_CONFIRMATION_MISMATCH,
    ERROR_CODE_REMOTE_CLEANUP_REQUIRED,
    ERROR_CODE_LOCAL_FORGET_CONFIRMATION_REQUIRED,
    ERROR_CODE_APPLY_FAILED,
    ERROR_CODE_PROFILE_ACTIVE,
    ERROR_CODE_RESOURCE_CONFLICT,
    ERROR_CODE_CONTRACT_MISMATCH,
  ];

  static final $core.List<ErrorCode?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 27);
  static ErrorCode? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ErrorCode._(super.value, super.name);
}

/// Transport error details carry Failure. Completed operations use Failure too.
class OperationState extends $pb.ProtobufEnum {
  static const OperationState OPERATION_STATE_UNSPECIFIED =
      OperationState._(0, _omitEnumNames ? '' : 'OPERATION_STATE_UNSPECIFIED');
  static const OperationState OPERATION_STATE_PENDING =
      OperationState._(1, _omitEnumNames ? '' : 'OPERATION_STATE_PENDING');
  static const OperationState OPERATION_STATE_RUNNING =
      OperationState._(2, _omitEnumNames ? '' : 'OPERATION_STATE_RUNNING');
  static const OperationState OPERATION_STATE_WAITING_FOR_USER =
      OperationState._(
          3, _omitEnumNames ? '' : 'OPERATION_STATE_WAITING_FOR_USER');
  static const OperationState OPERATION_STATE_SUCCEEDED =
      OperationState._(4, _omitEnumNames ? '' : 'OPERATION_STATE_SUCCEEDED');
  static const OperationState OPERATION_STATE_FAILED =
      OperationState._(5, _omitEnumNames ? '' : 'OPERATION_STATE_FAILED');
  static const OperationState OPERATION_STATE_CANCELLED =
      OperationState._(6, _omitEnumNames ? '' : 'OPERATION_STATE_CANCELLED');

  static const $core.List<OperationState> values = <OperationState>[
    OPERATION_STATE_UNSPECIFIED,
    OPERATION_STATE_PENDING,
    OPERATION_STATE_RUNNING,
    OPERATION_STATE_WAITING_FOR_USER,
    OPERATION_STATE_SUCCEEDED,
    OPERATION_STATE_FAILED,
    OPERATION_STATE_CANCELLED,
  ];

  static final $core.List<OperationState?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 6);
  static OperationState? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const OperationState._(super.value, super.name);
}

class ConnectionContinuity extends $pb.ProtobufEnum {
  static const ConnectionContinuity CONNECTION_CONTINUITY_UNSPECIFIED =
      ConnectionContinuity._(
          0, _omitEnumNames ? '' : 'CONNECTION_CONTINUITY_UNSPECIFIED');
  static const ConnectionContinuity CONNECTION_CONTINUITY_NOT_APPLICABLE =
      ConnectionContinuity._(
          1, _omitEnumNames ? '' : 'CONNECTION_CONTINUITY_NOT_APPLICABLE');
  static const ConnectionContinuity CONNECTION_CONTINUITY_PRESERVED =
      ConnectionContinuity._(
          2, _omitEnumNames ? '' : 'CONNECTION_CONTINUITY_PRESERVED');
  static const ConnectionContinuity CONNECTION_CONTINUITY_INTERRUPTED =
      ConnectionContinuity._(
          3, _omitEnumNames ? '' : 'CONNECTION_CONTINUITY_INTERRUPTED');
  static const ConnectionContinuity CONNECTION_CONTINUITY_UNKNOWN =
      ConnectionContinuity._(
          4, _omitEnumNames ? '' : 'CONNECTION_CONTINUITY_UNKNOWN');

  static const $core.List<ConnectionContinuity> values = <ConnectionContinuity>[
    CONNECTION_CONTINUITY_UNSPECIFIED,
    CONNECTION_CONTINUITY_NOT_APPLICABLE,
    CONNECTION_CONTINUITY_PRESERVED,
    CONNECTION_CONTINUITY_INTERRUPTED,
    CONNECTION_CONTINUITY_UNKNOWN,
  ];

  static final $core.List<ConnectionContinuity?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 4);
  static ConnectionContinuity? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ConnectionContinuity._(super.value, super.name);
}

class CleanupOutcome extends $pb.ProtobufEnum {
  static const CleanupOutcome CLEANUP_OUTCOME_UNSPECIFIED =
      CleanupOutcome._(0, _omitEnumNames ? '' : 'CLEANUP_OUTCOME_UNSPECIFIED');
  static const CleanupOutcome CLEANUP_OUTCOME_REMOTE_CONFIRMED =
      CleanupOutcome._(
          1, _omitEnumNames ? '' : 'CLEANUP_OUTCOME_REMOTE_CONFIRMED');
  static const CleanupOutcome CLEANUP_OUTCOME_REMOTE_UNCONFIRMED =
      CleanupOutcome._(
          2, _omitEnumNames ? '' : 'CLEANUP_OUTCOME_REMOTE_UNCONFIRMED');
  static const CleanupOutcome CLEANUP_OUTCOME_NOT_REGISTERED = CleanupOutcome._(
      3, _omitEnumNames ? '' : 'CLEANUP_OUTCOME_NOT_REGISTERED');

  static const $core.List<CleanupOutcome> values = <CleanupOutcome>[
    CLEANUP_OUTCOME_UNSPECIFIED,
    CLEANUP_OUTCOME_REMOTE_CONFIRMED,
    CLEANUP_OUTCOME_REMOTE_UNCONFIRMED,
    CLEANUP_OUTCOME_NOT_REGISTERED,
  ];

  static final $core.List<CleanupOutcome?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static CleanupOutcome? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const CleanupOutcome._(super.value, super.name);
}

/// Closed mutation vocabulary; UNSPECIFIED is invalid in returned operations.
class OperationKind extends $pb.ProtobufEnum {
  static const OperationKind OPERATION_KIND_UNSPECIFIED =
      OperationKind._(0, _omitEnumNames ? '' : 'OPERATION_KIND_UNSPECIFIED');
  static const OperationKind OPERATION_KIND_ENROLL =
      OperationKind._(1, _omitEnumNames ? '' : 'OPERATION_KIND_ENROLL');
  static const OperationKind OPERATION_KIND_CONNECT =
      OperationKind._(2, _omitEnumNames ? '' : 'OPERATION_KIND_CONNECT');
  static const OperationKind OPERATION_KIND_DISCONNECT =
      OperationKind._(3, _omitEnumNames ? '' : 'OPERATION_KIND_DISCONNECT');
  static const OperationKind OPERATION_KIND_TRUST_SERVER_IDENTITY =
      OperationKind._(
          4, _omitEnumNames ? '' : 'OPERATION_KIND_TRUST_SERVER_IDENTITY');
  static const OperationKind OPERATION_KIND_LOGOUT =
      OperationKind._(5, _omitEnumNames ? '' : 'OPERATION_KIND_LOGOUT');
  static const OperationKind OPERATION_KIND_FORGET_LOCAL_ENROLLMENT =
      OperationKind._(
          6, _omitEnumNames ? '' : 'OPERATION_KIND_FORGET_LOCAL_ENROLLMENT');
  static const OperationKind OPERATION_KIND_SELECT_NETWORK =
      OperationKind._(7, _omitEnumNames ? '' : 'OPERATION_KIND_SELECT_NETWORK');
  static const OperationKind OPERATION_KIND_CREATE_DIAGNOSTICS_BUNDLE =
      OperationKind._(
          8, _omitEnumNames ? '' : 'OPERATION_KIND_CREATE_DIAGNOSTICS_BUNDLE');
  static const OperationKind OPERATION_KIND_CREATE_PROFILE =
      OperationKind._(9, _omitEnumNames ? '' : 'OPERATION_KIND_CREATE_PROFILE');
  static const OperationKind OPERATION_KIND_SELECT_PROFILE = OperationKind._(
      10, _omitEnumNames ? '' : 'OPERATION_KIND_SELECT_PROFILE');
  static const OperationKind OPERATION_KIND_RENAME_PROFILE = OperationKind._(
      11, _omitEnumNames ? '' : 'OPERATION_KIND_RENAME_PROFILE');
  static const OperationKind OPERATION_KIND_REMOVE_PROFILE = OperationKind._(
      12, _omitEnumNames ? '' : 'OPERATION_KIND_REMOVE_PROFILE');
  static const OperationKind OPERATION_KIND_RENEW_SESSION =
      OperationKind._(13, _omitEnumNames ? '' : 'OPERATION_KIND_RENEW_SESSION');
  static const OperationKind OPERATION_KIND_SELECT_EXIT_NODE = OperationKind._(
      14, _omitEnumNames ? '' : 'OPERATION_KIND_SELECT_EXIT_NODE');
  static const OperationKind OPERATION_KIND_CLEAR_EXIT_NODE = OperationKind._(
      15, _omitEnumNames ? '' : 'OPERATION_KIND_CLEAR_EXIT_NODE');
  static const OperationKind OPERATION_KIND_SET_PREFERENCES = OperationKind._(
      16, _omitEnumNames ? '' : 'OPERATION_KIND_SET_PREFERENCES');
  static const OperationKind OPERATION_KIND_RESET_PREFERENCES = OperationKind._(
      17, _omitEnumNames ? '' : 'OPERATION_KIND_RESET_PREFERENCES');
  static const OperationKind OPERATION_KIND_SET_RESOURCE_ENABLED =
      OperationKind._(
          18, _omitEnumNames ? '' : 'OPERATION_KIND_SET_RESOURCE_ENABLED');
  static const OperationKind OPERATION_KIND_NOTIFY_LIFECYCLE = OperationKind._(
      19, _omitEnumNames ? '' : 'OPERATION_KIND_NOTIFY_LIFECYCLE');

  static const $core.List<OperationKind> values = <OperationKind>[
    OPERATION_KIND_UNSPECIFIED,
    OPERATION_KIND_ENROLL,
    OPERATION_KIND_CONNECT,
    OPERATION_KIND_DISCONNECT,
    OPERATION_KIND_TRUST_SERVER_IDENTITY,
    OPERATION_KIND_LOGOUT,
    OPERATION_KIND_FORGET_LOCAL_ENROLLMENT,
    OPERATION_KIND_SELECT_NETWORK,
    OPERATION_KIND_CREATE_DIAGNOSTICS_BUNDLE,
    OPERATION_KIND_CREATE_PROFILE,
    OPERATION_KIND_SELECT_PROFILE,
    OPERATION_KIND_RENAME_PROFILE,
    OPERATION_KIND_REMOVE_PROFILE,
    OPERATION_KIND_RENEW_SESSION,
    OPERATION_KIND_SELECT_EXIT_NODE,
    OPERATION_KIND_CLEAR_EXIT_NODE,
    OPERATION_KIND_SET_PREFERENCES,
    OPERATION_KIND_RESET_PREFERENCES,
    OPERATION_KIND_SET_RESOURCE_ENABLED,
    OPERATION_KIND_NOTIFY_LIFECYCLE,
  ];

  static final $core.List<OperationKind?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 19);
  static OperationKind? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const OperationKind._(super.value, super.name);
}

class UserAction_Kind extends $pb.ProtobufEnum {
  static const UserAction_Kind KIND_UNSPECIFIED =
      UserAction_Kind._(0, _omitEnumNames ? '' : 'KIND_UNSPECIFIED');
  static const UserAction_Kind KIND_OPEN_BROWSER =
      UserAction_Kind._(1, _omitEnumNames ? '' : 'KIND_OPEN_BROWSER');
  static const UserAction_Kind KIND_WAIT_FOR_APPROVAL =
      UserAction_Kind._(2, _omitEnumNames ? '' : 'KIND_WAIT_FOR_APPROVAL');
  static const UserAction_Kind KIND_GRANT_VPN_PERMISSION =
      UserAction_Kind._(3, _omitEnumNames ? '' : 'KIND_GRANT_VPN_PERMISSION');
  static const UserAction_Kind KIND_USE_PRIVILEGED_HELPER =
      UserAction_Kind._(4, _omitEnumNames ? '' : 'KIND_USE_PRIVILEGED_HELPER');
  static const UserAction_Kind KIND_CONTACT_ACCESS_ADMINISTRATOR =
      UserAction_Kind._(
          5, _omitEnumNames ? '' : 'KIND_CONTACT_ACCESS_ADMINISTRATOR');

  static const $core.List<UserAction_Kind> values = <UserAction_Kind>[
    KIND_UNSPECIFIED,
    KIND_OPEN_BROWSER,
    KIND_WAIT_FOR_APPROVAL,
    KIND_GRANT_VPN_PERMISSION,
    KIND_USE_PRIVILEGED_HELPER,
    KIND_CONTACT_ACCESS_ADMINISTRATOR,
  ];

  static final $core.List<UserAction_Kind?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 5);
  static UserAction_Kind? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const UserAction_Kind._(super.value, super.name);
}

const $core.bool _omitEnumNames =
    $core.bool.fromEnvironment('protobuf.omit_enum_names');
