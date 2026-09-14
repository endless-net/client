// Dependency-free contract checks, executed by the Protobuf CI workflow.
import 'dart:typed_data';

import 'package:endlessnet_client_api/client_api.dart';

void check(bool condition, String message) {
  if (!condition) throw StateError(message);
}

void rejectsMutation(void Function() mutation) {
  try {
    mutation();
  } on UnsupportedError {
    return;
  }
  throw StateError('Contract value was mutable');
}

// Exhaustive switches check that consumers can distinguish every legal outcome.
String unaryKind(MobileUnaryResult result) => switch (result) {
  MobileSuccess() => 'success',
  MobileRpcError() => 'rpc',
  MobileBridgeError() => 'bridge',
};

String openKind(MobileOpenResult result) => switch (result) {
  MobileOpened() => 'opened',
  MobileBridgeError() => 'bridge',
};

String streamKind(MobileStreamEnd result) => switch (result) {
  MobileCompleted() => 'completed',
  MobileRpcError() => 'rpc',
  MobileBridgeError() => 'bridge',
};

void main() {
  final bytes = Uint8List.fromList(
    GetOperationRequest(requestId: 'fixture-request').writeToBuffer(),
  );
  final metadata = [
    const MobileMetadataEntry('x-endlessnet-ipc-version', '0'),
    const MobileMetadataEntry('x-endlessnet-ipc-version', 'wrong'),
  ];
  final request = MobileRequest(
    procedure: '/client.v0.ClientService/GetOperation',
    metadata: metadata,
    payload: bytes,
  );
  bytes.fillRange(0, bytes.length, 0);
  metadata.clear();
  check(
    GetOperationRequest.fromBuffer(request.payload).requestId ==
        'fixture-request',
    'Request must own its Protobuf bytes',
  );
  check(request.metadata.length == 2, 'Duplicate metadata was lost');
  check(request.metadata.last.value == 'wrong', 'Metadata was corrected');
  rejectsMutation(() => request.payload[0] = 0);
  rejectsMutation(() => request.metadata.clear());

  final responseBytes = GetRuntimeInfoResponse(
    runtime: RuntimeInfo(
      protocol: ClientContract.protocol,
      ipcVersion: ClientContract.version,
      contractSha256: ClientContract.sha256,
      instanceId: 'fixture-instance',
    ),
  ).writeToBuffer();
  final success = MobileSuccess(responseBytes);
  responseBytes.fillRange(0, responseBytes.length, 0);
  final runtime = GetRuntimeInfoResponse.fromBuffer(success.payload).runtime;
  check(runtime.contractSha256 == ClientContract.sha256, 'Digest was lost');
  check(runtime.instanceId == 'fixture-instance', 'Instance was lost');
  rejectsMutation(() => success.payload[0] = 0);
  check(unaryKind(success) == 'success', 'Success type mismatch');

  final failureBytes = Failure(
    code: ErrorCode.ERROR_CODE_OWNER_REQUIRED,
    reasonKey: 'owner_required',
  ).writeToBuffer();
  final rpcError = MobileRpcError(
    status: MobileRpcStatus.permissionDenied,
    failure: failureBytes,
  );
  failureBytes.fillRange(0, failureBytes.length, 0);
  check(
    Failure.fromBuffer(rpcError.failure!).code ==
        ErrorCode.ERROR_CODE_OWNER_REQUIRED,
    'Typed RPC failure was lost',
  );
  rejectsMutation(() => rpcError.failure![0] = 0);
  check(unaryKind(rpcError) == 'rpc', 'Unary RPC error type mismatch');
  check(streamKind(rpcError) == 'rpc', 'Stream RPC error type mismatch');
  check(
    MobileRpcError(status: MobileRpcStatus.unavailable).failure == null,
    'Missing typed detail must remain absent',
  );

  const lost = MobileBridgeError(MobileBridgeCode.channelLost);
  check(unaryKind(lost) == 'bridge', 'Unary bridge error type mismatch');
  check(openKind(lost) == 'bridge', 'Open bridge error type mismatch');
  check(streamKind(lost) == 'bridge', 'Stream bridge error type mismatch');
  check(
    streamKind(const MobileCompleted()) == 'completed',
    'Clean stream completion type mismatch',
  );
}
