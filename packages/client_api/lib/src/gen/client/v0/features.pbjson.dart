// This is a generated file - do not edit.
//
// Generated from client/v0/features.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, prefer_relative_imports
// ignore_for_file: unused_import

import 'dart:convert' as $convert;
import 'dart:core' as $core;
import 'dart:typed_data' as $typed_data;

@$core.Deprecated('Use settingSourceDescriptor instead')
const SettingSource$json = {
  '1': 'SettingSource',
  '2': [
    {'1': 'SETTING_SOURCE_UNSPECIFIED', '2': 0},
    {'1': 'SETTING_SOURCE_DEFAULT', '2': 1},
    {'1': 'SETTING_SOURCE_USER', '2': 2},
    {'1': 'SETTING_SOURCE_DEVICE_POLICY', '2': 3},
    {'1': 'SETTING_SOURCE_ACCOUNT_POLICY', '2': 4},
    {'1': 'SETTING_SOURCE_PLATFORM', '2': 5},
  ],
};

/// Descriptor for `SettingSource`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List settingSourceDescriptor = $convert.base64Decode(
    'Cg1TZXR0aW5nU291cmNlEh4KGlNFVFRJTkdfU09VUkNFX1VOU1BFQ0lGSUVEEAASGgoWU0VUVE'
    'lOR19TT1VSQ0VfREVGQVVMVBABEhcKE1NFVFRJTkdfU09VUkNFX1VTRVIQAhIgChxTRVRUSU5H'
    'X1NPVVJDRV9ERVZJQ0VfUE9MSUNZEAMSIQodU0VUVElOR19TT1VSQ0VfQUNDT1VOVF9QT0xJQ1'
    'kQBBIbChdTRVRUSU5HX1NPVVJDRV9QTEFURk9STRAF');

@$core.Deprecated('Use lifecycleBehaviorDescriptor instead')
const LifecycleBehavior$json = {
  '1': 'LifecycleBehavior',
  '2': [
    {'1': 'LIFECYCLE_BEHAVIOR_UNSPECIFIED', '2': 0},
    {'1': 'LIFECYCLE_BEHAVIOR_KEEP_INTENT', '2': 1},
    {'1': 'LIFECYCLE_BEHAVIOR_CONNECT', '2': 2},
    {'1': 'LIFECYCLE_BEHAVIOR_DISCONNECT', '2': 3},
    {'1': 'LIFECYCLE_BEHAVIOR_PLATFORM_MANAGED', '2': 4},
  ],
};

/// Descriptor for `LifecycleBehavior`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List lifecycleBehaviorDescriptor = $convert.base64Decode(
    'ChFMaWZlY3ljbGVCZWhhdmlvchIiCh5MSUZFQ1lDTEVfQkVIQVZJT1JfVU5TUEVDSUZJRUQQAB'
    'IiCh5MSUZFQ1lDTEVfQkVIQVZJT1JfS0VFUF9JTlRFTlQQARIeChpMSUZFQ1lDTEVfQkVIQVZJ'
    'T1JfQ09OTkVDVBACEiEKHUxJRkVDWUNMRV9CRUhBVklPUl9ESVNDT05ORUNUEAMSJwojTElGRU'
    'NZQ0xFX0JFSEFWSU9SX1BMQVRGT1JNX01BTkFHRUQQBA==');

@$core.Deprecated('Use preferenceKeyDescriptor instead')
const PreferenceKey$json = {
  '1': 'PreferenceKey',
  '2': [
    {'1': 'PREFERENCE_KEY_UNSPECIFIED', '2': 0},
    {'1': 'PREFERENCE_KEY_ALLOW_INBOUND', '2': 1},
    {'1': 'PREFERENCE_KEY_ACCEPT_DNS', '2': 2},
    {'1': 'PREFERENCE_KEY_ACCEPT_ROUTES', '2': 3},
    {'1': 'PREFERENCE_KEY_RUNTIME_START', '2': 4},
    {'1': 'PREFERENCE_KEY_UI_QUIT', '2': 5},
    {'1': 'PREFERENCE_KEY_USER_LOGOFF', '2': 6},
    {'1': 'PREFERENCE_KEY_SUSPEND', '2': 7},
    {'1': 'PREFERENCE_KEY_RESUME', '2': 8},
  ],
};

/// Descriptor for `PreferenceKey`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List preferenceKeyDescriptor = $convert.base64Decode(
    'Cg1QcmVmZXJlbmNlS2V5Eh4KGlBSRUZFUkVOQ0VfS0VZX1VOU1BFQ0lGSUVEEAASIAocUFJFRk'
    'VSRU5DRV9LRVlfQUxMT1dfSU5CT1VORBABEh0KGVBSRUZFUkVOQ0VfS0VZX0FDQ0VQVF9ETlMQ'
    'AhIgChxQUkVGRVJFTkNFX0tFWV9BQ0NFUFRfUk9VVEVTEAMSIAocUFJFRkVSRU5DRV9LRVlfUl'
    'VOVElNRV9TVEFSVBAEEhoKFlBSRUZFUkVOQ0VfS0VZX1VJX1FVSVQQBRIeChpQUkVGRVJFTkNF'
    'X0tFWV9VU0VSX0xPR09GRhAGEhoKFlBSRUZFUkVOQ0VfS0VZX1NVU1BFTkQQBxIZChVQUkVGRV'
    'JFTkNFX0tFWV9SRVNVTUUQCA==');

@$core.Deprecated('Use lanAccessDescriptor instead')
const LanAccess$json = {
  '1': 'LanAccess',
  '2': [
    {'1': 'LAN_ACCESS_UNSPECIFIED', '2': 0},
    {'1': 'LAN_ACCESS_BLOCK', '2': 1},
    {'1': 'LAN_ACCESS_ALLOW', '2': 2},
  ],
};

/// Descriptor for `LanAccess`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List lanAccessDescriptor = $convert.base64Decode(
    'CglMYW5BY2Nlc3MSGgoWTEFOX0FDQ0VTU19VTlNQRUNJRklFRBAAEhQKEExBTl9BQ0NFU1NfQk'
    'xPQ0sQARIUChBMQU5fQUNDRVNTX0FMTE9XEAI=');

@$core.Deprecated('Use exitFamilyModeDescriptor instead')
const ExitFamilyMode$json = {
  '1': 'ExitFamilyMode',
  '2': [
    {'1': 'EXIT_FAMILY_MODE_UNSPECIFIED', '2': 0},
    {'1': 'EXIT_FAMILY_MODE_NONE', '2': 1},
    {'1': 'EXIT_FAMILY_MODE_IPV4_ONLY', '2': 2},
    {'1': 'EXIT_FAMILY_MODE_IPV6_ONLY', '2': 3},
    {'1': 'EXIT_FAMILY_MODE_DUAL_STACK', '2': 4},
  ],
};

/// Descriptor for `ExitFamilyMode`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List exitFamilyModeDescriptor = $convert.base64Decode(
    'Cg5FeGl0RmFtaWx5TW9kZRIgChxFWElUX0ZBTUlMWV9NT0RFX1VOU1BFQ0lGSUVEEAASGQoVRV'
    'hJVF9GQU1JTFlfTU9ERV9OT05FEAESHgoaRVhJVF9GQU1JTFlfTU9ERV9JUFY0X09OTFkQAhIe'
    'ChpFWElUX0ZBTUlMWV9NT0RFX0lQVjZfT05MWRADEh8KG0VYSVRfRkFNSUxZX01PREVfRFVBTF'
    '9TVEFDSxAE');

@$core.Deprecated('Use applyStateDescriptor instead')
const ApplyState$json = {
  '1': 'ApplyState',
  '2': [
    {'1': 'APPLY_STATE_UNSPECIFIED', '2': 0},
    {'1': 'APPLY_STATE_PENDING', '2': 1},
    {'1': 'APPLY_STATE_APPLIED', '2': 2},
    {'1': 'APPLY_STATE_FAILED', '2': 3},
  ],
};

/// Descriptor for `ApplyState`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List applyStateDescriptor = $convert.base64Decode(
    'CgpBcHBseVN0YXRlEhsKF0FQUExZX1NUQVRFX1VOU1BFQ0lGSUVEEAASFwoTQVBQTFlfU1RBVE'
    'VfUEVORElORxABEhcKE0FQUExZX1NUQVRFX0FQUExJRUQQAhIWChJBUFBMWV9TVEFURV9GQUlM'
    'RUQQAw==');

@$core.Deprecated('Use resourceKindDescriptor instead')
const ResourceKind$json = {
  '1': 'ResourceKind',
  '2': [
    {'1': 'RESOURCE_KIND_UNSPECIFIED', '2': 0},
    {'1': 'RESOURCE_KIND_HOST', '2': 1},
    {'1': 'RESOURCE_KIND_SUBNET', '2': 2},
    {'1': 'RESOURCE_KIND_SERVICE', '2': 3},
    {'1': 'RESOURCE_KIND_APPLICATION', '2': 4},
  ],
};

/// Descriptor for `ResourceKind`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List resourceKindDescriptor = $convert.base64Decode(
    'CgxSZXNvdXJjZUtpbmQSHQoZUkVTT1VSQ0VfS0lORF9VTlNQRUNJRklFRBAAEhYKElJFU09VUk'
    'NFX0tJTkRfSE9TVBABEhgKFFJFU09VUkNFX0tJTkRfU1VCTkVUEAISGQoVUkVTT1VSQ0VfS0lO'
    'RF9TRVJWSUNFEAMSHQoZUkVTT1VSQ0VfS0lORF9BUFBMSUNBVElPThAE');

@$core.Deprecated('Use updateClassificationDescriptor instead')
const UpdateClassification$json = {
  '1': 'UpdateClassification',
  '2': [
    {'1': 'UPDATE_CLASSIFICATION_UNSPECIFIED', '2': 0},
    {'1': 'UPDATE_CLASSIFICATION_ORDINARY', '2': 1},
    {'1': 'UPDATE_CLASSIFICATION_SECURITY', '2': 2},
    {'1': 'UPDATE_CLASSIFICATION_MANDATORY', '2': 3},
  ],
};

/// Descriptor for `UpdateClassification`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List updateClassificationDescriptor = $convert.base64Decode(
    'ChRVcGRhdGVDbGFzc2lmaWNhdGlvbhIlCiFVUERBVEVfQ0xBU1NJRklDQVRJT05fVU5TUEVDSU'
    'ZJRUQQABIiCh5VUERBVEVfQ0xBU1NJRklDQVRJT05fT1JESU5BUlkQARIiCh5VUERBVEVfQ0xB'
    'U1NJRklDQVRJT05fU0VDVVJJVFkQAhIjCh9VUERBVEVfQ0xBU1NJRklDQVRJT05fTUFOREFUT1'
    'JZEAM=');

@$core.Deprecated('Use distributionChannelDescriptor instead')
const DistributionChannel$json = {
  '1': 'DistributionChannel',
  '2': [
    {'1': 'DISTRIBUTION_CHANNEL_UNSPECIFIED', '2': 0},
    {'1': 'DISTRIBUTION_CHANNEL_VENDOR_PACKAGE', '2': 1},
    {'1': 'DISTRIBUTION_CHANNEL_PACKAGE_MANAGER', '2': 2},
    {'1': 'DISTRIBUTION_CHANNEL_APP_STORE', '2': 3},
    {'1': 'DISTRIBUTION_CHANNEL_ENTERPRISE', '2': 4},
  ],
};

/// Descriptor for `DistributionChannel`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List distributionChannelDescriptor = $convert.base64Decode(
    'ChNEaXN0cmlidXRpb25DaGFubmVsEiQKIERJU1RSSUJVVElPTl9DSEFOTkVMX1VOU1BFQ0lGSU'
    'VEEAASJwojRElTVFJJQlVUSU9OX0NIQU5ORUxfVkVORE9SX1BBQ0tBR0UQARIoCiRESVNUUklC'
    'VVRJT05fQ0hBTk5FTF9QQUNLQUdFX01BTkFHRVIQAhIiCh5ESVNUUklCVVRJT05fQ0hBTk5FTF'
    '9BUFBfU1RPUkUQAxIjCh9ESVNUUklCVVRJT05fQ0hBTk5FTF9FTlRFUlBSSVNFEAQ=');

@$core.Deprecated('Use compatibilityStateDescriptor instead')
const CompatibilityState$json = {
  '1': 'CompatibilityState',
  '2': [
    {'1': 'COMPATIBILITY_STATE_UNSPECIFIED', '2': 0},
    {'1': 'COMPATIBILITY_STATE_COMPATIBLE', '2': 1},
    {'1': 'COMPATIBILITY_STATE_INCOMPATIBLE', '2': 2},
    {'1': 'COMPATIBILITY_STATE_UNKNOWN', '2': 3},
  ],
};

/// Descriptor for `CompatibilityState`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List compatibilityStateDescriptor = $convert.base64Decode(
    'ChJDb21wYXRpYmlsaXR5U3RhdGUSIwofQ09NUEFUSUJJTElUWV9TVEFURV9VTlNQRUNJRklFRB'
    'AAEiIKHkNPTVBBVElCSUxJVFlfU1RBVEVfQ09NUEFUSUJMRRABEiQKIENPTVBBVElCSUxJVFlf'
    'U1RBVEVfSU5DT01QQVRJQkxFEAISHwobQ09NUEFUSUJJTElUWV9TVEFURV9VTktOT1dOEAM=');

@$core.Deprecated('Use updateStateDescriptor instead')
const UpdateState$json = {
  '1': 'UpdateState',
  '2': [
    {'1': 'UPDATE_STATE_UNSPECIFIED', '2': 0},
    {'1': 'UPDATE_STATE_UNKNOWN', '2': 1},
    {'1': 'UPDATE_STATE_UP_TO_DATE', '2': 2},
    {'1': 'UPDATE_STATE_AVAILABLE', '2': 3},
    {'1': 'UPDATE_STATE_EXTERNAL_MANAGER_REQUIRED', '2': 4},
    {'1': 'UPDATE_STATE_SOURCE_UNAVAILABLE', '2': 5},
    {'1': 'UPDATE_STATE_VERIFICATION_FAILED', '2': 6},
  ],
};

/// Descriptor for `UpdateState`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List updateStateDescriptor = $convert.base64Decode(
    'CgtVcGRhdGVTdGF0ZRIcChhVUERBVEVfU1RBVEVfVU5TUEVDSUZJRUQQABIYChRVUERBVEVfU1'
    'RBVEVfVU5LTk9XThABEhsKF1VQREFURV9TVEFURV9VUF9UT19EQVRFEAISGgoWVVBEQVRFX1NU'
    'QVRFX0FWQUlMQUJMRRADEioKJlVQREFURV9TVEFURV9FWFRFUk5BTF9NQU5BR0VSX1JFUVVJUk'
    'VEEAQSIwofVVBEQVRFX1NUQVRFX1NPVVJDRV9VTkFWQUlMQUJMRRAFEiQKIFVQREFURV9TVEFU'
    'RV9WRVJJRklDQVRJT05fRkFJTEVEEAY=');

@$core.Deprecated('Use lifecycleEventDescriptor instead')
const LifecycleEvent$json = {
  '1': 'LifecycleEvent',
  '2': [
    {'1': 'LIFECYCLE_EVENT_UNSPECIFIED', '2': 0},
    {'1': 'LIFECYCLE_EVENT_UI_QUIT', '2': 1},
  ],
};

/// Descriptor for `LifecycleEvent`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List lifecycleEventDescriptor = $convert.base64Decode(
    'Cg5MaWZlY3ljbGVFdmVudBIfChtMSUZFQ1lDTEVfRVZFTlRfVU5TUEVDSUZJRUQQABIbChdMSU'
    'ZFQ1lDTEVfRVZFTlRfVUlfUVVJVBAB');

@$core.Deprecated('Use settingControlDescriptor instead')
const SettingControl$json = {
  '1': 'SettingControl',
  '2': [
    {
      '1': 'source',
      '3': 1,
      '4': 1,
      '5': 14,
      '6': '.client.v0.SettingSource',
      '10': 'source'
    },
    {'1': 'locked', '3': 2, '4': 1, '5': 8, '10': 'locked'},
    {
      '1': 'mutation',
      '3': 3,
      '4': 1,
      '5': 11,
      '6': '.client.v0.Restriction',
      '10': 'mutation'
    },
    {'1': 'policy_id', '3': 4, '4': 1, '5': 9, '10': 'policyId'},
  ],
};

/// Descriptor for `SettingControl`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List settingControlDescriptor = $convert.base64Decode(
    'Cg5TZXR0aW5nQ29udHJvbBIwCgZzb3VyY2UYASABKA4yGC5jbGllbnQudjAuU2V0dGluZ1NvdX'
    'JjZVIGc291cmNlEhYKBmxvY2tlZBgCIAEoCFIGbG9ja2VkEjIKCG11dGF0aW9uGAMgASgLMhYu'
    'Y2xpZW50LnYwLlJlc3RyaWN0aW9uUghtdXRhdGlvbhIbCglwb2xpY3lfaWQYBCABKAlSCHBvbG'
    'ljeUlk');

@$core.Deprecated('Use booleanSettingDescriptor instead')
const BooleanSetting$json = {
  '1': 'BooleanSetting',
  '2': [
    {'1': 'effective', '3': 1, '4': 1, '5': 8, '10': 'effective'},
    {
      '1': 'requested',
      '3': 2,
      '4': 1,
      '5': 8,
      '9': 0,
      '10': 'requested',
      '17': true
    },
    {
      '1': 'control',
      '3': 3,
      '4': 1,
      '5': 11,
      '6': '.client.v0.SettingControl',
      '10': 'control'
    },
  ],
  '8': [
    {'1': '_requested'},
  ],
};

/// Descriptor for `BooleanSetting`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List booleanSettingDescriptor = $convert.base64Decode(
    'Cg5Cb29sZWFuU2V0dGluZxIcCgllZmZlY3RpdmUYASABKAhSCWVmZmVjdGl2ZRIhCglyZXF1ZX'
    'N0ZWQYAiABKAhIAFIJcmVxdWVzdGVkiAEBEjMKB2NvbnRyb2wYAyABKAsyGS5jbGllbnQudjAu'
    'U2V0dGluZ0NvbnRyb2xSB2NvbnRyb2xCDAoKX3JlcXVlc3RlZA==');

@$core.Deprecated('Use lifecycleSettingDescriptor instead')
const LifecycleSetting$json = {
  '1': 'LifecycleSetting',
  '2': [
    {
      '1': 'effective',
      '3': 1,
      '4': 1,
      '5': 14,
      '6': '.client.v0.LifecycleBehavior',
      '10': 'effective'
    },
    {
      '1': 'requested',
      '3': 2,
      '4': 1,
      '5': 14,
      '6': '.client.v0.LifecycleBehavior',
      '9': 0,
      '10': 'requested',
      '17': true
    },
    {
      '1': 'control',
      '3': 3,
      '4': 1,
      '5': 11,
      '6': '.client.v0.SettingControl',
      '10': 'control'
    },
    {
      '1': 'allowed_values',
      '3': 4,
      '4': 3,
      '5': 14,
      '6': '.client.v0.LifecycleBehavior',
      '10': 'allowedValues'
    },
  ],
  '8': [
    {'1': '_requested'},
  ],
};

/// Descriptor for `LifecycleSetting`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List lifecycleSettingDescriptor = $convert.base64Decode(
    'ChBMaWZlY3ljbGVTZXR0aW5nEjoKCWVmZmVjdGl2ZRgBIAEoDjIcLmNsaWVudC52MC5MaWZlY3'
    'ljbGVCZWhhdmlvclIJZWZmZWN0aXZlEj8KCXJlcXVlc3RlZBgCIAEoDjIcLmNsaWVudC52MC5M'
    'aWZlY3ljbGVCZWhhdmlvckgAUglyZXF1ZXN0ZWSIAQESMwoHY29udHJvbBgDIAEoCzIZLmNsaW'
    'VudC52MC5TZXR0aW5nQ29udHJvbFIHY29udHJvbBJDCg5hbGxvd2VkX3ZhbHVlcxgEIAMoDjIc'
    'LmNsaWVudC52MC5MaWZlY3ljbGVCZWhhdmlvclINYWxsb3dlZFZhbHVlc0IMCgpfcmVxdWVzdG'
    'Vk');

@$core.Deprecated('Use runtimeLifecycleDescriptor instead')
const RuntimeLifecycle$json = {
  '1': 'RuntimeLifecycle',
  '2': [
    {
      '1': 'runtime_start',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.LifecycleSetting',
      '10': 'runtimeStart'
    },
    {
      '1': 'ui_quit',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.client.v0.LifecycleSetting',
      '10': 'uiQuit'
    },
    {
      '1': 'user_logoff',
      '3': 3,
      '4': 1,
      '5': 11,
      '6': '.client.v0.LifecycleSetting',
      '10': 'userLogoff'
    },
    {
      '1': 'suspend',
      '3': 4,
      '4': 1,
      '5': 11,
      '6': '.client.v0.LifecycleSetting',
      '10': 'suspend'
    },
    {
      '1': 'resume',
      '3': 5,
      '4': 1,
      '5': 11,
      '6': '.client.v0.LifecycleSetting',
      '10': 'resume'
    },
  ],
};

/// Descriptor for `RuntimeLifecycle`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List runtimeLifecycleDescriptor = $convert.base64Decode(
    'ChBSdW50aW1lTGlmZWN5Y2xlEkAKDXJ1bnRpbWVfc3RhcnQYASABKAsyGy5jbGllbnQudjAuTG'
    'lmZWN5Y2xlU2V0dGluZ1IMcnVudGltZVN0YXJ0EjQKB3VpX3F1aXQYAiABKAsyGy5jbGllbnQu'
    'djAuTGlmZWN5Y2xlU2V0dGluZ1IGdWlRdWl0EjwKC3VzZXJfbG9nb2ZmGAMgASgLMhsuY2xpZW'
    '50LnYwLkxpZmVjeWNsZVNldHRpbmdSCnVzZXJMb2dvZmYSNQoHc3VzcGVuZBgEIAEoCzIbLmNs'
    'aWVudC52MC5MaWZlY3ljbGVTZXR0aW5nUgdzdXNwZW5kEjMKBnJlc3VtZRgFIAEoCzIbLmNsaW'
    'VudC52MC5MaWZlY3ljbGVTZXR0aW5nUgZyZXN1bWU=');

@$core.Deprecated('Use preferencesDescriptor instead')
const Preferences$json = {
  '1': 'Preferences',
  '2': [
    {
      '1': 'metadata',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.SnapshotMetadata',
      '10': 'metadata'
    },
    {'1': 'profile_id', '3': 2, '4': 1, '5': 9, '10': 'profileId'},
    {
      '1': 'allow_inbound',
      '3': 3,
      '4': 1,
      '5': 11,
      '6': '.client.v0.BooleanSetting',
      '10': 'allowInbound'
    },
    {
      '1': 'accept_dns',
      '3': 4,
      '4': 1,
      '5': 11,
      '6': '.client.v0.BooleanSetting',
      '10': 'acceptDns'
    },
    {
      '1': 'accept_routes',
      '3': 5,
      '4': 1,
      '5': 11,
      '6': '.client.v0.BooleanSetting',
      '10': 'acceptRoutes'
    },
    {
      '1': 'lifecycle',
      '3': 6,
      '4': 1,
      '5': 11,
      '6': '.client.v0.RuntimeLifecycle',
      '10': 'lifecycle'
    },
  ],
};

/// Descriptor for `Preferences`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List preferencesDescriptor = $convert.base64Decode(
    'CgtQcmVmZXJlbmNlcxI3CghtZXRhZGF0YRgBIAEoCzIbLmNsaWVudC52MC5TbmFwc2hvdE1ldG'
    'FkYXRhUghtZXRhZGF0YRIdCgpwcm9maWxlX2lkGAIgASgJUglwcm9maWxlSWQSPgoNYWxsb3df'
    'aW5ib3VuZBgDIAEoCzIZLmNsaWVudC52MC5Cb29sZWFuU2V0dGluZ1IMYWxsb3dJbmJvdW5kEj'
    'gKCmFjY2VwdF9kbnMYBCABKAsyGS5jbGllbnQudjAuQm9vbGVhblNldHRpbmdSCWFjY2VwdERu'
    'cxI+Cg1hY2NlcHRfcm91dGVzGAUgASgLMhkuY2xpZW50LnYwLkJvb2xlYW5TZXR0aW5nUgxhY2'
    'NlcHRSb3V0ZXMSOQoJbGlmZWN5Y2xlGAYgASgLMhsuY2xpZW50LnYwLlJ1bnRpbWVMaWZlY3lj'
    'bGVSCWxpZmVjeWNsZQ==');

@$core.Deprecated('Use preferencesPatchDescriptor instead')
const PreferencesPatch$json = {
  '1': 'PreferencesPatch',
  '2': [
    {
      '1': 'allow_inbound',
      '3': 1,
      '4': 1,
      '5': 8,
      '9': 0,
      '10': 'allowInbound',
      '17': true
    },
    {
      '1': 'accept_dns',
      '3': 2,
      '4': 1,
      '5': 8,
      '9': 1,
      '10': 'acceptDns',
      '17': true
    },
    {
      '1': 'accept_routes',
      '3': 3,
      '4': 1,
      '5': 8,
      '9': 2,
      '10': 'acceptRoutes',
      '17': true
    },
    {
      '1': 'runtime_start',
      '3': 4,
      '4': 1,
      '5': 14,
      '6': '.client.v0.LifecycleBehavior',
      '9': 3,
      '10': 'runtimeStart',
      '17': true
    },
    {
      '1': 'ui_quit',
      '3': 5,
      '4': 1,
      '5': 14,
      '6': '.client.v0.LifecycleBehavior',
      '9': 4,
      '10': 'uiQuit',
      '17': true
    },
    {
      '1': 'user_logoff',
      '3': 6,
      '4': 1,
      '5': 14,
      '6': '.client.v0.LifecycleBehavior',
      '9': 5,
      '10': 'userLogoff',
      '17': true
    },
    {
      '1': 'suspend',
      '3': 7,
      '4': 1,
      '5': 14,
      '6': '.client.v0.LifecycleBehavior',
      '9': 6,
      '10': 'suspend',
      '17': true
    },
    {
      '1': 'resume',
      '3': 8,
      '4': 1,
      '5': 14,
      '6': '.client.v0.LifecycleBehavior',
      '9': 7,
      '10': 'resume',
      '17': true
    },
  ],
  '8': [
    {'1': '_allow_inbound'},
    {'1': '_accept_dns'},
    {'1': '_accept_routes'},
    {'1': '_runtime_start'},
    {'1': '_ui_quit'},
    {'1': '_user_logoff'},
    {'1': '_suspend'},
    {'1': '_resume'},
  ],
};

/// Descriptor for `PreferencesPatch`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List preferencesPatchDescriptor = $convert.base64Decode(
    'ChBQcmVmZXJlbmNlc1BhdGNoEigKDWFsbG93X2luYm91bmQYASABKAhIAFIMYWxsb3dJbmJvdW'
    '5kiAEBEiIKCmFjY2VwdF9kbnMYAiABKAhIAVIJYWNjZXB0RG5ziAEBEigKDWFjY2VwdF9yb3V0'
    'ZXMYAyABKAhIAlIMYWNjZXB0Um91dGVziAEBEkYKDXJ1bnRpbWVfc3RhcnQYBCABKA4yHC5jbG'
    'llbnQudjAuTGlmZWN5Y2xlQmVoYXZpb3JIA1IMcnVudGltZVN0YXJ0iAEBEjoKB3VpX3F1aXQY'
    'BSABKA4yHC5jbGllbnQudjAuTGlmZWN5Y2xlQmVoYXZpb3JIBFIGdWlRdWl0iAEBEkIKC3VzZX'
    'JfbG9nb2ZmGAYgASgOMhwuY2xpZW50LnYwLkxpZmVjeWNsZUJlaGF2aW9ySAVSCnVzZXJMb2dv'
    'ZmaIAQESOwoHc3VzcGVuZBgHIAEoDjIcLmNsaWVudC52MC5MaWZlY3ljbGVCZWhhdmlvckgGUg'
    'dzdXNwZW5kiAEBEjkKBnJlc3VtZRgIIAEoDjIcLmNsaWVudC52MC5MaWZlY3ljbGVCZWhhdmlv'
    'ckgHUgZyZXN1bWWIAQFCEAoOX2FsbG93X2luYm91bmRCDQoLX2FjY2VwdF9kbnNCEAoOX2FjY2'
    'VwdF9yb3V0ZXNCEAoOX3J1bnRpbWVfc3RhcnRCCgoIX3VpX3F1aXRCDgoMX3VzZXJfbG9nb2Zm'
    'QgoKCF9zdXNwZW5kQgkKB19yZXN1bWU=');

@$core.Deprecated('Use managedSettingDescriptor instead')
const ManagedSetting$json = {
  '1': 'ManagedSetting',
  '2': [
    {
      '1': 'key',
      '3': 1,
      '4': 1,
      '5': 14,
      '6': '.client.v0.PreferenceKey',
      '10': 'key'
    },
    {
      '1': 'control',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.client.v0.SettingControl',
      '10': 'control'
    },
    {
      '1': 'boolean_value',
      '3': 3,
      '4': 1,
      '5': 8,
      '9': 0,
      '10': 'booleanValue'
    },
    {
      '1': 'lifecycle_value',
      '3': 4,
      '4': 1,
      '5': 14,
      '6': '.client.v0.LifecycleBehavior',
      '9': 0,
      '10': 'lifecycleValue'
    },
  ],
  '8': [
    {'1': 'effective_value'},
  ],
};

/// Descriptor for `ManagedSetting`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List managedSettingDescriptor = $convert.base64Decode(
    'Cg5NYW5hZ2VkU2V0dGluZxIqCgNrZXkYASABKA4yGC5jbGllbnQudjAuUHJlZmVyZW5jZUtleV'
    'IDa2V5EjMKB2NvbnRyb2wYAiABKAsyGS5jbGllbnQudjAuU2V0dGluZ0NvbnRyb2xSB2NvbnRy'
    'b2wSJQoNYm9vbGVhbl92YWx1ZRgDIAEoCEgAUgxib29sZWFuVmFsdWUSRwoPbGlmZWN5Y2xlX3'
    'ZhbHVlGAQgASgOMhwuY2xpZW50LnYwLkxpZmVjeWNsZUJlaGF2aW9ySABSDmxpZmVjeWNsZVZh'
    'bHVlQhEKD2VmZmVjdGl2ZV92YWx1ZQ==');

@$core.Deprecated('Use exitNodeDescriptor instead')
const ExitNode$json = {
  '1': 'ExitNode',
  '2': [
    {'1': 'id', '3': 1, '4': 1, '5': 9, '10': 'id'},
    {'1': 'display_name', '3': 2, '4': 1, '5': 9, '10': 'displayName'},
    {'1': 'peer_id', '3': 3, '4': 1, '5': 9, '10': 'peerId'},
    {
      '1': 'selection',
      '3': 4,
      '4': 1,
      '5': 11,
      '6': '.client.v0.Restriction',
      '10': 'selection'
    },
    {
      '1': 'allowed_lan_access',
      '3': 5,
      '4': 3,
      '5': 14,
      '6': '.client.v0.LanAccess',
      '10': 'allowedLanAccess'
    },
    {
      '1': 'allowed_family_modes',
      '3': 6,
      '4': 3,
      '5': 14,
      '6': '.client.v0.ExitFamilyMode',
      '10': 'allowedFamilyModes'
    },
  ],
};

/// Descriptor for `ExitNode`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List exitNodeDescriptor = $convert.base64Decode(
    'CghFeGl0Tm9kZRIOCgJpZBgBIAEoCVICaWQSIQoMZGlzcGxheV9uYW1lGAIgASgJUgtkaXNwbG'
    'F5TmFtZRIXCgdwZWVyX2lkGAMgASgJUgZwZWVySWQSNAoJc2VsZWN0aW9uGAQgASgLMhYuY2xp'
    'ZW50LnYwLlJlc3RyaWN0aW9uUglzZWxlY3Rpb24SQgoSYWxsb3dlZF9sYW5fYWNjZXNzGAUgAy'
    'gOMhQuY2xpZW50LnYwLkxhbkFjY2Vzc1IQYWxsb3dlZExhbkFjY2VzcxJLChRhbGxvd2VkX2Zh'
    'bWlseV9tb2RlcxgGIAMoDjIZLmNsaWVudC52MC5FeGl0RmFtaWx5TW9kZVISYWxsb3dlZEZhbW'
    'lseU1vZGVz');

@$core.Deprecated('Use exitFamilyStatusDescriptor instead')
const ExitFamilyStatus$json = {
  '1': 'ExitFamilyStatus',
  '2': [
    {
      '1': 'requested_exit_node_id',
      '3': 1,
      '4': 1,
      '5': 9,
      '9': 0,
      '10': 'requestedExitNodeId',
      '17': true
    },
    {
      '1': 'effective_exit_node_id',
      '3': 2,
      '4': 1,
      '5': 9,
      '9': 1,
      '10': 'effectiveExitNodeId',
      '17': true
    },
    {
      '1': 'apply_state',
      '3': 3,
      '4': 1,
      '5': 14,
      '6': '.client.v0.ApplyState',
      '10': 'applyState'
    },
    {
      '1': 'failure',
      '3': 4,
      '4': 1,
      '5': 11,
      '6': '.client.v0.Failure',
      '10': 'failure'
    },
    {'1': 'fail_closed', '3': 5, '4': 1, '5': 8, '10': 'failClosed'},
  ],
  '8': [
    {'1': '_requested_exit_node_id'},
    {'1': '_effective_exit_node_id'},
  ],
};

/// Descriptor for `ExitFamilyStatus`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List exitFamilyStatusDescriptor = $convert.base64Decode(
    'ChBFeGl0RmFtaWx5U3RhdHVzEjgKFnJlcXVlc3RlZF9leGl0X25vZGVfaWQYASABKAlIAFITcm'
    'VxdWVzdGVkRXhpdE5vZGVJZIgBARI4ChZlZmZlY3RpdmVfZXhpdF9ub2RlX2lkGAIgASgJSAFS'
    'E2VmZmVjdGl2ZUV4aXROb2RlSWSIAQESNgoLYXBwbHlfc3RhdGUYAyABKA4yFS5jbGllbnQudj'
    'AuQXBwbHlTdGF0ZVIKYXBwbHlTdGF0ZRIsCgdmYWlsdXJlGAQgASgLMhIuY2xpZW50LnYwLkZh'
    'aWx1cmVSB2ZhaWx1cmUSHwoLZmFpbF9jbG9zZWQYBSABKAhSCmZhaWxDbG9zZWRCGQoXX3JlcX'
    'Vlc3RlZF9leGl0X25vZGVfaWRCGQoXX2VmZmVjdGl2ZV9leGl0X25vZGVfaWQ=');

@$core.Deprecated('Use exitNodeStatusDescriptor instead')
const ExitNodeStatus$json = {
  '1': 'ExitNodeStatus',
  '2': [
    {
      '1': 'metadata',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.SnapshotMetadata',
      '10': 'metadata'
    },
    {'1': 'profile_id', '3': 2, '4': 1, '5': 9, '10': 'profileId'},
    {
      '1': 'requested_exit_node_id',
      '3': 3,
      '4': 1,
      '5': 9,
      '9': 0,
      '10': 'requestedExitNodeId',
      '17': true
    },
    {
      '1': 'effective_exit_node_id',
      '3': 4,
      '4': 1,
      '5': 9,
      '9': 1,
      '10': 'effectiveExitNodeId',
      '17': true
    },
    {
      '1': 'requested_lan_access',
      '3': 5,
      '4': 1,
      '5': 14,
      '6': '.client.v0.LanAccess',
      '10': 'requestedLanAccess'
    },
    {
      '1': 'effective_lan_access',
      '3': 6,
      '4': 1,
      '5': 14,
      '6': '.client.v0.LanAccess',
      '10': 'effectiveLanAccess'
    },
    {
      '1': 'apply_state',
      '3': 7,
      '4': 1,
      '5': 14,
      '6': '.client.v0.ApplyState',
      '10': 'applyState'
    },
    {
      '1': 'control',
      '3': 8,
      '4': 1,
      '5': 11,
      '6': '.client.v0.SettingControl',
      '10': 'control'
    },
    {
      '1': 'failure',
      '3': 9,
      '4': 1,
      '5': 11,
      '6': '.client.v0.Failure',
      '10': 'failure'
    },
    {'1': 'fail_closed', '3': 10, '4': 1, '5': 8, '10': 'failClosed'},
    {
      '1': 'requested_family_mode',
      '3': 11,
      '4': 1,
      '5': 14,
      '6': '.client.v0.ExitFamilyMode',
      '10': 'requestedFamilyMode'
    },
    {
      '1': 'ipv4',
      '3': 12,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ExitFamilyStatus',
      '10': 'ipv4'
    },
    {
      '1': 'ipv6',
      '3': 13,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ExitFamilyStatus',
      '10': 'ipv6'
    },
  ],
  '8': [
    {'1': '_requested_exit_node_id'},
    {'1': '_effective_exit_node_id'},
  ],
};

/// Descriptor for `ExitNodeStatus`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List exitNodeStatusDescriptor = $convert.base64Decode(
    'Cg5FeGl0Tm9kZVN0YXR1cxI3CghtZXRhZGF0YRgBIAEoCzIbLmNsaWVudC52MC5TbmFwc2hvdE'
    '1ldGFkYXRhUghtZXRhZGF0YRIdCgpwcm9maWxlX2lkGAIgASgJUglwcm9maWxlSWQSOAoWcmVx'
    'dWVzdGVkX2V4aXRfbm9kZV9pZBgDIAEoCUgAUhNyZXF1ZXN0ZWRFeGl0Tm9kZUlkiAEBEjgKFm'
    'VmZmVjdGl2ZV9leGl0X25vZGVfaWQYBCABKAlIAVITZWZmZWN0aXZlRXhpdE5vZGVJZIgBARJG'
    'ChRyZXF1ZXN0ZWRfbGFuX2FjY2VzcxgFIAEoDjIULmNsaWVudC52MC5MYW5BY2Nlc3NSEnJlcX'
    'Vlc3RlZExhbkFjY2VzcxJGChRlZmZlY3RpdmVfbGFuX2FjY2VzcxgGIAEoDjIULmNsaWVudC52'
    'MC5MYW5BY2Nlc3NSEmVmZmVjdGl2ZUxhbkFjY2VzcxI2CgthcHBseV9zdGF0ZRgHIAEoDjIVLm'
    'NsaWVudC52MC5BcHBseVN0YXRlUgphcHBseVN0YXRlEjMKB2NvbnRyb2wYCCABKAsyGS5jbGll'
    'bnQudjAuU2V0dGluZ0NvbnRyb2xSB2NvbnRyb2wSLAoHZmFpbHVyZRgJIAEoCzISLmNsaWVudC'
    '52MC5GYWlsdXJlUgdmYWlsdXJlEh8KC2ZhaWxfY2xvc2VkGAogASgIUgpmYWlsQ2xvc2VkEk0K'
    'FXJlcXVlc3RlZF9mYW1pbHlfbW9kZRgLIAEoDjIZLmNsaWVudC52MC5FeGl0RmFtaWx5TW9kZV'
    'ITcmVxdWVzdGVkRmFtaWx5TW9kZRIvCgRpcHY0GAwgASgLMhsuY2xpZW50LnYwLkV4aXRGYW1p'
    'bHlTdGF0dXNSBGlwdjQSLwoEaXB2NhgNIAEoCzIbLmNsaWVudC52MC5FeGl0RmFtaWx5U3RhdH'
    'VzUgRpcHY2QhkKF19yZXF1ZXN0ZWRfZXhpdF9ub2RlX2lkQhkKF19lZmZlY3RpdmVfZXhpdF9u'
    'b2RlX2lk');

@$core.Deprecated('Use hostTargetDescriptor instead')
const HostTarget$json = {
  '1': 'HostTarget',
  '2': [
    {'1': 'addresses', '3': 1, '4': 3, '5': 9, '10': 'addresses'},
    {'1': 'hostname', '3': 2, '4': 1, '5': 9, '10': 'hostname'},
  ],
};

/// Descriptor for `HostTarget`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List hostTargetDescriptor = $convert.base64Decode(
    'CgpIb3N0VGFyZ2V0EhwKCWFkZHJlc3NlcxgBIAMoCVIJYWRkcmVzc2VzEhoKCGhvc3RuYW1lGA'
    'IgASgJUghob3N0bmFtZQ==');

@$core.Deprecated('Use subnetTargetDescriptor instead')
const SubnetTarget$json = {
  '1': 'SubnetTarget',
  '2': [
    {'1': 'cidr', '3': 1, '4': 1, '5': 9, '10': 'cidr'},
  ],
};

/// Descriptor for `SubnetTarget`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List subnetTargetDescriptor =
    $convert.base64Decode('CgxTdWJuZXRUYXJnZXQSEgoEY2lkchgBIAEoCVIEY2lkcg==');

@$core.Deprecated('Use serviceTargetDescriptor instead')
const ServiceTarget$json = {
  '1': 'ServiceTarget',
  '2': [
    {'1': 'hostname', '3': 1, '4': 1, '5': 9, '10': 'hostname'},
    {'1': 'port', '3': 2, '4': 1, '5': 13, '10': 'port'},
    {'1': 'protocol', '3': 3, '4': 1, '5': 9, '10': 'protocol'},
  ],
};

/// Descriptor for `ServiceTarget`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List serviceTargetDescriptor = $convert.base64Decode(
    'Cg1TZXJ2aWNlVGFyZ2V0EhoKCGhvc3RuYW1lGAEgASgJUghob3N0bmFtZRISCgRwb3J0GAIgAS'
    'gNUgRwb3J0EhoKCHByb3RvY29sGAMgASgJUghwcm90b2NvbA==');

@$core.Deprecated('Use applicationTargetDescriptor instead')
const ApplicationTarget$json = {
  '1': 'ApplicationTarget',
  '2': [
    {'1': 'display_address', '3': 1, '4': 1, '5': 9, '10': 'displayAddress'},
    {'1': 'browser_url', '3': 2, '4': 1, '5': 9, '10': 'browserUrl'},
  ],
};

/// Descriptor for `ApplicationTarget`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List applicationTargetDescriptor = $convert.base64Decode(
    'ChFBcHBsaWNhdGlvblRhcmdldBInCg9kaXNwbGF5X2FkZHJlc3MYASABKAlSDmRpc3BsYXlBZG'
    'RyZXNzEh8KC2Jyb3dzZXJfdXJsGAIgASgJUgpicm93c2VyVXJs');

@$core.Deprecated('Use resourceDescriptor instead')
const Resource$json = {
  '1': 'Resource',
  '2': [
    {'1': 'id', '3': 1, '4': 1, '5': 9, '10': 'id'},
    {'1': 'display_name', '3': 2, '4': 1, '5': 9, '10': 'displayName'},
    {
      '1': 'kind',
      '3': 3,
      '4': 1,
      '5': 14,
      '6': '.client.v0.ResourceKind',
      '10': 'kind'
    },
    {'1': 'network_id', '3': 4, '4': 1, '5': 9, '10': 'networkId'},
    {
      '1': 'availability',
      '3': 5,
      '4': 1,
      '5': 11,
      '6': '.client.v0.Restriction',
      '10': 'availability'
    },
    {
      '1': 'enabled',
      '3': 6,
      '4': 1,
      '5': 11,
      '6': '.client.v0.BooleanSetting',
      '10': 'enabled'
    },
    {
      '1': 'overlapping_resource_ids',
      '3': 7,
      '4': 3,
      '5': 9,
      '10': 'overlappingResourceIds'
    },
    {
      '1': 'overlap_reason_key',
      '3': 8,
      '4': 1,
      '5': 9,
      '10': 'overlapReasonKey'
    },
    {
      '1': 'host',
      '3': 10,
      '4': 1,
      '5': 11,
      '6': '.client.v0.HostTarget',
      '9': 0,
      '10': 'host'
    },
    {
      '1': 'subnet',
      '3': 11,
      '4': 1,
      '5': 11,
      '6': '.client.v0.SubnetTarget',
      '9': 0,
      '10': 'subnet'
    },
    {
      '1': 'service',
      '3': 12,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ServiceTarget',
      '9': 0,
      '10': 'service'
    },
    {
      '1': 'application',
      '3': 13,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ApplicationTarget',
      '9': 0,
      '10': 'application'
    },
  ],
  '8': [
    {'1': 'target'},
  ],
};

/// Descriptor for `Resource`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List resourceDescriptor = $convert.base64Decode(
    'CghSZXNvdXJjZRIOCgJpZBgBIAEoCVICaWQSIQoMZGlzcGxheV9uYW1lGAIgASgJUgtkaXNwbG'
    'F5TmFtZRIrCgRraW5kGAMgASgOMhcuY2xpZW50LnYwLlJlc291cmNlS2luZFIEa2luZBIdCgpu'
    'ZXR3b3JrX2lkGAQgASgJUgluZXR3b3JrSWQSOgoMYXZhaWxhYmlsaXR5GAUgASgLMhYuY2xpZW'
    '50LnYwLlJlc3RyaWN0aW9uUgxhdmFpbGFiaWxpdHkSMwoHZW5hYmxlZBgGIAEoCzIZLmNsaWVu'
    'dC52MC5Cb29sZWFuU2V0dGluZ1IHZW5hYmxlZBI4ChhvdmVybGFwcGluZ19yZXNvdXJjZV9pZH'
    'MYByADKAlSFm92ZXJsYXBwaW5nUmVzb3VyY2VJZHMSLAoSb3ZlcmxhcF9yZWFzb25fa2V5GAgg'
    'ASgJUhBvdmVybGFwUmVhc29uS2V5EisKBGhvc3QYCiABKAsyFS5jbGllbnQudjAuSG9zdFRhcm'
    'dldEgAUgRob3N0EjEKBnN1Ym5ldBgLIAEoCzIXLmNsaWVudC52MC5TdWJuZXRUYXJnZXRIAFIG'
    'c3VibmV0EjQKB3NlcnZpY2UYDCABKAsyGC5jbGllbnQudjAuU2VydmljZVRhcmdldEgAUgdzZX'
    'J2aWNlEkAKC2FwcGxpY2F0aW9uGA0gASgLMhwuY2xpZW50LnYwLkFwcGxpY2F0aW9uVGFyZ2V0'
    'SABSC2FwcGxpY2F0aW9uQggKBnRhcmdldA==');

@$core.Deprecated('Use compatibilityDescriptor instead')
const Compatibility$json = {
  '1': 'Compatibility',
  '2': [
    {
      '1': 'state',
      '3': 1,
      '4': 1,
      '5': 14,
      '6': '.client.v0.CompatibilityState',
      '10': 'state'
    },
    {'1': 'reason_key', '3': 2, '4': 1, '5': 9, '10': 'reasonKey'},
    {
      '1': 'minimum_ipc_version',
      '3': 3,
      '4': 1,
      '5': 13,
      '10': 'minimumIpcVersion'
    },
    {
      '1': 'maximum_ipc_version',
      '3': 4,
      '4': 1,
      '5': 13,
      '10': 'maximumIpcVersion'
    },
    {
      '1': 'accepted_contract_sha256',
      '3': 5,
      '4': 3,
      '5': 9,
      '10': 'acceptedContractSha256'
    },
  ],
};

/// Descriptor for `Compatibility`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List compatibilityDescriptor = $convert.base64Decode(
    'Cg1Db21wYXRpYmlsaXR5EjMKBXN0YXRlGAEgASgOMh0uY2xpZW50LnYwLkNvbXBhdGliaWxpdH'
    'lTdGF0ZVIFc3RhdGUSHQoKcmVhc29uX2tleRgCIAEoCVIJcmVhc29uS2V5Ei4KE21pbmltdW1f'
    'aXBjX3ZlcnNpb24YAyABKA1SEW1pbmltdW1JcGNWZXJzaW9uEi4KE21heGltdW1faXBjX3Zlcn'
    'Npb24YBCABKA1SEW1heGltdW1JcGNWZXJzaW9uEjgKGGFjY2VwdGVkX2NvbnRyYWN0X3NoYTI1'
    'NhgFIAMoCVIWYWNjZXB0ZWRDb250cmFjdFNoYTI1Ng==');

@$core.Deprecated('Use verifiedUpdateDescriptor instead')
const VerifiedUpdate$json = {
  '1': 'VerifiedUpdate',
  '2': [
    {'1': 'release_id', '3': 1, '4': 1, '5': 9, '10': 'releaseId'},
    {'1': 'manifest_sha256', '3': 2, '4': 1, '5': 9, '10': 'manifestSha256'},
    {'1': 'signing_key_id', '3': 3, '4': 1, '5': 9, '10': 'signingKeyId'},
    {
      '1': 'verified_at',
      '3': 4,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'verifiedAt'
    },
    {
      '1': 'expires_at',
      '3': 5,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'expiresAt'
    },
    {
      '1': 'runtime',
      '3': 6,
      '4': 1,
      '5': 11,
      '6': '.client.v0.BuildIdentity',
      '10': 'runtime'
    },
    {'1': 'paired_ui_version', '3': 7, '4': 1, '5': 9, '10': 'pairedUiVersion'},
    {
      '1': 'compatibility',
      '3': 8,
      '4': 1,
      '5': 11,
      '6': '.client.v0.Compatibility',
      '10': 'compatibility'
    },
    {
      '1': 'classification',
      '3': 9,
      '4': 1,
      '5': 14,
      '6': '.client.v0.UpdateClassification',
      '10': 'classification'
    },
    {
      '1': 'channel',
      '3': 10,
      '4': 1,
      '5': 14,
      '6': '.client.v0.DistributionChannel',
      '10': 'channel'
    },
    {
      '1': 'mandatory_after',
      '3': 11,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'mandatoryAfter'
    },
    {'1': 'action_url', '3': 12, '4': 1, '5': 9, '10': 'actionUrl'},
    {
      '1': 'release_notes_url',
      '3': 13,
      '4': 1,
      '5': 9,
      '10': 'releaseNotesUrl'
    },
  ],
};

/// Descriptor for `VerifiedUpdate`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List verifiedUpdateDescriptor = $convert.base64Decode(
    'Cg5WZXJpZmllZFVwZGF0ZRIdCgpyZWxlYXNlX2lkGAEgASgJUglyZWxlYXNlSWQSJwoPbWFuaW'
    'Zlc3Rfc2hhMjU2GAIgASgJUg5tYW5pZmVzdFNoYTI1NhIkCg5zaWduaW5nX2tleV9pZBgDIAEo'
    'CVIMc2lnbmluZ0tleUlkEjsKC3ZlcmlmaWVkX2F0GAQgASgLMhouZ29vZ2xlLnByb3RvYnVmLl'
    'RpbWVzdGFtcFIKdmVyaWZpZWRBdBI5CgpleHBpcmVzX2F0GAUgASgLMhouZ29vZ2xlLnByb3Rv'
    'YnVmLlRpbWVzdGFtcFIJZXhwaXJlc0F0EjIKB3J1bnRpbWUYBiABKAsyGC5jbGllbnQudjAuQn'
    'VpbGRJZGVudGl0eVIHcnVudGltZRIqChFwYWlyZWRfdWlfdmVyc2lvbhgHIAEoCVIPcGFpcmVk'
    'VWlWZXJzaW9uEj4KDWNvbXBhdGliaWxpdHkYCCABKAsyGC5jbGllbnQudjAuQ29tcGF0aWJpbG'
    'l0eVINY29tcGF0aWJpbGl0eRJHCg5jbGFzc2lmaWNhdGlvbhgJIAEoDjIfLmNsaWVudC52MC5V'
    'cGRhdGVDbGFzc2lmaWNhdGlvblIOY2xhc3NpZmljYXRpb24SOAoHY2hhbm5lbBgKIAEoDjIeLm'
    'NsaWVudC52MC5EaXN0cmlidXRpb25DaGFubmVsUgdjaGFubmVsEkMKD21hbmRhdG9yeV9hZnRl'
    'chgLIAEoCzIaLmdvb2dsZS5wcm90b2J1Zi5UaW1lc3RhbXBSDm1hbmRhdG9yeUFmdGVyEh0KCm'
    'FjdGlvbl91cmwYDCABKAlSCWFjdGlvblVybBIqChFyZWxlYXNlX25vdGVzX3VybBgNIAEoCVIP'
    'cmVsZWFzZU5vdGVzVXJs');

@$core.Deprecated('Use updateInfoDescriptor instead')
const UpdateInfo$json = {
  '1': 'UpdateInfo',
  '2': [
    {
      '1': 'metadata',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.SnapshotMetadata',
      '10': 'metadata'
    },
    {
      '1': 'installed_runtime',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.client.v0.BuildIdentity',
      '10': 'installedRuntime'
    },
    {
      '1': 'reported_ui',
      '3': 3,
      '4': 1,
      '5': 11,
      '6': '.client.v0.BuildIdentity',
      '10': 'reportedUi'
    },
    {
      '1': 'installed_pair',
      '3': 4,
      '4': 1,
      '5': 11,
      '6': '.client.v0.Compatibility',
      '10': 'installedPair'
    },
    {
      '1': 'state',
      '3': 5,
      '4': 1,
      '5': 14,
      '6': '.client.v0.UpdateState',
      '10': 'state'
    },
    {
      '1': 'available',
      '3': 6,
      '4': 1,
      '5': 11,
      '6': '.client.v0.VerifiedUpdate',
      '10': 'available'
    },
    {
      '1': 'discovery',
      '3': 7,
      '4': 1,
      '5': 11,
      '6': '.client.v0.Restriction',
      '10': 'discovery'
    },
  ],
};

/// Descriptor for `UpdateInfo`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List updateInfoDescriptor = $convert.base64Decode(
    'CgpVcGRhdGVJbmZvEjcKCG1ldGFkYXRhGAEgASgLMhsuY2xpZW50LnYwLlNuYXBzaG90TWV0YW'
    'RhdGFSCG1ldGFkYXRhEkUKEWluc3RhbGxlZF9ydW50aW1lGAIgASgLMhguY2xpZW50LnYwLkJ1'
    'aWxkSWRlbnRpdHlSEGluc3RhbGxlZFJ1bnRpbWUSOQoLcmVwb3J0ZWRfdWkYAyABKAsyGC5jbG'
    'llbnQudjAuQnVpbGRJZGVudGl0eVIKcmVwb3J0ZWRVaRI/Cg5pbnN0YWxsZWRfcGFpchgEIAEo'
    'CzIYLmNsaWVudC52MC5Db21wYXRpYmlsaXR5Ug1pbnN0YWxsZWRQYWlyEiwKBXN0YXRlGAUgAS'
    'gOMhYuY2xpZW50LnYwLlVwZGF0ZVN0YXRlUgVzdGF0ZRI3CglhdmFpbGFibGUYBiABKAsyGS5j'
    'bGllbnQudjAuVmVyaWZpZWRVcGRhdGVSCWF2YWlsYWJsZRI0CglkaXNjb3ZlcnkYByABKAsyFi'
    '5jbGllbnQudjAuUmVzdHJpY3Rpb25SCWRpc2NvdmVyeQ==');

@$core.Deprecated('Use supportInfoDescriptor instead')
const SupportInfo$json = {
  '1': 'SupportInfo',
  '2': [
    {
      '1': 'runtime',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.BuildIdentity',
      '10': 'runtime'
    },
    {'1': 'product_name', '3': 2, '4': 1, '5': 9, '10': 'productName'},
    {
      '1': 'documentation_url',
      '3': 3,
      '4': 1,
      '5': 9,
      '10': 'documentationUrl'
    },
    {'1': 'support_url', '3': 4, '4': 1, '5': 9, '10': 'supportUrl'},
    {'1': 'privacy_url', '3': 5, '4': 1, '5': 9, '10': 'privacyUrl'},
    {'1': 'license_url', '3': 6, '4': 1, '5': 9, '10': 'licenseUrl'},
    {'1': 'offline_help_key', '3': 7, '4': 1, '5': 9, '10': 'offlineHelpKey'},
  ],
};

/// Descriptor for `SupportInfo`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List supportInfoDescriptor = $convert.base64Decode(
    'CgtTdXBwb3J0SW5mbxIyCgdydW50aW1lGAEgASgLMhguY2xpZW50LnYwLkJ1aWxkSWRlbnRpdH'
    'lSB3J1bnRpbWUSIQoMcHJvZHVjdF9uYW1lGAIgASgJUgtwcm9kdWN0TmFtZRIrChFkb2N1bWVu'
    'dGF0aW9uX3VybBgDIAEoCVIQZG9jdW1lbnRhdGlvblVybBIfCgtzdXBwb3J0X3VybBgEIAEoCV'
    'IKc3VwcG9ydFVybBIfCgtwcml2YWN5X3VybBgFIAEoCVIKcHJpdmFjeVVybBIfCgtsaWNlbnNl'
    'X3VybBgGIAEoCVIKbGljZW5zZVVybBIoChBvZmZsaW5lX2hlbHBfa2V5GAcgASgJUg5vZmZsaW'
    '5lSGVscEtleQ==');
