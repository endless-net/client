// This is a generated file - do not edit.
//
// Generated from client/v0/service.proto.

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

@$core.Deprecated('Use domainDescriptor instead')
const Domain$json = {
  '1': 'Domain',
  '2': [
    {'1': 'DOMAIN_UNSPECIFIED', '2': 0},
    {'1': 'DOMAIN_PROFILES', '2': 1},
    {'1': 'DOMAIN_NETWORKS', '2': 2},
    {'1': 'DOMAIN_PEERS', '2': 3},
    {'1': 'DOMAIN_EXIT_NODE', '2': 4},
    {'1': 'DOMAIN_PREFERENCES', '2': 5},
    {'1': 'DOMAIN_MANAGED_SETTINGS', '2': 6},
    {'1': 'DOMAIN_RESOURCES', '2': 7},
    {'1': 'DOMAIN_UPDATES', '2': 8},
    {'1': 'DOMAIN_SUPPORT', '2': 9},
    {'1': 'DOMAIN_SERVER_IDENTITY', '2': 10},
    {'1': 'DOMAIN_SESSION', '2': 11},
  ],
};

/// Descriptor for `Domain`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List domainDescriptor = $convert.base64Decode(
    'CgZEb21haW4SFgoSRE9NQUlOX1VOU1BFQ0lGSUVEEAASEwoPRE9NQUlOX1BST0ZJTEVTEAESEw'
    'oPRE9NQUlOX05FVFdPUktTEAISEAoMRE9NQUlOX1BFRVJTEAMSFAoQRE9NQUlOX0VYSVRfTk9E'
    'RRAEEhYKEkRPTUFJTl9QUkVGRVJFTkNFUxAFEhsKF0RPTUFJTl9NQU5BR0VEX1NFVFRJTkdTEA'
    'YSFAoQRE9NQUlOX1JFU09VUkNFUxAHEhIKDkRPTUFJTl9VUERBVEVTEAgSEgoORE9NQUlOX1NV'
    'UFBPUlQQCRIaChZET01BSU5fU0VSVkVSX0lERU5USVRZEAoSEgoORE9NQUlOX1NFU1NJT04QCw'
    '==');

@$core.Deprecated('Use getRuntimeInfoRequestDescriptor instead')
const GetRuntimeInfoRequest$json = {
  '1': 'GetRuntimeInfoRequest',
};

/// Descriptor for `GetRuntimeInfoRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List getRuntimeInfoRequestDescriptor =
    $convert.base64Decode('ChVHZXRSdW50aW1lSW5mb1JlcXVlc3Q=');

@$core.Deprecated('Use getRuntimeInfoResponseDescriptor instead')
const GetRuntimeInfoResponse$json = {
  '1': 'GetRuntimeInfoResponse',
  '2': [
    {
      '1': 'runtime',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.RuntimeInfo',
      '10': 'runtime'
    },
  ],
};

/// Descriptor for `GetRuntimeInfoResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List getRuntimeInfoResponseDescriptor =
    $convert.base64Decode(
        'ChZHZXRSdW50aW1lSW5mb1Jlc3BvbnNlEjAKB3J1bnRpbWUYASABKAsyFi5jbGllbnQudjAuUn'
        'VudGltZUluZm9SB3J1bnRpbWU=');

@$core.Deprecated('Use getStatusRequestDescriptor instead')
const GetStatusRequest$json = {
  '1': 'GetStatusRequest',
};

/// Descriptor for `GetStatusRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List getStatusRequestDescriptor =
    $convert.base64Decode('ChBHZXRTdGF0dXNSZXF1ZXN0');

@$core.Deprecated('Use getStatusResponseDescriptor instead')
const GetStatusResponse$json = {
  '1': 'GetStatusResponse',
  '2': [
    {
      '1': 'status',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.Status',
      '10': 'status'
    },
  ],
};

/// Descriptor for `GetStatusResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List getStatusResponseDescriptor = $convert.base64Decode(
    'ChFHZXRTdGF0dXNSZXNwb25zZRIpCgZzdGF0dXMYASABKAsyES5jbGllbnQudjAuU3RhdHVzUg'
    'ZzdGF0dXM=');

@$core.Deprecated('Use watchEventsRequestDescriptor instead')
const WatchEventsRequest$json = {
  '1': 'WatchEventsRequest',
};

/// Descriptor for `WatchEventsRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List watchEventsRequestDescriptor =
    $convert.base64Decode('ChJXYXRjaEV2ZW50c1JlcXVlc3Q=');

@$core.Deprecated('Use enrollRequestDescriptor instead')
const EnrollRequest$json = {
  '1': 'EnrollRequest',
  '2': [
    {
      '1': 'mutation',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.MutationContext',
      '10': 'mutation'
    },
    {
      '1': 'profile',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ProfileRef',
      '10': 'profile'
    },
    {
      '1': 'mode',
      '3': 3,
      '4': 1,
      '5': 14,
      '6': '.client.v0.EnrollmentMode',
      '10': 'mode'
    },
    {'1': 'hostname', '3': 4, '4': 1, '5': 9, '10': 'hostname'},
    {
      '1': 'enrollment_token',
      '3': 5,
      '4': 1,
      '5': 9,
      '9': 0,
      '10': 'enrollmentToken'
    },
    {
      '1': 'browser_login',
      '3': 6,
      '4': 1,
      '5': 8,
      '9': 0,
      '10': 'browserLogin'
    },
  ],
  '8': [
    {'1': 'authentication'},
  ],
};

/// Descriptor for `EnrollRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List enrollRequestDescriptor = $convert.base64Decode(
    'Cg1FbnJvbGxSZXF1ZXN0EjYKCG11dGF0aW9uGAEgASgLMhouY2xpZW50LnYwLk11dGF0aW9uQ2'
    '9udGV4dFIIbXV0YXRpb24SLwoHcHJvZmlsZRgCIAEoCzIVLmNsaWVudC52MC5Qcm9maWxlUmVm'
    'Ugdwcm9maWxlEi0KBG1vZGUYAyABKA4yGS5jbGllbnQudjAuRW5yb2xsbWVudE1vZGVSBG1vZG'
    'USGgoIaG9zdG5hbWUYBCABKAlSCGhvc3RuYW1lEisKEGVucm9sbG1lbnRfdG9rZW4YBSABKAlI'
    'AFIPZW5yb2xsbWVudFRva2VuEiUKDWJyb3dzZXJfbG9naW4YBiABKAhIAFIMYnJvd3NlckxvZ2'
    'luQhAKDmF1dGhlbnRpY2F0aW9u');

@$core.Deprecated('Use enrollResponseDescriptor instead')
const EnrollResponse$json = {
  '1': 'EnrollResponse',
  '2': [
    {
      '1': 'enrollment',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.EnrollmentResult',
      '9': 0,
      '10': 'enrollment'
    },
    {
      '1': 'browser_login',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.client.v0.BrowserLoginChallenge',
      '9': 0,
      '10': 'browserLogin'
    },
  ],
  '8': [
    {'1': 'result'},
  ],
};

/// Descriptor for `EnrollResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List enrollResponseDescriptor = $convert.base64Decode(
    'Cg5FbnJvbGxSZXNwb25zZRI9CgplbnJvbGxtZW50GAEgASgLMhsuY2xpZW50LnYwLkVucm9sbG'
    '1lbnRSZXN1bHRIAFIKZW5yb2xsbWVudBJHCg1icm93c2VyX2xvZ2luGAIgASgLMiAuY2xpZW50'
    'LnYwLkJyb3dzZXJMb2dpbkNoYWxsZW5nZUgAUgxicm93c2VyTG9naW5CCAoGcmVzdWx0');

@$core.Deprecated('Use waitBrowserEnrollmentRequestDescriptor instead')
const WaitBrowserEnrollmentRequest$json = {
  '1': 'WaitBrowserEnrollmentRequest',
  '2': [
    {
      '1': 'profile',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ProfileRef',
      '10': 'profile'
    },
    {'1': 'user_code', '3': 2, '4': 1, '5': 9, '10': 'userCode'},
  ],
};

/// Descriptor for `WaitBrowserEnrollmentRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List waitBrowserEnrollmentRequestDescriptor =
    $convert.base64Decode(
        'ChxXYWl0QnJvd3NlckVucm9sbG1lbnRSZXF1ZXN0Ei8KB3Byb2ZpbGUYASABKAsyFS5jbGllbn'
        'QudjAuUHJvZmlsZVJlZlIHcHJvZmlsZRIbCgl1c2VyX2NvZGUYAiABKAlSCHVzZXJDb2Rl');

@$core.Deprecated('Use waitBrowserEnrollmentResponseDescriptor instead')
const WaitBrowserEnrollmentResponse$json = {
  '1': 'WaitBrowserEnrollmentResponse',
  '2': [
    {
      '1': 'enrollment',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.EnrollmentResult',
      '10': 'enrollment'
    },
  ],
};

/// Descriptor for `WaitBrowserEnrollmentResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List waitBrowserEnrollmentResponseDescriptor =
    $convert.base64Decode(
        'Ch1XYWl0QnJvd3NlckVucm9sbG1lbnRSZXNwb25zZRI7CgplbnJvbGxtZW50GAEgASgLMhsuY2'
        'xpZW50LnYwLkVucm9sbG1lbnRSZXN1bHRSCmVucm9sbG1lbnQ=');

@$core.Deprecated('Use connectRequestDescriptor instead')
const ConnectRequest$json = {
  '1': 'ConnectRequest',
  '2': [
    {
      '1': 'mutation',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.MutationContext',
      '10': 'mutation'
    },
    {
      '1': 'profile',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ProfileRef',
      '10': 'profile'
    },
    {'1': 'async', '3': 3, '4': 1, '5': 8, '10': 'async'},
  ],
};

/// Descriptor for `ConnectRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List connectRequestDescriptor = $convert.base64Decode(
    'Cg5Db25uZWN0UmVxdWVzdBI2CghtdXRhdGlvbhgBIAEoCzIaLmNsaWVudC52MC5NdXRhdGlvbk'
    'NvbnRleHRSCG11dGF0aW9uEi8KB3Byb2ZpbGUYAiABKAsyFS5jbGllbnQudjAuUHJvZmlsZVJl'
    'ZlIHcHJvZmlsZRIUCgVhc3luYxgDIAEoCFIFYXN5bmM=');

@$core.Deprecated('Use connectResponseDescriptor instead')
const ConnectResponse$json = {
  '1': 'ConnectResponse',
};

/// Descriptor for `ConnectResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List connectResponseDescriptor =
    $convert.base64Decode('Cg9Db25uZWN0UmVzcG9uc2U=');

@$core.Deprecated('Use disconnectRequestDescriptor instead')
const DisconnectRequest$json = {
  '1': 'DisconnectRequest',
  '2': [
    {
      '1': 'mutation',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.MutationContext',
      '10': 'mutation'
    },
    {
      '1': 'profile',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ProfileRef',
      '10': 'profile'
    },
  ],
};

/// Descriptor for `DisconnectRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List disconnectRequestDescriptor = $convert.base64Decode(
    'ChFEaXNjb25uZWN0UmVxdWVzdBI2CghtdXRhdGlvbhgBIAEoCzIaLmNsaWVudC52MC5NdXRhdG'
    'lvbkNvbnRleHRSCG11dGF0aW9uEi8KB3Byb2ZpbGUYAiABKAsyFS5jbGllbnQudjAuUHJvZmls'
    'ZVJlZlIHcHJvZmlsZQ==');

@$core.Deprecated('Use disconnectResponseDescriptor instead')
const DisconnectResponse$json = {
  '1': 'DisconnectResponse',
  '2': [
    {
      '1': 'change',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ChangeResult',
      '10': 'change'
    },
  ],
};

/// Descriptor for `DisconnectResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List disconnectResponseDescriptor = $convert.base64Decode(
    'ChJEaXNjb25uZWN0UmVzcG9uc2USLwoGY2hhbmdlGAEgASgLMhcuY2xpZW50LnYwLkNoYW5nZV'
    'Jlc3VsdFIGY2hhbmdl');

@$core.Deprecated('Use getServerIdentityRequestDescriptor instead')
const GetServerIdentityRequest$json = {
  '1': 'GetServerIdentityRequest',
  '2': [
    {
      '1': 'profile',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ProfileRef',
      '10': 'profile'
    },
  ],
};

/// Descriptor for `GetServerIdentityRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List getServerIdentityRequestDescriptor =
    $convert.base64Decode(
        'ChhHZXRTZXJ2ZXJJZGVudGl0eVJlcXVlc3QSLwoHcHJvZmlsZRgBIAEoCzIVLmNsaWVudC52MC'
        '5Qcm9maWxlUmVmUgdwcm9maWxl');

@$core.Deprecated('Use getServerIdentityResponseDescriptor instead')
const GetServerIdentityResponse$json = {
  '1': 'GetServerIdentityResponse',
  '2': [
    {
      '1': 'identity',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ServerIdentity',
      '10': 'identity'
    },
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

/// Descriptor for `GetServerIdentityResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List getServerIdentityResponseDescriptor = $convert.base64Decode(
    'ChlHZXRTZXJ2ZXJJZGVudGl0eVJlc3BvbnNlEjUKCGlkZW50aXR5GAEgASgLMhkuY2xpZW50Ln'
    'YwLlNlcnZlcklkZW50aXR5UghpZGVudGl0eRI3CghtZXRhZGF0YRgCIAEoCzIbLmNsaWVudC52'
    'MC5TbmFwc2hvdE1ldGFkYXRhUghtZXRhZGF0YQ==');

@$core.Deprecated('Use trustServerIdentityRequestDescriptor instead')
const TrustServerIdentityRequest$json = {
  '1': 'TrustServerIdentityRequest',
  '2': [
    {
      '1': 'mutation',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.MutationContext',
      '10': 'mutation'
    },
    {
      '1': 'profile',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ProfileRef',
      '10': 'profile'
    },
    {
      '1': 'confirmed_control_origin',
      '3': 3,
      '4': 1,
      '5': 9,
      '10': 'confirmedControlOrigin'
    },
    {'1': 'confirmed_key_id', '3': 4, '4': 1, '5': 9, '10': 'confirmedKeyId'},
    {
      '1': 'confirmed_announcement_id',
      '3': 5,
      '4': 1,
      '5': 9,
      '10': 'confirmedAnnouncementId'
    },
  ],
};

/// Descriptor for `TrustServerIdentityRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List trustServerIdentityRequestDescriptor = $convert.base64Decode(
    'ChpUcnVzdFNlcnZlcklkZW50aXR5UmVxdWVzdBI2CghtdXRhdGlvbhgBIAEoCzIaLmNsaWVudC'
    '52MC5NdXRhdGlvbkNvbnRleHRSCG11dGF0aW9uEi8KB3Byb2ZpbGUYAiABKAsyFS5jbGllbnQu'
    'djAuUHJvZmlsZVJlZlIHcHJvZmlsZRI4Chhjb25maXJtZWRfY29udHJvbF9vcmlnaW4YAyABKA'
    'lSFmNvbmZpcm1lZENvbnRyb2xPcmlnaW4SKAoQY29uZmlybWVkX2tleV9pZBgEIAEoCVIOY29u'
    'ZmlybWVkS2V5SWQSOgoZY29uZmlybWVkX2Fubm91bmNlbWVudF9pZBgFIAEoCVIXY29uZmlybW'
    'VkQW5ub3VuY2VtZW50SWQ=');

@$core.Deprecated('Use trustServerIdentityResponseDescriptor instead')
const TrustServerIdentityResponse$json = {
  '1': 'TrustServerIdentityResponse',
  '2': [
    {
      '1': 'change',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ChangeResult',
      '10': 'change'
    },
  ],
};

/// Descriptor for `TrustServerIdentityResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List trustServerIdentityResponseDescriptor =
    $convert.base64Decode(
        'ChtUcnVzdFNlcnZlcklkZW50aXR5UmVzcG9uc2USLwoGY2hhbmdlGAEgASgLMhcuY2xpZW50Ln'
        'YwLkNoYW5nZVJlc3VsdFIGY2hhbmdl');

@$core.Deprecated('Use logoutRequestDescriptor instead')
const LogoutRequest$json = {
  '1': 'LogoutRequest',
  '2': [
    {
      '1': 'mutation',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.MutationContext',
      '10': 'mutation'
    },
    {
      '1': 'profile',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ProfileRef',
      '10': 'profile'
    },
  ],
};

/// Descriptor for `LogoutRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List logoutRequestDescriptor = $convert.base64Decode(
    'Cg1Mb2dvdXRSZXF1ZXN0EjYKCG11dGF0aW9uGAEgASgLMhouY2xpZW50LnYwLk11dGF0aW9uQ2'
    '9udGV4dFIIbXV0YXRpb24SLwoHcHJvZmlsZRgCIAEoCzIVLmNsaWVudC52MC5Qcm9maWxlUmVm'
    'Ugdwcm9maWxl');

@$core.Deprecated('Use logoutResponseDescriptor instead')
const LogoutResponse$json = {
  '1': 'LogoutResponse',
  '2': [
    {
      '1': 'cleanup',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.CleanupResult',
      '10': 'cleanup'
    },
  ],
};

/// Descriptor for `LogoutResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List logoutResponseDescriptor = $convert.base64Decode(
    'Cg5Mb2dvdXRSZXNwb25zZRIyCgdjbGVhbnVwGAEgASgLMhguY2xpZW50LnYwLkNsZWFudXBSZX'
    'N1bHRSB2NsZWFudXA=');

@$core.Deprecated('Use forgetLocalEnrollmentRequestDescriptor instead')
const ForgetLocalEnrollmentRequest$json = {
  '1': 'ForgetLocalEnrollmentRequest',
  '2': [
    {
      '1': 'mutation',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.MutationContext',
      '10': 'mutation'
    },
    {
      '1': 'profile',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ProfileRef',
      '10': 'profile'
    },
    {'1': 'confirmed', '3': 3, '4': 1, '5': 8, '10': 'confirmed'},
  ],
};

/// Descriptor for `ForgetLocalEnrollmentRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List forgetLocalEnrollmentRequestDescriptor = $convert.base64Decode(
    'ChxGb3JnZXRMb2NhbEVucm9sbG1lbnRSZXF1ZXN0EjYKCG11dGF0aW9uGAEgASgLMhouY2xpZW'
    '50LnYwLk11dGF0aW9uQ29udGV4dFIIbXV0YXRpb24SLwoHcHJvZmlsZRgCIAEoCzIVLmNsaWVu'
    'dC52MC5Qcm9maWxlUmVmUgdwcm9maWxlEhwKCWNvbmZpcm1lZBgDIAEoCFIJY29uZmlybWVk');

@$core.Deprecated('Use forgetLocalEnrollmentResponseDescriptor instead')
const ForgetLocalEnrollmentResponse$json = {
  '1': 'ForgetLocalEnrollmentResponse',
  '2': [
    {
      '1': 'cleanup',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.CleanupResult',
      '10': 'cleanup'
    },
  ],
};

/// Descriptor for `ForgetLocalEnrollmentResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List forgetLocalEnrollmentResponseDescriptor =
    $convert.base64Decode(
        'Ch1Gb3JnZXRMb2NhbEVucm9sbG1lbnRSZXNwb25zZRIyCgdjbGVhbnVwGAEgASgLMhguY2xpZW'
        '50LnYwLkNsZWFudXBSZXN1bHRSB2NsZWFudXA=');

@$core.Deprecated('Use listNetworksRequestDescriptor instead')
const ListNetworksRequest$json = {
  '1': 'ListNetworksRequest',
  '2': [
    {
      '1': 'profile',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ProfileRef',
      '10': 'profile'
    },
    {
      '1': 'page',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.client.v0.PageRequest',
      '10': 'page'
    },
  ],
};

/// Descriptor for `ListNetworksRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listNetworksRequestDescriptor = $convert.base64Decode(
    'ChNMaXN0TmV0d29ya3NSZXF1ZXN0Ei8KB3Byb2ZpbGUYASABKAsyFS5jbGllbnQudjAuUHJvZm'
    'lsZVJlZlIHcHJvZmlsZRIqCgRwYWdlGAIgASgLMhYuY2xpZW50LnYwLlBhZ2VSZXF1ZXN0UgRw'
    'YWdl');

@$core.Deprecated('Use listNetworksResponseDescriptor instead')
const ListNetworksResponse$json = {
  '1': 'ListNetworksResponse',
  '2': [
    {
      '1': 'networks',
      '3': 1,
      '4': 3,
      '5': 11,
      '6': '.client.v0.Network',
      '10': 'networks'
    },
    {
      '1': 'selected_network_id',
      '3': 2,
      '4': 1,
      '5': 9,
      '10': 'selectedNetworkId'
    },
    {
      '1': 'page',
      '3': 3,
      '4': 1,
      '5': 11,
      '6': '.client.v0.PageResponse',
      '10': 'page'
    },
  ],
};

/// Descriptor for `ListNetworksResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listNetworksResponseDescriptor = $convert.base64Decode(
    'ChRMaXN0TmV0d29ya3NSZXNwb25zZRIuCghuZXR3b3JrcxgBIAMoCzISLmNsaWVudC52MC5OZX'
    'R3b3JrUghuZXR3b3JrcxIuChNzZWxlY3RlZF9uZXR3b3JrX2lkGAIgASgJUhFzZWxlY3RlZE5l'
    'dHdvcmtJZBIrCgRwYWdlGAMgASgLMhcuY2xpZW50LnYwLlBhZ2VSZXNwb25zZVIEcGFnZQ==');

@$core.Deprecated('Use selectNetworkRequestDescriptor instead')
const SelectNetworkRequest$json = {
  '1': 'SelectNetworkRequest',
  '2': [
    {
      '1': 'mutation',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.MutationContext',
      '10': 'mutation'
    },
    {
      '1': 'profile',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ProfileRef',
      '10': 'profile'
    },
    {'1': 'network_id', '3': 3, '4': 1, '5': 9, '10': 'networkId'},
  ],
};

/// Descriptor for `SelectNetworkRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List selectNetworkRequestDescriptor = $convert.base64Decode(
    'ChRTZWxlY3ROZXR3b3JrUmVxdWVzdBI2CghtdXRhdGlvbhgBIAEoCzIaLmNsaWVudC52MC5NdX'
    'RhdGlvbkNvbnRleHRSCG11dGF0aW9uEi8KB3Byb2ZpbGUYAiABKAsyFS5jbGllbnQudjAuUHJv'
    'ZmlsZVJlZlIHcHJvZmlsZRIdCgpuZXR3b3JrX2lkGAMgASgJUgluZXR3b3JrSWQ=');

@$core.Deprecated('Use selectNetworkResponseDescriptor instead')
const SelectNetworkResponse$json = {
  '1': 'SelectNetworkResponse',
  '2': [
    {
      '1': 'selection',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.SelectionResult',
      '10': 'selection'
    },
    {
      '1': 'continuity',
      '3': 2,
      '4': 1,
      '5': 14,
      '6': '.client.v0.ConnectionContinuity',
      '10': 'continuity'
    },
  ],
};

/// Descriptor for `SelectNetworkResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List selectNetworkResponseDescriptor = $convert.base64Decode(
    'ChVTZWxlY3ROZXR3b3JrUmVzcG9uc2USOAoJc2VsZWN0aW9uGAEgASgLMhouY2xpZW50LnYwLl'
    'NlbGVjdGlvblJlc3VsdFIJc2VsZWN0aW9uEj8KCmNvbnRpbnVpdHkYAiABKA4yHy5jbGllbnQu'
    'djAuQ29ubmVjdGlvbkNvbnRpbnVpdHlSCmNvbnRpbnVpdHk=');

@$core.Deprecated('Use listPeersRequestDescriptor instead')
const ListPeersRequest$json = {
  '1': 'ListPeersRequest',
  '2': [
    {
      '1': 'profile',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ProfileRef',
      '10': 'profile'
    },
    {
      '1': 'page',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.client.v0.PageRequest',
      '10': 'page'
    },
    {'1': 'search', '3': 3, '4': 1, '5': 9, '10': 'search'},
  ],
};

/// Descriptor for `ListPeersRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listPeersRequestDescriptor = $convert.base64Decode(
    'ChBMaXN0UGVlcnNSZXF1ZXN0Ei8KB3Byb2ZpbGUYASABKAsyFS5jbGllbnQudjAuUHJvZmlsZV'
    'JlZlIHcHJvZmlsZRIqCgRwYWdlGAIgASgLMhYuY2xpZW50LnYwLlBhZ2VSZXF1ZXN0UgRwYWdl'
    'EhYKBnNlYXJjaBgDIAEoCVIGc2VhcmNo');

@$core.Deprecated('Use listPeersResponseDescriptor instead')
const ListPeersResponse$json = {
  '1': 'ListPeersResponse',
  '2': [
    {
      '1': 'peers',
      '3': 1,
      '4': 3,
      '5': 11,
      '6': '.client.v0.Peer',
      '10': 'peers'
    },
    {
      '1': 'page',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.client.v0.PageResponse',
      '10': 'page'
    },
    {
      '1': 'snapshot_state',
      '3': 3,
      '4': 1,
      '5': 14,
      '6': '.client.v0.AgentSnapshotState',
      '10': 'snapshotState'
    },
    {'1': 'map_revision', '3': 4, '4': 1, '5': 4, '10': 'mapRevision'},
    {
      '1': 'target_map_revision',
      '3': 5,
      '4': 1,
      '5': 4,
      '10': 'targetMapRevision'
    },
  ],
};

/// Descriptor for `ListPeersResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listPeersResponseDescriptor = $convert.base64Decode(
    'ChFMaXN0UGVlcnNSZXNwb25zZRIlCgVwZWVycxgBIAMoCzIPLmNsaWVudC52MC5QZWVyUgVwZW'
    'VycxIrCgRwYWdlGAIgASgLMhcuY2xpZW50LnYwLlBhZ2VSZXNwb25zZVIEcGFnZRJECg5zbmFw'
    'c2hvdF9zdGF0ZRgDIAEoDjIdLmNsaWVudC52MC5BZ2VudFNuYXBzaG90U3RhdGVSDXNuYXBzaG'
    '90U3RhdGUSIQoMbWFwX3JldmlzaW9uGAQgASgEUgttYXBSZXZpc2lvbhIuChN0YXJnZXRfbWFw'
    'X3JldmlzaW9uGAUgASgEUhF0YXJnZXRNYXBSZXZpc2lvbg==');

@$core.Deprecated('Use getDiagnosticsRequestDescriptor instead')
const GetDiagnosticsRequest$json = {
  '1': 'GetDiagnosticsRequest',
  '2': [
    {
      '1': 'profile',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ProfileRef',
      '10': 'profile'
    },
  ],
};

/// Descriptor for `GetDiagnosticsRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List getDiagnosticsRequestDescriptor = $convert.base64Decode(
    'ChVHZXREaWFnbm9zdGljc1JlcXVlc3QSLwoHcHJvZmlsZRgBIAEoCzIVLmNsaWVudC52MC5Qcm'
    '9maWxlUmVmUgdwcm9maWxl');

@$core.Deprecated('Use getDiagnosticsResponseDescriptor instead')
const GetDiagnosticsResponse$json = {
  '1': 'GetDiagnosticsResponse',
  '2': [
    {
      '1': 'diagnostics',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.Diagnostics',
      '10': 'diagnostics'
    },
  ],
};

/// Descriptor for `GetDiagnosticsResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List getDiagnosticsResponseDescriptor =
    $convert.base64Decode(
        'ChZHZXREaWFnbm9zdGljc1Jlc3BvbnNlEjgKC2RpYWdub3N0aWNzGAEgASgLMhYuY2xpZW50Ln'
        'YwLkRpYWdub3N0aWNzUgtkaWFnbm9zdGljcw==');

@$core.Deprecated('Use createDiagnosticsBundleRequestDescriptor instead')
const CreateDiagnosticsBundleRequest$json = {
  '1': 'CreateDiagnosticsBundleRequest',
  '2': [
    {
      '1': 'mutation',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.MutationContext',
      '10': 'mutation'
    },
    {
      '1': 'profile',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ProfileRef',
      '10': 'profile'
    },
  ],
};

/// Descriptor for `CreateDiagnosticsBundleRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List createDiagnosticsBundleRequestDescriptor =
    $convert.base64Decode(
        'Ch5DcmVhdGVEaWFnbm9zdGljc0J1bmRsZVJlcXVlc3QSNgoIbXV0YXRpb24YASABKAsyGi5jbG'
        'llbnQudjAuTXV0YXRpb25Db250ZXh0UghtdXRhdGlvbhIvCgdwcm9maWxlGAIgASgLMhUuY2xp'
        'ZW50LnYwLlByb2ZpbGVSZWZSB3Byb2ZpbGU=');

@$core.Deprecated('Use createDiagnosticsBundleResponseDescriptor instead')
const CreateDiagnosticsBundleResponse$json = {
  '1': 'CreateDiagnosticsBundleResponse',
  '2': [
    {
      '1': 'bundle',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.BundleResult',
      '10': 'bundle'
    },
  ],
};

/// Descriptor for `CreateDiagnosticsBundleResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List createDiagnosticsBundleResponseDescriptor =
    $convert.base64Decode(
        'Ch9DcmVhdGVEaWFnbm9zdGljc0J1bmRsZVJlc3BvbnNlEi8KBmJ1bmRsZRgBIAEoCzIXLmNsaW'
        'VudC52MC5CdW5kbGVSZXN1bHRSBmJ1bmRsZQ==');

@$core.Deprecated('Use readDiagnosticsBundleRequestDescriptor instead')
const ReadDiagnosticsBundleRequest$json = {
  '1': 'ReadDiagnosticsBundleRequest',
  '2': [
    {'1': 'bundle_id', '3': 1, '4': 1, '5': 9, '10': 'bundleId'},
    {'1': 'offset', '3': 2, '4': 1, '5': 4, '10': 'offset'},
    {'1': 'max_bytes', '3': 3, '4': 1, '5': 13, '10': 'maxBytes'},
  ],
};

/// Descriptor for `ReadDiagnosticsBundleRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List readDiagnosticsBundleRequestDescriptor =
    $convert.base64Decode(
        'ChxSZWFkRGlhZ25vc3RpY3NCdW5kbGVSZXF1ZXN0EhsKCWJ1bmRsZV9pZBgBIAEoCVIIYnVuZG'
        'xlSWQSFgoGb2Zmc2V0GAIgASgEUgZvZmZzZXQSGwoJbWF4X2J5dGVzGAMgASgNUghtYXhCeXRl'
        'cw==');

@$core.Deprecated('Use readDiagnosticsBundleResponseDescriptor instead')
const ReadDiagnosticsBundleResponse$json = {
  '1': 'ReadDiagnosticsBundleResponse',
  '2': [
    {'1': 'data', '3': 1, '4': 1, '5': 12, '10': 'data'},
    {'1': 'next_offset', '3': 2, '4': 1, '5': 4, '10': 'nextOffset'},
    {'1': 'eof', '3': 3, '4': 1, '5': 8, '10': 'eof'},
  ],
};

/// Descriptor for `ReadDiagnosticsBundleResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List readDiagnosticsBundleResponseDescriptor =
    $convert.base64Decode(
        'Ch1SZWFkRGlhZ25vc3RpY3NCdW5kbGVSZXNwb25zZRISCgRkYXRhGAEgASgMUgRkYXRhEh8KC2'
        '5leHRfb2Zmc2V0GAIgASgEUgpuZXh0T2Zmc2V0EhAKA2VvZhgDIAEoCFIDZW9m');

@$core.Deprecated('Use listRecentLogsRequestDescriptor instead')
const ListRecentLogsRequest$json = {
  '1': 'ListRecentLogsRequest',
  '2': [
    {
      '1': 'profile',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ProfileRef',
      '10': 'profile'
    },
    {
      '1': 'page',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.client.v0.PageRequest',
      '10': 'page'
    },
  ],
};

/// Descriptor for `ListRecentLogsRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listRecentLogsRequestDescriptor = $convert.base64Decode(
    'ChVMaXN0UmVjZW50TG9nc1JlcXVlc3QSLwoHcHJvZmlsZRgBIAEoCzIVLmNsaWVudC52MC5Qcm'
    '9maWxlUmVmUgdwcm9maWxlEioKBHBhZ2UYAiABKAsyFi5jbGllbnQudjAuUGFnZVJlcXVlc3RS'
    'BHBhZ2U=');

@$core.Deprecated('Use listRecentLogsResponseDescriptor instead')
const ListRecentLogsResponse$json = {
  '1': 'ListRecentLogsResponse',
  '2': [
    {
      '1': 'logs',
      '3': 1,
      '4': 3,
      '5': 11,
      '6': '.client.v0.LogEntry',
      '10': 'logs'
    },
    {
      '1': 'page',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.client.v0.PageResponse',
      '10': 'page'
    },
  ],
};

/// Descriptor for `ListRecentLogsResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listRecentLogsResponseDescriptor = $convert.base64Decode(
    'ChZMaXN0UmVjZW50TG9nc1Jlc3BvbnNlEicKBGxvZ3MYASADKAsyEy5jbGllbnQudjAuTG9nRW'
    '50cnlSBGxvZ3MSKwoEcGFnZRgCIAEoCzIXLmNsaWVudC52MC5QYWdlUmVzcG9uc2VSBHBhZ2U=');

@$core.Deprecated('Use listProfilesRequestDescriptor instead')
const ListProfilesRequest$json = {
  '1': 'ListProfilesRequest',
  '2': [
    {
      '1': 'page',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.PageRequest',
      '10': 'page'
    },
  ],
};

/// Descriptor for `ListProfilesRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listProfilesRequestDescriptor = $convert.base64Decode(
    'ChNMaXN0UHJvZmlsZXNSZXF1ZXN0EioKBHBhZ2UYASABKAsyFi5jbGllbnQudjAuUGFnZVJlcX'
    'Vlc3RSBHBhZ2U=');

@$core.Deprecated('Use listProfilesResponseDescriptor instead')
const ListProfilesResponse$json = {
  '1': 'ListProfilesResponse',
  '2': [
    {
      '1': 'profiles',
      '3': 1,
      '4': 3,
      '5': 11,
      '6': '.client.v0.Profile',
      '10': 'profiles'
    },
    {'1': 'active_profile_id', '3': 2, '4': 1, '5': 9, '10': 'activeProfileId'},
    {
      '1': 'page',
      '3': 3,
      '4': 1,
      '5': 11,
      '6': '.client.v0.PageResponse',
      '10': 'page'
    },
  ],
};

/// Descriptor for `ListProfilesResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listProfilesResponseDescriptor = $convert.base64Decode(
    'ChRMaXN0UHJvZmlsZXNSZXNwb25zZRIuCghwcm9maWxlcxgBIAMoCzISLmNsaWVudC52MC5Qcm'
    '9maWxlUghwcm9maWxlcxIqChFhY3RpdmVfcHJvZmlsZV9pZBgCIAEoCVIPYWN0aXZlUHJvZmls'
    'ZUlkEisKBHBhZ2UYAyABKAsyFy5jbGllbnQudjAuUGFnZVJlc3BvbnNlUgRwYWdl');

@$core.Deprecated('Use createProfileRequestDescriptor instead')
const CreateProfileRequest$json = {
  '1': 'CreateProfileRequest',
  '2': [
    {
      '1': 'mutation',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.MutationContext',
      '10': 'mutation'
    },
    {'1': 'display_name', '3': 2, '4': 1, '5': 9, '10': 'displayName'},
    {'1': 'control_origin', '3': 3, '4': 1, '5': 9, '10': 'controlOrigin'},
  ],
};

/// Descriptor for `CreateProfileRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List createProfileRequestDescriptor = $convert.base64Decode(
    'ChRDcmVhdGVQcm9maWxlUmVxdWVzdBI2CghtdXRhdGlvbhgBIAEoCzIaLmNsaWVudC52MC5NdX'
    'RhdGlvbkNvbnRleHRSCG11dGF0aW9uEiEKDGRpc3BsYXlfbmFtZRgCIAEoCVILZGlzcGxheU5h'
    'bWUSJQoOY29udHJvbF9vcmlnaW4YAyABKAlSDWNvbnRyb2xPcmlnaW4=');

@$core.Deprecated('Use createProfileResponseDescriptor instead')
const CreateProfileResponse$json = {
  '1': 'CreateProfileResponse',
  '2': [
    {
      '1': 'profile',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ProfileResult',
      '10': 'profile'
    },
  ],
};

/// Descriptor for `CreateProfileResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List createProfileResponseDescriptor = $convert.base64Decode(
    'ChVDcmVhdGVQcm9maWxlUmVzcG9uc2USMgoHcHJvZmlsZRgBIAEoCzIYLmNsaWVudC52MC5Qcm'
    '9maWxlUmVzdWx0Ugdwcm9maWxl');

@$core.Deprecated('Use selectProfileRequestDescriptor instead')
const SelectProfileRequest$json = {
  '1': 'SelectProfileRequest',
  '2': [
    {
      '1': 'mutation',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.MutationContext',
      '10': 'mutation'
    },
    {
      '1': 'profile',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ProfileRef',
      '10': 'profile'
    },
  ],
};

/// Descriptor for `SelectProfileRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List selectProfileRequestDescriptor = $convert.base64Decode(
    'ChRTZWxlY3RQcm9maWxlUmVxdWVzdBI2CghtdXRhdGlvbhgBIAEoCzIaLmNsaWVudC52MC5NdX'
    'RhdGlvbkNvbnRleHRSCG11dGF0aW9uEi8KB3Byb2ZpbGUYAiABKAsyFS5jbGllbnQudjAuUHJv'
    'ZmlsZVJlZlIHcHJvZmlsZQ==');

@$core.Deprecated('Use selectProfileResponseDescriptor instead')
const SelectProfileResponse$json = {
  '1': 'SelectProfileResponse',
  '2': [
    {
      '1': 'selection',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.SelectionResult',
      '10': 'selection'
    },
    {
      '1': 'continuity',
      '3': 2,
      '4': 1,
      '5': 14,
      '6': '.client.v0.ConnectionContinuity',
      '10': 'continuity'
    },
  ],
};

/// Descriptor for `SelectProfileResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List selectProfileResponseDescriptor = $convert.base64Decode(
    'ChVTZWxlY3RQcm9maWxlUmVzcG9uc2USOAoJc2VsZWN0aW9uGAEgASgLMhouY2xpZW50LnYwLl'
    'NlbGVjdGlvblJlc3VsdFIJc2VsZWN0aW9uEj8KCmNvbnRpbnVpdHkYAiABKA4yHy5jbGllbnQu'
    'djAuQ29ubmVjdGlvbkNvbnRpbnVpdHlSCmNvbnRpbnVpdHk=');

@$core.Deprecated('Use renameProfileRequestDescriptor instead')
const RenameProfileRequest$json = {
  '1': 'RenameProfileRequest',
  '2': [
    {
      '1': 'mutation',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.MutationContext',
      '10': 'mutation'
    },
    {
      '1': 'profile',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ProfileRef',
      '10': 'profile'
    },
    {'1': 'display_name', '3': 3, '4': 1, '5': 9, '10': 'displayName'},
  ],
};

/// Descriptor for `RenameProfileRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List renameProfileRequestDescriptor = $convert.base64Decode(
    'ChRSZW5hbWVQcm9maWxlUmVxdWVzdBI2CghtdXRhdGlvbhgBIAEoCzIaLmNsaWVudC52MC5NdX'
    'RhdGlvbkNvbnRleHRSCG11dGF0aW9uEi8KB3Byb2ZpbGUYAiABKAsyFS5jbGllbnQudjAuUHJv'
    'ZmlsZVJlZlIHcHJvZmlsZRIhCgxkaXNwbGF5X25hbWUYAyABKAlSC2Rpc3BsYXlOYW1l');

@$core.Deprecated('Use renameProfileResponseDescriptor instead')
const RenameProfileResponse$json = {
  '1': 'RenameProfileResponse',
  '2': [
    {
      '1': 'change',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ChangeResult',
      '10': 'change'
    },
  ],
};

/// Descriptor for `RenameProfileResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List renameProfileResponseDescriptor = $convert.base64Decode(
    'ChVSZW5hbWVQcm9maWxlUmVzcG9uc2USLwoGY2hhbmdlGAEgASgLMhcuY2xpZW50LnYwLkNoYW'
    '5nZVJlc3VsdFIGY2hhbmdl');

@$core.Deprecated('Use removeProfileRequestDescriptor instead')
const RemoveProfileRequest$json = {
  '1': 'RemoveProfileRequest',
  '2': [
    {
      '1': 'mutation',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.MutationContext',
      '10': 'mutation'
    },
    {
      '1': 'profile',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ProfileRef',
      '10': 'profile'
    },
  ],
};

/// Descriptor for `RemoveProfileRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List removeProfileRequestDescriptor = $convert.base64Decode(
    'ChRSZW1vdmVQcm9maWxlUmVxdWVzdBI2CghtdXRhdGlvbhgBIAEoCzIaLmNsaWVudC52MC5NdX'
    'RhdGlvbkNvbnRleHRSCG11dGF0aW9uEi8KB3Byb2ZpbGUYAiABKAsyFS5jbGllbnQudjAuUHJv'
    'ZmlsZVJlZlIHcHJvZmlsZQ==');

@$core.Deprecated('Use removeProfileResponseDescriptor instead')
const RemoveProfileResponse$json = {
  '1': 'RemoveProfileResponse',
  '2': [
    {
      '1': 'change',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ChangeResult',
      '10': 'change'
    },
  ],
};

/// Descriptor for `RemoveProfileResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List removeProfileResponseDescriptor = $convert.base64Decode(
    'ChVSZW1vdmVQcm9maWxlUmVzcG9uc2USLwoGY2hhbmdlGAEgASgLMhcuY2xpZW50LnYwLkNoYW'
    '5nZVJlc3VsdFIGY2hhbmdl');

@$core.Deprecated('Use getSessionRequestDescriptor instead')
const GetSessionRequest$json = {
  '1': 'GetSessionRequest',
  '2': [
    {
      '1': 'profile',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ProfileRef',
      '10': 'profile'
    },
  ],
};

/// Descriptor for `GetSessionRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List getSessionRequestDescriptor = $convert.base64Decode(
    'ChFHZXRTZXNzaW9uUmVxdWVzdBIvCgdwcm9maWxlGAEgASgLMhUuY2xpZW50LnYwLlByb2ZpbG'
    'VSZWZSB3Byb2ZpbGU=');

@$core.Deprecated('Use getSessionResponseDescriptor instead')
const GetSessionResponse$json = {
  '1': 'GetSessionResponse',
  '2': [
    {
      '1': 'session',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.Session',
      '10': 'session'
    },
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

/// Descriptor for `GetSessionResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List getSessionResponseDescriptor = $convert.base64Decode(
    'ChJHZXRTZXNzaW9uUmVzcG9uc2USLAoHc2Vzc2lvbhgBIAEoCzISLmNsaWVudC52MC5TZXNzaW'
    '9uUgdzZXNzaW9uEjcKCG1ldGFkYXRhGAIgASgLMhsuY2xpZW50LnYwLlNuYXBzaG90TWV0YWRh'
    'dGFSCG1ldGFkYXRh');

@$core.Deprecated('Use renewSessionRequestDescriptor instead')
const RenewSessionRequest$json = {
  '1': 'RenewSessionRequest',
  '2': [
    {
      '1': 'mutation',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.MutationContext',
      '10': 'mutation'
    },
    {
      '1': 'profile',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ProfileRef',
      '10': 'profile'
    },
  ],
};

/// Descriptor for `RenewSessionRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List renewSessionRequestDescriptor = $convert.base64Decode(
    'ChNSZW5ld1Nlc3Npb25SZXF1ZXN0EjYKCG11dGF0aW9uGAEgASgLMhouY2xpZW50LnYwLk11dG'
    'F0aW9uQ29udGV4dFIIbXV0YXRpb24SLwoHcHJvZmlsZRgCIAEoCzIVLmNsaWVudC52MC5Qcm9m'
    'aWxlUmVmUgdwcm9maWxl');

@$core.Deprecated('Use renewSessionResponseDescriptor instead')
const RenewSessionResponse$json = {
  '1': 'RenewSessionResponse',
  '2': [
    {
      '1': 'renewal',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.RenewalResult',
      '9': 0,
      '10': 'renewal'
    },
    {
      '1': 'browser_login',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.client.v0.BrowserLoginChallenge',
      '9': 0,
      '10': 'browserLogin'
    },
    {
      '1': 'continuity',
      '3': 3,
      '4': 1,
      '5': 14,
      '6': '.client.v0.ConnectionContinuity',
      '10': 'continuity'
    },
  ],
  '8': [
    {'1': 'result'},
  ],
};

/// Descriptor for `RenewSessionResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List renewSessionResponseDescriptor = $convert.base64Decode(
    'ChRSZW5ld1Nlc3Npb25SZXNwb25zZRI0CgdyZW5ld2FsGAEgASgLMhguY2xpZW50LnYwLlJlbm'
    'V3YWxSZXN1bHRIAFIHcmVuZXdhbBJHCg1icm93c2VyX2xvZ2luGAIgASgLMiAuY2xpZW50LnYw'
    'LkJyb3dzZXJMb2dpbkNoYWxsZW5nZUgAUgxicm93c2VyTG9naW4SPwoKY29udGludWl0eRgDIA'
    'EoDjIfLmNsaWVudC52MC5Db25uZWN0aW9uQ29udGludWl0eVIKY29udGludWl0eUIICgZyZXN1'
    'bHQ=');

@$core.Deprecated('Use waitSessionRenewalRequestDescriptor instead')
const WaitSessionRenewalRequest$json = {
  '1': 'WaitSessionRenewalRequest',
  '2': [
    {
      '1': 'profile',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ProfileRef',
      '10': 'profile'
    },
    {'1': 'user_code', '3': 2, '4': 1, '5': 9, '10': 'userCode'},
  ],
};

/// Descriptor for `WaitSessionRenewalRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List waitSessionRenewalRequestDescriptor =
    $convert.base64Decode(
        'ChlXYWl0U2Vzc2lvblJlbmV3YWxSZXF1ZXN0Ei8KB3Byb2ZpbGUYASABKAsyFS5jbGllbnQudj'
        'AuUHJvZmlsZVJlZlIHcHJvZmlsZRIbCgl1c2VyX2NvZGUYAiABKAlSCHVzZXJDb2Rl');

@$core.Deprecated('Use waitSessionRenewalResponseDescriptor instead')
const WaitSessionRenewalResponse$json = {
  '1': 'WaitSessionRenewalResponse',
  '2': [
    {
      '1': 'renewal',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.RenewalResult',
      '10': 'renewal'
    },
    {
      '1': 'continuity',
      '3': 2,
      '4': 1,
      '5': 14,
      '6': '.client.v0.ConnectionContinuity',
      '10': 'continuity'
    },
  ],
};

/// Descriptor for `WaitSessionRenewalResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List waitSessionRenewalResponseDescriptor =
    $convert.base64Decode(
        'ChpXYWl0U2Vzc2lvblJlbmV3YWxSZXNwb25zZRIyCgdyZW5ld2FsGAEgASgLMhguY2xpZW50Ln'
        'YwLlJlbmV3YWxSZXN1bHRSB3JlbmV3YWwSPwoKY29udGludWl0eRgCIAEoDjIfLmNsaWVudC52'
        'MC5Db25uZWN0aW9uQ29udGludWl0eVIKY29udGludWl0eQ==');

@$core.Deprecated('Use listExitNodesRequestDescriptor instead')
const ListExitNodesRequest$json = {
  '1': 'ListExitNodesRequest',
  '2': [
    {
      '1': 'profile',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ProfileRef',
      '10': 'profile'
    },
    {
      '1': 'page',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.client.v0.PageRequest',
      '10': 'page'
    },
  ],
};

/// Descriptor for `ListExitNodesRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listExitNodesRequestDescriptor = $convert.base64Decode(
    'ChRMaXN0RXhpdE5vZGVzUmVxdWVzdBIvCgdwcm9maWxlGAEgASgLMhUuY2xpZW50LnYwLlByb2'
    'ZpbGVSZWZSB3Byb2ZpbGUSKgoEcGFnZRgCIAEoCzIWLmNsaWVudC52MC5QYWdlUmVxdWVzdFIE'
    'cGFnZQ==');

@$core.Deprecated('Use listExitNodesResponseDescriptor instead')
const ListExitNodesResponse$json = {
  '1': 'ListExitNodesResponse',
  '2': [
    {
      '1': 'exit_nodes',
      '3': 1,
      '4': 3,
      '5': 11,
      '6': '.client.v0.ExitNode',
      '10': 'exitNodes'
    },
    {
      '1': 'page',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.client.v0.PageResponse',
      '10': 'page'
    },
  ],
};

/// Descriptor for `ListExitNodesResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listExitNodesResponseDescriptor = $convert.base64Decode(
    'ChVMaXN0RXhpdE5vZGVzUmVzcG9uc2USMgoKZXhpdF9ub2RlcxgBIAMoCzITLmNsaWVudC52MC'
    '5FeGl0Tm9kZVIJZXhpdE5vZGVzEisKBHBhZ2UYAiABKAsyFy5jbGllbnQudjAuUGFnZVJlc3Bv'
    'bnNlUgRwYWdl');

@$core.Deprecated('Use getExitNodeRequestDescriptor instead')
const GetExitNodeRequest$json = {
  '1': 'GetExitNodeRequest',
  '2': [
    {
      '1': 'profile',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ProfileRef',
      '10': 'profile'
    },
  ],
};

/// Descriptor for `GetExitNodeRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List getExitNodeRequestDescriptor = $convert.base64Decode(
    'ChJHZXRFeGl0Tm9kZVJlcXVlc3QSLwoHcHJvZmlsZRgBIAEoCzIVLmNsaWVudC52MC5Qcm9maW'
    'xlUmVmUgdwcm9maWxl');

@$core.Deprecated('Use getExitNodeResponseDescriptor instead')
const GetExitNodeResponse$json = {
  '1': 'GetExitNodeResponse',
  '2': [
    {
      '1': 'status',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ExitNodeStatus',
      '10': 'status'
    },
  ],
};

/// Descriptor for `GetExitNodeResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List getExitNodeResponseDescriptor = $convert.base64Decode(
    'ChNHZXRFeGl0Tm9kZVJlc3BvbnNlEjEKBnN0YXR1cxgBIAEoCzIZLmNsaWVudC52MC5FeGl0Tm'
    '9kZVN0YXR1c1IGc3RhdHVz');

@$core.Deprecated('Use selectExitNodeRequestDescriptor instead')
const SelectExitNodeRequest$json = {
  '1': 'SelectExitNodeRequest',
  '2': [
    {
      '1': 'mutation',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.MutationContext',
      '10': 'mutation'
    },
    {
      '1': 'profile',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ProfileRef',
      '10': 'profile'
    },
    {'1': 'exit_node_id', '3': 3, '4': 1, '5': 9, '10': 'exitNodeId'},
    {
      '1': 'lan_access',
      '3': 4,
      '4': 1,
      '5': 14,
      '6': '.client.v0.LanAccess',
      '10': 'lanAccess'
    },
    {
      '1': 'family_mode',
      '3': 5,
      '4': 1,
      '5': 14,
      '6': '.client.v0.ExitFamilyMode',
      '10': 'familyMode'
    },
  ],
};

/// Descriptor for `SelectExitNodeRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List selectExitNodeRequestDescriptor = $convert.base64Decode(
    'ChVTZWxlY3RFeGl0Tm9kZVJlcXVlc3QSNgoIbXV0YXRpb24YASABKAsyGi5jbGllbnQudjAuTX'
    'V0YXRpb25Db250ZXh0UghtdXRhdGlvbhIvCgdwcm9maWxlGAIgASgLMhUuY2xpZW50LnYwLlBy'
    'b2ZpbGVSZWZSB3Byb2ZpbGUSIAoMZXhpdF9ub2RlX2lkGAMgASgJUgpleGl0Tm9kZUlkEjMKCm'
    'xhbl9hY2Nlc3MYBCABKA4yFC5jbGllbnQudjAuTGFuQWNjZXNzUglsYW5BY2Nlc3MSOgoLZmFt'
    'aWx5X21vZGUYBSABKA4yGS5jbGllbnQudjAuRXhpdEZhbWlseU1vZGVSCmZhbWlseU1vZGU=');

@$core.Deprecated('Use selectExitNodeResponseDescriptor instead')
const SelectExitNodeResponse$json = {
  '1': 'SelectExitNodeResponse',
  '2': [
    {
      '1': 'selection',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.SelectionResult',
      '10': 'selection'
    },
    {
      '1': 'continuity',
      '3': 2,
      '4': 1,
      '5': 14,
      '6': '.client.v0.ConnectionContinuity',
      '10': 'continuity'
    },
  ],
};

/// Descriptor for `SelectExitNodeResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List selectExitNodeResponseDescriptor = $convert.base64Decode(
    'ChZTZWxlY3RFeGl0Tm9kZVJlc3BvbnNlEjgKCXNlbGVjdGlvbhgBIAEoCzIaLmNsaWVudC52MC'
    '5TZWxlY3Rpb25SZXN1bHRSCXNlbGVjdGlvbhI/Cgpjb250aW51aXR5GAIgASgOMh8uY2xpZW50'
    'LnYwLkNvbm5lY3Rpb25Db250aW51aXR5Ugpjb250aW51aXR5');

@$core.Deprecated('Use clearExitNodeRequestDescriptor instead')
const ClearExitNodeRequest$json = {
  '1': 'ClearExitNodeRequest',
  '2': [
    {
      '1': 'mutation',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.MutationContext',
      '10': 'mutation'
    },
    {
      '1': 'profile',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ProfileRef',
      '10': 'profile'
    },
  ],
};

/// Descriptor for `ClearExitNodeRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List clearExitNodeRequestDescriptor = $convert.base64Decode(
    'ChRDbGVhckV4aXROb2RlUmVxdWVzdBI2CghtdXRhdGlvbhgBIAEoCzIaLmNsaWVudC52MC5NdX'
    'RhdGlvbkNvbnRleHRSCG11dGF0aW9uEi8KB3Byb2ZpbGUYAiABKAsyFS5jbGllbnQudjAuUHJv'
    'ZmlsZVJlZlIHcHJvZmlsZQ==');

@$core.Deprecated('Use clearExitNodeResponseDescriptor instead')
const ClearExitNodeResponse$json = {
  '1': 'ClearExitNodeResponse',
  '2': [
    {
      '1': 'selection',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.SelectionResult',
      '10': 'selection'
    },
    {
      '1': 'continuity',
      '3': 2,
      '4': 1,
      '5': 14,
      '6': '.client.v0.ConnectionContinuity',
      '10': 'continuity'
    },
  ],
};

/// Descriptor for `ClearExitNodeResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List clearExitNodeResponseDescriptor = $convert.base64Decode(
    'ChVDbGVhckV4aXROb2RlUmVzcG9uc2USOAoJc2VsZWN0aW9uGAEgASgLMhouY2xpZW50LnYwLl'
    'NlbGVjdGlvblJlc3VsdFIJc2VsZWN0aW9uEj8KCmNvbnRpbnVpdHkYAiABKA4yHy5jbGllbnQu'
    'djAuQ29ubmVjdGlvbkNvbnRpbnVpdHlSCmNvbnRpbnVpdHk=');

@$core.Deprecated('Use getPreferencesRequestDescriptor instead')
const GetPreferencesRequest$json = {
  '1': 'GetPreferencesRequest',
  '2': [
    {
      '1': 'profile',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ProfileRef',
      '10': 'profile'
    },
  ],
};

/// Descriptor for `GetPreferencesRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List getPreferencesRequestDescriptor = $convert.base64Decode(
    'ChVHZXRQcmVmZXJlbmNlc1JlcXVlc3QSLwoHcHJvZmlsZRgBIAEoCzIVLmNsaWVudC52MC5Qcm'
    '9maWxlUmVmUgdwcm9maWxl');

@$core.Deprecated('Use getPreferencesResponseDescriptor instead')
const GetPreferencesResponse$json = {
  '1': 'GetPreferencesResponse',
  '2': [
    {
      '1': 'preferences',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.Preferences',
      '10': 'preferences'
    },
  ],
};

/// Descriptor for `GetPreferencesResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List getPreferencesResponseDescriptor =
    $convert.base64Decode(
        'ChZHZXRQcmVmZXJlbmNlc1Jlc3BvbnNlEjgKC3ByZWZlcmVuY2VzGAEgASgLMhYuY2xpZW50Ln'
        'YwLlByZWZlcmVuY2VzUgtwcmVmZXJlbmNlcw==');

@$core.Deprecated('Use setPreferencesRequestDescriptor instead')
const SetPreferencesRequest$json = {
  '1': 'SetPreferencesRequest',
  '2': [
    {
      '1': 'mutation',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.MutationContext',
      '10': 'mutation'
    },
    {
      '1': 'profile',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ProfileRef',
      '10': 'profile'
    },
    {
      '1': 'patch',
      '3': 3,
      '4': 1,
      '5': 11,
      '6': '.client.v0.PreferencesPatch',
      '10': 'patch'
    },
  ],
};

/// Descriptor for `SetPreferencesRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List setPreferencesRequestDescriptor = $convert.base64Decode(
    'ChVTZXRQcmVmZXJlbmNlc1JlcXVlc3QSNgoIbXV0YXRpb24YASABKAsyGi5jbGllbnQudjAuTX'
    'V0YXRpb25Db250ZXh0UghtdXRhdGlvbhIvCgdwcm9maWxlGAIgASgLMhUuY2xpZW50LnYwLlBy'
    'b2ZpbGVSZWZSB3Byb2ZpbGUSMQoFcGF0Y2gYAyABKAsyGy5jbGllbnQudjAuUHJlZmVyZW5jZX'
    'NQYXRjaFIFcGF0Y2g=');

@$core.Deprecated('Use setPreferencesResponseDescriptor instead')
const SetPreferencesResponse$json = {
  '1': 'SetPreferencesResponse',
  '2': [
    {
      '1': 'change',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ChangeResult',
      '10': 'change'
    },
  ],
};

/// Descriptor for `SetPreferencesResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List setPreferencesResponseDescriptor =
    $convert.base64Decode(
        'ChZTZXRQcmVmZXJlbmNlc1Jlc3BvbnNlEi8KBmNoYW5nZRgBIAEoCzIXLmNsaWVudC52MC5DaG'
        'FuZ2VSZXN1bHRSBmNoYW5nZQ==');

@$core.Deprecated('Use resetPreferencesRequestDescriptor instead')
const ResetPreferencesRequest$json = {
  '1': 'ResetPreferencesRequest',
  '2': [
    {
      '1': 'mutation',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.MutationContext',
      '10': 'mutation'
    },
    {
      '1': 'profile',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ProfileRef',
      '10': 'profile'
    },
    {
      '1': 'keys',
      '3': 3,
      '4': 3,
      '5': 14,
      '6': '.client.v0.PreferenceKey',
      '10': 'keys'
    },
  ],
};

/// Descriptor for `ResetPreferencesRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List resetPreferencesRequestDescriptor = $convert.base64Decode(
    'ChdSZXNldFByZWZlcmVuY2VzUmVxdWVzdBI2CghtdXRhdGlvbhgBIAEoCzIaLmNsaWVudC52MC'
    '5NdXRhdGlvbkNvbnRleHRSCG11dGF0aW9uEi8KB3Byb2ZpbGUYAiABKAsyFS5jbGllbnQudjAu'
    'UHJvZmlsZVJlZlIHcHJvZmlsZRIsCgRrZXlzGAMgAygOMhguY2xpZW50LnYwLlByZWZlcmVuY2'
    'VLZXlSBGtleXM=');

@$core.Deprecated('Use resetPreferencesResponseDescriptor instead')
const ResetPreferencesResponse$json = {
  '1': 'ResetPreferencesResponse',
  '2': [
    {
      '1': 'change',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ChangeResult',
      '10': 'change'
    },
  ],
};

/// Descriptor for `ResetPreferencesResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List resetPreferencesResponseDescriptor =
    $convert.base64Decode(
        'ChhSZXNldFByZWZlcmVuY2VzUmVzcG9uc2USLwoGY2hhbmdlGAEgASgLMhcuY2xpZW50LnYwLk'
        'NoYW5nZVJlc3VsdFIGY2hhbmdl');

@$core.Deprecated('Use listManagedSettingsRequestDescriptor instead')
const ListManagedSettingsRequest$json = {
  '1': 'ListManagedSettingsRequest',
  '2': [
    {
      '1': 'profile',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ProfileRef',
      '10': 'profile'
    },
  ],
};

/// Descriptor for `ListManagedSettingsRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listManagedSettingsRequestDescriptor =
    $convert.base64Decode(
        'ChpMaXN0TWFuYWdlZFNldHRpbmdzUmVxdWVzdBIvCgdwcm9maWxlGAEgASgLMhUuY2xpZW50Ln'
        'YwLlByb2ZpbGVSZWZSB3Byb2ZpbGU=');

@$core.Deprecated('Use listManagedSettingsResponseDescriptor instead')
const ListManagedSettingsResponse$json = {
  '1': 'ListManagedSettingsResponse',
  '2': [
    {
      '1': 'settings',
      '3': 1,
      '4': 3,
      '5': 11,
      '6': '.client.v0.ManagedSetting',
      '10': 'settings'
    },
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

/// Descriptor for `ListManagedSettingsResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listManagedSettingsResponseDescriptor =
    $convert.base64Decode(
        'ChtMaXN0TWFuYWdlZFNldHRpbmdzUmVzcG9uc2USNQoIc2V0dGluZ3MYASADKAsyGS5jbGllbn'
        'QudjAuTWFuYWdlZFNldHRpbmdSCHNldHRpbmdzEjcKCG1ldGFkYXRhGAIgASgLMhsuY2xpZW50'
        'LnYwLlNuYXBzaG90TWV0YWRhdGFSCG1ldGFkYXRh');

@$core.Deprecated('Use listResourcesRequestDescriptor instead')
const ListResourcesRequest$json = {
  '1': 'ListResourcesRequest',
  '2': [
    {
      '1': 'profile',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ProfileRef',
      '10': 'profile'
    },
    {
      '1': 'page',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.client.v0.PageRequest',
      '10': 'page'
    },
    {'1': 'search', '3': 3, '4': 1, '5': 9, '10': 'search'},
    {
      '1': 'kinds',
      '3': 4,
      '4': 3,
      '5': 14,
      '6': '.client.v0.ResourceKind',
      '10': 'kinds'
    },
  ],
};

/// Descriptor for `ListResourcesRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listResourcesRequestDescriptor = $convert.base64Decode(
    'ChRMaXN0UmVzb3VyY2VzUmVxdWVzdBIvCgdwcm9maWxlGAEgASgLMhUuY2xpZW50LnYwLlByb2'
    'ZpbGVSZWZSB3Byb2ZpbGUSKgoEcGFnZRgCIAEoCzIWLmNsaWVudC52MC5QYWdlUmVxdWVzdFIE'
    'cGFnZRIWCgZzZWFyY2gYAyABKAlSBnNlYXJjaBItCgVraW5kcxgEIAMoDjIXLmNsaWVudC52MC'
    '5SZXNvdXJjZUtpbmRSBWtpbmRz');

@$core.Deprecated('Use listResourcesResponseDescriptor instead')
const ListResourcesResponse$json = {
  '1': 'ListResourcesResponse',
  '2': [
    {
      '1': 'resources',
      '3': 1,
      '4': 3,
      '5': 11,
      '6': '.client.v0.Resource',
      '10': 'resources'
    },
    {
      '1': 'page',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.client.v0.PageResponse',
      '10': 'page'
    },
  ],
};

/// Descriptor for `ListResourcesResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listResourcesResponseDescriptor = $convert.base64Decode(
    'ChVMaXN0UmVzb3VyY2VzUmVzcG9uc2USMQoJcmVzb3VyY2VzGAEgAygLMhMuY2xpZW50LnYwLl'
    'Jlc291cmNlUglyZXNvdXJjZXMSKwoEcGFnZRgCIAEoCzIXLmNsaWVudC52MC5QYWdlUmVzcG9u'
    'c2VSBHBhZ2U=');

@$core.Deprecated('Use setResourceEnabledRequestDescriptor instead')
const SetResourceEnabledRequest$json = {
  '1': 'SetResourceEnabledRequest',
  '2': [
    {
      '1': 'mutation',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.MutationContext',
      '10': 'mutation'
    },
    {
      '1': 'profile',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ProfileRef',
      '10': 'profile'
    },
    {'1': 'resource_id', '3': 3, '4': 1, '5': 9, '10': 'resourceId'},
    {'1': 'enabled', '3': 4, '4': 1, '5': 8, '10': 'enabled'},
  ],
};

/// Descriptor for `SetResourceEnabledRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List setResourceEnabledRequestDescriptor = $convert.base64Decode(
    'ChlTZXRSZXNvdXJjZUVuYWJsZWRSZXF1ZXN0EjYKCG11dGF0aW9uGAEgASgLMhouY2xpZW50Ln'
    'YwLk11dGF0aW9uQ29udGV4dFIIbXV0YXRpb24SLwoHcHJvZmlsZRgCIAEoCzIVLmNsaWVudC52'
    'MC5Qcm9maWxlUmVmUgdwcm9maWxlEh8KC3Jlc291cmNlX2lkGAMgASgJUgpyZXNvdXJjZUlkEh'
    'gKB2VuYWJsZWQYBCABKAhSB2VuYWJsZWQ=');

@$core.Deprecated('Use setResourceEnabledResponseDescriptor instead')
const SetResourceEnabledResponse$json = {
  '1': 'SetResourceEnabledResponse',
  '2': [
    {
      '1': 'change',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ChangeResult',
      '10': 'change'
    },
  ],
};

/// Descriptor for `SetResourceEnabledResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List setResourceEnabledResponseDescriptor =
    $convert.base64Decode(
        'ChpTZXRSZXNvdXJjZUVuYWJsZWRSZXNwb25zZRIvCgZjaGFuZ2UYASABKAsyFy5jbGllbnQudj'
        'AuQ2hhbmdlUmVzdWx0UgZjaGFuZ2U=');

@$core.Deprecated('Use notifyLifecycleRequestDescriptor instead')
const NotifyLifecycleRequest$json = {
  '1': 'NotifyLifecycleRequest',
  '2': [
    {
      '1': 'mutation',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.MutationContext',
      '10': 'mutation'
    },
    {
      '1': 'profile',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ProfileRef',
      '10': 'profile'
    },
    {
      '1': 'event',
      '3': 3,
      '4': 1,
      '5': 14,
      '6': '.client.v0.LifecycleEvent',
      '10': 'event'
    },
  ],
};

/// Descriptor for `NotifyLifecycleRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List notifyLifecycleRequestDescriptor = $convert.base64Decode(
    'ChZOb3RpZnlMaWZlY3ljbGVSZXF1ZXN0EjYKCG11dGF0aW9uGAEgASgLMhouY2xpZW50LnYwLk'
    '11dGF0aW9uQ29udGV4dFIIbXV0YXRpb24SLwoHcHJvZmlsZRgCIAEoCzIVLmNsaWVudC52MC5Q'
    'cm9maWxlUmVmUgdwcm9maWxlEi8KBWV2ZW50GAMgASgOMhkuY2xpZW50LnYwLkxpZmVjeWNsZU'
    'V2ZW50UgVldmVudA==');

@$core.Deprecated('Use notifyLifecycleResponseDescriptor instead')
const NotifyLifecycleResponse$json = {
  '1': 'NotifyLifecycleResponse',
  '2': [
    {
      '1': 'change',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ChangeResult',
      '10': 'change'
    },
  ],
};

/// Descriptor for `NotifyLifecycleResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List notifyLifecycleResponseDescriptor =
    $convert.base64Decode(
        'ChdOb3RpZnlMaWZlY3ljbGVSZXNwb25zZRIvCgZjaGFuZ2UYASABKAsyFy5jbGllbnQudjAuQ2'
        'hhbmdlUmVzdWx0UgZjaGFuZ2U=');

@$core.Deprecated('Use getUpdateInfoRequestDescriptor instead')
const GetUpdateInfoRequest$json = {
  '1': 'GetUpdateInfoRequest',
  '2': [
    {
      '1': 'reported_ui',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.BuildIdentity',
      '10': 'reportedUi'
    },
  ],
};

/// Descriptor for `GetUpdateInfoRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List getUpdateInfoRequestDescriptor = $convert.base64Decode(
    'ChRHZXRVcGRhdGVJbmZvUmVxdWVzdBI5CgtyZXBvcnRlZF91aRgBIAEoCzIYLmNsaWVudC52MC'
    '5CdWlsZElkZW50aXR5UgpyZXBvcnRlZFVp');

@$core.Deprecated('Use getUpdateInfoResponseDescriptor instead')
const GetUpdateInfoResponse$json = {
  '1': 'GetUpdateInfoResponse',
  '2': [
    {
      '1': 'info',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.UpdateInfo',
      '10': 'info'
    },
  ],
};

/// Descriptor for `GetUpdateInfoResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List getUpdateInfoResponseDescriptor = $convert.base64Decode(
    'ChVHZXRVcGRhdGVJbmZvUmVzcG9uc2USKQoEaW5mbxgBIAEoCzIVLmNsaWVudC52MC5VcGRhdG'
    'VJbmZvUgRpbmZv');

@$core.Deprecated('Use getSupportInfoRequestDescriptor instead')
const GetSupportInfoRequest$json = {
  '1': 'GetSupportInfoRequest',
};

/// Descriptor for `GetSupportInfoRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List getSupportInfoRequestDescriptor =
    $convert.base64Decode('ChVHZXRTdXBwb3J0SW5mb1JlcXVlc3Q=');

@$core.Deprecated('Use getSupportInfoResponseDescriptor instead')
const GetSupportInfoResponse$json = {
  '1': 'GetSupportInfoResponse',
  '2': [
    {
      '1': 'info',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.SupportInfo',
      '10': 'info'
    },
  ],
};

/// Descriptor for `GetSupportInfoResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List getSupportInfoResponseDescriptor =
    $convert.base64Decode(
        'ChZHZXRTdXBwb3J0SW5mb1Jlc3BvbnNlEioKBGluZm8YASABKAsyFi5jbGllbnQudjAuU3VwcG'
        '9ydEluZm9SBGluZm8=');

@$core.Deprecated('Use watchEventsResponseDescriptor instead')
const WatchEventsResponse$json = {
  '1': 'WatchEventsResponse',
  '2': [
    {'1': 'sequence', '3': 1, '4': 1, '5': 4, '10': 'sequence'},
    {
      '1': 'metadata',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.client.v0.SnapshotMetadata',
      '10': 'metadata'
    },
    {
      '1': 'snapshot',
      '3': 3,
      '4': 1,
      '5': 11,
      '6': '.client.v0.SnapshotEvent',
      '9': 0,
      '10': 'snapshot'
    },
    {
      '1': 'status_changed',
      '3': 4,
      '4': 1,
      '5': 11,
      '6': '.client.v0.Status',
      '9': 0,
      '10': 'statusChanged'
    },
    {
      '1': 'invalidated',
      '3': 5,
      '4': 1,
      '5': 11,
      '6': '.client.v0.DomainInvalidated',
      '9': 0,
      '10': 'invalidated'
    },
    {
      '1': 'failure',
      '3': 7,
      '4': 1,
      '5': 11,
      '6': '.client.v0.Failure',
      '9': 0,
      '10': 'failure'
    },
    {
      '1': 'session_changed',
      '3': 8,
      '4': 1,
      '5': 11,
      '6': '.client.v0.Session',
      '9': 0,
      '10': 'sessionChanged'
    },
  ],
  '8': [
    {'1': 'event'},
  ],
};

/// Descriptor for `WatchEventsResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List watchEventsResponseDescriptor = $convert.base64Decode(
    'ChNXYXRjaEV2ZW50c1Jlc3BvbnNlEhoKCHNlcXVlbmNlGAEgASgEUghzZXF1ZW5jZRI3CghtZX'
    'RhZGF0YRgCIAEoCzIbLmNsaWVudC52MC5TbmFwc2hvdE1ldGFkYXRhUghtZXRhZGF0YRI2Cghz'
    'bmFwc2hvdBgDIAEoCzIYLmNsaWVudC52MC5TbmFwc2hvdEV2ZW50SABSCHNuYXBzaG90EjoKDn'
    'N0YXR1c19jaGFuZ2VkGAQgASgLMhEuY2xpZW50LnYwLlN0YXR1c0gAUg1zdGF0dXNDaGFuZ2Vk'
    'EkAKC2ludmFsaWRhdGVkGAUgASgLMhwuY2xpZW50LnYwLkRvbWFpbkludmFsaWRhdGVkSABSC2'
    'ludmFsaWRhdGVkEi4KB2ZhaWx1cmUYByABKAsyEi5jbGllbnQudjAuRmFpbHVyZUgAUgdmYWls'
    'dXJlEj0KD3Nlc3Npb25fY2hhbmdlZBgIIAEoCzISLmNsaWVudC52MC5TZXNzaW9uSABSDnNlc3'
    'Npb25DaGFuZ2VkQgcKBWV2ZW50');

@$core.Deprecated('Use snapshotEventDescriptor instead')
const SnapshotEvent$json = {
  '1': 'SnapshotEvent',
  '2': [
    {
      '1': 'runtime',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.RuntimeInfo',
      '10': 'runtime'
    },
    {
      '1': 'status',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.client.v0.Status',
      '10': 'status'
    },
  ],
};

/// Descriptor for `SnapshotEvent`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List snapshotEventDescriptor = $convert.base64Decode(
    'Cg1TbmFwc2hvdEV2ZW50EjAKB3J1bnRpbWUYASABKAsyFi5jbGllbnQudjAuUnVudGltZUluZm'
    '9SB3J1bnRpbWUSKQoGc3RhdHVzGAIgASgLMhEuY2xpZW50LnYwLlN0YXR1c1IGc3RhdHVz');

@$core.Deprecated('Use domainInvalidatedDescriptor instead')
const DomainInvalidated$json = {
  '1': 'DomainInvalidated',
  '2': [
    {
      '1': 'domain',
      '3': 1,
      '4': 1,
      '5': 14,
      '6': '.client.v0.Domain',
      '10': 'domain'
    },
    {'1': 'profile_id', '3': 2, '4': 1, '5': 9, '10': 'profileId'},
  ],
};

/// Descriptor for `DomainInvalidated`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List domainInvalidatedDescriptor = $convert.base64Decode(
    'ChFEb21haW5JbnZhbGlkYXRlZBIpCgZkb21haW4YASABKA4yES5jbGllbnQudjAuRG9tYWluUg'
    'Zkb21haW4SHQoKcHJvZmlsZV9pZBgCIAEoCVIJcHJvZmlsZUlk');
