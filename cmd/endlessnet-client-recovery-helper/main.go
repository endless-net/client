package main

import (
	"context"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const helperRequestTimeout = 2 * time.Minute

type recoveryHelperRequester interface {
	Bootstrap(context.Context) (*ipc.RuntimeInfo, error)
	TrustServerIdentity(context.Context, *connect.Request[ipc.TrustServerIdentityRequest]) (*connect.Response[ipc.TrustServerIdentityResponse], error)
	ForgetLocalEnrollment(context.Context, *connect.Request[ipc.ForgetLocalEnrollmentRequest]) (*connect.Response[ipc.ForgetLocalEnrollmentResponse], error)
}

type recoveryHelperOptions struct {
	Operation               string
	Mutation                *ipc.MutationContext
	Profile                 *ipc.ProfileRef
	ConfirmedControlOrigin  string
	ConfirmedKeyID          string
	ConfirmedAnnouncementID string
	ConfirmedLocalForget    bool
}

func main() {
	consumer, err := local.NewClient("") // Fixed platform endpoint only.
	if err != nil {
		os.Exit(1)
	}
	defer consumer.Close()
	if runRecoveryHelper(os.Args[1:], os.Stdout, consumer) != nil {
		os.Exit(1)
	}
}

func runRecoveryHelper(args []string, output io.Writer, requester recoveryHelperRequester) error {
	if requester == nil {
		return errors.New("service IPC client is required")
	}
	opts, err := parseRecoveryHelperOptions(args)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), helperRequestTimeout)
	defer cancel()
	_, err = requester.Bootstrap(ctx)
	var operation *ipc.Operation
	if err == nil {
		switch opts.Operation {
		case "trust-server-identity":
			response, callErr := requester.TrustServerIdentity(ctx, connect.NewRequest(&ipc.TrustServerIdentityRequest{Mutation: opts.Mutation, Profile: opts.Profile, ConfirmedControlOrigin: opts.ConfirmedControlOrigin, ConfirmedKeyId: opts.ConfirmedKeyID, ConfirmedAnnouncementId: opts.ConfirmedAnnouncementID}))
			err = callErr
			if err == nil && response != nil {
				operation = response.Msg.GetOperation()
			}
		case "forget-local-enrollment":
			response, callErr := requester.ForgetLocalEnrollment(ctx, connect.NewRequest(&ipc.ForgetLocalEnrollmentRequest{Mutation: opts.Mutation, Profile: opts.Profile, Confirmed: true}))
			err = callErr
			if err == nil && response != nil {
				operation = response.Msg.GetOperation()
			}
		}
	}
	expectedKind := ipc.OperationKind_OPERATION_KIND_TRUST_SERVER_IDENTITY
	if opts.Operation == "forget-local-enrollment" {
		expectedKind = ipc.OperationKind_OPERATION_KIND_FORGET_LOCAL_ENROLLMENT
	}
	if err == nil && (operation == nil || operation.Id == "" || operation.RequestId != opts.Mutation.RequestId || operation.ProfileId != opts.Profile.ProfileId || operation.Kind != expectedKind) {
		err = rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
	}
	var result proto.Message = operation
	if err != nil {
		failure := rpc.FailureFromError(err)
		if failure == nil {
			failure = &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_UNAVAILABLE, ReasonKey: "recovery_request_unconfirmed"}
		}
		result = failure
	}
	if output != nil {
		encoded, encodeErr := protojson.Marshal(result)
		if encodeErr == nil {
			_, encodeErr = fmt.Fprintln(output, string(encoded))
		}
		if encodeErr != nil {
			return encodeErr
		}
	}
	return err
}

func parseRecoveryHelperOptions(args []string) (recoveryHelperOptions, error) {
	fs := flag.NewFlagSet("endlessnet-client-recovery-helper", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	opts := recoveryHelperOptions{Mutation: &ipc.MutationContext{}, Profile: &ipc.ProfileRef{}}
	fs.StringVar(&opts.Operation, "operation", "", "")
	fs.StringVar(&opts.Mutation.RequestId, "request-id", "", "")
	fs.StringVar(&opts.Mutation.ExpectedInstanceId, "expected-instance-id", "", "")
	fs.Uint64Var(&opts.Mutation.ExpectedRevision, "expected-revision", 0, "")
	fs.StringVar(&opts.Profile.ProfileId, "profile-id", "", "")
	fs.StringVar(&opts.ConfirmedControlOrigin, "confirmed-control-origin", "", "")
	fs.StringVar(&opts.ConfirmedKeyID, "confirmed-key-id", "", "")
	fs.StringVar(&opts.ConfirmedAnnouncementID, "confirmed-announcement-id", "", "")
	fs.BoolVar(&opts.ConfirmedLocalForget, "confirmed-local-forget", false, "")
	if err := fs.Parse(args); err != nil {
		return recoveryHelperOptions{}, errors.New("invalid recovery helper arguments")
	}
	if fs.NArg() != 0 {
		return recoveryHelperOptions{}, errors.New("recovery helper does not accept positional arguments")
	}
	id := opts.Mutation.RequestId
	if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		return recoveryHelperOptions{}, errors.New("request UUID is required")
	}
	raw := strings.ReplaceAll(id, "-", "")
	if decoded, err := hex.DecodeString(raw); err != nil || len(decoded) != 16 || strings.Trim(raw, "0") == "" {
		return recoveryHelperOptions{}, errors.New("request UUID is required")
	}
	if strings.TrimSpace(opts.Profile.ProfileId) == "" || strings.TrimSpace(opts.Mutation.ExpectedInstanceId) == "" || opts.Mutation.ExpectedRevision == 0 {
		return recoveryHelperOptions{}, errors.New("profile and snapshot context are required")
	}
	switch opts.Operation {
	case "trust-server-identity":
		announcement, err := hex.DecodeString(opts.ConfirmedAnnouncementID)
		if strings.TrimSpace(opts.ConfirmedControlOrigin) == "" || strings.TrimSpace(opts.ConfirmedKeyID) == "" || err != nil || len(announcement) != 32 || opts.ConfirmedLocalForget {
			return recoveryHelperOptions{}, errors.New("complete trust confirmation is required")
		}
	case "forget-local-enrollment":
		if !opts.ConfirmedLocalForget || opts.ConfirmedControlOrigin != "" || opts.ConfirmedKeyID != "" || opts.ConfirmedAnnouncementID != "" {
			return recoveryHelperOptions{}, errors.New("local forget requires only explicit cleanup confirmation")
		}
	default:
		return recoveryHelperOptions{}, errors.New("recovery helper operation is required")
	}
	return opts, nil
}
