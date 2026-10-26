// Producer-specific launcher for the pinned public protoc_plugin package.
import 'dart:io';
import 'package:protoc_plugin/protoc.dart';

void main() {
  if (Platform.version.split(' ').first != '3.13.5') {
    stderr.writeln('Client API generator requires Dart 3.13.5.');
    exitCode = 78;
    return;
  }
  CodeGenerator(stdin, stdout).generate();
}
