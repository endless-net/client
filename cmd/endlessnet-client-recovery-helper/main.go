package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	ipc "github.com/endless-net/client/ipc/v2"
)

const helperRequestTimeout = 2 * time.Minute

type recoveryHelperRequester interface {
	Request(context.Context, string, string, any, any) error
}

type recoveryHelperOptions struct {
	Operation              ipc.RecoveryOperation
	ConfirmedControlOrigin string
	ConfirmedKeyID         string
	ConfirmedLocalForget   bool
}

func main() {
	client, err := ipc.NewLocalClient("")
	if err != nil {
		os.Exit(1)
	}
	if runRecoveryHelper(os.Args[1:], os.Stdout, client) != nil {
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

	result := ipc.RecoveryHelperResult{Metadata: ipc.NewMetadata(ipc.Version), Operation: opts.Operation}
	switch opts.Operation {
	case ipc.RecoveryOperationTrustServerIdentity:
		var response ipc.TrustServerResponse
		err = requester.Request(ctx, http.MethodPost, ipc.PathTrustServer, ipc.TrustServerRequest{
			ConfirmedControlOrigin: opts.ConfirmedControlOrigin,
			ConfirmedKeyID:         opts.ConfirmedKeyID,
		}, &response)
		if err == nil {
			result.Metadata = response.Metadata
			result.Outcome = response.Outcome
			result.State = response.State
		}
	case ipc.RecoveryOperationForgetEnrollment:
		var response ipc.LocalForgetResponse
		err = requester.Request(ctx, http.MethodPost, ipc.PathLocalForget, ipc.LocalForgetRequest{Confirmed: true}, &response)
		if err == nil {
			result.Metadata = response.Metadata
			result.Outcome = ipc.RecoveryOutcomeCompleted
			result.State = response.State
		}
	default:
		return errors.New("unsupported recovery helper operation")
	}
	if err != nil {
		var ipcErr ipc.Error
		if errors.As(err, &ipcErr) {
			result.ErrorCode = ipcErr.Code
		} else {
			result.ErrorCode = ipc.ErrorRequestFailed
		}
	}
	if output != nil {
		if encodeErr := json.NewEncoder(output).Encode(result); encodeErr != nil {
			return encodeErr
		}
	}
	return err
}

func parseRecoveryHelperOptions(args []string) (recoveryHelperOptions, error) {
	fs := flag.NewFlagSet("endlessnet-client-recovery-helper", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	operation := fs.String("operation", "", "")
	confirmedControlOrigin := fs.String("confirmed-control-origin", "", "")
	confirmedKeyID := fs.String("confirmed-key-id", "", "")
	confirmedLocalForget := fs.Bool("confirmed-local-forget", false, "")
	if err := fs.Parse(args); err != nil {
		return recoveryHelperOptions{}, errors.New("invalid recovery helper arguments")
	}
	if fs.NArg() != 0 {
		return recoveryHelperOptions{}, errors.New("recovery helper does not accept positional arguments")
	}
	opts := recoveryHelperOptions{
		Operation:              recoveryHelperOperation(*operation),
		ConfirmedControlOrigin: strings.TrimSpace(*confirmedControlOrigin),
		ConfirmedKeyID:         strings.TrimSpace(*confirmedKeyID),
		ConfirmedLocalForget:   *confirmedLocalForget,
	}
	switch opts.Operation {
	case ipc.RecoveryOperationTrustServerIdentity:
		if opts.ConfirmedControlOrigin == "" || opts.ConfirmedKeyID == "" || opts.ConfirmedLocalForget {
			return recoveryHelperOptions{}, errors.New("trust-server-identity requires only the confirmed control origin and key ID")
		}
	case ipc.RecoveryOperationForgetEnrollment:
		if !opts.ConfirmedLocalForget || opts.ConfirmedControlOrigin != "" || opts.ConfirmedKeyID != "" {
			return recoveryHelperOptions{}, errors.New("forget-local-enrollment requires only explicit local-forget confirmation")
		}
	default:
		return recoveryHelperOptions{}, errors.New("recovery helper operation is required")
	}
	return opts, nil
}

func recoveryHelperOperation(value string) ipc.RecoveryOperation {
	switch strings.TrimSpace(value) {
	case "trust-server-identity":
		return ipc.RecoveryOperationTrustServerIdentity
	case "forget-local-enrollment":
		return ipc.RecoveryOperationForgetEnrollment
	default:
		return ""
	}
}
