package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"strings"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func nativeRequestUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	raw := strings.ReplaceAll(value, "-", "")
	_, err := hex.DecodeString(raw)
	return err == nil && len(raw) == 32 && strings.Trim(raw, "0") != ""
}

// Explicit request identity and CAS values let scripts retain the exact command
// before sending. Never refresh/replay a mutation automatically on an error.
func cmdServiceRPCMutation(command string, args []string, output io.Writer) error {
	fs := flag.NewFlagSet("service "+command, flag.ContinueOnError)
	pipe, socket := serviceIPCTransportFlags(fs)
	timeoutValue := fs.String("timeout", "30s", "maximum time for native mutation acceptance")
	requestID := fs.String("request-id", "", "required durable mutation UUID; retain for operation lookup")
	instance := fs.String("expected-instance-id", "", "required runtime instance from a fresh snapshot")
	revision := fs.Uint64("expected-revision", 0, "required state revision from a fresh snapshot")
	profile := fs.String("profile-id", "", "required target profile UUID")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || !nativeRequestUUID(*requestID) || strings.TrimSpace(*instance) == "" || *revision == 0 || strings.TrimSpace(*profile) == "" {
		return fmt.Errorf("service %s requires --request-id UUID, --expected-instance-id, --expected-revision and --profile-id, with no positional arguments", command)
	}
	if command != "connect" && command != "disconnect" && command != "logout" {
		return fmt.Errorf("unknown native mutation %q", command)
	}
	timeout, err := parsePositiveServiceIPCTimeout(*timeoutValue)
	if err != nil {
		return err
	}
	endpoint, err := nativeServiceEndpoint(*pipe, *socket)
	if err != nil {
		return err
	}
	consumer, err := local.NewClient(endpoint)
	if err != nil {
		return err
	}
	defer consumer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if _, err := consumer.Bootstrap(ctx); err != nil {
		return err
	}
	mutation := &ipc.MutationContext{RequestId: *requestID, ExpectedInstanceId: *instance, ExpectedRevision: *revision}
	ref := &ipc.ProfileRef{ProfileId: *profile}
	var message proto.Message
	switch command {
	case "connect":
		response, callErr := consumer.Connect(ctx, connect.NewRequest(&ipc.ConnectRequest{Mutation: mutation, Profile: ref}))
		err = callErr
		if err == nil {
			message = response.Msg
		}
	case "disconnect":
		response, callErr := consumer.Disconnect(ctx, connect.NewRequest(&ipc.DisconnectRequest{Mutation: mutation, Profile: ref}))
		err = callErr
		if err == nil {
			message = response.Msg
		}
	case "logout":
		response, callErr := consumer.Logout(ctx, connect.NewRequest(&ipc.LogoutRequest{Mutation: mutation, Profile: ref}))
		err = callErr
		if err == nil {
			message = response.Msg
		}
	}
	if err == nil {
		var encoded []byte
		encoded, err = protojson.Marshal(message)
		if err == nil {
			_, err = fmt.Fprintln(output, string(encoded))
		}
	}
	if err != nil {
		return fmt.Errorf("service %s: %w; inspect service operation --request-id %s using the same endpoint before deciding whether to retry", command, err, *requestID)
	}
	return nil
}
