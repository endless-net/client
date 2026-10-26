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

class SettingSource extends $pb.ProtobufEnum {
  static const SettingSource SETTING_SOURCE_UNSPECIFIED =
      SettingSource._(0, _omitEnumNames ? '' : 'SETTING_SOURCE_UNSPECIFIED');
  static const SettingSource SETTING_SOURCE_DEFAULT =
      SettingSource._(1, _omitEnumNames ? '' : 'SETTING_SOURCE_DEFAULT');
  static const SettingSource SETTING_SOURCE_USER =
      SettingSource._(2, _omitEnumNames ? '' : 'SETTING_SOURCE_USER');
  static const SettingSource SETTING_SOURCE_DEVICE_POLICY =
      SettingSource._(3, _omitEnumNames ? '' : 'SETTING_SOURCE_DEVICE_POLICY');
  static const SettingSource SETTING_SOURCE_ACCOUNT_POLICY =
      SettingSource._(4, _omitEnumNames ? '' : 'SETTING_SOURCE_ACCOUNT_POLICY');
  static const SettingSource SETTING_SOURCE_PLATFORM =
      SettingSource._(5, _omitEnumNames ? '' : 'SETTING_SOURCE_PLATFORM');

  static const $core.List<SettingSource> values = <SettingSource>[
    SETTING_SOURCE_UNSPECIFIED,
    SETTING_SOURCE_DEFAULT,
    SETTING_SOURCE_USER,
    SETTING_SOURCE_DEVICE_POLICY,
    SETTING_SOURCE_ACCOUNT_POLICY,
    SETTING_SOURCE_PLATFORM,
  ];

  static final $core.List<SettingSource?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 5);
  static SettingSource? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const SettingSource._(super.value, super.name);
}

class LifecycleBehavior extends $pb.ProtobufEnum {
  static const LifecycleBehavior LIFECYCLE_BEHAVIOR_UNSPECIFIED =
      LifecycleBehavior._(
          0, _omitEnumNames ? '' : 'LIFECYCLE_BEHAVIOR_UNSPECIFIED');
  static const LifecycleBehavior LIFECYCLE_BEHAVIOR_KEEP_INTENT =
      LifecycleBehavior._(
          1, _omitEnumNames ? '' : 'LIFECYCLE_BEHAVIOR_KEEP_INTENT');
  static const LifecycleBehavior LIFECYCLE_BEHAVIOR_CONNECT =
      LifecycleBehavior._(
          2, _omitEnumNames ? '' : 'LIFECYCLE_BEHAVIOR_CONNECT');
  static const LifecycleBehavior LIFECYCLE_BEHAVIOR_DISCONNECT =
      LifecycleBehavior._(
          3, _omitEnumNames ? '' : 'LIFECYCLE_BEHAVIOR_DISCONNECT');
  static const LifecycleBehavior LIFECYCLE_BEHAVIOR_PLATFORM_MANAGED =
      LifecycleBehavior._(
          4, _omitEnumNames ? '' : 'LIFECYCLE_BEHAVIOR_PLATFORM_MANAGED');

  static const $core.List<LifecycleBehavior> values = <LifecycleBehavior>[
    LIFECYCLE_BEHAVIOR_UNSPECIFIED,
    LIFECYCLE_BEHAVIOR_KEEP_INTENT,
    LIFECYCLE_BEHAVIOR_CONNECT,
    LIFECYCLE_BEHAVIOR_DISCONNECT,
    LIFECYCLE_BEHAVIOR_PLATFORM_MANAGED,
  ];

  static final $core.List<LifecycleBehavior?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 4);
  static LifecycleBehavior? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const LifecycleBehavior._(super.value, super.name);
}

class PreferenceKey extends $pb.ProtobufEnum {
  static const PreferenceKey PREFERENCE_KEY_UNSPECIFIED =
      PreferenceKey._(0, _omitEnumNames ? '' : 'PREFERENCE_KEY_UNSPECIFIED');
  static const PreferenceKey PREFERENCE_KEY_ALLOW_INBOUND =
      PreferenceKey._(1, _omitEnumNames ? '' : 'PREFERENCE_KEY_ALLOW_INBOUND');
  static const PreferenceKey PREFERENCE_KEY_ACCEPT_DNS =
      PreferenceKey._(2, _omitEnumNames ? '' : 'PREFERENCE_KEY_ACCEPT_DNS');
  static const PreferenceKey PREFERENCE_KEY_ACCEPT_ROUTES =
      PreferenceKey._(3, _omitEnumNames ? '' : 'PREFERENCE_KEY_ACCEPT_ROUTES');
  static const PreferenceKey PREFERENCE_KEY_RUNTIME_START =
      PreferenceKey._(4, _omitEnumNames ? '' : 'PREFERENCE_KEY_RUNTIME_START');
  static const PreferenceKey PREFERENCE_KEY_UI_QUIT =
      PreferenceKey._(5, _omitEnumNames ? '' : 'PREFERENCE_KEY_UI_QUIT');
  static const PreferenceKey PREFERENCE_KEY_USER_LOGOFF =
      PreferenceKey._(6, _omitEnumNames ? '' : 'PREFERENCE_KEY_USER_LOGOFF');
  static const PreferenceKey PREFERENCE_KEY_SUSPEND =
      PreferenceKey._(7, _omitEnumNames ? '' : 'PREFERENCE_KEY_SUSPEND');
  static const PreferenceKey PREFERENCE_KEY_RESUME =
      PreferenceKey._(8, _omitEnumNames ? '' : 'PREFERENCE_KEY_RESUME');

  static const $core.List<PreferenceKey> values = <PreferenceKey>[
    PREFERENCE_KEY_UNSPECIFIED,
    PREFERENCE_KEY_ALLOW_INBOUND,
    PREFERENCE_KEY_ACCEPT_DNS,
    PREFERENCE_KEY_ACCEPT_ROUTES,
    PREFERENCE_KEY_RUNTIME_START,
    PREFERENCE_KEY_UI_QUIT,
    PREFERENCE_KEY_USER_LOGOFF,
    PREFERENCE_KEY_SUSPEND,
    PREFERENCE_KEY_RESUME,
  ];

  static final $core.List<PreferenceKey?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 8);
  static PreferenceKey? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const PreferenceKey._(super.value, super.name);
}

class LanAccess extends $pb.ProtobufEnum {
  static const LanAccess LAN_ACCESS_UNSPECIFIED =
      LanAccess._(0, _omitEnumNames ? '' : 'LAN_ACCESS_UNSPECIFIED');
  static const LanAccess LAN_ACCESS_BLOCK =
      LanAccess._(1, _omitEnumNames ? '' : 'LAN_ACCESS_BLOCK');
  static const LanAccess LAN_ACCESS_ALLOW =
      LanAccess._(2, _omitEnumNames ? '' : 'LAN_ACCESS_ALLOW');

  static const $core.List<LanAccess> values = <LanAccess>[
    LAN_ACCESS_UNSPECIFIED,
    LAN_ACCESS_BLOCK,
    LAN_ACCESS_ALLOW,
  ];

  static final $core.List<LanAccess?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static LanAccess? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const LanAccess._(super.value, super.name);
}

/// NONE is a status-only cleared selection, never a SelectExitNode mode.
class ExitFamilyMode extends $pb.ProtobufEnum {
  static const ExitFamilyMode EXIT_FAMILY_MODE_UNSPECIFIED =
      ExitFamilyMode._(0, _omitEnumNames ? '' : 'EXIT_FAMILY_MODE_UNSPECIFIED');
  static const ExitFamilyMode EXIT_FAMILY_MODE_NONE =
      ExitFamilyMode._(1, _omitEnumNames ? '' : 'EXIT_FAMILY_MODE_NONE');
  static const ExitFamilyMode EXIT_FAMILY_MODE_IPV4_ONLY =
      ExitFamilyMode._(2, _omitEnumNames ? '' : 'EXIT_FAMILY_MODE_IPV4_ONLY');
  static const ExitFamilyMode EXIT_FAMILY_MODE_IPV6_ONLY =
      ExitFamilyMode._(3, _omitEnumNames ? '' : 'EXIT_FAMILY_MODE_IPV6_ONLY');
  static const ExitFamilyMode EXIT_FAMILY_MODE_DUAL_STACK =
      ExitFamilyMode._(4, _omitEnumNames ? '' : 'EXIT_FAMILY_MODE_DUAL_STACK');

  static const $core.List<ExitFamilyMode> values = <ExitFamilyMode>[
    EXIT_FAMILY_MODE_UNSPECIFIED,
    EXIT_FAMILY_MODE_NONE,
    EXIT_FAMILY_MODE_IPV4_ONLY,
    EXIT_FAMILY_MODE_IPV6_ONLY,
    EXIT_FAMILY_MODE_DUAL_STACK,
  ];

  static final $core.List<ExitFamilyMode?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 4);
  static ExitFamilyMode? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ExitFamilyMode._(super.value, super.name);
}

class ApplyState extends $pb.ProtobufEnum {
  static const ApplyState APPLY_STATE_UNSPECIFIED =
      ApplyState._(0, _omitEnumNames ? '' : 'APPLY_STATE_UNSPECIFIED');
  static const ApplyState APPLY_STATE_PENDING =
      ApplyState._(1, _omitEnumNames ? '' : 'APPLY_STATE_PENDING');
  static const ApplyState APPLY_STATE_APPLIED =
      ApplyState._(2, _omitEnumNames ? '' : 'APPLY_STATE_APPLIED');
  static const ApplyState APPLY_STATE_FAILED =
      ApplyState._(3, _omitEnumNames ? '' : 'APPLY_STATE_FAILED');

  static const $core.List<ApplyState> values = <ApplyState>[
    APPLY_STATE_UNSPECIFIED,
    APPLY_STATE_PENDING,
    APPLY_STATE_APPLIED,
    APPLY_STATE_FAILED,
  ];

  static final $core.List<ApplyState?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static ApplyState? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ApplyState._(super.value, super.name);
}

class ResourceKind extends $pb.ProtobufEnum {
  static const ResourceKind RESOURCE_KIND_UNSPECIFIED =
      ResourceKind._(0, _omitEnumNames ? '' : 'RESOURCE_KIND_UNSPECIFIED');
  static const ResourceKind RESOURCE_KIND_HOST =
      ResourceKind._(1, _omitEnumNames ? '' : 'RESOURCE_KIND_HOST');
  static const ResourceKind RESOURCE_KIND_SUBNET =
      ResourceKind._(2, _omitEnumNames ? '' : 'RESOURCE_KIND_SUBNET');
  static const ResourceKind RESOURCE_KIND_SERVICE =
      ResourceKind._(3, _omitEnumNames ? '' : 'RESOURCE_KIND_SERVICE');
  static const ResourceKind RESOURCE_KIND_APPLICATION =
      ResourceKind._(4, _omitEnumNames ? '' : 'RESOURCE_KIND_APPLICATION');

  static const $core.List<ResourceKind> values = <ResourceKind>[
    RESOURCE_KIND_UNSPECIFIED,
    RESOURCE_KIND_HOST,
    RESOURCE_KIND_SUBNET,
    RESOURCE_KIND_SERVICE,
    RESOURCE_KIND_APPLICATION,
  ];

  static final $core.List<ResourceKind?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 4);
  static ResourceKind? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ResourceKind._(super.value, super.name);
}

class UpdateClassification extends $pb.ProtobufEnum {
  static const UpdateClassification UPDATE_CLASSIFICATION_UNSPECIFIED =
      UpdateClassification._(
          0, _omitEnumNames ? '' : 'UPDATE_CLASSIFICATION_UNSPECIFIED');
  static const UpdateClassification UPDATE_CLASSIFICATION_ORDINARY =
      UpdateClassification._(
          1, _omitEnumNames ? '' : 'UPDATE_CLASSIFICATION_ORDINARY');
  static const UpdateClassification UPDATE_CLASSIFICATION_SECURITY =
      UpdateClassification._(
          2, _omitEnumNames ? '' : 'UPDATE_CLASSIFICATION_SECURITY');
  static const UpdateClassification UPDATE_CLASSIFICATION_MANDATORY =
      UpdateClassification._(
          3, _omitEnumNames ? '' : 'UPDATE_CLASSIFICATION_MANDATORY');

  static const $core.List<UpdateClassification> values = <UpdateClassification>[
    UPDATE_CLASSIFICATION_UNSPECIFIED,
    UPDATE_CLASSIFICATION_ORDINARY,
    UPDATE_CLASSIFICATION_SECURITY,
    UPDATE_CLASSIFICATION_MANDATORY,
  ];

  static final $core.List<UpdateClassification?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static UpdateClassification? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const UpdateClassification._(super.value, super.name);
}

class DistributionChannel extends $pb.ProtobufEnum {
  static const DistributionChannel DISTRIBUTION_CHANNEL_UNSPECIFIED =
      DistributionChannel._(
          0, _omitEnumNames ? '' : 'DISTRIBUTION_CHANNEL_UNSPECIFIED');
  static const DistributionChannel DISTRIBUTION_CHANNEL_VENDOR_PACKAGE =
      DistributionChannel._(
          1, _omitEnumNames ? '' : 'DISTRIBUTION_CHANNEL_VENDOR_PACKAGE');
  static const DistributionChannel DISTRIBUTION_CHANNEL_PACKAGE_MANAGER =
      DistributionChannel._(
          2, _omitEnumNames ? '' : 'DISTRIBUTION_CHANNEL_PACKAGE_MANAGER');
  static const DistributionChannel DISTRIBUTION_CHANNEL_APP_STORE =
      DistributionChannel._(
          3, _omitEnumNames ? '' : 'DISTRIBUTION_CHANNEL_APP_STORE');
  static const DistributionChannel DISTRIBUTION_CHANNEL_ENTERPRISE =
      DistributionChannel._(
          4, _omitEnumNames ? '' : 'DISTRIBUTION_CHANNEL_ENTERPRISE');

  static const $core.List<DistributionChannel> values = <DistributionChannel>[
    DISTRIBUTION_CHANNEL_UNSPECIFIED,
    DISTRIBUTION_CHANNEL_VENDOR_PACKAGE,
    DISTRIBUTION_CHANNEL_PACKAGE_MANAGER,
    DISTRIBUTION_CHANNEL_APP_STORE,
    DISTRIBUTION_CHANNEL_ENTERPRISE,
  ];

  static final $core.List<DistributionChannel?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 4);
  static DistributionChannel? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const DistributionChannel._(super.value, super.name);
}

class CompatibilityState extends $pb.ProtobufEnum {
  static const CompatibilityState COMPATIBILITY_STATE_UNSPECIFIED =
      CompatibilityState._(
          0, _omitEnumNames ? '' : 'COMPATIBILITY_STATE_UNSPECIFIED');
  static const CompatibilityState COMPATIBILITY_STATE_COMPATIBLE =
      CompatibilityState._(
          1, _omitEnumNames ? '' : 'COMPATIBILITY_STATE_COMPATIBLE');
  static const CompatibilityState COMPATIBILITY_STATE_INCOMPATIBLE =
      CompatibilityState._(
          2, _omitEnumNames ? '' : 'COMPATIBILITY_STATE_INCOMPATIBLE');
  static const CompatibilityState COMPATIBILITY_STATE_UNKNOWN =
      CompatibilityState._(
          3, _omitEnumNames ? '' : 'COMPATIBILITY_STATE_UNKNOWN');

  static const $core.List<CompatibilityState> values = <CompatibilityState>[
    COMPATIBILITY_STATE_UNSPECIFIED,
    COMPATIBILITY_STATE_COMPATIBLE,
    COMPATIBILITY_STATE_INCOMPATIBLE,
    COMPATIBILITY_STATE_UNKNOWN,
  ];

  static final $core.List<CompatibilityState?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static CompatibilityState? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const CompatibilityState._(super.value, super.name);
}

class UpdateState extends $pb.ProtobufEnum {
  static const UpdateState UPDATE_STATE_UNSPECIFIED =
      UpdateState._(0, _omitEnumNames ? '' : 'UPDATE_STATE_UNSPECIFIED');
  static const UpdateState UPDATE_STATE_UNKNOWN =
      UpdateState._(1, _omitEnumNames ? '' : 'UPDATE_STATE_UNKNOWN');
  static const UpdateState UPDATE_STATE_UP_TO_DATE =
      UpdateState._(2, _omitEnumNames ? '' : 'UPDATE_STATE_UP_TO_DATE');
  static const UpdateState UPDATE_STATE_AVAILABLE =
      UpdateState._(3, _omitEnumNames ? '' : 'UPDATE_STATE_AVAILABLE');
  static const UpdateState UPDATE_STATE_EXTERNAL_MANAGER_REQUIRED =
      UpdateState._(
          4, _omitEnumNames ? '' : 'UPDATE_STATE_EXTERNAL_MANAGER_REQUIRED');
  static const UpdateState UPDATE_STATE_SOURCE_UNAVAILABLE =
      UpdateState._(5, _omitEnumNames ? '' : 'UPDATE_STATE_SOURCE_UNAVAILABLE');
  static const UpdateState UPDATE_STATE_VERIFICATION_FAILED = UpdateState._(
      6, _omitEnumNames ? '' : 'UPDATE_STATE_VERIFICATION_FAILED');

  static const $core.List<UpdateState> values = <UpdateState>[
    UPDATE_STATE_UNSPECIFIED,
    UPDATE_STATE_UNKNOWN,
    UPDATE_STATE_UP_TO_DATE,
    UPDATE_STATE_AVAILABLE,
    UPDATE_STATE_EXTERNAL_MANAGER_REQUIRED,
    UPDATE_STATE_SOURCE_UNAVAILABLE,
    UPDATE_STATE_VERIFICATION_FAILED,
  ];

  static final $core.List<UpdateState?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 6);
  static UpdateState? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const UpdateState._(super.value, super.name);
}

class LifecycleEvent extends $pb.ProtobufEnum {
  static const LifecycleEvent LIFECYCLE_EVENT_UNSPECIFIED =
      LifecycleEvent._(0, _omitEnumNames ? '' : 'LIFECYCLE_EVENT_UNSPECIFIED');
  static const LifecycleEvent LIFECYCLE_EVENT_UI_QUIT =
      LifecycleEvent._(1, _omitEnumNames ? '' : 'LIFECYCLE_EVENT_UI_QUIT');

  static const $core.List<LifecycleEvent> values = <LifecycleEvent>[
    LIFECYCLE_EVENT_UNSPECIFIED,
    LIFECYCLE_EVENT_UI_QUIT,
  ];

  static final $core.List<LifecycleEvent?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 1);
  static LifecycleEvent? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const LifecycleEvent._(super.value, super.name);
}

const $core.bool _omitEnumNames =
    $core.bool.fromEnvironment('protobuf.omit_enum_names');
