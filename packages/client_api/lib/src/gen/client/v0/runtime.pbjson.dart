// This is a generated file - do not edit.
//
// Generated from client/v0/runtime.proto.

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

@$core.Deprecated('Use serviceStateDescriptor instead')
const ServiceState$json = {
  '1': 'ServiceState',
  '2': [
    {'1': 'SERVICE_STATE_UNSPECIFIED', '2': 0},
    {'1': 'SERVICE_STATE_CONNECTED', '2': 1},
    {'1': 'SERVICE_STATE_DISCONNECTED', '2': 2},
    {'1': 'SERVICE_STATE_DEGRADED', '2': 3},
    {'1': 'SERVICE_STATE_ERROR', '2': 4},
    {'1': 'SERVICE_STATE_NEEDS_ENROLLMENT', '2': 5},
    {'1': 'SERVICE_STATE_NEEDS_APPROVAL', '2': 6},
    {'1': 'SERVICE_STATE_SERVER_IDENTITY_CHANGED', '2': 7},
    {'1': 'SERVICE_STATE_RECOVERING', '2': 8},
    {'1': 'SERVICE_STATE_RECOVERY_BLOCKED', '2': 9},
    {'1': 'SERVICE_STATE_POLICY_BLOCKED', '2': 10},
    {'1': 'SERVICE_STATE_NEEDS_LOGIN', '2': 11},
  ],
};

/// Descriptor for `ServiceState`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List serviceStateDescriptor = $convert.base64Decode(
    'CgxTZXJ2aWNlU3RhdGUSHQoZU0VSVklDRV9TVEFURV9VTlNQRUNJRklFRBAAEhsKF1NFUlZJQ0'
    'VfU1RBVEVfQ09OTkVDVEVEEAESHgoaU0VSVklDRV9TVEFURV9ESVNDT05ORUNURUQQAhIaChZT'
    'RVJWSUNFX1NUQVRFX0RFR1JBREVEEAMSFwoTU0VSVklDRV9TVEFURV9FUlJPUhAEEiIKHlNFUl'
    'ZJQ0VfU1RBVEVfTkVFRFNfRU5ST0xMTUVOVBAFEiAKHFNFUlZJQ0VfU1RBVEVfTkVFRFNfQVBQ'
    'Uk9WQUwQBhIpCiVTRVJWSUNFX1NUQVRFX1NFUlZFUl9JREVOVElUWV9DSEFOR0VEEAcSHAoYU0'
    'VSVklDRV9TVEFURV9SRUNPVkVSSU5HEAgSIgoeU0VSVklDRV9TVEFURV9SRUNPVkVSWV9CTE9D'
    'S0VEEAkSIAocU0VSVklDRV9TVEFURV9QT0xJQ1lfQkxPQ0tFRBAKEh0KGVNFUlZJQ0VfU1RBVE'
    'VfTkVFRFNfTE9HSU4QCw==');

@$core.Deprecated('Use controlStateDescriptor instead')
const ControlState$json = {
  '1': 'ControlState',
  '2': [
    {'1': 'CONTROL_STATE_UNSPECIFIED', '2': 0},
    {'1': 'CONTROL_STATE_PENDING_APPROVAL', '2': 1},
    {'1': 'CONTROL_STATE_DEGRADED', '2': 2},
    {'1': 'CONTROL_STATE_OFFLINE_CACHE', '2': 3},
    {'1': 'CONTROL_STATE_READY', '2': 4},
    {'1': 'CONTROL_STATE_REGISTERED', '2': 5},
    {'1': 'CONTROL_STATE_CACHE_INVALID', '2': 6},
    {'1': 'CONTROL_STATE_ERROR', '2': 7},
    {'1': 'CONTROL_STATE_NOT_REGISTERED', '2': 8},
    {'1': 'CONTROL_STATE_DISCONNECTED', '2': 9},
    {'1': 'CONTROL_STATE_SERVER_IDENTITY_CHANGED', '2': 10},
    {'1': 'CONTROL_STATE_RECOVERING', '2': 11},
    {'1': 'CONTROL_STATE_RECOVERY_BLOCKED', '2': 12},
    {'1': 'CONTROL_STATE_POLICY_BLOCKED', '2': 13},
    {'1': 'CONTROL_STATE_NEEDS_LOGIN', '2': 14},
  ],
};

/// Descriptor for `ControlState`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List controlStateDescriptor = $convert.base64Decode(
    'CgxDb250cm9sU3RhdGUSHQoZQ09OVFJPTF9TVEFURV9VTlNQRUNJRklFRBAAEiIKHkNPTlRST0'
    'xfU1RBVEVfUEVORElOR19BUFBST1ZBTBABEhoKFkNPTlRST0xfU1RBVEVfREVHUkFERUQQAhIf'
    'ChtDT05UUk9MX1NUQVRFX09GRkxJTkVfQ0FDSEUQAxIXChNDT05UUk9MX1NUQVRFX1JFQURZEA'
    'QSHAoYQ09OVFJPTF9TVEFURV9SRUdJU1RFUkVEEAUSHwobQ09OVFJPTF9TVEFURV9DQUNIRV9J'
    'TlZBTElEEAYSFwoTQ09OVFJPTF9TVEFURV9FUlJPUhAHEiAKHENPTlRST0xfU1RBVEVfTk9UX1'
    'JFR0lTVEVSRUQQCBIeChpDT05UUk9MX1NUQVRFX0RJU0NPTk5FQ1RFRBAJEikKJUNPTlRST0xf'
    'U1RBVEVfU0VSVkVSX0lERU5USVRZX0NIQU5HRUQQChIcChhDT05UUk9MX1NUQVRFX1JFQ09WRV'
    'JJTkcQCxIiCh5DT05UUk9MX1NUQVRFX1JFQ09WRVJZX0JMT0NLRUQQDBIgChxDT05UUk9MX1NU'
    'QVRFX1BPTElDWV9CTE9DS0VEEA0SHQoZQ09OVFJPTF9TVEFURV9ORUVEU19MT0dJThAO');

@$core.Deprecated('Use desiredStateDescriptor instead')
const DesiredState$json = {
  '1': 'DesiredState',
  '2': [
    {'1': 'DESIRED_STATE_UNSPECIFIED', '2': 0},
    {'1': 'DESIRED_STATE_CONNECTED', '2': 1},
    {'1': 'DESIRED_STATE_DISCONNECTED', '2': 2},
  ],
};

/// Descriptor for `DesiredState`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List desiredStateDescriptor = $convert.base64Decode(
    'CgxEZXNpcmVkU3RhdGUSHQoZREVTSVJFRF9TVEFURV9VTlNQRUNJRklFRBAAEhsKF0RFU0lSRU'
    'RfU1RBVEVfQ09OTkVDVEVEEAESHgoaREVTSVJFRF9TVEFURV9ESVNDT05ORUNURUQQAg==');

@$core.Deprecated('Use agentSnapshotStateDescriptor instead')
const AgentSnapshotState$json = {
  '1': 'AgentSnapshotState',
  '2': [
    {'1': 'AGENT_SNAPSHOT_STATE_UNSPECIFIED', '2': 0},
    {'1': 'AGENT_SNAPSHOT_STATE_ABSENT', '2': 1},
    {'1': 'AGENT_SNAPSHOT_STATE_CURRENT', '2': 2},
    {'1': 'AGENT_SNAPSHOT_STATE_PREVIOUS', '2': 3},
  ],
};

/// Descriptor for `AgentSnapshotState`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List agentSnapshotStateDescriptor = $convert.base64Decode(
    'ChJBZ2VudFNuYXBzaG90U3RhdGUSJAogQUdFTlRfU05BUFNIT1RfU1RBVEVfVU5TUEVDSUZJRU'
    'QQABIfChtBR0VOVF9TTkFQU0hPVF9TVEFURV9BQlNFTlQQARIgChxBR0VOVF9TTkFQU0hPVF9T'
    'VEFURV9DVVJSRU5UEAISIQodQUdFTlRfU05BUFNIT1RfU1RBVEVfUFJFVklPVVMQAw==');

@$core.Deprecated('Use pathKindDescriptor instead')
const PathKind$json = {
  '1': 'PathKind',
  '2': [
    {'1': 'PATH_KIND_UNSPECIFIED', '2': 0},
    {'1': 'PATH_KIND_DIRECT', '2': 1},
    {'1': 'PATH_KIND_RELAY', '2': 2},
  ],
};

/// Descriptor for `PathKind`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List pathKindDescriptor = $convert.base64Decode(
    'CghQYXRoS2luZBIZChVQQVRIX0tJTkRfVU5TUEVDSUZJRUQQABIUChBQQVRIX0tJTkRfRElSRU'
    'NUEAESEwoPUEFUSF9LSU5EX1JFTEFZEAI=');

@$core.Deprecated('Use pathHealthDescriptor instead')
const PathHealth$json = {
  '1': 'PathHealth',
  '2': [
    {'1': 'PATH_HEALTH_UNSPECIFIED', '2': 0},
    {'1': 'PATH_HEALTH_UNKNOWN', '2': 1},
    {'1': 'PATH_HEALTH_CHECKING', '2': 2},
    {'1': 'PATH_HEALTH_REACHABLE', '2': 3},
    {'1': 'PATH_HEALTH_UNREACHABLE', '2': 4},
  ],
};

/// Descriptor for `PathHealth`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List pathHealthDescriptor = $convert.base64Decode(
    'CgpQYXRoSGVhbHRoEhsKF1BBVEhfSEVBTFRIX1VOU1BFQ0lGSUVEEAASFwoTUEFUSF9IRUFMVE'
    'hfVU5LTk9XThABEhgKFFBBVEhfSEVBTFRIX0NIRUNLSU5HEAISGQoVUEFUSF9IRUFMVEhfUkVB'
    'Q0hBQkxFEAMSGwoXUEFUSF9IRUFMVEhfVU5SRUFDSEFCTEUQBA==');

@$core.Deprecated('Use sessionStateDescriptor instead')
const SessionState$json = {
  '1': 'SessionState',
  '2': [
    {'1': 'SESSION_STATE_UNSPECIFIED', '2': 0},
    {'1': 'SESSION_STATE_NOT_AUTHENTICATED', '2': 1},
    {'1': 'SESSION_STATE_ACTIVE', '2': 2},
    {'1': 'SESSION_STATE_EXPIRING', '2': 3},
    {'1': 'SESSION_STATE_EXPIRED', '2': 4},
    {'1': 'SESSION_STATE_RENEWING', '2': 5},
  ],
};

/// Descriptor for `SessionState`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List sessionStateDescriptor = $convert.base64Decode(
    'CgxTZXNzaW9uU3RhdGUSHQoZU0VTU0lPTl9TVEFURV9VTlNQRUNJRklFRBAAEiMKH1NFU1NJT0'
    '5fU1RBVEVfTk9UX0FVVEhFTlRJQ0FURUQQARIYChRTRVNTSU9OX1NUQVRFX0FDVElWRRACEhoK'
    'FlNFU1NJT05fU1RBVEVfRVhQSVJJTkcQAxIZChVTRVNTSU9OX1NUQVRFX0VYUElSRUQQBBIaCh'
    'ZTRVNTSU9OX1NUQVRFX1JFTkVXSU5HEAU=');

@$core.Deprecated('Use enrollmentModeDescriptor instead')
const EnrollmentMode$json = {
  '1': 'EnrollmentMode',
  '2': [
    {'1': 'ENROLLMENT_MODE_UNSPECIFIED', '2': 0},
    {'1': 'ENROLLMENT_MODE_WORKSTATION', '2': 1},
    {'1': 'ENROLLMENT_MODE_SERVER', '2': 2},
    {'1': 'ENROLLMENT_MODE_SUBNET_ROUTER', '2': 3},
    {'1': 'ENROLLMENT_MODE_INTERACTIVE', '2': 4},
  ],
};

/// Descriptor for `EnrollmentMode`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List enrollmentModeDescriptor = $convert.base64Decode(
    'Cg5FbnJvbGxtZW50TW9kZRIfChtFTlJPTExNRU5UX01PREVfVU5TUEVDSUZJRUQQABIfChtFTl'
    'JPTExNRU5UX01PREVfV09SS1NUQVRJT04QARIaChZFTlJPTExNRU5UX01PREVfU0VSVkVSEAIS'
    'IQodRU5ST0xMTUVOVF9NT0RFX1NVQk5FVF9ST1VURVIQAxIfChtFTlJPTExNRU5UX01PREVfSU'
    '5URVJBQ1RJVkUQBA==');

@$core.Deprecated('Use profileStateDescriptor instead')
const ProfileState$json = {
  '1': 'ProfileState',
  '2': [
    {'1': 'PROFILE_STATE_UNSPECIFIED', '2': 0},
    {'1': 'PROFILE_STATE_EMPTY', '2': 1},
    {'1': 'PROFILE_STATE_REGISTERED', '2': 2},
    {'1': 'PROFILE_STATE_NEEDS_LOGIN', '2': 3},
    {'1': 'PROFILE_STATE_NEEDS_APPROVAL', '2': 4},
    {'1': 'PROFILE_STATE_BLOCKED', '2': 5},
  ],
};

/// Descriptor for `ProfileState`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List profileStateDescriptor = $convert.base64Decode(
    'CgxQcm9maWxlU3RhdGUSHQoZUFJPRklMRV9TVEFURV9VTlNQRUNJRklFRBAAEhcKE1BST0ZJTE'
    'VfU1RBVEVfRU1QVFkQARIcChhQUk9GSUxFX1NUQVRFX1JFR0lTVEVSRUQQAhIdChlQUk9GSUxF'
    'X1NUQVRFX05FRURTX0xPR0lOEAMSIAocUFJPRklMRV9TVEFURV9ORUVEU19BUFBST1ZBTBAEEh'
    'kKFVBST0ZJTEVfU1RBVEVfQkxPQ0tFRBAF');

@$core.Deprecated('Use credentialStateDescriptor instead')
const CredentialState$json = {
  '1': 'CredentialState',
  '2': [
    {'1': 'CREDENTIAL_STATE_UNSPECIFIED', '2': 0},
    {'1': 'CREDENTIAL_STATE_ABSENT', '2': 1},
    {'1': 'CREDENTIAL_STATE_VALID', '2': 2},
    {'1': 'CREDENTIAL_STATE_EXPIRING', '2': 3},
    {'1': 'CREDENTIAL_STATE_EXPIRED', '2': 4},
    {'1': 'CREDENTIAL_STATE_RENEWING', '2': 5},
    {'1': 'CREDENTIAL_STATE_BLOCKED', '2': 6},
  ],
};

/// Descriptor for `CredentialState`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List credentialStateDescriptor = $convert.base64Decode(
    'Cg9DcmVkZW50aWFsU3RhdGUSIAocQ1JFREVOVElBTF9TVEFURV9VTlNQRUNJRklFRBAAEhsKF0'
    'NSRURFTlRJQUxfU1RBVEVfQUJTRU5UEAESGgoWQ1JFREVOVElBTF9TVEFURV9WQUxJRBACEh0K'
    'GUNSRURFTlRJQUxfU1RBVEVfRVhQSVJJTkcQAxIcChhDUkVERU5USUFMX1NUQVRFX0VYUElSRU'
    'QQBBIdChlDUkVERU5USUFMX1NUQVRFX1JFTkVXSU5HEAUSHAoYQ1JFREVOVElBTF9TVEFURV9C'
    'TE9DS0VEEAY=');

@$core.Deprecated('Use connectionPhaseDescriptor instead')
const ConnectionPhase$json = {
  '1': 'ConnectionPhase',
  '2': [
    {'1': 'CONNECTION_PHASE_UNSPECIFIED', '2': 0},
    {'1': 'CONNECTION_PHASE_DISCONNECTED', '2': 1},
    {'1': 'CONNECTION_PHASE_CONNECTING', '2': 2},
    {'1': 'CONNECTION_PHASE_CONNECTED', '2': 3},
    {'1': 'CONNECTION_PHASE_DISCONNECTING', '2': 4},
  ],
};

/// Descriptor for `ConnectionPhase`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List connectionPhaseDescriptor = $convert.base64Decode(
    'Cg9Db25uZWN0aW9uUGhhc2USIAocQ09OTkVDVElPTl9QSEFTRV9VTlNQRUNJRklFRBAAEiEKHU'
    'NPTk5FQ1RJT05fUEhBU0VfRElTQ09OTkVDVEVEEAESHwobQ09OTkVDVElPTl9QSEFTRV9DT05O'
    'RUNUSU5HEAISHgoaQ09OTkVDVElPTl9QSEFTRV9DT05ORUNURUQQAxIiCh5DT05ORUNUSU9OX1'
    'BIQVNFX0RJU0NPTk5FQ1RJTkcQBA==');

@$core.Deprecated('Use connectionIntentDescriptor instead')
const ConnectionIntent$json = {
  '1': 'ConnectionIntent',
  '2': [
    {
      '1': 'desired_state',
      '3': 1,
      '4': 1,
      '5': 14,
      '6': '.client.v0.DesiredState',
      '10': 'desiredState'
    },
    {'1': 'reason_key', '3': 2, '4': 1, '5': 9, '10': 'reasonKey'},
    {
      '1': 'updated_at',
      '3': 3,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'updatedAt'
    },
  ],
};

/// Descriptor for `ConnectionIntent`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List connectionIntentDescriptor = $convert.base64Decode(
    'ChBDb25uZWN0aW9uSW50ZW50EjwKDWRlc2lyZWRfc3RhdGUYASABKA4yFy5jbGllbnQudjAuRG'
    'VzaXJlZFN0YXRlUgxkZXNpcmVkU3RhdGUSHQoKcmVhc29uX2tleRgCIAEoCVIJcmVhc29uS2V5'
    'EjkKCnVwZGF0ZWRfYXQYAyABKAsyGi5nb29nbGUucHJvdG9idWYuVGltZXN0YW1wUgl1cGRhdG'
    'VkQXQ=');

@$core.Deprecated('Use storedStatePresenceDescriptor instead')
const StoredStatePresence$json = {
  '1': 'StoredStatePresence',
  '2': [
    {
      '1': 'cached_map_present',
      '3': 1,
      '4': 1,
      '5': 8,
      '10': 'cachedMapPresent'
    },
    {'1': 'cached_map_valid', '3': 2, '4': 1, '5': 8, '10': 'cachedMapValid'},
    {
      '1': 'map_signing_trust_present',
      '3': 3,
      '4': 1,
      '5': 8,
      '10': 'mapSigningTrustPresent'
    },
    {'1': 'token_present', '3': 4, '4': 1, '5': 8, '10': 'tokenPresent'},
    {
      '1': 'node_credential_present',
      '3': 5,
      '4': 1,
      '5': 8,
      '10': 'nodeCredentialPresent'
    },
    {
      '1': 'device_fingerprint_present',
      '3': 6,
      '4': 1,
      '5': 8,
      '10': 'deviceFingerprintPresent'
    },
    {
      '1': 'identity_private_key_present',
      '3': 7,
      '4': 1,
      '5': 8,
      '10': 'identityPrivateKeyPresent'
    },
    {
      '1': 'tunnel_private_key_present',
      '3': 8,
      '4': 1,
      '5': 8,
      '10': 'tunnelPrivateKeyPresent'
    },
  ],
};

/// Descriptor for `StoredStatePresence`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List storedStatePresenceDescriptor = $convert.base64Decode(
    'ChNTdG9yZWRTdGF0ZVByZXNlbmNlEiwKEmNhY2hlZF9tYXBfcHJlc2VudBgBIAEoCFIQY2FjaG'
    'VkTWFwUHJlc2VudBIoChBjYWNoZWRfbWFwX3ZhbGlkGAIgASgIUg5jYWNoZWRNYXBWYWxpZBI5'
    'ChltYXBfc2lnbmluZ190cnVzdF9wcmVzZW50GAMgASgIUhZtYXBTaWduaW5nVHJ1c3RQcmVzZW'
    '50EiMKDXRva2VuX3ByZXNlbnQYBCABKAhSDHRva2VuUHJlc2VudBI2Chdub2RlX2NyZWRlbnRp'
    'YWxfcHJlc2VudBgFIAEoCFIVbm9kZUNyZWRlbnRpYWxQcmVzZW50EjwKGmRldmljZV9maW5nZX'
    'JwcmludF9wcmVzZW50GAYgASgIUhhkZXZpY2VGaW5nZXJwcmludFByZXNlbnQSPwocaWRlbnRp'
    'dHlfcHJpdmF0ZV9rZXlfcHJlc2VudBgHIAEoCFIZaWRlbnRpdHlQcml2YXRlS2V5UHJlc2VudB'
    'I7Chp0dW5uZWxfcHJpdmF0ZV9rZXlfcHJlc2VudBgIIAEoCFIXdHVubmVsUHJpdmF0ZUtleVBy'
    'ZXNlbnQ=');

@$core.Deprecated('Use networkDescriptor instead')
const Network$json = {
  '1': 'Network',
  '2': [
    {'1': 'id', '3': 1, '4': 1, '5': 9, '10': 'id'},
    {'1': 'name', '3': 2, '4': 1, '5': 9, '10': 'name'},
    {'1': 'account_id', '3': 3, '4': 1, '5': 9, '10': 'accountId'},
    {'1': 'ipv4_cidr', '3': 4, '4': 1, '5': 9, '10': 'ipv4Cidr'},
    {'1': 'ipv6_cidr', '3': 5, '4': 1, '5': 9, '10': 'ipv6Cidr'},
    {
      '1': 'selection',
      '3': 6,
      '4': 1,
      '5': 11,
      '6': '.client.v0.Restriction',
      '10': 'selection'
    },
  ],
};

/// Descriptor for `Network`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List networkDescriptor = $convert.base64Decode(
    'CgdOZXR3b3JrEg4KAmlkGAEgASgJUgJpZBISCgRuYW1lGAIgASgJUgRuYW1lEh0KCmFjY291bn'
    'RfaWQYAyABKAlSCWFjY291bnRJZBIbCglpcHY0X2NpZHIYBCABKAlSCGlwdjRDaWRyEhsKCWlw'
    'djZfY2lkchgFIAEoCVIIaXB2NkNpZHISNAoJc2VsZWN0aW9uGAYgASgLMhYuY2xpZW50LnYwLl'
    'Jlc3RyaWN0aW9uUglzZWxlY3Rpb24=');

@$core.Deprecated('Use profileDescriptor instead')
const Profile$json = {
  '1': 'Profile',
  '2': [
    {'1': 'id', '3': 1, '4': 1, '5': 9, '10': 'id'},
    {'1': 'display_name', '3': 2, '4': 1, '5': 9, '10': 'displayName'},
    {'1': 'control_origin', '3': 3, '4': 1, '5': 9, '10': 'controlOrigin'},
    {'1': 'account_id', '3': 4, '4': 1, '5': 9, '10': 'accountId'},
    {
      '1': 'identity_display_name',
      '3': 5,
      '4': 1,
      '5': 9,
      '10': 'identityDisplayName'
    },
    {
      '1': 'state',
      '3': 6,
      '4': 1,
      '5': 14,
      '6': '.client.v0.ProfileState',
      '10': 'state'
    },
    {'1': 'active', '3': 7, '4': 1, '5': 8, '10': 'active'},
    {
      '1': 'selected_network_id',
      '3': 8,
      '4': 1,
      '5': 9,
      '10': 'selectedNetworkId'
    },
    {
      '1': 'selection',
      '3': 9,
      '4': 1,
      '5': 11,
      '6': '.client.v0.Restriction',
      '10': 'selection'
    },
  ],
};

/// Descriptor for `Profile`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List profileDescriptor = $convert.base64Decode(
    'CgdQcm9maWxlEg4KAmlkGAEgASgJUgJpZBIhCgxkaXNwbGF5X25hbWUYAiABKAlSC2Rpc3BsYX'
    'lOYW1lEiUKDmNvbnRyb2xfb3JpZ2luGAMgASgJUg1jb250cm9sT3JpZ2luEh0KCmFjY291bnRf'
    'aWQYBCABKAlSCWFjY291bnRJZBIyChVpZGVudGl0eV9kaXNwbGF5X25hbWUYBSABKAlSE2lkZW'
    '50aXR5RGlzcGxheU5hbWUSLQoFc3RhdGUYBiABKA4yFy5jbGllbnQudjAuUHJvZmlsZVN0YXRl'
    'UgVzdGF0ZRIWCgZhY3RpdmUYByABKAhSBmFjdGl2ZRIuChNzZWxlY3RlZF9uZXR3b3JrX2lkGA'
    'ggASgJUhFzZWxlY3RlZE5ldHdvcmtJZBI0CglzZWxlY3Rpb24YCSABKAsyFi5jbGllbnQudjAu'
    'UmVzdHJpY3Rpb25SCXNlbGVjdGlvbg==');

@$core.Deprecated('Use credentialStatusDescriptor instead')
const CredentialStatus$json = {
  '1': 'CredentialStatus',
  '2': [
    {
      '1': 'state',
      '3': 1,
      '4': 1,
      '5': 14,
      '6': '.client.v0.CredentialState',
      '10': 'state'
    },
    {
      '1': 'expires_at',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'expiresAt'
    },
    {
      '1': 'warning_at',
      '3': 3,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'warningAt'
    },
    {
      '1': 'automatic_renewal_supported',
      '3': 4,
      '4': 1,
      '5': 8,
      '10': 'automaticRenewalSupported'
    },
    {
      '1': 'recovery',
      '3': 5,
      '4': 1,
      '5': 11,
      '6': '.client.v0.Restriction',
      '10': 'recovery'
    },
    {
      '1': 'renewal_operation_id',
      '3': 6,
      '4': 1,
      '5': 9,
      '10': 'renewalOperationId'
    },
  ],
};

/// Descriptor for `CredentialStatus`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List credentialStatusDescriptor = $convert.base64Decode(
    'ChBDcmVkZW50aWFsU3RhdHVzEjAKBXN0YXRlGAEgASgOMhouY2xpZW50LnYwLkNyZWRlbnRpYW'
    'xTdGF0ZVIFc3RhdGUSOQoKZXhwaXJlc19hdBgCIAEoCzIaLmdvb2dsZS5wcm90b2J1Zi5UaW1l'
    'c3RhbXBSCWV4cGlyZXNBdBI5Cgp3YXJuaW5nX2F0GAMgASgLMhouZ29vZ2xlLnByb3RvYnVmLl'
    'RpbWVzdGFtcFIJd2FybmluZ0F0Ej4KG2F1dG9tYXRpY19yZW5ld2FsX3N1cHBvcnRlZBgEIAEo'
    'CFIZYXV0b21hdGljUmVuZXdhbFN1cHBvcnRlZBIyCghyZWNvdmVyeRgFIAEoCzIWLmNsaWVudC'
    '52MC5SZXN0cmljdGlvblIIcmVjb3ZlcnkSMAoUcmVuZXdhbF9vcGVyYXRpb25faWQYBiABKAlS'
    'EnJlbmV3YWxPcGVyYXRpb25JZA==');

@$core.Deprecated('Use sessionDescriptor instead')
const Session$json = {
  '1': 'Session',
  '2': [
    {
      '1': 'state',
      '3': 1,
      '4': 1,
      '5': 14,
      '6': '.client.v0.SessionState',
      '10': 'state'
    },
    {
      '1': 'expires_at',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'expiresAt'
    },
    {
      '1': 'warning_at',
      '3': 3,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'warningAt'
    },
    {
      '1': 'renewal',
      '3': 4,
      '4': 1,
      '5': 11,
      '6': '.client.v0.Restriction',
      '10': 'renewal'
    },
    {
      '1': 'seamless_renewal_supported',
      '3': 5,
      '4': 1,
      '5': 8,
      '10': 'seamlessRenewalSupported'
    },
    {
      '1': 'renewal_operation_id',
      '3': 6,
      '4': 1,
      '5': 9,
      '10': 'renewalOperationId'
    },
  ],
};

/// Descriptor for `Session`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List sessionDescriptor = $convert.base64Decode(
    'CgdTZXNzaW9uEi0KBXN0YXRlGAEgASgOMhcuY2xpZW50LnYwLlNlc3Npb25TdGF0ZVIFc3RhdG'
    'USOQoKZXhwaXJlc19hdBgCIAEoCzIaLmdvb2dsZS5wcm90b2J1Zi5UaW1lc3RhbXBSCWV4cGly'
    'ZXNBdBI5Cgp3YXJuaW5nX2F0GAMgASgLMhouZ29vZ2xlLnByb3RvYnVmLlRpbWVzdGFtcFIJd2'
    'FybmluZ0F0EjAKB3JlbmV3YWwYBCABKAsyFi5jbGllbnQudjAuUmVzdHJpY3Rpb25SB3JlbmV3'
    'YWwSPAoac2VhbWxlc3NfcmVuZXdhbF9zdXBwb3J0ZWQYBSABKAhSGHNlYW1sZXNzUmVuZXdhbF'
    'N1cHBvcnRlZBIwChRyZW5ld2FsX29wZXJhdGlvbl9pZBgGIAEoCVIScmVuZXdhbE9wZXJhdGlv'
    'bklk');

@$core.Deprecated('Use recoveryDescriptor instead')
const Recovery$json = {
  '1': 'Recovery',
  '2': [
    {'1': 'operation_id', '3': 1, '4': 1, '5': 9, '10': 'operationId'},
    {
      '1': 'state',
      '3': 2,
      '4': 1,
      '5': 14,
      '6': '.client.v0.ServiceState',
      '10': 'state'
    },
    {
      '1': 'failure',
      '3': 3,
      '4': 1,
      '5': 11,
      '6': '.client.v0.Failure',
      '10': 'failure'
    },
  ],
};

/// Descriptor for `Recovery`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List recoveryDescriptor = $convert.base64Decode(
    'CghSZWNvdmVyeRIhCgxvcGVyYXRpb25faWQYASABKAlSC29wZXJhdGlvbklkEi0KBXN0YXRlGA'
    'IgASgOMhcuY2xpZW50LnYwLlNlcnZpY2VTdGF0ZVIFc3RhdGUSLAoHZmFpbHVyZRgDIAEoCzIS'
    'LmNsaWVudC52MC5GYWlsdXJlUgdmYWlsdXJl');

@$core.Deprecated('Use serverIdentityDescriptor instead')
const ServerIdentity$json = {
  '1': 'ServerIdentity',
  '2': [
    {'1': 'profile_id', '3': 1, '4': 1, '5': 9, '10': 'profileId'},
    {'1': 'control_origin', '3': 2, '4': 1, '5': 9, '10': 'controlOrigin'},
    {'1': 'trusted_key_id', '3': 3, '4': 1, '5': 9, '10': 'trustedKeyId'},
    {'1': 'announced_key_id', '3': 4, '4': 1, '5': 9, '10': 'announcedKeyId'},
    {'1': 'changed', '3': 5, '4': 1, '5': 8, '10': 'changed'},
    {'1': 'announcement_id', '3': 6, '4': 1, '5': 9, '10': 'announcementId'},
  ],
};

/// Descriptor for `ServerIdentity`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List serverIdentityDescriptor = $convert.base64Decode(
    'Cg5TZXJ2ZXJJZGVudGl0eRIdCgpwcm9maWxlX2lkGAEgASgJUglwcm9maWxlSWQSJQoOY29udH'
    'JvbF9vcmlnaW4YAiABKAlSDWNvbnRyb2xPcmlnaW4SJAoOdHJ1c3RlZF9rZXlfaWQYAyABKAlS'
    'DHRydXN0ZWRLZXlJZBIoChBhbm5vdW5jZWRfa2V5X2lkGAQgASgJUg5hbm5vdW5jZWRLZXlJZB'
    'IYCgdjaGFuZ2VkGAUgASgIUgdjaGFuZ2VkEicKD2Fubm91bmNlbWVudF9pZBgGIAEoCVIOYW5u'
    'b3VuY2VtZW50SWQ=');

@$core.Deprecated('Use endpointDescriptor instead')
const Endpoint$json = {
  '1': 'Endpoint',
  '2': [
    {'1': 'id', '3': 1, '4': 1, '5': 9, '10': 'id'},
    {'1': 'address', '3': 2, '4': 1, '5': 9, '10': 'address'},
    {'1': 'protocol', '3': 3, '4': 1, '5': 9, '10': 'protocol'},
    {'1': 'priority', '3': 4, '4': 1, '5': 13, '10': 'priority'},
  ],
};

/// Descriptor for `Endpoint`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List endpointDescriptor = $convert.base64Decode(
    'CghFbmRwb2ludBIOCgJpZBgBIAEoCVICaWQSGAoHYWRkcmVzcxgCIAEoCVIHYWRkcmVzcxIaCg'
    'hwcm90b2NvbBgDIAEoCVIIcHJvdG9jb2wSGgoIcHJpb3JpdHkYBCABKA1SCHByaW9yaXR5');

@$core.Deprecated('Use pathCandidateDescriptor instead')
const PathCandidate$json = {
  '1': 'PathCandidate',
  '2': [
    {
      '1': 'kind',
      '3': 1,
      '4': 1,
      '5': 14,
      '6': '.client.v0.PathKind',
      '10': 'kind'
    },
    {
      '1': 'health',
      '3': 2,
      '4': 1,
      '5': 14,
      '6': '.client.v0.PathHealth',
      '10': 'health'
    },
    {'1': 'endpoint', '3': 3, '4': 1, '5': 9, '10': 'endpoint'},
    {'1': 'relay_id', '3': 4, '4': 1, '5': 9, '10': 'relayId'},
    {'1': 'protocol', '3': 5, '4': 1, '5': 9, '10': 'protocol'},
    {'1': 'priority', '3': 6, '4': 1, '5': 13, '10': 'priority'},
    {
      '1': 'rtt',
      '3': 7,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Duration',
      '10': 'rtt'
    },
    {
      '1': 'checked_at',
      '3': 8,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'checkedAt'
    },
    {
      '1': 'last_reachable_at',
      '3': 9,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'lastReachableAt'
    },
    {
      '1': 'consecutive_failures',
      '3': 10,
      '4': 1,
      '5': 13,
      '10': 'consecutiveFailures'
    },
    {'1': 'reason_key', '3': 11, '4': 1, '5': 9, '10': 'reasonKey'},
    {'1': 'tier', '3': 12, '4': 1, '5': 9, '10': 'tier'},
  ],
};

/// Descriptor for `PathCandidate`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List pathCandidateDescriptor = $convert.base64Decode(
    'Cg1QYXRoQ2FuZGlkYXRlEicKBGtpbmQYASABKA4yEy5jbGllbnQudjAuUGF0aEtpbmRSBGtpbm'
    'QSLQoGaGVhbHRoGAIgASgOMhUuY2xpZW50LnYwLlBhdGhIZWFsdGhSBmhlYWx0aBIaCghlbmRw'
    'b2ludBgDIAEoCVIIZW5kcG9pbnQSGQoIcmVsYXlfaWQYBCABKAlSB3JlbGF5SWQSGgoIcHJvdG'
    '9jb2wYBSABKAlSCHByb3RvY29sEhoKCHByaW9yaXR5GAYgASgNUghwcmlvcml0eRIrCgNydHQY'
    'ByABKAsyGS5nb29nbGUucHJvdG9idWYuRHVyYXRpb25SA3J0dBI5CgpjaGVja2VkX2F0GAggAS'
    'gLMhouZ29vZ2xlLnByb3RvYnVmLlRpbWVzdGFtcFIJY2hlY2tlZEF0EkYKEWxhc3RfcmVhY2hh'
    'YmxlX2F0GAkgASgLMhouZ29vZ2xlLnByb3RvYnVmLlRpbWVzdGFtcFIPbGFzdFJlYWNoYWJsZU'
    'F0EjEKFGNvbnNlY3V0aXZlX2ZhaWx1cmVzGAogASgNUhNjb25zZWN1dGl2ZUZhaWx1cmVzEh0K'
    'CnJlYXNvbl9rZXkYCyABKAlSCXJlYXNvbktleRISCgR0aWVyGAwgASgJUgR0aWVy');

@$core.Deprecated('Use peerDescriptor instead')
const Peer$json = {
  '1': 'Peer',
  '2': [
    {'1': 'id', '3': 1, '4': 1, '5': 9, '10': 'id'},
    {'1': 'hostname', '3': 2, '4': 1, '5': 9, '10': 'hostname'},
    {
      '1': 'overlay_addresses',
      '3': 3,
      '4': 3,
      '5': 9,
      '10': 'overlayAddresses'
    },
    {
      '1': 'candidates',
      '3': 4,
      '4': 3,
      '5': 11,
      '6': '.client.v0.PathCandidate',
      '10': 'candidates'
    },
    {
      '1': 'selected_path',
      '3': 5,
      '4': 1,
      '5': 14,
      '6': '.client.v0.PathKind',
      '10': 'selectedPath'
    },
    {
      '1': 'selected_endpoint',
      '3': 6,
      '4': 1,
      '5': 9,
      '10': 'selectedEndpoint'
    },
    {
      '1': 'last_transition_at',
      '3': 7,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'lastTransitionAt'
    },
    {
      '1': 'selection_reason_key',
      '3': 8,
      '4': 1,
      '5': 9,
      '10': 'selectionReasonKey'
    },
  ],
};

/// Descriptor for `Peer`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List peerDescriptor = $convert.base64Decode(
    'CgRQZWVyEg4KAmlkGAEgASgJUgJpZBIaCghob3N0bmFtZRgCIAEoCVIIaG9zdG5hbWUSKwoRb3'
    'ZlcmxheV9hZGRyZXNzZXMYAyADKAlSEG92ZXJsYXlBZGRyZXNzZXMSOAoKY2FuZGlkYXRlcxgE'
    'IAMoCzIYLmNsaWVudC52MC5QYXRoQ2FuZGlkYXRlUgpjYW5kaWRhdGVzEjgKDXNlbGVjdGVkX3'
    'BhdGgYBSABKA4yEy5jbGllbnQudjAuUGF0aEtpbmRSDHNlbGVjdGVkUGF0aBIrChFzZWxlY3Rl'
    'ZF9lbmRwb2ludBgGIAEoCVIQc2VsZWN0ZWRFbmRwb2ludBJIChJsYXN0X3RyYW5zaXRpb25fYX'
    'QYByABKAsyGi5nb29nbGUucHJvdG9idWYuVGltZXN0YW1wUhBsYXN0VHJhbnNpdGlvbkF0EjAK'
    'FHNlbGVjdGlvbl9yZWFzb25fa2V5GAggASgJUhJzZWxlY3Rpb25SZWFzb25LZXk=');

@$core.Deprecated('Use agentStatusDescriptor instead')
const AgentStatus$json = {
  '1': 'AgentStatus',
  '2': [
    {
      '1': 'snapshot_state',
      '3': 1,
      '4': 1,
      '5': 14,
      '6': '.client.v0.AgentSnapshotState',
      '10': 'snapshotState'
    },
    {'1': 'map_revision', '3': 2, '4': 1, '5': 4, '10': 'mapRevision'},
    {
      '1': 'target_map_revision',
      '3': 3,
      '4': 1,
      '5': 4,
      '10': 'targetMapRevision'
    },
    {
      '1': 'generated_at',
      '3': 4,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'generatedAt'
    },
    {
      '1': 'connection_paused',
      '3': 5,
      '4': 1,
      '5': 8,
      '10': 'connectionPaused'
    },
    {'1': 'node_id', '3': 6, '4': 1, '5': 9, '10': 'nodeId'},
    {'1': 'network_id', '3': 7, '4': 1, '5': 9, '10': 'networkId'},
    {
      '1': 'overlay_addresses',
      '3': 8,
      '4': 3,
      '5': 9,
      '10': 'overlayAddresses'
    },
    {'1': 'peer_count', '3': 9, '4': 1, '5': 13, '10': 'peerCount'},
    {'1': 'stun_ok', '3': 10, '4': 1, '5': 8, '10': 'stunOk'},
    {'1': 'relay_ok', '3': 11, '4': 1, '5': 8, '10': 'relayOk'},
    {
      '1': 'selected_relay',
      '3': 12,
      '4': 1,
      '5': 11,
      '6': '.client.v0.Endpoint',
      '10': 'selectedRelay'
    },
    {
      '1': 'relay_attempt_count',
      '3': 13,
      '4': 1,
      '5': 13,
      '10': 'relayAttemptCount'
    },
    {
      '1': 'direct_path_count',
      '3': 14,
      '4': 1,
      '5': 13,
      '10': 'directPathCount'
    },
    {'1': 'relay_path_count', '3': 15, '4': 1, '5': 13, '10': 'relayPathCount'},
    {
      '1': 'last_failure',
      '3': 16,
      '4': 1,
      '5': 11,
      '6': '.client.v0.Failure',
      '10': 'lastFailure'
    },
  ],
};

/// Descriptor for `AgentStatus`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List agentStatusDescriptor = $convert.base64Decode(
    'CgtBZ2VudFN0YXR1cxJECg5zbmFwc2hvdF9zdGF0ZRgBIAEoDjIdLmNsaWVudC52MC5BZ2VudF'
    'NuYXBzaG90U3RhdGVSDXNuYXBzaG90U3RhdGUSIQoMbWFwX3JldmlzaW9uGAIgASgEUgttYXBS'
    'ZXZpc2lvbhIuChN0YXJnZXRfbWFwX3JldmlzaW9uGAMgASgEUhF0YXJnZXRNYXBSZXZpc2lvbh'
    'I9CgxnZW5lcmF0ZWRfYXQYBCABKAsyGi5nb29nbGUucHJvdG9idWYuVGltZXN0YW1wUgtnZW5l'
    'cmF0ZWRBdBIrChFjb25uZWN0aW9uX3BhdXNlZBgFIAEoCFIQY29ubmVjdGlvblBhdXNlZBIXCg'
    'dub2RlX2lkGAYgASgJUgZub2RlSWQSHQoKbmV0d29ya19pZBgHIAEoCVIJbmV0d29ya0lkEisK'
    'EW92ZXJsYXlfYWRkcmVzc2VzGAggAygJUhBvdmVybGF5QWRkcmVzc2VzEh0KCnBlZXJfY291bn'
    'QYCSABKA1SCXBlZXJDb3VudBIXCgdzdHVuX29rGAogASgIUgZzdHVuT2sSGQoIcmVsYXlfb2sY'
    'CyABKAhSB3JlbGF5T2sSOgoOc2VsZWN0ZWRfcmVsYXkYDCABKAsyEy5jbGllbnQudjAuRW5kcG'
    '9pbnRSDXNlbGVjdGVkUmVsYXkSLgoTcmVsYXlfYXR0ZW1wdF9jb3VudBgNIAEoDVIRcmVsYXlB'
    'dHRlbXB0Q291bnQSKgoRZGlyZWN0X3BhdGhfY291bnQYDiABKA1SD2RpcmVjdFBhdGhDb3VudB'
    'IoChByZWxheV9wYXRoX2NvdW50GA8gASgNUg5yZWxheVBhdGhDb3VudBI1CgxsYXN0X2ZhaWx1'
    'cmUYECABKAsyEi5jbGllbnQudjAuRmFpbHVyZVILbGFzdEZhaWx1cmU=');

@$core.Deprecated('Use controlProbeDescriptor instead')
const ControlProbe$json = {
  '1': 'ControlProbe',
  '2': [
    {'1': 'ok', '3': 1, '4': 1, '5': 8, '10': 'ok'},
    {'1': 'origin', '3': 2, '4': 1, '5': 9, '10': 'origin'},
    {'1': 'http_status', '3': 3, '4': 1, '5': 13, '10': 'httpStatus'},
    {
      '1': 'failure',
      '3': 4,
      '4': 1,
      '5': 11,
      '6': '.client.v0.Failure',
      '10': 'failure'
    },
    {
      '1': 'attempts',
      '3': 5,
      '4': 3,
      '5': 11,
      '6': '.client.v0.ControlProbe',
      '10': 'attempts'
    },
  ],
};

/// Descriptor for `ControlProbe`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List controlProbeDescriptor = $convert.base64Decode(
    'CgxDb250cm9sUHJvYmUSDgoCb2sYASABKAhSAm9rEhYKBm9yaWdpbhgCIAEoCVIGb3JpZ2luEh'
    '8KC2h0dHBfc3RhdHVzGAMgASgNUgpodHRwU3RhdHVzEiwKB2ZhaWx1cmUYBCABKAsyEi5jbGll'
    'bnQudjAuRmFpbHVyZVIHZmFpbHVyZRIzCghhdHRlbXB0cxgFIAMoCzIXLmNsaWVudC52MC5Db2'
    '50cm9sUHJvYmVSCGF0dGVtcHRz');

@$core.Deprecated('Use statusDescriptor instead')
const Status$json = {
  '1': 'Status',
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
      '1': 'service_state',
      '3': 2,
      '4': 1,
      '5': 14,
      '6': '.client.v0.ServiceState',
      '10': 'serviceState'
    },
    {
      '1': 'control_state',
      '3': 3,
      '4': 1,
      '5': 14,
      '6': '.client.v0.ControlState',
      '10': 'controlState'
    },
    {
      '1': 'intent',
      '3': 4,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ConnectionIntent',
      '10': 'intent'
    },
    {
      '1': 'user_disconnected',
      '3': 5,
      '4': 1,
      '5': 8,
      '10': 'userDisconnected'
    },
    {'1': 'active_profile_id', '3': 6, '4': 1, '5': 9, '10': 'activeProfileId'},
    {'1': 'account_id', '3': 7, '4': 1, '5': 9, '10': 'accountId'},
    {'1': 'node_id', '3': 8, '4': 1, '5': 9, '10': 'nodeId'},
    {'1': 'hostname', '3': 9, '4': 1, '5': 9, '10': 'hostname'},
    {
      '1': 'network',
      '3': 10,
      '4': 1,
      '5': 11,
      '6': '.client.v0.Network',
      '10': 'network'
    },
    {'1': 'ephemeral', '3': 11, '4': 1, '5': 8, '10': 'ephemeral'},
    {
      '1': 'enrollment_request_id',
      '3': 12,
      '4': 1,
      '5': 9,
      '10': 'enrollmentRequestId'
    },
    {
      '1': 'pending_action',
      '3': 13,
      '4': 1,
      '5': 11,
      '6': '.client.v0.UserAction',
      '10': 'pendingAction'
    },
    {
      '1': 'overlay_addresses',
      '3': 14,
      '4': 3,
      '5': 9,
      '10': 'overlayAddresses'
    },
    {'1': 'map_revision', '3': 15, '4': 1, '5': 4, '10': 'mapRevision'},
    {'1': 'peer_count', '3': 16, '4': 1, '5': 13, '10': 'peerCount'},
    {
      '1': 'stored_state',
      '3': 17,
      '4': 1,
      '5': 11,
      '6': '.client.v0.StoredStatePresence',
      '10': 'storedState'
    },
    {
      '1': 'recovery',
      '3': 18,
      '4': 1,
      '5': 11,
      '6': '.client.v0.Recovery',
      '10': 'recovery'
    },
    {
      '1': 'session',
      '3': 19,
      '4': 1,
      '5': 11,
      '6': '.client.v0.Session',
      '10': 'session'
    },
    {
      '1': 'agent',
      '3': 20,
      '4': 1,
      '5': 11,
      '6': '.client.v0.AgentStatus',
      '10': 'agent'
    },
    {
      '1': 'control',
      '3': 21,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ControlProbe',
      '10': 'control'
    },
    {
      '1': 'stun_endpoints',
      '3': 22,
      '4': 3,
      '5': 11,
      '6': '.client.v0.Endpoint',
      '10': 'stunEndpoints'
    },
    {
      '1': 'relay_endpoints',
      '3': 23,
      '4': 3,
      '5': 11,
      '6': '.client.v0.Endpoint',
      '10': 'relayEndpoints'
    },
    {
      '1': 'failures',
      '3': 24,
      '4': 3,
      '5': 11,
      '6': '.client.v0.Failure',
      '10': 'failures'
    },
    {'1': 'route_table', '3': 25, '4': 1, '5': 9, '10': 'routeTable'},
    {
      '1': 'connection_phase',
      '3': 26,
      '4': 1,
      '5': 14,
      '6': '.client.v0.ConnectionPhase',
      '10': 'connectionPhase'
    },
    {
      '1': 'current_operations',
      '3': 27,
      '4': 3,
      '5': 11,
      '6': '.client.v0.Operation',
      '10': 'currentOperations'
    },
    {
      '1': 'credential',
      '3': 28,
      '4': 1,
      '5': 11,
      '6': '.client.v0.CredentialStatus',
      '10': 'credential'
    },
  ],
};

/// Descriptor for `Status`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List statusDescriptor = $convert.base64Decode(
    'CgZTdGF0dXMSNwoIbWV0YWRhdGEYASABKAsyGy5jbGllbnQudjAuU25hcHNob3RNZXRhZGF0YV'
    'IIbWV0YWRhdGESPAoNc2VydmljZV9zdGF0ZRgCIAEoDjIXLmNsaWVudC52MC5TZXJ2aWNlU3Rh'
    'dGVSDHNlcnZpY2VTdGF0ZRI8Cg1jb250cm9sX3N0YXRlGAMgASgOMhcuY2xpZW50LnYwLkNvbn'
    'Ryb2xTdGF0ZVIMY29udHJvbFN0YXRlEjMKBmludGVudBgEIAEoCzIbLmNsaWVudC52MC5Db25u'
    'ZWN0aW9uSW50ZW50UgZpbnRlbnQSKwoRdXNlcl9kaXNjb25uZWN0ZWQYBSABKAhSEHVzZXJEaX'
    'Njb25uZWN0ZWQSKgoRYWN0aXZlX3Byb2ZpbGVfaWQYBiABKAlSD2FjdGl2ZVByb2ZpbGVJZBId'
    'CgphY2NvdW50X2lkGAcgASgJUglhY2NvdW50SWQSFwoHbm9kZV9pZBgIIAEoCVIGbm9kZUlkEh'
    'oKCGhvc3RuYW1lGAkgASgJUghob3N0bmFtZRIsCgduZXR3b3JrGAogASgLMhIuY2xpZW50LnYw'
    'Lk5ldHdvcmtSB25ldHdvcmsSHAoJZXBoZW1lcmFsGAsgASgIUgllcGhlbWVyYWwSMgoVZW5yb2'
    'xsbWVudF9yZXF1ZXN0X2lkGAwgASgJUhNlbnJvbGxtZW50UmVxdWVzdElkEjwKDnBlbmRpbmdf'
    'YWN0aW9uGA0gASgLMhUuY2xpZW50LnYwLlVzZXJBY3Rpb25SDXBlbmRpbmdBY3Rpb24SKwoRb3'
    'ZlcmxheV9hZGRyZXNzZXMYDiADKAlSEG92ZXJsYXlBZGRyZXNzZXMSIQoMbWFwX3JldmlzaW9u'
    'GA8gASgEUgttYXBSZXZpc2lvbhIdCgpwZWVyX2NvdW50GBAgASgNUglwZWVyQ291bnQSQQoMc3'
    'RvcmVkX3N0YXRlGBEgASgLMh4uY2xpZW50LnYwLlN0b3JlZFN0YXRlUHJlc2VuY2VSC3N0b3Jl'
    'ZFN0YXRlEi8KCHJlY292ZXJ5GBIgASgLMhMuY2xpZW50LnYwLlJlY292ZXJ5UghyZWNvdmVyeR'
    'IsCgdzZXNzaW9uGBMgASgLMhIuY2xpZW50LnYwLlNlc3Npb25SB3Nlc3Npb24SLAoFYWdlbnQY'
    'FCABKAsyFi5jbGllbnQudjAuQWdlbnRTdGF0dXNSBWFnZW50EjEKB2NvbnRyb2wYFSABKAsyFy'
    '5jbGllbnQudjAuQ29udHJvbFByb2JlUgdjb250cm9sEjoKDnN0dW5fZW5kcG9pbnRzGBYgAygL'
    'MhMuY2xpZW50LnYwLkVuZHBvaW50Ug1zdHVuRW5kcG9pbnRzEjwKD3JlbGF5X2VuZHBvaW50cx'
    'gXIAMoCzITLmNsaWVudC52MC5FbmRwb2ludFIOcmVsYXlFbmRwb2ludHMSLgoIZmFpbHVyZXMY'
    'GCADKAsyEi5jbGllbnQudjAuRmFpbHVyZVIIZmFpbHVyZXMSHwoLcm91dGVfdGFibGUYGSABKA'
    'lSCnJvdXRlVGFibGUSRQoQY29ubmVjdGlvbl9waGFzZRgaIAEoDjIaLmNsaWVudC52MC5Db25u'
    'ZWN0aW9uUGhhc2VSD2Nvbm5lY3Rpb25QaGFzZRJDChJjdXJyZW50X29wZXJhdGlvbnMYGyADKA'
    'syFC5jbGllbnQudjAuT3BlcmF0aW9uUhFjdXJyZW50T3BlcmF0aW9ucxI7CgpjcmVkZW50aWFs'
    'GBwgASgLMhsuY2xpZW50LnYwLkNyZWRlbnRpYWxTdGF0dXNSCmNyZWRlbnRpYWw=');

@$core.Deprecated('Use tunnelPeerDescriptor instead')
const TunnelPeer$json = {
  '1': 'TunnelPeer',
  '2': [
    {'1': 'peer_id', '3': 1, '4': 1, '5': 9, '10': 'peerId'},
    {'1': 'public_key', '3': 2, '4': 1, '5': 9, '10': 'publicKey'},
    {'1': 'endpoint', '3': 3, '4': 1, '5': 9, '10': 'endpoint'},
    {'1': 'allowed_ips', '3': 4, '4': 3, '5': 9, '10': 'allowedIps'},
    {
      '1': 'latest_handshake',
      '3': 5,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'latestHandshake'
    },
    {'1': 'received_bytes', '3': 6, '4': 1, '5': 4, '10': 'receivedBytes'},
    {
      '1': 'transmitted_bytes',
      '3': 7,
      '4': 1,
      '5': 4,
      '10': 'transmittedBytes'
    },
    {
      '1': 'persistent_keepalive',
      '3': 8,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Duration',
      '10': 'persistentKeepalive'
    },
  ],
};

/// Descriptor for `TunnelPeer`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List tunnelPeerDescriptor = $convert.base64Decode(
    'CgpUdW5uZWxQZWVyEhcKB3BlZXJfaWQYASABKAlSBnBlZXJJZBIdCgpwdWJsaWNfa2V5GAIgAS'
    'gJUglwdWJsaWNLZXkSGgoIZW5kcG9pbnQYAyABKAlSCGVuZHBvaW50Eh8KC2FsbG93ZWRfaXBz'
    'GAQgAygJUgphbGxvd2VkSXBzEkUKEGxhdGVzdF9oYW5kc2hha2UYBSABKAsyGi5nb29nbGUucH'
    'JvdG9idWYuVGltZXN0YW1wUg9sYXRlc3RIYW5kc2hha2USJQoOcmVjZWl2ZWRfYnl0ZXMYBiAB'
    'KARSDXJlY2VpdmVkQnl0ZXMSKwoRdHJhbnNtaXR0ZWRfYnl0ZXMYByABKARSEHRyYW5zbWl0dG'
    'VkQnl0ZXMSTAoUcGVyc2lzdGVudF9rZWVwYWxpdmUYCCABKAsyGS5nb29nbGUucHJvdG9idWYu'
    'RHVyYXRpb25SE3BlcnNpc3RlbnRLZWVwYWxpdmU=');

@$core.Deprecated('Use tunnelInspectionDescriptor instead')
const TunnelInspection$json = {
  '1': 'TunnelInspection',
  '2': [
    {'1': 'ok', '3': 1, '4': 1, '5': 8, '10': 'ok'},
    {'1': 'interface_name', '3': 2, '4': 1, '5': 9, '10': 'interfaceName'},
    {'1': 'mtu', '3': 3, '4': 1, '5': 13, '10': 'mtu'},
    {'1': 'listen_port', '3': 4, '4': 1, '5': 13, '10': 'listenPort'},
    {
      '1': 'peers',
      '3': 5,
      '4': 3,
      '5': 11,
      '6': '.client.v0.TunnelPeer',
      '10': 'peers'
    },
    {
      '1': 'failure',
      '3': 6,
      '4': 1,
      '5': 11,
      '6': '.client.v0.Failure',
      '10': 'failure'
    },
  ],
};

/// Descriptor for `TunnelInspection`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List tunnelInspectionDescriptor = $convert.base64Decode(
    'ChBUdW5uZWxJbnNwZWN0aW9uEg4KAm9rGAEgASgIUgJvaxIlCg5pbnRlcmZhY2VfbmFtZRgCIA'
    'EoCVINaW50ZXJmYWNlTmFtZRIQCgNtdHUYAyABKA1SA210dRIfCgtsaXN0ZW5fcG9ydBgEIAEo'
    'DVIKbGlzdGVuUG9ydBIrCgVwZWVycxgFIAMoCzIVLmNsaWVudC52MC5UdW5uZWxQZWVyUgVwZW'
    'VycxIsCgdmYWlsdXJlGAYgASgLMhIuY2xpZW50LnYwLkZhaWx1cmVSB2ZhaWx1cmU=');

@$core.Deprecated('Use interfaceDescriptor instead')
const Interface$json = {
  '1': 'Interface',
  '2': [
    {'1': 'name', '3': 1, '4': 1, '5': 9, '10': 'name'},
    {'1': 'index', '3': 2, '4': 1, '5': 13, '10': 'index'},
    {'1': 'mtu', '3': 3, '4': 1, '5': 13, '10': 'mtu'},
    {'1': 'addresses', '3': 4, '4': 3, '5': 9, '10': 'addresses'},
    {'1': 'prefixes', '3': 5, '4': 3, '5': 9, '10': 'prefixes'},
    {'1': 'flags', '3': 6, '4': 3, '5': 9, '10': 'flags'},
    {
      '1': 'failure',
      '3': 7,
      '4': 1,
      '5': 11,
      '6': '.client.v0.Failure',
      '10': 'failure'
    },
  ],
};

/// Descriptor for `Interface`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List interfaceDescriptor = $convert.base64Decode(
    'CglJbnRlcmZhY2USEgoEbmFtZRgBIAEoCVIEbmFtZRIUCgVpbmRleBgCIAEoDVIFaW5kZXgSEA'
    'oDbXR1GAMgASgNUgNtdHUSHAoJYWRkcmVzc2VzGAQgAygJUglhZGRyZXNzZXMSGgoIcHJlZml4'
    'ZXMYBSADKAlSCHByZWZpeGVzEhQKBWZsYWdzGAYgAygJUgVmbGFncxIsCgdmYWlsdXJlGAcgAS'
    'gLMhIuY2xpZW50LnYwLkZhaWx1cmVSB2ZhaWx1cmU=');

@$core.Deprecated('Use routeInspectionDescriptor instead')
const RouteInspection$json = {
  '1': 'RouteInspection',
  '2': [
    {'1': 'target', '3': 1, '4': 1, '5': 9, '10': 'target'},
    {'1': 'interface_name', '3': 2, '4': 1, '5': 9, '10': 'interfaceName'},
    {'1': 'uses_interface', '3': 3, '4': 1, '5': 8, '10': 'usesInterface'},
    {'1': 'peer_id', '3': 4, '4': 1, '5': 9, '10': 'peerId'},
    {
      '1': 'failure',
      '3': 5,
      '4': 1,
      '5': 11,
      '6': '.client.v0.Failure',
      '10': 'failure'
    },
  ],
};

/// Descriptor for `RouteInspection`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List routeInspectionDescriptor = $convert.base64Decode(
    'Cg9Sb3V0ZUluc3BlY3Rpb24SFgoGdGFyZ2V0GAEgASgJUgZ0YXJnZXQSJQoOaW50ZXJmYWNlX2'
    '5hbWUYAiABKAlSDWludGVyZmFjZU5hbWUSJQoOdXNlc19pbnRlcmZhY2UYAyABKAhSDXVzZXNJ'
    'bnRlcmZhY2USFwoHcGVlcl9pZBgEIAEoCVIGcGVlcklkEiwKB2ZhaWx1cmUYBSABKAsyEi5jbG'
    'llbnQudjAuRmFpbHVyZVIHZmFpbHVyZQ==');

@$core.Deprecated('Use routeConflictDescriptor instead')
const RouteConflict$json = {
  '1': 'RouteConflict',
  '2': [
    {'1': 'overlay_cidr', '3': 1, '4': 1, '5': 9, '10': 'overlayCidr'},
    {'1': 'local_prefix', '3': 2, '4': 1, '5': 9, '10': 'localPrefix'},
    {'1': 'interface_name', '3': 3, '4': 1, '5': 9, '10': 'interfaceName'},
    {'1': 'reason_key', '3': 4, '4': 1, '5': 9, '10': 'reasonKey'},
  ],
};

/// Descriptor for `RouteConflict`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List routeConflictDescriptor = $convert.base64Decode(
    'Cg1Sb3V0ZUNvbmZsaWN0EiEKDG92ZXJsYXlfY2lkchgBIAEoCVILb3ZlcmxheUNpZHISIQoMbG'
    '9jYWxfcHJlZml4GAIgASgJUgtsb2NhbFByZWZpeBIlCg5pbnRlcmZhY2VfbmFtZRgDIAEoCVIN'
    'aW50ZXJmYWNlTmFtZRIdCgpyZWFzb25fa2V5GAQgASgJUglyZWFzb25LZXk=');

@$core.Deprecated('Use dnsRecordDescriptor instead')
const DnsRecord$json = {
  '1': 'DnsRecord',
  '2': [
    {'1': 'node_id', '3': 1, '4': 1, '5': 9, '10': 'nodeId'},
    {'1': 'hostname', '3': 2, '4': 1, '5': 9, '10': 'hostname'},
    {'1': 'label', '3': 3, '4': 1, '5': 9, '10': 'label'},
    {'1': 'fqdn', '3': 4, '4': 1, '5': 9, '10': 'fqdn'},
    {'1': 'addresses', '3': 5, '4': 3, '5': 9, '10': 'addresses'},
  ],
};

/// Descriptor for `DnsRecord`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List dnsRecordDescriptor = $convert.base64Decode(
    'CglEbnNSZWNvcmQSFwoHbm9kZV9pZBgBIAEoCVIGbm9kZUlkEhoKCGhvc3RuYW1lGAIgASgJUg'
    'hob3N0bmFtZRIUCgVsYWJlbBgDIAEoCVIFbGFiZWwSEgoEZnFkbhgEIAEoCVIEZnFkbhIcCglh'
    'ZGRyZXNzZXMYBSADKAlSCWFkZHJlc3Nlcw==');

@$core.Deprecated('Use dnsDiagnosticsDescriptor instead')
const DnsDiagnostics$json = {
  '1': 'DnsDiagnostics',
  '2': [
    {'1': 'search_domain', '3': 1, '4': 1, '5': 9, '10': 'searchDomain'},
    {
      '1': 'ttl',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Duration',
      '10': 'ttl'
    },
    {'1': 'servers', '3': 3, '4': 3, '5': 9, '10': 'servers'},
    {
      '1': 'records',
      '3': 4,
      '4': 3,
      '5': 11,
      '6': '.client.v0.DnsRecord',
      '10': 'records'
    },
  ],
};

/// Descriptor for `DnsDiagnostics`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List dnsDiagnosticsDescriptor = $convert.base64Decode(
    'Cg5EbnNEaWFnbm9zdGljcxIjCg1zZWFyY2hfZG9tYWluGAEgASgJUgxzZWFyY2hEb21haW4SKw'
    'oDdHRsGAIgASgLMhkuZ29vZ2xlLnByb3RvYnVmLkR1cmF0aW9uUgN0dGwSGAoHc2VydmVycxgD'
    'IAMoCVIHc2VydmVycxIuCgdyZWNvcmRzGAQgAygLMhQuY2xpZW50LnYwLkRuc1JlY29yZFIHcm'
    'Vjb3Jkcw==');

@$core.Deprecated('Use logEntryDescriptor instead')
const LogEntry$json = {
  '1': 'LogEntry',
  '2': [
    {
      '1': 'timestamp',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'timestamp'
    },
    {'1': 'message', '3': 2, '4': 1, '5': 9, '10': 'message'},
  ],
};

/// Descriptor for `LogEntry`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List logEntryDescriptor = $convert.base64Decode(
    'CghMb2dFbnRyeRI4Cgl0aW1lc3RhbXAYASABKAsyGi5nb29nbGUucHJvdG9idWYuVGltZXN0YW'
    '1wUgl0aW1lc3RhbXASGAoHbWVzc2FnZRgCIAEoCVIHbWVzc2FnZQ==');

@$core.Deprecated('Use diagnosticsDescriptor instead')
const Diagnostics$json = {
  '1': 'Diagnostics',
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
      '1': 'client',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.client.v0.BuildIdentity',
      '10': 'client'
    },
    {'1': 'os_name', '3': 3, '4': 1, '5': 9, '10': 'osName'},
    {'1': 'os_version', '3': 4, '4': 1, '5': 9, '10': 'osVersion'},
    {'1': 'go_version', '3': 5, '4': 1, '5': 9, '10': 'goVersion'},
    {
      '1': 'status',
      '3': 6,
      '4': 1,
      '5': 11,
      '6': '.client.v0.Status',
      '10': 'status'
    },
    {
      '1': 'tunnel',
      '3': 7,
      '4': 1,
      '5': 11,
      '6': '.client.v0.TunnelInspection',
      '10': 'tunnel'
    },
    {
      '1': 'interfaces',
      '3': 8,
      '4': 3,
      '5': 11,
      '6': '.client.v0.Interface',
      '10': 'interfaces'
    },
    {
      '1': 'routes',
      '3': 9,
      '4': 3,
      '5': 11,
      '6': '.client.v0.RouteInspection',
      '10': 'routes'
    },
    {
      '1': 'route_conflicts',
      '3': 10,
      '4': 3,
      '5': 11,
      '6': '.client.v0.RouteConflict',
      '10': 'routeConflicts'
    },
    {
      '1': 'dns',
      '3': 11,
      '4': 1,
      '5': 11,
      '6': '.client.v0.DnsDiagnostics',
      '10': 'dns'
    },
    {
      '1': 'recent_logs',
      '3': 12,
      '4': 3,
      '5': 11,
      '6': '.client.v0.LogEntry',
      '10': 'recentLogs'
    },
    {
      '1': 'failures',
      '3': 13,
      '4': 3,
      '5': 11,
      '6': '.client.v0.Failure',
      '10': 'failures'
    },
    {'1': 'truncated', '3': 14, '4': 1, '5': 8, '10': 'truncated'},
    {
      '1': 'peers',
      '3': 15,
      '4': 3,
      '5': 11,
      '6': '.client.v0.Peer',
      '10': 'peers'
    },
    {
      '1': 'default_route_present',
      '3': 16,
      '4': 1,
      '5': 8,
      '10': 'defaultRoutePresent'
    },
  ],
};

/// Descriptor for `Diagnostics`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List diagnosticsDescriptor = $convert.base64Decode(
    'CgtEaWFnbm9zdGljcxI3CghtZXRhZGF0YRgBIAEoCzIbLmNsaWVudC52MC5TbmFwc2hvdE1ldG'
    'FkYXRhUghtZXRhZGF0YRIwCgZjbGllbnQYAiABKAsyGC5jbGllbnQudjAuQnVpbGRJZGVudGl0'
    'eVIGY2xpZW50EhcKB29zX25hbWUYAyABKAlSBm9zTmFtZRIdCgpvc192ZXJzaW9uGAQgASgJUg'
    'lvc1ZlcnNpb24SHQoKZ29fdmVyc2lvbhgFIAEoCVIJZ29WZXJzaW9uEikKBnN0YXR1cxgGIAEo'
    'CzIRLmNsaWVudC52MC5TdGF0dXNSBnN0YXR1cxIzCgZ0dW5uZWwYByABKAsyGy5jbGllbnQudj'
    'AuVHVubmVsSW5zcGVjdGlvblIGdHVubmVsEjQKCmludGVyZmFjZXMYCCADKAsyFC5jbGllbnQu'
    'djAuSW50ZXJmYWNlUgppbnRlcmZhY2VzEjIKBnJvdXRlcxgJIAMoCzIaLmNsaWVudC52MC5Sb3'
    'V0ZUluc3BlY3Rpb25SBnJvdXRlcxJBCg9yb3V0ZV9jb25mbGljdHMYCiADKAsyGC5jbGllbnQu'
    'djAuUm91dGVDb25mbGljdFIOcm91dGVDb25mbGljdHMSKwoDZG5zGAsgASgLMhkuY2xpZW50Ln'
    'YwLkRuc0RpYWdub3N0aWNzUgNkbnMSNAoLcmVjZW50X2xvZ3MYDCADKAsyEy5jbGllbnQudjAu'
    'TG9nRW50cnlSCnJlY2VudExvZ3MSLgoIZmFpbHVyZXMYDSADKAsyEi5jbGllbnQudjAuRmFpbH'
    'VyZVIIZmFpbHVyZXMSHAoJdHJ1bmNhdGVkGA4gASgIUgl0cnVuY2F0ZWQSJQoFcGVlcnMYDyAD'
    'KAsyDy5jbGllbnQudjAuUGVlclIFcGVlcnMSMgoVZGVmYXVsdF9yb3V0ZV9wcmVzZW50GBAgAS'
    'gIUhNkZWZhdWx0Um91dGVQcmVzZW50');
