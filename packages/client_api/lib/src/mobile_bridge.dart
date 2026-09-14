import 'dart:typed_data';

/// Producer-owned logical mobile boundary. Implement this for a UI mock or a
/// native adapter; no native transport is supplied by this package.
///
/// Normative lifecycle: docs/client-mobile-bridge.md in the pinned source commit.
abstract interface class MobileBridge {
  /// Opens the installation's fixed host, without connecting VPN or prompting.
  /// Non-positive timeouts complete with [MobileBridgeCode.invalidArgument].
  MobileOpenAttempt open({required int timeoutMillis});
}

abstract interface class MobileOpenAttempt {
  Future<MobileOpenResult> get result;

  /// Idempotent; cancels waiting, not an already completed open.
  void cancel();
}

abstract interface class MobileChannel {
  MobileCall unary(MobileRequest request, {required int timeoutMillis});

  /// Only WatchEvents is server-streaming in v0. Callbacks are sequential and
  /// their futures acknowledge consumption. An absent lifetime has no timeout.
  MobileSubscription subscribe(
    MobileRequest request, {
    int? lifetimeMillis,
    required Future<void> Function(Uint8List event) onEvent,
  });

  /// Idempotently closes handles without Disconnect, Logout or NotifyLifecycle.
  Future<void> close();
}

abstract interface class MobileCall {
  /// Channel-scoped transport ID, never MutationContext.request_id.
  String get id;
  Future<MobileUnaryResult> get result;

  /// Cancels waiting only; accepted runtime work may still complete.
  void cancel();
}

abstract interface class MobileSubscription {
  String get id;
  Future<MobileStreamEnd> get ended;

  /// No new callbacks after terminal; an in-flight callback may finish later.
  void cancel();
}

/// List entries preserve duplicate headers for producer validation.
final class MobileMetadataEntry {
  const MobileMetadataEntry(this.name, this.value);

  /// Lowercase metadata name; values are not corrected or autofilled.
  final String name;
  final String value;
}

/// Immutable request; payload is one serialized v0 message without framing.
final class MobileRequest {
  MobileRequest({
    required this.procedure,
    required List<MobileMetadataEntry> metadata,
    required List<int> payload,
  }) : metadata = List<MobileMetadataEntry>.unmodifiable(metadata),
       payload = Uint8List.fromList(payload).asUnmodifiableView();

  /// Exact /client.v0.ClientService/<Method> name from the descriptor.
  final String procedure;
  final List<MobileMetadataEntry> metadata;
  final Uint8List payload;
}

sealed class MobileOpenResult {
  const MobileOpenResult();
}

sealed class MobileUnaryResult {
  const MobileUnaryResult();
}

sealed class MobileStreamEnd {
  const MobileStreamEnd();
}

final class MobileOpened extends MobileOpenResult {
  const MobileOpened(this.channel);
  final MobileChannel channel;
}

/// RPC success only. A returned Operation can still be pending or failed.
final class MobileSuccess extends MobileUnaryResult {
  MobileSuccess(List<int> payload)
    : payload = Uint8List.fromList(payload).asUnmodifiableView();

  final Uint8List payload;
}

/// Clean producer completion after all accepted events have been consumed.
final class MobileCompleted extends MobileStreamEnd {
  const MobileCompleted();
}

/// Standard RPC statuses, excluding OK. Not a new Protobuf error enumeration.
enum MobileRpcStatus {
  cancelled,
  unknown,
  invalidArgument,
  deadlineExceeded,
  notFound,
  alreadyExists,
  permissionDenied,
  resourceExhausted,
  failedPrecondition,
  aborted,
  outOfRange,
  unimplemented,
  internal,
  unavailable,
  dataLoss,
  unauthenticated,
}

/// A producer RPC error, distinct from a local channel error and from an
/// Operation.failure or WatchEventsResponse.failure inside a successful payload.
final class MobileRpcError extends MobileUnaryResult implements MobileStreamEnd {
  MobileRpcError({required this.status, List<int>? failure})
    : failure = failure == null
          ? null
          : Uint8List.fromList(failure).asUnmodifiableView();

  final MobileRpcStatus status;

  /// Serialized client.v0.Failure typed detail; absent when none was received.
  /// Never an exception string, JSON object, or fabricated domain failure.
  final Uint8List? failure;
}

enum MobileBridgeCode {
  unavailable,
  accessDenied,
  invalidArgument,
  invalidResponse,
  deadlineExceeded,
  cancelled,
  channelClosed,
  channelLost,
  limitExceeded,
  internal,
}

/// Local adapter error. Contains neither RPC status nor diagnostic payload.
final class MobileBridgeError extends MobileUnaryResult
    implements MobileOpenResult, MobileStreamEnd {
  const MobileBridgeError(this.code);
  final MobileBridgeCode code;
}

/// Byte limits exclude native framing. Queue bounds exclude one in-flight event.
abstract final class MobileBridgeLimits {
  static const maxRequestBytes = 64 * 1024;
  static const maxResponseBytes = 4 * 1024 * 1024;
  static const maxQueuedEvents = 64;
  static const maxQueuedEventBytes = 8 * 1024 * 1024;
}
