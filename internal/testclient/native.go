package testclient

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// NativeService preserves the published protobuf JSON schema, including enums,
// oneofs and presence. Never translate it into the retired HTTP DTOs or print
// arbitrary subprocess output on failure.
func (n *Node) NativeService(operation string, target proto.Message, options ...string) error {
	out, err := n.ServiceCommand(operation, options...)
	if err != nil {
		return NativeServiceCommandError(operation, out)
	}
	return decodeNativeService(out, target)
}

var nativeMutationFailureSuffix = regexp.MustCompile(`^; inspect service operation --request-id [0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12} using the same endpoint before deciding whether to retry$`)

type nativeServiceFailure struct {
	operation string
	transport connect.Code
	failure   ipc.ErrorCode
}

func (e *nativeServiceFailure) Error() string {
	return fmt.Sprintf("native service %s failed (transport=%d failure=%d; output withheld)", e.operation, e.transport, e.failure)
}

// IsNativeStaleState recognizes only a canonical, classified CAS rejection,
// never an arbitrary subprocess message or an uncertain transport failure.
func IsNativeStaleState(err error) bool {
	var failure *nativeServiceFailure
	return errors.As(err, &failure) && failure.transport == connect.CodeFailedPrecondition && failure.failure == ipc.ErrorCode_ERROR_CODE_STALE_STATE
}

// NativeServiceCommandError recognizes only the complete canonical typed failure line emitted by the CLI,
// optionally inside its exact mutation-outcome lookup hint. Never retain the request ID.
// Never retain subprocess output or infer a code from a substring in diagnostics.
func NativeServiceCommandError(operation string, output []byte) error {
	if len(output) <= 512 {
		line := strings.TrimSpace(string(output))
		if body, wrapped := strings.CutPrefix(line, "service "+operation+": "); wrapped {
			failure, suffix, found := strings.Cut(body, "; inspect service operation --request-id ")
			if found && nativeMutationFailureSuffix.MatchString("; inspect service operation --request-id "+suffix) {
				line = failure
			}
		}
		for code := connect.CodeCanceled; code <= connect.CodeUnauthenticated; code++ {
			for number := range ipc.ErrorCode_name {
				failure := ipc.ErrorCode(number)
				if number != 0 && line == rpc.Error(code, failure).Error() {
					return &nativeServiceFailure{operation: operation, transport: code, failure: failure}
				}
			}
		}
	}
	return fmt.Errorf("native service %s failed (unclassified subprocess failure; output withheld)", operation)
}

func decodeNativeService(out []byte, target proto.Message) error {
	if target == nil || !target.ProtoReflect().IsValid() {
		return errors.New("native response target required")
	}
	if err := protojson.Unmarshal(out, target); err != nil {
		return errors.New("invalid native protobuf JSON (output withheld)")
	}
	return nil
}

func (n *Node) AwaitNativeStatus(match func(*ipc.Status) bool) *ipc.Status {
	n.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var status *ipc.Status
	var lastErr error
	if err := Await(ctx, func() bool {
		response := &ipc.GetStatusResponse{}
		lastErr = n.NativeService("status", response, "--timeout", "1s")
		if lastErr != nil {
			return false
		}
		status = response.Status
		return status != nil && match(status)
	}); err != nil {
		n.logWireGuardStartupStages()
		// Keep only public enum values, counters and shape predicates. The last
		// successful observation can precede a failed read; report both separately.
		n.t.Logf("native status last observation: present=%t service=%d control=%d connection=%d desired=%d disconnected=%t revision=%d map=%d agent_present=%t agent_state=%d agent_map=%d agent_failure=%d last_read_error=%v",
			status != nil, status.GetServiceState(), status.GetControlState(), status.GetConnectionPhase(),
			status.GetIntent().GetDesiredState(), status.GetUserDisconnected(), status.GetMetadata().GetRevision(), status.GetMapRevision(),
			status.GetAgent() != nil, status.GetAgent().GetSnapshotState(), status.GetAgent().GetMapRevision(), status.GetAgent().GetLastFailure().GetCode(), lastErr)
		n.t.Fatal("native status condition not reached")
	}
	return status
}

// Mutation arguments are captured once. Callers retain and explicitly replay
// these exact arguments for idempotency tests; no CAS refresh or automatic retry.
func NativeMutationArguments(id string, status *ipc.Status) []string {
	return []string{"--request-id", id, "--profile-id", status.ActiveProfileId, "--expected-instance-id", status.GetMetadata().GetInstanceId(), "--expected-revision", fmt.Sprint(status.GetMetadata().GetRevision())}
}

func (n *Node) AwaitNativeOperation(id string) *ipc.Operation {
	n.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var op *ipc.Operation
	var lastErr error
	if err := Await(ctx, func() bool {
		response := &ipc.GetOperationResponse{}
		lastErr = n.NativeService("operation", response, "--operation-id", id, "--timeout", "1s")
		if lastErr != nil {
			return false
		}
		op = response.Operation
		return op != nil && (op.State == ipc.OperationState_OPERATION_STATE_SUCCEEDED || op.State == ipc.OperationState_OPERATION_STATE_FAILED || op.State == ipc.OperationState_OPERATION_STATE_CANCELLED)
	}); err != nil {
		// Last successful observation may be stale if the final read failed.
		// Never include operation/profile IDs or free-form failure details.
		n.t.Logf("native operation last observation: present=%t kind=%d state=%d failure=%d last_read_error=%v",
			op != nil, op.GetKind(), op.GetState(), op.GetFailure().GetCode(), lastErr)
		n.t.Fatal("native operation did not finish")
	}
	return op
}

func (n *Node) awaitNativeReady() {
	n.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var lastErr error
	var instancePresent bool
	if err := Await(ctx, func() bool {
		response := &ipc.GetRuntimeInfoResponse{}
		lastErr = n.NativeService("runtime-info", response, "--timeout", "1s")
		instancePresent = response.GetRuntime().GetInstanceId() != ""
		return lastErr == nil && instancePresent
	}); err != nil {
		// NativeService returns only fixed diagnostics or canonical numeric codes;
		// never expose subprocess output or the runtime instance identifier here.
		n.t.Logf("native runtime readiness: instance_present=%t error=%v", instancePresent, lastErr)
		completed, failed := observeAgentCompletion(n.done)
		n.t.Logf("agent readiness observation: completion_observed=%t unsuccessful_exit=%t", completed, failed)
		n.t.Fatal("native runtime did not become ready")
	}
}
