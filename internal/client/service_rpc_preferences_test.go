package client

import (
	"reflect"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestRPCPreferencePatchPresenceAndValidation(t *testing.T) {
	patch := &ipc.PreferencesPatch{AllowInbound: proto.Bool(false), AcceptDns: proto.Bool(true), AcceptRoutes: proto.Bool(false), RuntimeStart: ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_KEEP_INTENT.Enum(), UiQuit: ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT.Enum(), UserLogoff: ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_KEEP_INTENT.Enum(), Suspend: ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT.Enum(), Resume: ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_CONNECT.Enum()}
	before := proto.Clone(patch)
	keys, err := rpcPreferencePatchKeys(patch)
	if err != nil || !reflect.DeepEqual(keys, []ipc.PreferenceKey{1, 2, 3, 4, 5, 6, 7, 8}) || !proto.Equal(before, patch) {
		t.Fatal("patch presence or identity lost", err)
	}
	falseOnly := &ipc.PreferencesPatch{AllowInbound: proto.Bool(false)}
	keys, err = rpcPreferencePatchKeys(falseOnly)
	if err != nil || !reflect.DeepEqual(keys, []ipc.PreferenceKey{ipc.PreferenceKey_PREFERENCE_KEY_ALLOW_INBOUND}) {
		t.Fatal("explicit false was omitted", err)
	}
	unknown := &ipc.PreferencesPatch{AllowInbound: proto.Bool(true)}
	unknown.ProtoReflect().SetUnknown([]byte{0x48, 0x01})
	for _, invalid := range []*ipc.PreferencesPatch{nil, {}, {UiQuit: ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_UNSPECIFIED.Enum()}, {AllowInbound: proto.Bool(true), Resume: ipc.LifecycleBehavior(99).Enum()}, unknown} {
		keys, err := rpcPreferencePatchKeys(invalid)
		assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
		if keys != nil {
			t.Fatal("invalid patch yielded a partial apply list")
		}
	}
}

func TestRPCPreferenceResetValidation(t *testing.T) {
	for _, keys := range [][]ipc.PreferenceKey{nil, {}, {0}, {99}, {1, 1}, {1, 99}} {
		assertRPCFailure(t, rpcValidatePreferenceReset(keys), ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	}
	if err := rpcValidatePreferenceReset([]ipc.PreferenceKey{1, 2, 3, 4, 5, 6, 7, 8}); err != nil {
		t.Fatal(err)
	}
}
