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
