package main

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

type serviceOperationReader interface {
	GetOperation(context.Context, *connect.Request[ipc.GetOperationRequest]) (*connect.Response[ipc.GetOperationResponse], error)
}

// Wait by operation ID only: neither approval polling nor a lost response is
// authorization to submit Enroll/Connect again. report exposes pending actions.
func waitServiceOperation(ctx context.Context, reader serviceOperationReader, initial *ipc.Operation, interval time.Duration, report func(*ipc.Operation) error) (*ipc.Operation, error) {
	if initial.GetId() == "" || initial.GetRequestId() == "" || interval <= 0 || report == nil {
		return nil, fmt.Errorf("operation identity, poll interval and reporter are required")
	}
	// Bind to an immutable identity. A reporter must not alter the caller's
	// accepted operation or the identity used for subsequent lookups.
	initial = proto.Clone(initial).(*ipc.Operation)
	current := proto.Clone(initial).(*ipc.Operation)
	var previous *ipc.Operation
	for {
		if err := ctx.Err(); err != nil {
			return current, err
		}
		if current == nil || current.Id != initial.Id || current.RequestId != initial.RequestId || current.Kind != initial.Kind || current.ProfileId != initial.ProfileId {
			return nil, fmt.Errorf("operation identity changed while waiting")
		}
		if previous == nil || !proto.Equal(previous, current) {
			if err := report(proto.Clone(current).(*ipc.Operation)); err != nil {
				return current, err
			}
			previous = proto.Clone(current).(*ipc.Operation)
		}
		switch current.State {
		case ipc.OperationState_OPERATION_STATE_SUCCEEDED:
			return current, nil
		case ipc.OperationState_OPERATION_STATE_FAILED:
			return current, fmt.Errorf("operation %s failed: %s", current.Id, current.GetFailure().GetCode())
		case ipc.OperationState_OPERATION_STATE_CANCELLED:
			return current, fmt.Errorf("operation %s was cancelled", current.Id)
		case ipc.OperationState_OPERATION_STATE_PENDING, ipc.OperationState_OPERATION_STATE_RUNNING, ipc.OperationState_OPERATION_STATE_WAITING_FOR_USER:
		default:
			return current, fmt.Errorf("operation %s has an invalid state", current.Id)
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return current, ctx.Err()
		case <-timer.C:
		}
		if err := ctx.Err(); err != nil {
			return current, err
		}
		response, err := reader.GetOperation(ctx, connect.NewRequest(&ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: initial.Id}}))
		if err != nil {
			return current, err
		}
		if response == nil || response.Msg == nil || response.Msg.Operation == nil {
			return current, fmt.Errorf("operation lookup returned no operation")
		}
		current = proto.Clone(response.Msg.Operation).(*ipc.Operation)
	}
}
