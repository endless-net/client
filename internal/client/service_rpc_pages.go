package client

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
)

type rpcPageCursor struct {
	Binding  string `json:"binding"`
	Instance string `json:"instance"`
	Revision uint64 `json:"revision"`
	Offset   int    `json:"offset"`
	Size     uint32 `json:"size"`
	Expires  int64  `json:"expires"`
}

// pageRange uses one caller-filtered snapshot, never a fresh read per page field.
// Scope contains the method and normalized query/profile. Other list providers
// reuse it with their own scope; tokens cannot cross query or caller boundaries.
func (m *ClientRPCMutations) pageRange(peer local.Peer, scope string, page *ipc.PageRequest, cfg Config, count int) (int, int, string, error) {
	size := page.GetPageSize()
	if size == 0 {
		size = 100
	}
	if size > 500 {
		return 0, 0, "", rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	}
	offset := 0
	state := cfg.RPCState
	if state == nil {
		if page.GetPageToken() != "" {
			return 0, 0, "", rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		}
		return 0, 0, "", nil
	}
	mac := func(value []byte) []byte {
		h := hmac.New(sha256.New, state.DigestKey)
		_, _ = h.Write(value)
		return h.Sum(nil)
	}
	if len(state.DigestKey) != 32 {
		return 0, 0, "", rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
	}
	binding := base64.RawURLEncoding.EncodeToString(mac([]byte(strings.ToLower(peer.Identity) + "\x00" + scope)))
	if token := page.GetPageToken(); token != "" {
		stale := func() (int, int, string, error) {
			return 0, 0, "", rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		}
		if len(token) > 2048 {
			return stale()
		}
		parts := strings.Split(token, ".")
		if len(parts) != 2 {
			return stale()
		}
		payload, err := base64.RawURLEncoding.DecodeString(parts[0])
		if err != nil {
			return stale()
		}
		signature, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil || !hmac.Equal(signature, mac(payload)) {
			return stale()
		}
		var cursor rpcPageCursor
		if json.Unmarshal(payload, &cursor) != nil || cursor.Binding != binding || cursor.Instance != m.instanceID || cursor.Revision != state.Revision || cursor.Size != size || cursor.Expires <= m.now().UnixNano() || cursor.Offset <= 0 || cursor.Offset >= count {
			return stale()
		}
		offset = cursor.Offset
	}
	end := min(offset+int(size), count)
	if end == count {
		return offset, end, "", nil
	}
	payload, err := json.Marshal(rpcPageCursor{Binding: binding, Instance: m.instanceID, Revision: state.Revision, Offset: end, Size: size, Expires: m.now().Add(5 * time.Minute).UnixNano()})
	if err != nil {
		return 0, 0, "", err
	}
	return offset, end, base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac(payload)), nil
}
