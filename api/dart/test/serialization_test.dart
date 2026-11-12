import 'package:endlessnet_client_api/client_api.dart';
import 'package:fixnum/fixnum.dart';
import 'package:protobuf/protobuf.dart';
import 'package:test/test.dart';

void main() {
  test('sole node local readiness is distinct from peer connectivity', () {
    final status = Status(
      serviceState: ServiceState.SERVICE_STATE_LOCAL_READY,
      controlState: ControlState.CONTROL_STATE_READY,
      connectionPhase: ConnectionPhase.CONNECTION_PHASE_LOCAL_READY,
      mapRevision: Int64(7),
      overlayAddresses: ['100.96.0.1'],
    );
    final decoded = Status.fromBuffer(status.writeToBuffer());
    expect(decoded.serviceState, ServiceState.SERVICE_STATE_LOCAL_READY);
    expect(
      decoded.connectionPhase,
      ConnectionPhase.CONNECTION_PHASE_LOCAL_READY,
    );
    expect(
      decoded.connectionPhase,
      isNot(ConnectionPhase.CONNECTION_PHASE_CONNECTED),
    );
    expect(decoded.peerCount, 0);
    expect(decoded.mapRevision, Int64(7));
    expect(decoded.connectionPhase.value, 5);
    expect(decoded.serviceState.value, 12);
  });
  test('original mode and hostname support exact enrollment replay', () {
    final attempt = EnrollmentAttempt(
      requestId: '611fb038-329c-4de5-872a-89bcab17287a',
      mode: EnrollmentMode.ENROLLMENT_MODE_SERVER,
      hostname: 'host',
    );
    final decoded = EnrollmentAttempt.fromBuffer(attempt.writeToBuffer());
    final retry = EnrollRequest(
      requestId: decoded.requestId,
      mode: decoded.mode,
      hostname: decoded.hostname,
    );
    expect(retry.requestId, attempt.requestId);
    expect(retry.mode, EnrollmentMode.ENROLLMENT_MODE_SERVER);
    expect(retry.hostname, 'host');
    expect(
      EnrollmentAttempt(
        mode: EnrollmentMode.ENROLLMENT_MODE_SERVER,
        hostname: 'host',
      ).writeToBuffer(),
      [0x90, 0x01, 0x02, 0x9a, 0x01, 0x04, 0x68, 0x6f, 0x73, 0x74],
    );
    expect(
      EnrollmentAttempt().mode,
      EnrollmentMode.ENROLLMENT_MODE_UNSPECIFIED,
    );
    expect(EnrollmentAttempt().hostname, '');
  });
  test(
    'current enrollment lookup preserves oneof presence and shared wire',
    () {
      for (final value in [true, false]) {
        final lookup = GetEnrollmentAttemptRequest(current: value);
        final wire = lookup.writeToBuffer();
        expect(wire, [0x20, value ? 1 : 0]);
        final decoded = GetEnrollmentAttemptRequest.fromBuffer(wire);
        expect(
          decoded.whichSelector(),
          GetEnrollmentAttemptRequest_Selector.current,
        );
        expect(decoded.current, value);
        expect(decoded.hasRequestId(), isFalse);
        expect(decoded.hasAttemptId(), isFalse);
      }
      expect(
        GetEnrollmentAttemptRequest().whichSelector(),
        GetEnrollmentAttemptRequest_Selector.notSet,
      );
    },
  );

  test('binary lookup selectors use standard protobuf last wins', () {
    final current = GetEnrollmentAttemptRequest(current: true).writeToBuffer();
    final request = GetEnrollmentAttemptRequest(
      requestId: 'request-fixture',
    ).writeToBuffer();
    final decoded = GetEnrollmentAttemptRequest.fromBuffer([
      ...current,
      ...request,
    ]);
    expect(
      decoded.whichSelector(),
      GetEnrollmentAttemptRequest_Selector.requestId,
    );
    expect(decoded.requestId, 'request-fixture');
    expect(decoded.hasCurrent(), isFalse);
  });
  test('mutation scope and exact revision survive serialization', () {
    final original = SelectExitNodeRequest(
      mutation: MutationContext(
        expectedInstanceId: 'instance-fixture',
        expectedRevision: Int64.parseInt('9007199254740993'),
      ),
      profile: ProfileRef(profileId: 'profile-fixture'),
      exitNodeId: 'exit-fixture',
      familyMode: ExitFamilyMode.EXIT_FAMILY_MODE_DUAL_STACK,
    );
    final restored = SelectExitNodeRequest.fromBuffer(original.writeToBuffer());
    expect(restored.mutation.expectedRevision.toString(), '9007199254740993');
    expect(restored.mutation.expectedInstanceId, 'instance-fixture');
    expect(restored.profile.profileId, 'profile-fixture');
    expect(restored.exitNodeId, 'exit-fixture');
    expect(restored.familyMode, ExitFamilyMode.EXIT_FAMILY_MODE_DUAL_STACK);
  });

  test('explicit false is different from absent preference', () {
    expect(PreferencesPatch(acceptDns: false).hasAcceptDns(), isTrue);
    expect(PreferencesPatch().hasAcceptDns(), isFalse);
  });

  test('malformed typed mutation is rejected', () {
    for (final bytes in <List<int>>[
      [0x0a, 0x80],
      [0xff],
    ]) {
      expect(
        () => MutationContext.fromBuffer(bytes),
        throwsA(isA<InvalidProtocolBufferException>()),
      );
    }
  });
  test('006 exact command and unknown result retain their meaning', () {
    final command = RevokeNodeAccessRequest(
      profile: ProfileRef(profileId: 'profile'),
      commandId: '611fb038-329c-4de5-872a-89bcab17287a',
      target: NodeRevocationTarget(
        accountId: 'account',
        networkId: 'network',
        nodeId: 'lost',
        hostname: 'same-name',
        networkRevision: Int64.parseInt('9007199254740993'),
      ),
    );
    final restored = RevokeNodeAccessRequest.fromBuffer(
      command.writeToBuffer(),
    );
    expect(restored.commandId, command.commandId);
    expect(restored.target.nodeId, 'lost');
    expect(restored.target.networkRevision.toString(), '9007199254740993');
    final unknown = NodeRevocationResult(
      commandId: command.commandId,
      phase: NodeRevocationPhase.NODE_REVOCATION_PHASE_UNKNOWN,
    );
    final result = NodeRevocationResult.fromBuffer(unknown.writeToBuffer());
    expect(result.phase, NodeRevocationPhase.NODE_REVOCATION_PHASE_UNKNOWN);
    expect(result.hasAcceptedAt(), isFalse);
    expect(result.hasEnforceBy(), isFalse);
    expect(result.committedNetworkRevision, Int64.ZERO);
  });

  test('007 working Relay and Direct search remain independent', () {
    final status = Status(
      serviceState: ServiceState.SERVICE_STATE_CONNECTED,
      connectionPhase: ConnectionPhase.CONNECTION_PHASE_CONNECTED,
      peerTransports: [
        PeerTransportStatus(
          peerId: 'peer',
          selectedPath: PathKind.PATH_KIND_RELAY,
          engine: 'wireguard',
          protocolVersion: '1',
          transportProfileId: 'wg',
          transportProfileRevision: Int64.parseInt('9007199254740993'),
          searchState: TransportSearchState.TRANSPORT_SEARCH_STATE_SEARCHING,
          reasonKey: 'client.transport.searching',
        ),
      ],
    );
    final decoded = Status.fromBuffer(status.writeToBuffer());
    expect(decoded.serviceState, ServiceState.SERVICE_STATE_CONNECTED);
    expect(
      decoded.peerTransports.single.selectedPath,
      PathKind.PATH_KIND_RELAY,
    );
    expect(
      decoded.peerTransports.single.searchState,
      TransportSearchState.TRANSPORT_SEARCH_STATE_SEARCHING,
    );
    expect(
      decoded.peerTransports.single.transportProfileRevision.toString(),
      '9007199254740993',
    );
    expect(
      PeerTransportStatus(
        searchState: TransportSearchState.TRANSPORT_SEARCH_STATE_UNSUPPORTED,
      ).selectedPath,
      PathKind.PATH_KIND_UNSPECIFIED,
    );
  });

  test('003 request lookup and uncertain enrollment stay separate', () {
    final lookup = GetEnrollmentAttemptRequest(
      profile: ProfileRef(profileId: 'profile-fixture'),
      requestId: '611fb038-329c-4de5-872a-89bcab17287a',
    );
    final restored = GetEnrollmentAttemptRequest.fromBuffer(
      lookup.writeToBuffer(),
    );
    expect(restored.requestId, lookup.requestId);
    expect(restored.hasAttemptId(), isFalse);
    final attempt = EnrollmentAttempt(
      phase: EnrollmentPhase.ENROLLMENT_PHASE_COMPLETING,
      certainty: EnrollmentCertainty.ENROLLMENT_CERTAINTY_UNCERTAIN,
    );
    final decoded = EnrollmentAttempt.fromBuffer(attempt.writeToBuffer());
    expect(
      decoded.certainty,
      EnrollmentCertainty.ENROLLMENT_CERTAINTY_UNCERTAIN,
    );
    expect(decoded.phase, isNot(EnrollmentPhase.ENROLLMENT_PHASE_REGISTERED));
    expect(decoded.hasNodeId(), isFalse);
  });
}
