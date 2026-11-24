// This is a generated file - do not edit.
//
// Generated from client/v0/dashboard.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, prefer_relative_imports

import 'dart:core' as $core;

import 'package:protobuf/protobuf.dart' as $pb;

class DashboardWidgetKind extends $pb.ProtobufEnum {
  static const DashboardWidgetKind DASHBOARD_WIDGET_KIND_UNSPECIFIED =
      DashboardWidgetKind._(
          0, _omitEnumNames ? '' : 'DASHBOARD_WIDGET_KIND_UNSPECIFIED');
  static const DashboardWidgetKind DASHBOARD_WIDGET_KIND_HARDWARE =
      DashboardWidgetKind._(
          1, _omitEnumNames ? '' : 'DASHBOARD_WIDGET_KIND_HARDWARE');
  static const DashboardWidgetKind DASHBOARD_WIDGET_KIND_NETWORK =
      DashboardWidgetKind._(
          2, _omitEnumNames ? '' : 'DASHBOARD_WIDGET_KIND_NETWORK');
  static const DashboardWidgetKind DASHBOARD_WIDGET_KIND_DISK =
      DashboardWidgetKind._(
          3, _omitEnumNames ? '' : 'DASHBOARD_WIDGET_KIND_DISK');
  static const DashboardWidgetKind DASHBOARD_WIDGET_KIND_PERIPHERALS =
      DashboardWidgetKind._(
          4, _omitEnumNames ? '' : 'DASHBOARD_WIDGET_KIND_PERIPHERALS');
  static const DashboardWidgetKind DASHBOARD_WIDGET_KIND_COMPUTER =
      DashboardWidgetKind._(
          5, _omitEnumNames ? '' : 'DASHBOARD_WIDGET_KIND_COMPUTER');
  static const DashboardWidgetKind DASHBOARD_WIDGET_KIND_TUNNEL =
      DashboardWidgetKind._(
          6, _omitEnumNames ? '' : 'DASHBOARD_WIDGET_KIND_TUNNEL');

  static const $core.List<DashboardWidgetKind> values = <DashboardWidgetKind>[
    DASHBOARD_WIDGET_KIND_UNSPECIFIED,
    DASHBOARD_WIDGET_KIND_HARDWARE,
    DASHBOARD_WIDGET_KIND_NETWORK,
    DASHBOARD_WIDGET_KIND_DISK,
    DASHBOARD_WIDGET_KIND_PERIPHERALS,
    DASHBOARD_WIDGET_KIND_COMPUTER,
    DASHBOARD_WIDGET_KIND_TUNNEL,
  ];

  static final $core.List<DashboardWidgetKind?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 6);
  static DashboardWidgetKind? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const DashboardWidgetKind._(super.value, super.name);
}

class DashboardWidgetSize extends $pb.ProtobufEnum {
  static const DashboardWidgetSize DASHBOARD_WIDGET_SIZE_UNSPECIFIED =
      DashboardWidgetSize._(
          0, _omitEnumNames ? '' : 'DASHBOARD_WIDGET_SIZE_UNSPECIFIED');
  static const DashboardWidgetSize DASHBOARD_WIDGET_SIZE_SQUARE =
      DashboardWidgetSize._(
          1, _omitEnumNames ? '' : 'DASHBOARD_WIDGET_SIZE_SQUARE');
  static const DashboardWidgetSize DASHBOARD_WIDGET_SIZE_WIDE =
      DashboardWidgetSize._(
          2, _omitEnumNames ? '' : 'DASHBOARD_WIDGET_SIZE_WIDE');
  static const DashboardWidgetSize DASHBOARD_WIDGET_SIZE_LARGE =
      DashboardWidgetSize._(
          3, _omitEnumNames ? '' : 'DASHBOARD_WIDGET_SIZE_LARGE');

  static const $core.List<DashboardWidgetSize> values = <DashboardWidgetSize>[
    DASHBOARD_WIDGET_SIZE_UNSPECIFIED,
    DASHBOARD_WIDGET_SIZE_SQUARE,
    DASHBOARD_WIDGET_SIZE_WIDE,
    DASHBOARD_WIDGET_SIZE_LARGE,
  ];

  static final $core.List<DashboardWidgetSize?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static DashboardWidgetSize? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const DashboardWidgetSize._(super.value, super.name);
}

class DashboardImage extends $pb.ProtobufEnum {
  static const DashboardImage DASHBOARD_IMAGE_UNSPECIFIED =
      DashboardImage._(0, _omitEnumNames ? '' : 'DASHBOARD_IMAGE_UNSPECIFIED');
  static const DashboardImage DASHBOARD_IMAGE_RIBBON =
      DashboardImage._(1, _omitEnumNames ? '' : 'DASHBOARD_IMAGE_RIBBON');
  static const DashboardImage DASHBOARD_IMAGE_CABIN =
      DashboardImage._(2, _omitEnumNames ? '' : 'DASHBOARD_IMAGE_CABIN');
  static const DashboardImage DASHBOARD_IMAGE_GLASS =
      DashboardImage._(3, _omitEnumNames ? '' : 'DASHBOARD_IMAGE_GLASS');

  static const $core.List<DashboardImage> values = <DashboardImage>[
    DASHBOARD_IMAGE_UNSPECIFIED,
    DASHBOARD_IMAGE_RIBBON,
    DASHBOARD_IMAGE_CABIN,
    DASHBOARD_IMAGE_GLASS,
  ];

  static final $core.List<DashboardImage?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static DashboardImage? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const DashboardImage._(super.value, super.name);
}

const $core.bool _omitEnumNames =
    $core.bool.fromEnvironment('protobuf.omit_enum_names');
