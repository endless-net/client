package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/encoding/protojson"
)

type recordingRecoveryHelperRequester struct {
	emptyResponse         bool
	bootstrapErr, callErr error
	bootstraps, calls     int
	trust                 *ipc.TrustServerIdentityRequest
	forget                *ipc.ForgetLocalEnrollmentRequest
}

func (r *recordingRecoveryHelperRequester) Bootstrap(context.Context) (*ipc.RuntimeInfo, error) {
	r.bootstraps++
	return &ipc.RuntimeInfo{}, r.bootstrapErr
}
func (r *recordingRecoveryHelperRequester) TrustServerIdentity(_ context.Context, req *connect.Request[ipc.TrustServerIdentityRequest]) (*connect.Response[ipc.TrustServerIdentityResponse], error) {
	r.calls++
	r.trust = req.Msg
	return connect.NewResponse(&ipc.TrustServerIdentityResponse{Operation: &ipc.Operation{Id: "operation", RequestId: req.Msg.Mutation.RequestId, ProfileId: req.Msg.Profile.ProfileId, Kind: ipc.OperationKind_OPERATION_KIND_TRUST_SERVER_IDENTITY, State: ipc.OperationState_OPERATION_STATE_PENDING}}), r.callErr
}
func (r *recordingRecoveryHelperRequester) ForgetLocalEnrollment(_ context.Context, req *connect.Request[ipc.ForgetLocalEnrollmentRequest]) (*connect.Response[ipc.ForgetLocalEnrollmentResponse], error) {
	r.calls++
	r.forget = req.Msg
	if r.emptyResponse {
		return connect.NewResponse(&ipc.ForgetLocalEnrollmentResponse{}), nil
	}
	return connect.NewResponse(&ipc.ForgetLocalEnrollmentResponse{Operation: &ipc.Operation{Id: "operation", RequestId: req.Msg.Mutation.RequestId, ProfileId: req.Msg.Profile.ProfileId, Kind: ipc.OperationKind_OPERATION_KIND_FORGET_LOCAL_ENROLLMENT, State: ipc.OperationState_OPERATION_STATE_PENDING}}), r.callErr
}

type failedHelperOutput struct{}

func (failedHelperOutput) Write([]byte) (int, error) { return 0, errors.New("output unavailable") }

func TestRecoveryHelperMissingResultAndOutputFailureDoNotReplay(t *testing.T) {
	r := &recordingRecoveryHelperRequester{emptyResponse: true}
	var output bytes.Buffer
	if err := runRecoveryHelper(helperArguments("forget-local-enrollment"), &output, r); err == nil || r.calls != 1 {
		t.Fatal("missing operation accepted or replayed")
	}
	var failure ipc.Failure
	if err := protojson.Unmarshal(output.Bytes(), &failure); err != nil || failure.Code != ipc.ErrorCode_ERROR_CODE_INTERNAL {
		t.Fatal("missing operation did not yield typed failure", err)
	}
	r = new(recordingRecoveryHelperRequester)
	if err := runRecoveryHelper(helperArguments("forget-local-enrollment"), failedHelperOutput{}, r); err == nil || r.calls != 1 {
		t.Fatal("output failure hidden or mutation replayed")
	}
	for _, hash := range []string{"", "not-hex", strings.Repeat("a", 62)} {
		r = new(recordingRecoveryHelperRequester)
		if err := runRecoveryHelper(append(helperArguments("trust-server-identity"), "--confirmed-announcement-id", hash), nil, r); err == nil || r.bootstraps != 0 {
			t.Fatal("incomplete announcement reached IPC")
		}
	}
}
func helperArguments(operation string) []string {
	args := []string{"--operation", operation, "--request-id", "53c3914f-8564-4c0b-af56-0c16cc2c71d7", "--profile-id", "profile", "--expected-instance-id", "instance", "--expected-revision", "7"}
	if operation == "trust-server-identity" {
		return append(args, "--confirmed-control-origin", "https://control.test", "--confirmed-key-id", "key", "--confirmed-announcement-id", strings.Repeat("a", 64))
	}
	return append(args, "--confirmed-local-forget")
}
func TestRecoveryHelperNativeClosedOperations(t *testing.T) {
	for _, operation := range []string{"trust-server-identity", "forget-local-enrollment"} {
		t.Run(operation, func(t *testing.T) {
			r := new(recordingRecoveryHelperRequester)
			var output bytes.Buffer
			if err := runRecoveryHelper(helperArguments(operation), &output, r); err != nil {
				t.Fatal(err)
			}
			var op ipc.Operation
			if err := protojson.Unmarshal(output.Bytes(), &op); err != nil {
				t.Fatal(err)
			}
			if r.bootstraps != 1 || r.calls != 1 || op.State != ipc.OperationState_OPERATION_STATE_PENDING || op.RequestId == "" {
				t.Fatal("acceptance was not reported as native operation")
			}
			if operation == "trust-server-identity" {
				if r.trust.ConfirmedAnnouncementId != strings.Repeat("a", 64) || r.trust.ConfirmedControlOrigin != "https://control.test" || r.trust.ConfirmedKeyId != "key" || r.trust.Mutation.ExpectedRevision != 7 {
					t.Fatal("confirmation/CAS changed")
				}
			} else if !r.forget.Confirmed || r.forget.Profile.ProfileId != "profile" {
				t.Fatal("forget confirmation lost")
			}
		})
	}
}
func TestRecoveryHelperRejectsUnsafeAndRetiredArguments(t *testing.T) {
	for _, extra := range [][]string{{"shell.exe"}, {"--credential", "secret"}, {"--state", "private-path"}, {"--ipc-pipe", "other"}, {"--confirmed-key-id", "key"}, {"--request-id", "invalid"}, {"--expected-revision", "0"}} {
		r := new(recordingRecoveryHelperRequester)
		err := runRecoveryHelper(append(helperArguments("forget-local-enrollment"), extra...), nil, r)
		if err == nil || r.bootstraps != 0 || r.calls != 0 || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "private-path") {
			t.Fatal("unsafe arguments dispatched/reflected", err)
		}
	}
	if _, err := parseRecoveryHelperOptions([]string{"--operation", "forget-local-enrollment", "--confirmed-local-forget"}); err == nil {
		t.Fatal("retired context-free call accepted")
	}
}
func TestRecoveryHelperErrorsAreTypedAndNeverReplayed(t *testing.T) {
	for _, bootstrap := range []bool{false, true} {
		r := &recordingRecoveryHelperRequester{callErr: rpc.Error(connect.CodePermissionDenied, ipc.ErrorCode_ERROR_CODE_ADMINISTRATOR_REQUIRED)}
		if bootstrap {
			r.bootstrapErr = errors.New("private transport detail")
		}
		var output bytes.Buffer
		if err := runRecoveryHelper(helperArguments("forget-local-enrollment"), &output, r); err == nil {
			t.Fatal("failure hidden")
		}
		var failure ipc.Failure
		if err := protojson.Unmarshal(output.Bytes(), &failure); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(output.String(), "private transport") || r.calls > 1 || bootstrap && r.calls != 0 {
			t.Fatal("failure leaked or dispatch replayed")
		}
		if bootstrap && failure.Code != ipc.ErrorCode_ERROR_CODE_UNAVAILABLE {
			t.Fatal("untyped bootstrap failure")
		}
		if !bootstrap && (failure.Code != ipc.ErrorCode_ERROR_CODE_ADMINISTRATOR_REQUIRED || r.calls != 1) {
			t.Fatal("administrator failure was not preserved")
		}
	}
}
