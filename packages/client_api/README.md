# endlessnet_client_api

Generated Dart/Flutter messages and gRPC client/server bindings for
`client.v0.ClientService`. Import `package:endlessnet_client_api/client_api.dart`.
The initial private package version is 0.0.0; the protocol remains v0.

The canonical source is `proto/client/v0` in this repository. Run `buf generate`
from the repository root with the generators pinned in the Protobuf CI workflow.
Do not edit `lib/src/gen` manually. CI regenerates it and runs Dart analysis.

Consumers must pin an immutable source commit. Supply a platform-authorized
local gRPC channel to the generated ClientServiceClient. These bindings do not
implement Windows named-pipe dialing, Unix peer credentials or a mobile bridge.
The Go Connect handler supports the gRPC protocol used by this Dart client.
No HTTP Connect wrapper or production transport adapter is included.

For Android/iOS adapter and UI mock development, use the producer-owned
[Mobile Bridge contract](../../docs/client-mobile-bridge.md). It defines logical
calls, Protobuf payloads, errors, subscriptions and cancellation. Implement the
same consumer interface for the mock and future native adapter. The exported
[`MobileBridge` interfaces and result types](lib/src/mobile_bridge.dart) are
handwritten contract code; they do not implement a native transport or a mobile
gRPC channel. Pin the specification and package to the same source commit.

```dart
import 'package:endlessnet_client_api/client_api.dart';

Future<GetRuntimeInfoResponse> bootstrap(MobileBridge bridge) async {
  final opened = await bridge.open(timeoutMillis: 5000).result;
  if (opened is! MobileOpened) throw StateError('Mobile host unavailable');
  final channel = opened.channel;
  try {
    final result = await channel.unary(
      MobileRequest(
        procedure: '/client.v0.ClientService/GetRuntimeInfo',
        metadata: const [],
        payload: GetRuntimeInfoRequest().writeToBuffer(),
      ),
      timeoutMillis: 5000,
    ).result;
    if (result is! MobileSuccess) throw StateError('Bootstrap failed');
    final response = GetRuntimeInfoResponse.fromBuffer(result.payload);
    final runtime = response.runtime;
    if (runtime.protocol != ClientContract.protocol ||
        runtime.ipcVersion != ClientContract.version ||
        runtime.contractSha256 != ClientContract.sha256 ||
        runtime.instanceId.isEmpty) {
      throw StateError('Incompatible mobile runtime');
    }
    return response;
  } finally {
    await channel.close();
  }
}
```

This short probe closes its channel. A running UI retains the channel for status
and subscriptions and handles `MobileRpcError` and `MobileBridgeError` separately.
For subsequent calls, convert `ClientContract.metadata.entries` to a list of
`MobileMetadataEntry(entry.key, entry.value)`; preserve duplicate entries in
negative fixtures. Decode `MobileRpcError.failure`, when present, with
`Failure.fromBuffer`. Implement `MobileBridge`, `MobileChannel`, `MobileCall`,
`MobileOpenAttempt` and `MobileSubscription` in your mock; the normative document
defines cancellation, terminal races, snapshot-first delivery and queue bounds.
Constructors copy payloads and metadata into immutable values; adapter methods
report validation failures asynchronously through the declared result types.

`ClientContract` exports the exact descriptor SHA-256, protocol/version and
lowercase gRPC metadata. Bootstrap must validate all three plus the runtime
instance ID; subsequent calls send `ClientContract.metadata`. Regenerate these
constants after the embedded descriptor with `go run ./internal/dartmetadata`
from `clientipc`. CI regenerates and checks them together with both SDKs.
