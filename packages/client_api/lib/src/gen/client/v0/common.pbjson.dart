// This is a generated file - do not edit.
//
// Generated from client/v0/common.proto.

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

@$core.Deprecated('Use accessDescriptor instead')
const Access$json = {
  '1': 'Access',
  '2': [
    {'1': 'ACCESS_UNSPECIFIED', '2': 0},
    {'1': 'ACCESS_OBSERVER', '2': 1},
    {'1': 'ACCESS_OWNER', '2': 2},
    {'1': 'ACCESS_ADMINISTRATOR', '2': 3},
  ],
};

/// Descriptor for `Access`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List accessDescriptor = $convert.base64Decode(
    'CgZBY2Nlc3MSFgoSQUNDRVNTX1VOU1BFQ0lGSUVEEAASEwoPQUNDRVNTX09CU0VSVkVSEAESEA'
    'oMQUNDRVNTX09XTkVSEAISGAoUQUNDRVNTX0FETUlOSVNUUkFUT1IQAw==');

@$core.Deprecated('Use platformDescriptor instead')
const Platform$json = {
  '1': 'Platform',
  '2': [
    {'1': 'PLATFORM_UNSPECIFIED', '2': 0},
    {'1': 'PLATFORM_WINDOWS', '2': 1},
    {'1': 'PLATFORM_MACOS', '2': 2},
    {'1': 'PLATFORM_LINUX', '2': 3},
    {'1': 'PLATFORM_ANDROID', '2': 4},
    {'1': 'PLATFORM_IOS', '2': 5},
  ],
};

/// Descriptor for `Platform`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List platformDescriptor = $convert.base64Decode(
    'CghQbGF0Zm9ybRIYChRQTEFURk9STV9VTlNQRUNJRklFRBAAEhQKEFBMQVRGT1JNX1dJTkRPV1'
    'MQARISCg5QTEFURk9STV9NQUNPUxACEhIKDlBMQVRGT1JNX0xJTlVYEAMSFAoQUExBVEZPUk1f'
    'QU5EUk9JRBAEEhAKDFBMQVRGT1JNX0lPUxAF');

@$core.Deprecated('Use capabilityDescriptor instead')
const Capability$json = {
  '1': 'Capability',
  '2': [
    {'1': 'CAPABILITY_UNSPECIFIED', '2': 0},
    {'1': 'CAPABILITY_ENROLLMENT', '2': 1},
    {'1': 'CAPABILITY_CONNECTION', '2': 2},
    {'1': 'CAPABILITY_PEERS', '2': 3},
    {'1': 'CAPABILITY_NETWORK_SELECTION', '2': 4},
    {'1': 'CAPABILITY_EXIT_NODE', '2': 5},
    {'1': 'CAPABILITY_IDENTITY_RECOVERY', '2': 6},
    {'1': 'CAPABILITY_DIAGNOSTICS', '2': 7},
    {'1': 'CAPABILITY_LOGOUT', '2': 8},
    {'1': 'CAPABILITY_LOCAL_FORGET', '2': 9},
    {'1': 'CAPABILITY_PROFILES', '2': 10},
    {'1': 'CAPABILITY_SESSION_RENEWAL', '2': 11},
    {'1': 'CAPABILITY_PREFERENCES', '2': 12},
    {'1': 'CAPABILITY_RESOURCES', '2': 13},
    {'1': 'CAPABILITY_MANAGED_SETTINGS', '2': 14},
    {'1': 'CAPABILITY_RUNTIME_LIFECYCLE', '2': 15},
    {'1': 'CAPABILITY_UPDATE_DISCOVERY', '2': 16},
    {'1': 'CAPABILITY_SUPPORT_INFO', '2': 17},
  ],
};

/// Descriptor for `Capability`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List capabilityDescriptor = $convert.base64Decode(
    'CgpDYXBhYmlsaXR5EhoKFkNBUEFCSUxJVFlfVU5TUEVDSUZJRUQQABIZChVDQVBBQklMSVRZX0'
    'VOUk9MTE1FTlQQARIZChVDQVBBQklMSVRZX0NPTk5FQ1RJT04QAhIUChBDQVBBQklMSVRZX1BF'
    'RVJTEAMSIAocQ0FQQUJJTElUWV9ORVRXT1JLX1NFTEVDVElPThAEEhgKFENBUEFCSUxJVFlfRV'
    'hJVF9OT0RFEAUSIAocQ0FQQUJJTElUWV9JREVOVElUWV9SRUNPVkVSWRAGEhoKFkNBUEFCSUxJ'
    'VFlfRElBR05PU1RJQ1MQBxIVChFDQVBBQklMSVRZX0xPR09VVBAIEhsKF0NBUEFCSUxJVFlfTE'
    '9DQUxfRk9SR0VUEAkSFwoTQ0FQQUJJTElUWV9QUk9GSUxFUxAKEh4KGkNBUEFCSUxJVFlfU0VT'
    'U0lPTl9SRU5FV0FMEAsSGgoWQ0FQQUJJTElUWV9QUkVGRVJFTkNFUxAMEhgKFENBUEFCSUxJVF'
    'lfUkVTT1VSQ0VTEA0SHwobQ0FQQUJJTElUWV9NQU5BR0VEX1NFVFRJTkdTEA4SIAocQ0FQQUJJ'
    'TElUWV9SVU5USU1FX0xJRkVDWUNMRRAPEh8KG0NBUEFCSUxJVFlfVVBEQVRFX0RJU0NPVkVSWR'
    'AQEhsKF0NBUEFCSUxJVFlfU1VQUE9SVF9JTkZPEBE=');

@$core.Deprecated('Use availabilityDescriptor instead')
const Availability$json = {
  '1': 'Availability',
  '2': [
    {'1': 'AVAILABILITY_UNSPECIFIED', '2': 0},
    {'1': 'AVAILABILITY_AVAILABLE', '2': 1},
    {'1': 'AVAILABILITY_UNSUPPORTED', '2': 2},
    {'1': 'AVAILABILITY_POLICY_BLOCKED', '2': 3},
    {'1': 'AVAILABILITY_PERMISSION_REQUIRED', '2': 4},
    {'1': 'AVAILABILITY_TEMPORARILY_UNAVAILABLE', '2': 5},
  ],
};

/// Descriptor for `Availability`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List availabilityDescriptor = $convert.base64Decode(
    'CgxBdmFpbGFiaWxpdHkSHAoYQVZBSUxBQklMSVRZX1VOU1BFQ0lGSUVEEAASGgoWQVZBSUxBQk'
    'lMSVRZX0FWQUlMQUJMRRABEhwKGEFWQUlMQUJJTElUWV9VTlNVUFBPUlRFRBACEh8KG0FWQUlM'
    'QUJJTElUWV9QT0xJQ1lfQkxPQ0tFRBADEiQKIEFWQUlMQUJJTElUWV9QRVJNSVNTSU9OX1JFUV'
    'VJUkVEEAQSKAokQVZBSUxBQklMSVRZX1RFTVBPUkFSSUxZX1VOQVZBSUxBQkxFEAU=');

@$core.Deprecated('Use actionOwnerDescriptor instead')
const ActionOwner$json = {
  '1': 'ActionOwner',
  '2': [
    {'1': 'ACTION_OWNER_UNSPECIFIED', '2': 0},
    {'1': 'ACTION_OWNER_USER', '2': 1},
    {'1': 'ACTION_OWNER_DEVICE_ADMINISTRATOR', '2': 2},
    {'1': 'ACTION_OWNER_ACCESS_ADMINISTRATOR', '2': 3},
    {'1': 'ACTION_OWNER_SUPPORT', '2': 4},
  ],
};

/// Descriptor for `ActionOwner`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List actionOwnerDescriptor = $convert.base64Decode(
    'CgtBY3Rpb25Pd25lchIcChhBQ1RJT05fT1dORVJfVU5TUEVDSUZJRUQQABIVChFBQ1RJT05fT1'
    'dORVJfVVNFUhABEiUKIUFDVElPTl9PV05FUl9ERVZJQ0VfQURNSU5JU1RSQVRPUhACEiUKIUFD'
    'VElPTl9PV05FUl9BQ0NFU1NfQURNSU5JU1RSQVRPUhADEhgKFEFDVElPTl9PV05FUl9TVVBQT1'
    'JUEAQ=');

@$core.Deprecated('Use errorCodeDescriptor instead')
const ErrorCode$json = {
  '1': 'ErrorCode',
  '2': [
    {'1': 'ERROR_CODE_UNSPECIFIED', '2': 0},
    {'1': 'ERROR_CODE_INVALID_ARGUMENT', '2': 1},
    {'1': 'ERROR_CODE_UNAUTHENTICATED', '2': 2},
    {'1': 'ERROR_CODE_OWNER_REQUIRED', '2': 3},
    {'1': 'ERROR_CODE_ADMINISTRATOR_REQUIRED', '2': 4},
    {'1': 'ERROR_CODE_UNSUPPORTED', '2': 5},
    {'1': 'ERROR_CODE_NOT_FOUND', '2': 6},
    {'1': 'ERROR_CODE_STALE_STATE', '2': 7},
    {'1': 'ERROR_CODE_POLICY_BLOCKED', '2': 8},
    {'1': 'ERROR_CODE_PERMISSION_REQUIRED', '2': 9},
    {'1': 'ERROR_CODE_BUSY', '2': 10},
    {'1': 'ERROR_CODE_LIMIT_EXCEEDED', '2': 11},
    {'1': 'ERROR_CODE_UNAVAILABLE', '2': 12},
    {'1': 'ERROR_CODE_DEADLINE_EXCEEDED', '2': 13},
    {'1': 'ERROR_CODE_CANCELLED', '2': 14},
    {'1': 'ERROR_CODE_INTERNAL', '2': 15},
    {'1': 'ERROR_CODE_NEEDS_ENROLLMENT', '2': 16},
    {'1': 'ERROR_CODE_NEEDS_LOGIN', '2': 17},
    {'1': 'ERROR_CODE_APPROVAL_REQUIRED', '2': 18},
    {'1': 'ERROR_CODE_APPROVAL_REJECTED', '2': 19},
    {'1': 'ERROR_CODE_SERVER_IDENTITY_CHANGED', '2': 20},
    {'1': 'ERROR_CODE_IDENTITY_CONFIRMATION_MISMATCH', '2': 21},
    {'1': 'ERROR_CODE_REMOTE_CLEANUP_REQUIRED', '2': 22},
    {'1': 'ERROR_CODE_LOCAL_FORGET_CONFIRMATION_REQUIRED', '2': 23},
    {'1': 'ERROR_CODE_APPLY_FAILED', '2': 24},
    {'1': 'ERROR_CODE_PROFILE_ACTIVE', '2': 25},
    {'1': 'ERROR_CODE_RESOURCE_CONFLICT', '2': 26},
    {'1': 'ERROR_CODE_CONTRACT_MISMATCH', '2': 27},
  ],
};

/// Descriptor for `ErrorCode`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List errorCodeDescriptor = $convert.base64Decode(
    'CglFcnJvckNvZGUSGgoWRVJST1JfQ09ERV9VTlNQRUNJRklFRBAAEh8KG0VSUk9SX0NPREVfSU'
    '5WQUxJRF9BUkdVTUVOVBABEh4KGkVSUk9SX0NPREVfVU5BVVRIRU5USUNBVEVEEAISHQoZRVJS'
    'T1JfQ09ERV9PV05FUl9SRVFVSVJFRBADEiUKIUVSUk9SX0NPREVfQURNSU5JU1RSQVRPUl9SRV'
    'FVSVJFRBAEEhoKFkVSUk9SX0NPREVfVU5TVVBQT1JURUQQBRIYChRFUlJPUl9DT0RFX05PVF9G'
    'T1VORBAGEhoKFkVSUk9SX0NPREVfU1RBTEVfU1RBVEUQBxIdChlFUlJPUl9DT0RFX1BPTElDWV'
    '9CTE9DS0VEEAgSIgoeRVJST1JfQ09ERV9QRVJNSVNTSU9OX1JFUVVJUkVEEAkSEwoPRVJST1Jf'
    'Q09ERV9CVVNZEAoSHQoZRVJST1JfQ09ERV9MSU1JVF9FWENFRURFRBALEhoKFkVSUk9SX0NPRE'
    'VfVU5BVkFJTEFCTEUQDBIgChxFUlJPUl9DT0RFX0RFQURMSU5FX0VYQ0VFREVEEA0SGAoURVJS'
    'T1JfQ09ERV9DQU5DRUxMRUQQDhIXChNFUlJPUl9DT0RFX0lOVEVSTkFMEA8SHwobRVJST1JfQ0'
    '9ERV9ORUVEU19FTlJPTExNRU5UEBASGgoWRVJST1JfQ09ERV9ORUVEU19MT0dJThAREiAKHEVS'
    'Uk9SX0NPREVfQVBQUk9WQUxfUkVRVUlSRUQQEhIgChxFUlJPUl9DT0RFX0FQUFJPVkFMX1JFSk'
    'VDVEVEEBMSJgoiRVJST1JfQ09ERV9TRVJWRVJfSURFTlRJVFlfQ0hBTkdFRBAUEi0KKUVSUk9S'
    'X0NPREVfSURFTlRJVFlfQ09ORklSTUFUSU9OX01JU01BVENIEBUSJgoiRVJST1JfQ09ERV9SRU'
    '1PVEVfQ0xFQU5VUF9SRVFVSVJFRBAWEjEKLUVSUk9SX0NPREVfTE9DQUxfRk9SR0VUX0NPTkZJ'
    'Uk1BVElPTl9SRVFVSVJFRBAXEhsKF0VSUk9SX0NPREVfQVBQTFlfRkFJTEVEEBgSHQoZRVJST1'
    'JfQ09ERV9QUk9GSUxFX0FDVElWRRAZEiAKHEVSUk9SX0NPREVfUkVTT1VSQ0VfQ09ORkxJQ1QQ'
    'GhIgChxFUlJPUl9DT0RFX0NPTlRSQUNUX01JU01BVENIEBs=');

@$core.Deprecated('Use operationStateDescriptor instead')
const OperationState$json = {
  '1': 'OperationState',
  '2': [
    {'1': 'OPERATION_STATE_UNSPECIFIED', '2': 0},
    {'1': 'OPERATION_STATE_PENDING', '2': 1},
    {'1': 'OPERATION_STATE_RUNNING', '2': 2},
    {'1': 'OPERATION_STATE_WAITING_FOR_USER', '2': 3},
    {'1': 'OPERATION_STATE_SUCCEEDED', '2': 4},
    {'1': 'OPERATION_STATE_FAILED', '2': 5},
    {'1': 'OPERATION_STATE_CANCELLED', '2': 6},
  ],
};

/// Descriptor for `OperationState`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List operationStateDescriptor = $convert.base64Decode(
    'Cg5PcGVyYXRpb25TdGF0ZRIfChtPUEVSQVRJT05fU1RBVEVfVU5TUEVDSUZJRUQQABIbChdPUE'
    'VSQVRJT05fU1RBVEVfUEVORElORxABEhsKF09QRVJBVElPTl9TVEFURV9SVU5OSU5HEAISJAog'
    'T1BFUkFUSU9OX1NUQVRFX1dBSVRJTkdfRk9SX1VTRVIQAxIdChlPUEVSQVRJT05fU1RBVEVfU1'
    'VDQ0VFREVEEAQSGgoWT1BFUkFUSU9OX1NUQVRFX0ZBSUxFRBAFEh0KGU9QRVJBVElPTl9TVEFU'
    'RV9DQU5DRUxMRUQQBg==');

@$core.Deprecated('Use connectionContinuityDescriptor instead')
const ConnectionContinuity$json = {
  '1': 'ConnectionContinuity',
  '2': [
    {'1': 'CONNECTION_CONTINUITY_UNSPECIFIED', '2': 0},
    {'1': 'CONNECTION_CONTINUITY_NOT_APPLICABLE', '2': 1},
    {'1': 'CONNECTION_CONTINUITY_PRESERVED', '2': 2},
    {'1': 'CONNECTION_CONTINUITY_INTERRUPTED', '2': 3},
    {'1': 'CONNECTION_CONTINUITY_UNKNOWN', '2': 4},
  ],
};

/// Descriptor for `ConnectionContinuity`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List connectionContinuityDescriptor = $convert.base64Decode(
    'ChRDb25uZWN0aW9uQ29udGludWl0eRIlCiFDT05ORUNUSU9OX0NPTlRJTlVJVFlfVU5TUEVDSU'
    'ZJRUQQABIoCiRDT05ORUNUSU9OX0NPTlRJTlVJVFlfTk9UX0FQUExJQ0FCTEUQARIjCh9DT05O'
    'RUNUSU9OX0NPTlRJTlVJVFlfUFJFU0VSVkVEEAISJQohQ09OTkVDVElPTl9DT05USU5VSVRZX0'
    'lOVEVSUlVQVEVEEAMSIQodQ09OTkVDVElPTl9DT05USU5VSVRZX1VOS05PV04QBA==');

@$core.Deprecated('Use cleanupOutcomeDescriptor instead')
const CleanupOutcome$json = {
  '1': 'CleanupOutcome',
  '2': [
    {'1': 'CLEANUP_OUTCOME_UNSPECIFIED', '2': 0},
    {'1': 'CLEANUP_OUTCOME_REMOTE_CONFIRMED', '2': 1},
    {'1': 'CLEANUP_OUTCOME_REMOTE_UNCONFIRMED', '2': 2},
    {'1': 'CLEANUP_OUTCOME_NOT_REGISTERED', '2': 3},
  ],
};

/// Descriptor for `CleanupOutcome`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List cleanupOutcomeDescriptor = $convert.base64Decode(
    'Cg5DbGVhbnVwT3V0Y29tZRIfChtDTEVBTlVQX09VVENPTUVfVU5TUEVDSUZJRUQQABIkCiBDTE'
    'VBTlVQX09VVENPTUVfUkVNT1RFX0NPTkZJUk1FRBABEiYKIkNMRUFOVVBfT1VUQ09NRV9SRU1P'
    'VEVfVU5DT05GSVJNRUQQAhIiCh5DTEVBTlVQX09VVENPTUVfTk9UX1JFR0lTVEVSRUQQAw==');

@$core.Deprecated('Use restrictionDescriptor instead')
const Restriction$json = {
  '1': 'Restriction',
  '2': [
    {
      '1': 'availability',
      '3': 1,
      '4': 1,
      '5': 14,
      '6': '.client.v0.Availability',
      '10': 'availability'
    },
    {'1': 'reason_key', '3': 2, '4': 1, '5': 9, '10': 'reasonKey'},
    {
      '1': 'action_owner',
      '3': 3,
      '4': 1,
      '5': 14,
      '6': '.client.v0.ActionOwner',
      '10': 'actionOwner'
    },
  ],
};

/// Descriptor for `Restriction`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List restrictionDescriptor = $convert.base64Decode(
    'CgtSZXN0cmljdGlvbhI7CgxhdmFpbGFiaWxpdHkYASABKA4yFy5jbGllbnQudjAuQXZhaWxhYm'
    'lsaXR5UgxhdmFpbGFiaWxpdHkSHQoKcmVhc29uX2tleRgCIAEoCVIJcmVhc29uS2V5EjkKDGFj'
    'dGlvbl9vd25lchgDIAEoDjIWLmNsaWVudC52MC5BY3Rpb25Pd25lclILYWN0aW9uT3duZXI=');

@$core.Deprecated('Use capabilityStatusDescriptor instead')
const CapabilityStatus$json = {
  '1': 'CapabilityStatus',
  '2': [
    {
      '1': 'capability',
      '3': 1,
      '4': 1,
      '5': 14,
      '6': '.client.v0.Capability',
      '10': 'capability'
    },
    {
      '1': 'restriction',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.client.v0.Restriction',
      '10': 'restriction'
    },
    {
      '1': 'platform',
      '3': 3,
      '4': 1,
      '5': 14,
      '6': '.client.v0.Platform',
      '10': 'platform'
    },
  ],
};

/// Descriptor for `CapabilityStatus`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List capabilityStatusDescriptor = $convert.base64Decode(
    'ChBDYXBhYmlsaXR5U3RhdHVzEjUKCmNhcGFiaWxpdHkYASABKA4yFS5jbGllbnQudjAuQ2FwYW'
    'JpbGl0eVIKY2FwYWJpbGl0eRI4CgtyZXN0cmljdGlvbhgCIAEoCzIWLmNsaWVudC52MC5SZXN0'
    'cmljdGlvblILcmVzdHJpY3Rpb24SLwoIcGxhdGZvcm0YAyABKA4yEy5jbGllbnQudjAuUGxhdG'
    'Zvcm1SCHBsYXRmb3Jt');

@$core.Deprecated('Use buildIdentityDescriptor instead')
const BuildIdentity$json = {
  '1': 'BuildIdentity',
  '2': [
    {'1': 'version', '3': 1, '4': 1, '5': 9, '10': 'version'},
    {'1': 'commit', '3': 2, '4': 1, '5': 9, '10': 'commit'},
    {'1': 'build_date', '3': 3, '4': 1, '5': 9, '10': 'buildDate'},
    {
      '1': 'platform',
      '3': 4,
      '4': 1,
      '5': 14,
      '6': '.client.v0.Platform',
      '10': 'platform'
    },
    {'1': 'architecture', '3': 5, '4': 1, '5': 9, '10': 'architecture'},
  ],
};

/// Descriptor for `BuildIdentity`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List buildIdentityDescriptor = $convert.base64Decode(
    'Cg1CdWlsZElkZW50aXR5EhgKB3ZlcnNpb24YASABKAlSB3ZlcnNpb24SFgoGY29tbWl0GAIgAS'
    'gJUgZjb21taXQSHQoKYnVpbGRfZGF0ZRgDIAEoCVIJYnVpbGREYXRlEi8KCHBsYXRmb3JtGAQg'
    'ASgOMhMuY2xpZW50LnYwLlBsYXRmb3JtUghwbGF0Zm9ybRIiCgxhcmNoaXRlY3R1cmUYBSABKA'
    'lSDGFyY2hpdGVjdHVyZQ==');

@$core.Deprecated('Use runtimeInfoDescriptor instead')
const RuntimeInfo$json = {
  '1': 'RuntimeInfo',
  '2': [
    {
      '1': 'build',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.BuildIdentity',
      '10': 'build'
    },
    {'1': 'instance_id', '3': 2, '4': 1, '5': 9, '10': 'instanceId'},
    {
      '1': 'capabilities',
      '3': 3,
      '4': 3,
      '5': 11,
      '6': '.client.v0.CapabilityStatus',
      '10': 'capabilities'
    },
    {
      '1': 'caller_access',
      '3': 4,
      '4': 1,
      '5': 14,
      '6': '.client.v0.Access',
      '10': 'callerAccess'
    },
    {'1': 'contract_sha256', '3': 5, '4': 1, '5': 9, '10': 'contractSha256'},
    {'1': 'protocol', '3': 6, '4': 1, '5': 9, '10': 'protocol'},
    {'1': 'ipc_version', '3': 7, '4': 1, '5': 13, '10': 'ipcVersion'},
  ],
};

/// Descriptor for `RuntimeInfo`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List runtimeInfoDescriptor = $convert.base64Decode(
    'CgtSdW50aW1lSW5mbxIuCgVidWlsZBgBIAEoCzIYLmNsaWVudC52MC5CdWlsZElkZW50aXR5Ug'
    'VidWlsZBIfCgtpbnN0YW5jZV9pZBgCIAEoCVIKaW5zdGFuY2VJZBI/CgxjYXBhYmlsaXRpZXMY'
    'AyADKAsyGy5jbGllbnQudjAuQ2FwYWJpbGl0eVN0YXR1c1IMY2FwYWJpbGl0aWVzEjYKDWNhbG'
    'xlcl9hY2Nlc3MYBCABKA4yES5jbGllbnQudjAuQWNjZXNzUgxjYWxsZXJBY2Nlc3MSJwoPY29u'
    'dHJhY3Rfc2hhMjU2GAUgASgJUg5jb250cmFjdFNoYTI1NhIaCghwcm90b2NvbBgGIAEoCVIIcH'
    'JvdG9jb2wSHwoLaXBjX3ZlcnNpb24YByABKA1SCmlwY1ZlcnNpb24=');

@$core.Deprecated('Use snapshotMetadataDescriptor instead')
const SnapshotMetadata$json = {
  '1': 'SnapshotMetadata',
  '2': [
    {'1': 'instance_id', '3': 1, '4': 1, '5': 9, '10': 'instanceId'},
    {'1': 'revision', '3': 2, '4': 1, '5': 4, '10': 'revision'},
    {
      '1': 'generated_at',
      '3': 3,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'generatedAt'
    },
  ],
};

/// Descriptor for `SnapshotMetadata`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List snapshotMetadataDescriptor = $convert.base64Decode(
    'ChBTbmFwc2hvdE1ldGFkYXRhEh8KC2luc3RhbmNlX2lkGAEgASgJUgppbnN0YW5jZUlkEhoKCH'
    'JldmlzaW9uGAIgASgEUghyZXZpc2lvbhI9CgxnZW5lcmF0ZWRfYXQYAyABKAsyGi5nb29nbGUu'
    'cHJvdG9idWYuVGltZXN0YW1wUgtnZW5lcmF0ZWRBdA==');

@$core.Deprecated('Use profileRefDescriptor instead')
const ProfileRef$json = {
  '1': 'ProfileRef',
  '2': [
    {'1': 'profile_id', '3': 1, '4': 1, '5': 9, '10': 'profileId'},
  ],
};

/// Descriptor for `ProfileRef`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List profileRefDescriptor = $convert.base64Decode(
    'CgpQcm9maWxlUmVmEh0KCnByb2ZpbGVfaWQYASABKAlSCXByb2ZpbGVJZA==');

@$core.Deprecated('Use mutationContextDescriptor instead')
const MutationContext$json = {
  '1': 'MutationContext',
  '2': [
    {'1': 'request_id', '3': 1, '4': 1, '5': 9, '10': 'requestId'},
    {
      '1': 'expected_instance_id',
      '3': 2,
      '4': 1,
      '5': 9,
      '10': 'expectedInstanceId'
    },
    {
      '1': 'expected_revision',
      '3': 3,
      '4': 1,
      '5': 4,
      '10': 'expectedRevision'
    },
  ],
};

/// Descriptor for `MutationContext`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List mutationContextDescriptor = $convert.base64Decode(
    'Cg9NdXRhdGlvbkNvbnRleHQSHQoKcmVxdWVzdF9pZBgBIAEoCVIJcmVxdWVzdElkEjAKFGV4cG'
    'VjdGVkX2luc3RhbmNlX2lkGAIgASgJUhJleHBlY3RlZEluc3RhbmNlSWQSKwoRZXhwZWN0ZWRf'
    'cmV2aXNpb24YAyABKARSEGV4cGVjdGVkUmV2aXNpb24=');

@$core.Deprecated('Use failureDescriptor instead')
const Failure$json = {
  '1': 'Failure',
  '2': [
    {
      '1': 'code',
      '3': 1,
      '4': 1,
      '5': 14,
      '6': '.client.v0.ErrorCode',
      '10': 'code'
    },
    {'1': 'reason_key', '3': 2, '4': 1, '5': 9, '10': 'reasonKey'},
    {
      '1': 'control_request_id',
      '3': 3,
      '4': 1,
      '5': 9,
      '10': 'controlRequestId'
    },
    {'1': 'retryable', '3': 4, '4': 1, '5': 8, '10': 'retryable'},
    {
      '1': 'retry_after',
      '3': 5,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Duration',
      '10': 'retryAfter'
    },
    {
      '1': 'action_owner',
      '3': 6,
      '4': 1,
      '5': 14,
      '6': '.client.v0.ActionOwner',
      '10': 'actionOwner'
    },
    {'1': 'field_paths', '3': 7, '4': 3, '5': 9, '10': 'fieldPaths'},
  ],
};

/// Descriptor for `Failure`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List failureDescriptor = $convert.base64Decode(
    'CgdGYWlsdXJlEigKBGNvZGUYASABKA4yFC5jbGllbnQudjAuRXJyb3JDb2RlUgRjb2RlEh0KCn'
    'JlYXNvbl9rZXkYAiABKAlSCXJlYXNvbktleRIsChJjb250cm9sX3JlcXVlc3RfaWQYAyABKAlS'
    'EGNvbnRyb2xSZXF1ZXN0SWQSHAoJcmV0cnlhYmxlGAQgASgIUglyZXRyeWFibGUSOgoLcmV0cn'
    'lfYWZ0ZXIYBSABKAsyGS5nb29nbGUucHJvdG9idWYuRHVyYXRpb25SCnJldHJ5QWZ0ZXISOQoM'
    'YWN0aW9uX293bmVyGAYgASgOMhYuY2xpZW50LnYwLkFjdGlvbk93bmVyUgthY3Rpb25Pd25lch'
    'IfCgtmaWVsZF9wYXRocxgHIAMoCVIKZmllbGRQYXRocw==');

@$core.Deprecated('Use userActionDescriptor instead')
const UserAction$json = {
  '1': 'UserAction',
  '2': [
    {
      '1': 'kind',
      '3': 1,
      '4': 1,
      '5': 14,
      '6': '.client.v0.UserAction.Kind',
      '10': 'kind'
    },
    {'1': 'browser_url', '3': 2, '4': 1, '5': 9, '10': 'browserUrl'},
    {
      '1': 'expires_at',
      '3': 3,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'expiresAt'
    },
    {'1': 'reason_key', '3': 4, '4': 1, '5': 9, '10': 'reasonKey'},
  ],
  '4': [UserAction_Kind$json],
};

@$core.Deprecated('Use userActionDescriptor instead')
const UserAction_Kind$json = {
  '1': 'Kind',
  '2': [
    {'1': 'KIND_UNSPECIFIED', '2': 0},
    {'1': 'KIND_OPEN_BROWSER', '2': 1},
    {'1': 'KIND_WAIT_FOR_APPROVAL', '2': 2},
    {'1': 'KIND_GRANT_VPN_PERMISSION', '2': 3},
    {'1': 'KIND_USE_PRIVILEGED_HELPER', '2': 4},
    {'1': 'KIND_CONTACT_ACCESS_ADMINISTRATOR', '2': 5},
  ],
};

/// Descriptor for `UserAction`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List userActionDescriptor = $convert.base64Decode(
    'CgpVc2VyQWN0aW9uEi4KBGtpbmQYASABKA4yGi5jbGllbnQudjAuVXNlckFjdGlvbi5LaW5kUg'
    'RraW5kEh8KC2Jyb3dzZXJfdXJsGAIgASgJUgpicm93c2VyVXJsEjkKCmV4cGlyZXNfYXQYAyAB'
    'KAsyGi5nb29nbGUucHJvdG9idWYuVGltZXN0YW1wUglleHBpcmVzQXQSHQoKcmVhc29uX2tleR'
    'gEIAEoCVIJcmVhc29uS2V5IrUBCgRLaW5kEhQKEEtJTkRfVU5TUEVDSUZJRUQQABIVChFLSU5E'
    'X09QRU5fQlJPV1NFUhABEhoKFktJTkRfV0FJVF9GT1JfQVBQUk9WQUwQAhIdChlLSU5EX0dSQU'
    '5UX1ZQTl9QRVJNSVNTSU9OEAMSHgoaS0lORF9VU0VfUFJJVklMRUdFRF9IRUxQRVIQBBIlCiFL'
    'SU5EX0NPTlRBQ1RfQUNDRVNTX0FETUlOSVNUUkFUT1IQBQ==');

@$core.Deprecated('Use changeResultDescriptor instead')
const ChangeResult$json = {
  '1': 'ChangeResult',
  '2': [
    {'1': 'changed', '3': 1, '4': 1, '5': 8, '10': 'changed'},
  ],
};

/// Descriptor for `ChangeResult`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List changeResultDescriptor = $convert
    .base64Decode('CgxDaGFuZ2VSZXN1bHQSGAoHY2hhbmdlZBgBIAEoCFIHY2hhbmdlZA==');

@$core.Deprecated('Use enrollmentResultDescriptor instead')
const EnrollmentResult$json = {
  '1': 'EnrollmentResult',
  '2': [
    {'1': 'profile_id', '3': 1, '4': 1, '5': 9, '10': 'profileId'},
    {'1': 'node_id', '3': 2, '4': 1, '5': 9, '10': 'nodeId'},
  ],
};

/// Descriptor for `EnrollmentResult`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List enrollmentResultDescriptor = $convert.base64Decode(
    'ChBFbnJvbGxtZW50UmVzdWx0Eh0KCnByb2ZpbGVfaWQYASABKAlSCXByb2ZpbGVJZBIXCgdub2'
    'RlX2lkGAIgASgJUgZub2RlSWQ=');

@$core.Deprecated('Use selectionResultDescriptor instead')
const SelectionResult$json = {
  '1': 'SelectionResult',
  '2': [
    {'1': 'selected_id', '3': 1, '4': 1, '5': 9, '10': 'selectedId'},
  ],
};

/// Descriptor for `SelectionResult`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List selectionResultDescriptor = $convert.base64Decode(
    'Cg9TZWxlY3Rpb25SZXN1bHQSHwoLc2VsZWN0ZWRfaWQYASABKAlSCnNlbGVjdGVkSWQ=');

@$core.Deprecated('Use cleanupResultDescriptor instead')
const CleanupResult$json = {
  '1': 'CleanupResult',
  '2': [
    {
      '1': 'outcome',
      '3': 1,
      '4': 1,
      '5': 14,
      '6': '.client.v0.CleanupOutcome',
      '10': 'outcome'
    },
    {
      '1': 'local_registration_removed',
      '3': 2,
      '4': 1,
      '5': 8,
      '10': 'localRegistrationRemoved'
    },
    {
      '1': 'control_request_id',
      '3': 3,
      '4': 1,
      '5': 9,
      '10': 'controlRequestId'
    },
  ],
};

/// Descriptor for `CleanupResult`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List cleanupResultDescriptor = $convert.base64Decode(
    'Cg1DbGVhbnVwUmVzdWx0EjMKB291dGNvbWUYASABKA4yGS5jbGllbnQudjAuQ2xlYW51cE91dG'
    'NvbWVSB291dGNvbWUSPAoabG9jYWxfcmVnaXN0cmF0aW9uX3JlbW92ZWQYAiABKAhSGGxvY2Fs'
    'UmVnaXN0cmF0aW9uUmVtb3ZlZBIsChJjb250cm9sX3JlcXVlc3RfaWQYAyABKAlSEGNvbnRyb2'
    'xSZXF1ZXN0SWQ=');

@$core.Deprecated('Use renewalResultDescriptor instead')
const RenewalResult$json = {
  '1': 'RenewalResult',
  '2': [
    {
      '1': 'expires_at',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'expiresAt'
    },
  ],
};

/// Descriptor for `RenewalResult`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List renewalResultDescriptor = $convert.base64Decode(
    'Cg1SZW5ld2FsUmVzdWx0EjkKCmV4cGlyZXNfYXQYASABKAsyGi5nb29nbGUucHJvdG9idWYuVG'
    'ltZXN0YW1wUglleHBpcmVzQXQ=');

@$core.Deprecated('Use bundleResultDescriptor instead')
const BundleResult$json = {
  '1': 'BundleResult',
  '2': [
    {'1': 'bundle_id', '3': 1, '4': 1, '5': 9, '10': 'bundleId'},
    {
      '1': 'created_at',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'createdAt'
    },
    {
      '1': 'expires_at',
      '3': 3,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'expiresAt'
    },
    {'1': 'size_bytes', '3': 4, '4': 1, '5': 4, '10': 'sizeBytes'},
    {'1': 'sha256', '3': 5, '4': 1, '5': 9, '10': 'sha256'},
    {'1': 'reused', '3': 6, '4': 1, '5': 8, '10': 'reused'},
  ],
};

/// Descriptor for `BundleResult`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List bundleResultDescriptor = $convert.base64Decode(
    'CgxCdW5kbGVSZXN1bHQSGwoJYnVuZGxlX2lkGAEgASgJUghidW5kbGVJZBI5CgpjcmVhdGVkX2'
    'F0GAIgASgLMhouZ29vZ2xlLnByb3RvYnVmLlRpbWVzdGFtcFIJY3JlYXRlZEF0EjkKCmV4cGly'
    'ZXNfYXQYAyABKAsyGi5nb29nbGUucHJvdG9idWYuVGltZXN0YW1wUglleHBpcmVzQXQSHQoKc2'
    'l6ZV9ieXRlcxgEIAEoBFIJc2l6ZUJ5dGVzEhYKBnNoYTI1NhgFIAEoCVIGc2hhMjU2EhYKBnJl'
    'dXNlZBgGIAEoCFIGcmV1c2Vk');

@$core.Deprecated('Use operationDescriptor instead')
const Operation$json = {
  '1': 'Operation',
  '2': [
    {'1': 'id', '3': 1, '4': 1, '5': 9, '10': 'id'},
    {'1': 'request_id', '3': 2, '4': 1, '5': 9, '10': 'requestId'},
    {'1': 'profile_id', '3': 3, '4': 1, '5': 9, '10': 'profileId'},
    {
      '1': 'state',
      '3': 4,
      '4': 1,
      '5': 14,
      '6': '.client.v0.OperationState',
      '10': 'state'
    },
    {
      '1': 'metadata',
      '3': 5,
      '4': 1,
      '5': 11,
      '6': '.client.v0.SnapshotMetadata',
      '10': 'metadata'
    },
    {
      '1': 'user_action',
      '3': 6,
      '4': 1,
      '5': 11,
      '6': '.client.v0.UserAction',
      '10': 'userAction'
    },
    {
      '1': 'continuity',
      '3': 7,
      '4': 1,
      '5': 14,
      '6': '.client.v0.ConnectionContinuity',
      '10': 'continuity'
    },
    {
      '1': 'failure',
      '3': 10,
      '4': 1,
      '5': 11,
      '6': '.client.v0.Failure',
      '9': 0,
      '10': 'failure'
    },
    {
      '1': 'change',
      '3': 11,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ChangeResult',
      '9': 0,
      '10': 'change'
    },
    {
      '1': 'enrollment',
      '3': 12,
      '4': 1,
      '5': 11,
      '6': '.client.v0.EnrollmentResult',
      '9': 0,
      '10': 'enrollment'
    },
    {
      '1': 'selection',
      '3': 13,
      '4': 1,
      '5': 11,
      '6': '.client.v0.SelectionResult',
      '9': 0,
      '10': 'selection'
    },
    {
      '1': 'cleanup',
      '3': 14,
      '4': 1,
      '5': 11,
      '6': '.client.v0.CleanupResult',
      '9': 0,
      '10': 'cleanup'
    },
    {
      '1': 'renewal',
      '3': 15,
      '4': 1,
      '5': 11,
      '6': '.client.v0.RenewalResult',
      '9': 0,
      '10': 'renewal'
    },
    {
      '1': 'bundle',
      '3': 16,
      '4': 1,
      '5': 11,
      '6': '.client.v0.BundleResult',
      '9': 0,
      '10': 'bundle'
    },
  ],
  '8': [
    {'1': 'outcome'},
  ],
};

/// Descriptor for `Operation`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List operationDescriptor = $convert.base64Decode(
    'CglPcGVyYXRpb24SDgoCaWQYASABKAlSAmlkEh0KCnJlcXVlc3RfaWQYAiABKAlSCXJlcXVlc3'
    'RJZBIdCgpwcm9maWxlX2lkGAMgASgJUglwcm9maWxlSWQSLwoFc3RhdGUYBCABKA4yGS5jbGll'
    'bnQudjAuT3BlcmF0aW9uU3RhdGVSBXN0YXRlEjcKCG1ldGFkYXRhGAUgASgLMhsuY2xpZW50Ln'
    'YwLlNuYXBzaG90TWV0YWRhdGFSCG1ldGFkYXRhEjYKC3VzZXJfYWN0aW9uGAYgASgLMhUuY2xp'
    'ZW50LnYwLlVzZXJBY3Rpb25SCnVzZXJBY3Rpb24SPwoKY29udGludWl0eRgHIAEoDjIfLmNsaW'
    'VudC52MC5Db25uZWN0aW9uQ29udGludWl0eVIKY29udGludWl0eRIuCgdmYWlsdXJlGAogASgL'
    'MhIuY2xpZW50LnYwLkZhaWx1cmVIAFIHZmFpbHVyZRIxCgZjaGFuZ2UYCyABKAsyFy5jbGllbn'
    'QudjAuQ2hhbmdlUmVzdWx0SABSBmNoYW5nZRI9CgplbnJvbGxtZW50GAwgASgLMhsuY2xpZW50'
    'LnYwLkVucm9sbG1lbnRSZXN1bHRIAFIKZW5yb2xsbWVudBI6CglzZWxlY3Rpb24YDSABKAsyGi'
    '5jbGllbnQudjAuU2VsZWN0aW9uUmVzdWx0SABSCXNlbGVjdGlvbhI0CgdjbGVhbnVwGA4gASgL'
    'MhguY2xpZW50LnYwLkNsZWFudXBSZXN1bHRIAFIHY2xlYW51cBI0CgdyZW5ld2FsGA8gASgLMh'
    'guY2xpZW50LnYwLlJlbmV3YWxSZXN1bHRIAFIHcmVuZXdhbBIxCgZidW5kbGUYECABKAsyFy5j'
    'bGllbnQudjAuQnVuZGxlUmVzdWx0SABSBmJ1bmRsZUIJCgdvdXRjb21l');

@$core.Deprecated('Use pageRequestDescriptor instead')
const PageRequest$json = {
  '1': 'PageRequest',
  '2': [
    {'1': 'page_size', '3': 1, '4': 1, '5': 13, '10': 'pageSize'},
    {'1': 'page_token', '3': 2, '4': 1, '5': 9, '10': 'pageToken'},
  ],
};

/// Descriptor for `PageRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List pageRequestDescriptor = $convert.base64Decode(
    'CgtQYWdlUmVxdWVzdBIbCglwYWdlX3NpemUYASABKA1SCHBhZ2VTaXplEh0KCnBhZ2VfdG9rZW'
    '4YAiABKAlSCXBhZ2VUb2tlbg==');

@$core.Deprecated('Use pageResponseDescriptor instead')
const PageResponse$json = {
  '1': 'PageResponse',
  '2': [
    {'1': 'next_page_token', '3': 1, '4': 1, '5': 9, '10': 'nextPageToken'},
    {
      '1': 'metadata',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.client.v0.SnapshotMetadata',
      '10': 'metadata'
    },
  ],
};

/// Descriptor for `PageResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List pageResponseDescriptor = $convert.base64Decode(
    'CgxQYWdlUmVzcG9uc2USJgoPbmV4dF9wYWdlX3Rva2VuGAEgASgJUg1uZXh0UGFnZVRva2VuEj'
    'cKCG1ldGFkYXRhGAIgASgLMhsuY2xpZW50LnYwLlNuYXBzaG90TWV0YWRhdGFSCG1ldGFkYXRh');
