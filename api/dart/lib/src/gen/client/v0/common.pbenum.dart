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

const $core.bool _omitEnumNames =
    $core.bool.fromEnvironment('protobuf.omit_enum_names');
