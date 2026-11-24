// This is a generated file - do not edit.
//
// Generated from client/v0/dashboard.proto.

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

@$core.Deprecated('Use dashboardWidgetKindDescriptor instead')
const DashboardWidgetKind$json = {
  '1': 'DashboardWidgetKind',
  '2': [
    {'1': 'DASHBOARD_WIDGET_KIND_UNSPECIFIED', '2': 0},
    {'1': 'DASHBOARD_WIDGET_KIND_HARDWARE', '2': 1},
    {'1': 'DASHBOARD_WIDGET_KIND_NETWORK', '2': 2},
    {'1': 'DASHBOARD_WIDGET_KIND_DISK', '2': 3},
    {'1': 'DASHBOARD_WIDGET_KIND_PERIPHERALS', '2': 4},
    {'1': 'DASHBOARD_WIDGET_KIND_COMPUTER', '2': 5},
    {'1': 'DASHBOARD_WIDGET_KIND_TUNNEL', '2': 6},
  ],
};

/// Descriptor for `DashboardWidgetKind`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List dashboardWidgetKindDescriptor = $convert.base64Decode(
    'ChNEYXNoYm9hcmRXaWRnZXRLaW5kEiUKIURBU0hCT0FSRF9XSURHRVRfS0lORF9VTlNQRUNJRk'
    'lFRBAAEiIKHkRBU0hCT0FSRF9XSURHRVRfS0lORF9IQVJEV0FSRRABEiEKHURBU0hCT0FSRF9X'
    'SURHRVRfS0lORF9ORVRXT1JLEAISHgoaREFTSEJPQVJEX1dJREdFVF9LSU5EX0RJU0sQAxIlCi'
    'FEQVNIQk9BUkRfV0lER0VUX0tJTkRfUEVSSVBIRVJBTFMQBBIiCh5EQVNIQk9BUkRfV0lER0VU'
    'X0tJTkRfQ09NUFVURVIQBRIgChxEQVNIQk9BUkRfV0lER0VUX0tJTkRfVFVOTkVMEAY=');

@$core.Deprecated('Use dashboardWidgetSizeDescriptor instead')
const DashboardWidgetSize$json = {
  '1': 'DashboardWidgetSize',
  '2': [
    {'1': 'DASHBOARD_WIDGET_SIZE_UNSPECIFIED', '2': 0},
    {'1': 'DASHBOARD_WIDGET_SIZE_SQUARE', '2': 1},
    {'1': 'DASHBOARD_WIDGET_SIZE_WIDE', '2': 2},
    {'1': 'DASHBOARD_WIDGET_SIZE_LARGE', '2': 3},
  ],
};

/// Descriptor for `DashboardWidgetSize`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List dashboardWidgetSizeDescriptor = $convert.base64Decode(
    'ChNEYXNoYm9hcmRXaWRnZXRTaXplEiUKIURBU0hCT0FSRF9XSURHRVRfU0laRV9VTlNQRUNJRk'
    'lFRBAAEiAKHERBU0hCT0FSRF9XSURHRVRfU0laRV9TUVVBUkUQARIeChpEQVNIQk9BUkRfV0lE'
    'R0VUX1NJWkVfV0lERRACEh8KG0RBU0hCT0FSRF9XSURHRVRfU0laRV9MQVJHRRAD');

@$core.Deprecated('Use dashboardImageDescriptor instead')
const DashboardImage$json = {
  '1': 'DashboardImage',
  '2': [
    {'1': 'DASHBOARD_IMAGE_UNSPECIFIED', '2': 0},
    {'1': 'DASHBOARD_IMAGE_RIBBON', '2': 1},
    {'1': 'DASHBOARD_IMAGE_CABIN', '2': 2},
    {'1': 'DASHBOARD_IMAGE_GLASS', '2': 3},
  ],
};

/// Descriptor for `DashboardImage`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List dashboardImageDescriptor = $convert.base64Decode(
    'Cg5EYXNoYm9hcmRJbWFnZRIfChtEQVNIQk9BUkRfSU1BR0VfVU5TUEVDSUZJRUQQABIaChZEQV'
    'NIQk9BUkRfSU1BR0VfUklCQk9OEAESGQoVREFTSEJPQVJEX0lNQUdFX0NBQklOEAISGQoVREFT'
    'SEJPQVJEX0lNQUdFX0dMQVNTEAM=');

@$core.Deprecated('Use dashboardSpaceDescriptor instead')
const DashboardSpace$json = {
  '1': 'DashboardSpace',
  '2': [
    {'1': 'account_id', '3': 1, '4': 1, '5': 9, '10': 'accountId'},
    {'1': 'display_name', '3': 2, '4': 1, '5': 9, '10': 'displayName'},
  ],
};

/// Descriptor for `DashboardSpace`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List dashboardSpaceDescriptor = $convert.base64Decode(
    'Cg5EYXNoYm9hcmRTcGFjZRIdCgphY2NvdW50X2lkGAEgASgJUglhY2NvdW50SWQSIQoMZGlzcG'
    'xheV9uYW1lGAIgASgJUgtkaXNwbGF5TmFtZQ==');

@$core.Deprecated('Use dashboardWidgetDescriptor instead')
const DashboardWidget$json = {
  '1': 'DashboardWidget',
  '2': [
    {
      '1': 'kind',
      '3': 1,
      '4': 1,
      '5': 14,
      '6': '.client.v0.DashboardWidgetKind',
      '10': 'kind'
    },
    {
      '1': 'size',
      '3': 2,
      '4': 1,
      '5': 14,
      '6': '.client.v0.DashboardWidgetSize',
      '10': 'size'
    },
  ],
};

/// Descriptor for `DashboardWidget`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List dashboardWidgetDescriptor = $convert.base64Decode(
    'Cg9EYXNoYm9hcmRXaWRnZXQSMgoEa2luZBgBIAEoDjIeLmNsaWVudC52MC5EYXNoYm9hcmRXaW'
    'RnZXRLaW5kUgRraW5kEjIKBHNpemUYAiABKA4yHi5jbGllbnQudjAuRGFzaGJvYXJkV2lkZ2V0'
    'U2l6ZVIEc2l6ZQ==');

@$core.Deprecated('Use dashboardLayoutDescriptor instead')
const DashboardLayout$json = {
  '1': 'DashboardLayout',
  '2': [
    {
      '1': 'image',
      '3': 1,
      '4': 1,
      '5': 14,
      '6': '.client.v0.DashboardImage',
      '10': 'image'
    },
    {
      '1': 'widgets',
      '3': 2,
      '4': 3,
      '5': 11,
      '6': '.client.v0.DashboardWidget',
      '10': 'widgets'
    },
  ],
};

/// Descriptor for `DashboardLayout`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List dashboardLayoutDescriptor = $convert.base64Decode(
    'Cg9EYXNoYm9hcmRMYXlvdXQSLwoFaW1hZ2UYASABKA4yGS5jbGllbnQudjAuRGFzaGJvYXJkSW'
    '1hZ2VSBWltYWdlEjQKB3dpZGdldHMYAiADKAsyGi5jbGllbnQudjAuRGFzaGJvYXJkV2lkZ2V0'
    'Ugd3aWRnZXRz');

@$core.Deprecated('Use dashboardBoardDescriptor instead')
const DashboardBoard$json = {
  '1': 'DashboardBoard',
  '2': [
    {'1': 'account_id', '3': 1, '4': 1, '5': 9, '10': 'accountId'},
    {'1': 'revision', '3': 2, '4': 1, '5': 4, '10': 'revision'},
    {
      '1': 'layout',
      '3': 3,
      '4': 1,
      '5': 11,
      '6': '.client.v0.DashboardLayout',
      '10': 'layout'
    },
    {'1': 'can_edit', '3': 4, '4': 1, '5': 8, '10': 'canEdit'},
  ],
};

/// Descriptor for `DashboardBoard`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List dashboardBoardDescriptor = $convert.base64Decode(
    'Cg5EYXNoYm9hcmRCb2FyZBIdCgphY2NvdW50X2lkGAEgASgJUglhY2NvdW50SWQSGgoIcmV2aX'
    'Npb24YAiABKARSCHJldmlzaW9uEjIKBmxheW91dBgDIAEoCzIaLmNsaWVudC52MC5EYXNoYm9h'
    'cmRMYXlvdXRSBmxheW91dBIZCghjYW5fZWRpdBgEIAEoCFIHY2FuRWRpdA==');

@$core.Deprecated('Use listDashboardSpacesRequestDescriptor instead')
const ListDashboardSpacesRequest$json = {
  '1': 'ListDashboardSpacesRequest',
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

/// Descriptor for `ListDashboardSpacesRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listDashboardSpacesRequestDescriptor =
    $convert.base64Decode(
        'ChpMaXN0RGFzaGJvYXJkU3BhY2VzUmVxdWVzdBIvCgdwcm9maWxlGAEgASgLMhUuY2xpZW50Ln'
        'YwLlByb2ZpbGVSZWZSB3Byb2ZpbGU=');

@$core.Deprecated('Use listDashboardSpacesResponseDescriptor instead')
const ListDashboardSpacesResponse$json = {
  '1': 'ListDashboardSpacesResponse',
  '2': [
    {
      '1': 'spaces',
      '3': 1,
      '4': 3,
      '5': 11,
      '6': '.client.v0.DashboardSpace',
      '10': 'spaces'
    },
    {
      '1': 'primary_account_id',
      '3': 2,
      '4': 1,
      '5': 9,
      '10': 'primaryAccountId'
    },
  ],
};

/// Descriptor for `ListDashboardSpacesResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listDashboardSpacesResponseDescriptor =
    $convert.base64Decode(
        'ChtMaXN0RGFzaGJvYXJkU3BhY2VzUmVzcG9uc2USMQoGc3BhY2VzGAEgAygLMhkuY2xpZW50Ln'
        'YwLkRhc2hib2FyZFNwYWNlUgZzcGFjZXMSLAoScHJpbWFyeV9hY2NvdW50X2lkGAIgASgJUhBw'
        'cmltYXJ5QWNjb3VudElk');

@$core.Deprecated('Use getDashboardBoardRequestDescriptor instead')
const GetDashboardBoardRequest$json = {
  '1': 'GetDashboardBoardRequest',
  '2': [
    {
      '1': 'profile',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ProfileRef',
      '10': 'profile'
    },
    {'1': 'account_id', '3': 2, '4': 1, '5': 9, '10': 'accountId'},
  ],
};

/// Descriptor for `GetDashboardBoardRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List getDashboardBoardRequestDescriptor =
    $convert.base64Decode(
        'ChhHZXREYXNoYm9hcmRCb2FyZFJlcXVlc3QSLwoHcHJvZmlsZRgBIAEoCzIVLmNsaWVudC52MC'
        '5Qcm9maWxlUmVmUgdwcm9maWxlEh0KCmFjY291bnRfaWQYAiABKAlSCWFjY291bnRJZA==');

@$core.Deprecated('Use getDashboardBoardResponseDescriptor instead')
const GetDashboardBoardResponse$json = {
  '1': 'GetDashboardBoardResponse',
  '2': [
    {
      '1': 'board',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.DashboardBoard',
      '10': 'board'
    },
  ],
};

/// Descriptor for `GetDashboardBoardResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List getDashboardBoardResponseDescriptor =
    $convert.base64Decode(
        'ChlHZXREYXNoYm9hcmRCb2FyZFJlc3BvbnNlEi8KBWJvYXJkGAEgASgLMhkuY2xpZW50LnYwLk'
        'Rhc2hib2FyZEJvYXJkUgVib2FyZA==');

@$core.Deprecated('Use replaceDashboardBoardRequestDescriptor instead')
const ReplaceDashboardBoardRequest$json = {
  '1': 'ReplaceDashboardBoardRequest',
  '2': [
    {
      '1': 'profile',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.ProfileRef',
      '10': 'profile'
    },
    {'1': 'account_id', '3': 2, '4': 1, '5': 9, '10': 'accountId'},
    {
      '1': 'expected_revision',
      '3': 3,
      '4': 1,
      '5': 4,
      '9': 0,
      '10': 'expectedRevision',
      '17': true
    },
    {
      '1': 'layout',
      '3': 4,
      '4': 1,
      '5': 11,
      '6': '.client.v0.DashboardLayout',
      '10': 'layout'
    },
  ],
  '8': [
    {'1': '_expected_revision'},
  ],
};

/// Descriptor for `ReplaceDashboardBoardRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List replaceDashboardBoardRequestDescriptor = $convert.base64Decode(
    'ChxSZXBsYWNlRGFzaGJvYXJkQm9hcmRSZXF1ZXN0Ei8KB3Byb2ZpbGUYASABKAsyFS5jbGllbn'
    'QudjAuUHJvZmlsZVJlZlIHcHJvZmlsZRIdCgphY2NvdW50X2lkGAIgASgJUglhY2NvdW50SWQS'
    'MAoRZXhwZWN0ZWRfcmV2aXNpb24YAyABKARIAFIQZXhwZWN0ZWRSZXZpc2lvbogBARIyCgZsYX'
    'lvdXQYBCABKAsyGi5jbGllbnQudjAuRGFzaGJvYXJkTGF5b3V0UgZsYXlvdXRCFAoSX2V4cGVj'
    'dGVkX3JldmlzaW9u');

@$core.Deprecated('Use replaceDashboardBoardResponseDescriptor instead')
const ReplaceDashboardBoardResponse$json = {
  '1': 'ReplaceDashboardBoardResponse',
  '2': [
    {
      '1': 'board',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.client.v0.DashboardBoard',
      '10': 'board'
    },
  ],
};

/// Descriptor for `ReplaceDashboardBoardResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List replaceDashboardBoardResponseDescriptor =
    $convert.base64Decode(
        'Ch1SZXBsYWNlRGFzaGJvYXJkQm9hcmRSZXNwb25zZRIvCgVib2FyZBgBIAEoCzIZLmNsaWVudC'
        '52MC5EYXNoYm9hcmRCb2FyZFIFYm9hcmQ=');
