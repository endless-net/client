// This is a generated file - do not edit.
//
// Generated from client/v0/service.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, prefer_relative_imports

import 'dart:core' as $core;

import 'package:protobuf/protobuf.dart' as $pb;

class Domain extends $pb.ProtobufEnum {
  static const Domain DOMAIN_UNSPECIFIED =
      Domain._(0, _omitEnumNames ? '' : 'DOMAIN_UNSPECIFIED');
  static const Domain DOMAIN_PROFILES =
      Domain._(1, _omitEnumNames ? '' : 'DOMAIN_PROFILES');
  static const Domain DOMAIN_NETWORKS =
      Domain._(2, _omitEnumNames ? '' : 'DOMAIN_NETWORKS');
  static const Domain DOMAIN_PEERS =
      Domain._(3, _omitEnumNames ? '' : 'DOMAIN_PEERS');
  static const Domain DOMAIN_EXIT_NODE =
      Domain._(4, _omitEnumNames ? '' : 'DOMAIN_EXIT_NODE');
  static const Domain DOMAIN_PREFERENCES =
      Domain._(5, _omitEnumNames ? '' : 'DOMAIN_PREFERENCES');
  static const Domain DOMAIN_MANAGED_SETTINGS =
      Domain._(6, _omitEnumNames ? '' : 'DOMAIN_MANAGED_SETTINGS');
  static const Domain DOMAIN_RESOURCES =
      Domain._(7, _omitEnumNames ? '' : 'DOMAIN_RESOURCES');
  static const Domain DOMAIN_UPDATES =
      Domain._(8, _omitEnumNames ? '' : 'DOMAIN_UPDATES');
  static const Domain DOMAIN_SUPPORT =
      Domain._(9, _omitEnumNames ? '' : 'DOMAIN_SUPPORT');
  static const Domain DOMAIN_SERVER_IDENTITY =
      Domain._(10, _omitEnumNames ? '' : 'DOMAIN_SERVER_IDENTITY');
  static const Domain DOMAIN_SESSION =
      Domain._(11, _omitEnumNames ? '' : 'DOMAIN_SESSION');

  static const $core.List<Domain> values = <Domain>[
    DOMAIN_UNSPECIFIED,
    DOMAIN_PROFILES,
    DOMAIN_NETWORKS,
    DOMAIN_PEERS,
    DOMAIN_EXIT_NODE,
    DOMAIN_PREFERENCES,
    DOMAIN_MANAGED_SETTINGS,
    DOMAIN_RESOURCES,
    DOMAIN_UPDATES,
    DOMAIN_SUPPORT,
    DOMAIN_SERVER_IDENTITY,
    DOMAIN_SESSION,
  ];

  static final $core.List<Domain?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 11);
  static Domain? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const Domain._(super.value, super.name);
}

const $core.bool _omitEnumNames =
    $core.bool.fromEnvironment('protobuf.omit_enum_names');
