package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	ipc "github.com/endless-net/client/ipc/v2"
)

type recordingRecoveryHelperRequester struct {
	method string
	path   string
	in     any
	err    error
}

func (r *recordingRecoveryHelperRequester) Request(_ context.Context, method, path string, in, out any) error {
	r.method, r.path, r.in = method, path, in
	if r.err != nil {
		return r.err
	}
	switch response := out.(type) {
	case *ipc.TrustServerResponse:
		*response = ipc.TrustServerResponse{
			Metadata: ipc.NewMetadata(ipc.Version), Operation: ipc.RecoveryOperationTrustServerIdentity,
			Outcome: ipc.RecoveryOutcomeAccepted, State: ipc.StateRecovering,
		}
	case *ipc.LocalForgetResponse:
		*response = ipc.LocalForgetResponse{
			Metadata: ipc.NewMetadata(ipc.Version), State: ipc.StateNeedsEnrollment,
			Outcome: ipc.LogoutOutcomeRemoteCleanupUnconfirmed,
		}
	}
	return nil
}

func TestRecoveryHelperTrustUsesOnlyConfirmedPublicValues(t *testing.T) {
	requester := &recordingRecoveryHelperRequester{}
	var output bytes.Buffer
	err := runRecoveryHelper([]string{
		"--operation", "trust-server-identity",
		"--confirmed-control-origin", "https://control.example.test",
		"--confirmed-key-id", "key-2",
	}, &output, requester)
	if err != nil {
		t.Fatal(err)
	}
	request, ok := requester.in.(ipc.TrustServerRequest)
	if requester.method != http.MethodPost || requester.path != ipc.PathTrustServer || !ok ||
		request.ConfirmedControlOrigin != "https://control.example.test" || request.ConfirmedKeyID != "key-2" {
		t.Fatalf("trust request = %s %s %#v", requester.method, requester.path, requester.in)
	}
	var result ipc.RecoveryHelperResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Operation != ipc.RecoveryOperationTrustServerIdentity || result.Outcome != ipc.RecoveryOutcomeAccepted || result.State != ipc.StateRecovering {
		t.Fatalf("helper result = %#v", result)
	}
}

func TestRecoveryHelperLocalForgetIsClosedOperation(t *testing.T) {
	requester := &recordingRecoveryHelperRequester{}
	if err := runRecoveryHelper([]string{"--operation", "forget-local-enrollment", "--confirmed-local-forget"}, nil, requester); err != nil {
		t.Fatal(err)
	}
	request, ok := requester.in.(ipc.LocalForgetRequest)
	if requester.path != ipc.PathLocalForget || !ok || !request.Confirmed {
		t.Fatalf("local-forget request = %s %#v", requester.path, requester.in)
	}
}

func TestRecoveryHelperRejectsSecretsPathsAndArbitraryCommands(t *testing.T) {
	for _, args := range [][]string{
		{"--operation", "trust-server-identity", "--confirmed-key-id", "key", "--confirmed-control-origin", "https://control.example.test", "powershell.exe"},
		{"--operation", "forget-local-enrollment", "--confirmed-local-forget", "--credential", "secret"},
		{"--operation", "forget-local-enrollment", "--confirmed-local-forget", "--state", "C:\\ProgramData\\EndlessNet\\client.json"},
		{"--operation", "forget-local-enrollment", "--confirmed-local-forget", "--ipc-pipe", `\\.\pipe\other`},
		{"--operation", "forget-local-enrollment", "--confirmed-local-forget", "--confirmed-key-id", "key"},
	} {
		if _, err := parseRecoveryHelperOptions(args); err == nil {
			t.Fatalf("unsafe helper arguments were accepted: %q", args)
		} else if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "ProgramData") {
			t.Fatalf("helper parse error reflected sensitive argument: %v", err)
		}
	}
}

func TestRecoveryHelperReturnsTypedIPCErrorCode(t *testing.T) {
	requester := &recordingRecoveryHelperRequester{err: ipc.NewError(http.StatusForbidden, ipc.ErrorAdministratorRequired, errors.New("diagnostic"))}
	var output bytes.Buffer
	err := runRecoveryHelper([]string{"--operation", "forget-local-enrollment", "--confirmed-local-forget"}, &output, requester)
	if err == nil {
		t.Fatal("helper hid IPC failure")
	}
	var result ipc.RecoveryHelperResult
	if decodeErr := json.Unmarshal(output.Bytes(), &result); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if result.ErrorCode != ipc.ErrorAdministratorRequired {
		t.Fatalf("helper error result = %#v", result)
	}
}
