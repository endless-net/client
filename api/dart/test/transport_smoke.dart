import 'dart:async';
import 'dart:io';

import 'package:endlessnet_client_api/client_api.dart' hide Platform;
import 'package:fixnum/fixnum.dart';
import 'package:grpc/grpc.dart';
import 'package:http2/transport.dart' show ClientTransportConnection;

void require(bool value, String message) {
  if (!value) throw StateError(message);
}

String hex(List<int> bytes) =>
    bytes.map((v) => v.toRadixString(16).padLeft(2, '0')).join();

final class UnixConnector implements ClientTransportConnector {
  UnixConnector(this.path);
  final String path;
  Socket? socket;
  @override
  String get authority => 'endlessnet.local';
  @override
  Future<void> get done => socket?.done.then<void>((_) {}) ?? Future.value();
  @override
  Future<ClientTransportConnection> connect() async {
    final opened = await Socket.connect(
      InternetAddress(path, type: InternetAddressType.unix),
      0,
    );
    socket = opened;
    return ClientTransportConnection.viaSocket(opened);
  }

  @override
  void shutdown() => socket?.destroy();
}

Future<void> main(List<String> args) async {
  final request = SelectExitNodeRequest(
    mutation: MutationContext(
      expectedInstanceId: 'instance-fixture',
      expectedRevision: Int64(7),
    ),
    profile: ProfileRef(profileId: 'profile-fixture'),
    exitNodeId: 'exit-fixture',
    lanAccess: LanAccess.LAN_ACCESS_BLOCK,
    familyMode: ExitFamilyMode.EXIT_FAMILY_MODE_DUAL_STACK,
  );
  const vector =
      '0a140a10696e7374616e63652d66697874757265100712110a0f70726f66696c652d666978747572651a0c657869742d6669787475726520012804';
  require(hex(request.writeToBuffer()) == vector, 'Go/Dart bytes differ');
  final decoded = SelectExitNodeRequest.fromBuffer(request.writeToBuffer());
  require(decoded.profile.profileId == 'profile-fixture', 'profile lost');
  require(
    decoded.familyMode == ExitFamilyMode.EXIT_FAMILY_MODE_DUAL_STACK,
    'family lost',
  );
  require(PreferencesPatch(acceptDns: false).hasAcceptDns(), 'presence lost');
  require(!PreferencesPatch().hasAcceptDns(), 'absence lost');
  for (final bad in <List<int>>[
    [0x0a, 0x80],
    [0xff],
  ]) {
    var rejected = false;
    try {
      MutationContext.fromBuffer(bad);
    } catch (_) {
      rejected = true;
    }
    require(rejected, 'malformed protobuf accepted');
  }
  stdout.writeln('DART_VECTOR=$vector');
  if (args.isEmpty) {
    stdout.writeln('serialization PASS; transport not requested');
    return;
  }
  final network = args[0];
  final endpoint = args[1];
  final digest = args[2];
  final mutation = args.length > 3 ? args[3] : '';
  const options = ChannelOptions(credentials: ChannelCredentials.insecure());
  final channel = network == 'unix'
      ? ClientTransportConnectorChannel(
          UnixConnector(endpoint),
          options: options,
        )
      : ClientChannel(
          '127.0.0.1',
          port: int.parse(endpoint.split(':').last),
          options: options,
        );
  final client = ClientServiceClient(channel);
  try {
    final runtime = (await client.getRuntimeInfo(
      GetRuntimeInfoRequest(),
      options: CallOptions(timeout: const Duration(seconds: 3)),
    )).runtime;
    require(
      runtime.instanceId.isNotEmpty &&
          runtime.protocol == 'endlessnet-client-ipc' &&
          runtime.ipcVersion == 0 &&
          runtime.contractSha256 == digest,
      'bootstrap mismatch',
    );
    final metadata = <String, String>{
      'x-endlessnet-ipc-contract-sha256': digest,
    };
    final status = await client.getStatus(
      GetStatusRequest(),
      options: CallOptions(
        metadata: metadata,
        timeout: const Duration(seconds: 3),
      ),
    );
    require(status.status.metadata.revision == Int64(7), 'unary revision lost');
    var rejected = false;
    try {
      await client.getStatus(
        GetStatusRequest(),
        options: CallOptions(
          metadata: {'x-endlessnet-ipc-contract-sha256': 'wrong'},
          timeout: const Duration(seconds: 3),
        ),
      );
    } on GrpcError catch (error) {
      rejected = error.code == StatusCode.failedPrecondition;
    }
    require(rejected, 'mismatched descriptor accepted');
    rejected = false;
    try {
      await client.disconnect(
        DisconnectRequest(),
        options: CallOptions(timeout: const Duration(seconds: 3)),
      );
    } on GrpcError catch (error) {
      require(error.code == StatusCode.permissionDenied, 'wrong RPC status');
      for (final detail in error.details ?? []) {
        if (detail.info_.qualifiedMessageName == 'google.protobuf.Any' &&
            detail.getField(1) == 'type.googleapis.com/client.v0.Failure') {
          final failure = Failure.fromBuffer(detail.getField(2) as List<int>);
          rejected =
              failure.code == ErrorCode.ERROR_CODE_OWNER_REQUIRED &&
              failure.reasonKey == 'fixture.owner_required';
        }
      }
    }
    require(rejected, 'typed Failure detail lost');
    final stream = client.watchEvents(
      WatchEventsRequest(),
      options: CallOptions(timeout: const Duration(seconds: 10)),
    );
    final iterator = StreamIterator<WatchEventsResponse>(stream);
    require(
      await iterator.moveNext() &&
          iterator.current.sequence == Int64(1) &&
          iterator.current.hasSnapshot(),
      'missing initial snapshot',
    );
    require(
      await iterator.moveNext() &&
          iterator.current.sequence == Int64(2) &&
          iterator.current.hasInvalidated(),
      'stream order lost',
    );
    if (mutation != 'omit-cancel') {
      await stream.cancel();
      await iterator.cancel();
    }
    // The server must observe cancellation while this channel is still open,
    // well before the stream's own deadline. Teardown cannot satisfy this probe.
    try {
      await client.getRuntimeInfo(
        GetRuntimeInfoRequest(),
        options: CallOptions(
          metadata: {'x-endlessnet-conformance-probe': 'watch-cancelled'},
          timeout: const Duration(seconds: 2),
        ),
      );
    } on GrpcError catch (error) {
      throw StateError('CONFORMANCE_CANCEL_ACK_FAILED: ${error.message}');
    }
    stdout.writeln('CONFORMANCE_CANCEL_ACK');
    rejected = false;
    try {
      await client.getEnrollmentAttempt(
        GetEnrollmentAttemptRequest(),
        options: CallOptions(timeout: const Duration(seconds: 3)),
      );
    } on GrpcError catch (error) {
      rejected =
          error.code == StatusCode.deadlineExceeded &&
          error.message == 'CONFORMANCE_SERVER_DEADLINE_EXCEEDED';
      if (!rejected) stderr.writeln('CONFORMANCE_DEADLINE_ERROR: $error');
    }
    require(rejected, 'CONFORMANCE_DEADLINE_NOT_FROM_SERVER');
    stdout.writeln('CONFORMANCE_DEADLINE_CONFIRMED');
    stdout.writeln(
      'bootstrap/unary/typed-error/stream/order/cancellation/deadline PASS ($network fixture; no OS-auth claim)',
    );
  } finally {
    await channel.shutdown();
  }
}
