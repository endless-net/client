package client

import (
	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Validate the entire patch before consulting policy or preparing any mutation.
// Presence (including explicit false) is significant. This is structural
// validation only: each selected key also needs provider/platform/policy checks
// and an atomic effect/rollback plan before the mutation can be accepted.
func rpcPreferencePatchKeys(patch *ipc.PreferencesPatch) ([]ipc.PreferenceKey, error) {
	invalid := func() ([]ipc.PreferenceKey, error) {
		return nil, rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	}
	if patch == nil || len(patch.ProtoReflect().GetUnknown()) != 0 {
		return invalid()
	}
	message := patch.ProtoReflect()
	keys := make([]ipc.PreferenceKey, 0, 8)
	fields := []struct {
		name protoreflect.Name
		key  ipc.PreferenceKey
	}{
		{"allow_inbound", ipc.PreferenceKey_PREFERENCE_KEY_ALLOW_INBOUND},
		{"accept_dns", ipc.PreferenceKey_PREFERENCE_KEY_ACCEPT_DNS},
		{"accept_routes", ipc.PreferenceKey_PREFERENCE_KEY_ACCEPT_ROUTES},
		{"runtime_start", ipc.PreferenceKey_PREFERENCE_KEY_RUNTIME_START},
		{"ui_quit", ipc.PreferenceKey_PREFERENCE_KEY_UI_QUIT},
		{"user_logoff", ipc.PreferenceKey_PREFERENCE_KEY_USER_LOGOFF},
		{"suspend", ipc.PreferenceKey_PREFERENCE_KEY_SUSPEND},
		{"resume", ipc.PreferenceKey_PREFERENCE_KEY_RESUME},
	}
	for _, entry := range fields {
		field := message.Descriptor().Fields().ByName(entry.name)
		if !message.Has(field) {
			continue
		}
		if field.Kind() == protoreflect.EnumKind {
			value := message.Get(field).Enum()
			if value == 0 || field.Enum().Values().ByNumber(value) == nil {
				return invalid()
			}
		}
		keys = append(keys, entry.key)
	}
	if len(keys) == 0 {
		return invalid()
	}
	return keys, nil
}

func rpcValidatePreferenceReset(keys []ipc.PreferenceKey) error {
	invalid := func() error { return rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT) }
	if len(keys) == 0 {
		return invalid()
	}
	seen := make(map[ipc.PreferenceKey]bool, len(keys))
	for _, key := range keys {
		if key == ipc.PreferenceKey_PREFERENCE_KEY_UNSPECIFIED || key.Descriptor().Values().ByNumber(protoreflect.EnumNumber(key)) == nil || seen[key] {
			return invalid()
		}
		seen[key] = true
	}
	return nil
}
