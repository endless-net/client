package client

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestRPCFailureTemporary(t *testing.T) {
	for value, name := range ipc.ErrorCode_name {
		code := ipc.ErrorCode(value)
		t.Run(name, func(t *testing.T) {
			want := code == ipc.ErrorCode_ERROR_CODE_UNAVAILABLE || code == ipc.ErrorCode_ERROR_CODE_DEADLINE_EXCEEDED || code == ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED
			err := rpc.Error(connect.CodeUnavailable, code)
			for _, candidate := range []error{err, fmt.Errorf("worker: %w", err)} {
				if rpcFailureTemporary(candidate) != want {
					t.Fatalf("wrong failure classification for %v", candidate)
				}
			}
		})
	}
	for _, err := range []error{nil, errors.New("unavailable"), context.Canceled, context.DeadlineExceeded, connect.NewError(connect.CodeUnavailable, errors.New("no typed detail"))} {
		if rpcFailureTemporary(err) {
			t.Fatalf("untyped error classified as temporary: %v", err)
		}
	}
}
