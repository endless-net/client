package client

import (
	"reflect"
	"testing"

	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestRPCPreferenceReadAuthorizationAndValidation(t *testing.T) {
	m, owner, profile := rpcPreferenceFixture(t)
	s := NewClientRPCService(m, nil)
	before := m.store.Read()
	for _, scenario := range []struct {
		name    string
		peer    local.Peer
		ref     *ipc.ProfileRef
		failure ipc.ErrorCode
	}{
		{"owner", owner, profile, ipc.ErrorCode_ERROR_CODE_UNSPECIFIED},
		{"administrator", local.Peer{Identity: "uid:2000", Administrator: true}, profile, ipc.ErrorCode_ERROR_CODE_UNSPECIFIED},
		{"anonymous", local.Peer{}, profile, ipc.ErrorCode_ERROR_CODE_UNAUTHENTICATED},
		{"anonymous administrator", local.Peer{Administrator: true}, profile, ipc.ErrorCode_ERROR_CODE_UNAUTHENTICATED},
		{"observer", local.Peer{Identity: "uid:2000"}, profile, ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED},
		{"observer missing profile", local.Peer{Identity: "uid:2000"}, &ipc.ProfileRef{ProfileId: "missing"}, ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED},
		{"observer malformed profile", local.Peer{Identity: "uid:2000"}, nil, ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED},
		{"owner nil profile", owner, nil, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT},
		{"owner empty profile", owner, &ipc.ProfileRef{}, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT},
		{"owner missing profile", owner, &ipc.ProfileRef{ProfileId: "missing"}, ipc.ErrorCode_ERROR_CODE_NOT_FOUND},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			response, err := s.preferencesAs(scenario.peer, &ipc.GetPreferencesRequest{Profile: scenario.ref})
			if scenario.failure != ipc.ErrorCode_ERROR_CODE_UNSPECIFIED {
				assertRPCFailure(t, err, scenario.failure)
				if response != nil {
					t.Fatal("failed read exposed preferences")
				}
			} else if err != nil || response.Msg.Preferences.ProfileId != profile.ProfileId {
				t.Fatal("authorized read failed", err)
			}
			if !reflect.DeepEqual(before, m.store.Read()) {
				t.Fatal("preference read changed durable state")
			}
		})
	}
}

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
