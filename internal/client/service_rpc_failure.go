package client

import (
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
)

// rpcFailureTemporary classifies typed failures only. Each worker decides
// whether its current durable phase permits retrying that failure.
func rpcFailureTemporary(err error) bool {
	switch rpc.FailureFromError(err).GetCode() {
	case ipc.ErrorCode_ERROR_CODE_UNAVAILABLE, ipc.ErrorCode_ERROR_CODE_DEADLINE_EXCEEDED, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED:
		return true
	default:
		return false
	}
}
