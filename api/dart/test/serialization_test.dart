import 'package:endlessnet_client_api/client_api.dart';
import 'package:fixnum/fixnum.dart';
import 'package:protobuf/protobuf.dart';
import 'package:test/test.dart';

void main() {
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
