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
}
