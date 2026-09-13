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
	var profile, displayName, controlOrigin string
	var confirmed bool
	if command == "local-forget" {
		fs.BoolVar(&confirmed, "confirm-local-forget", false, "confirm local cleanup without confirmed remote revocation")
	}
	if command != "create-profile" {
		fs.StringVar(&profile, "profile-id", "", "required target profile UUID")
	}
	if command == "create-profile" || command == "rename-profile" {
		fs.StringVar(&displayName, "display-name", "", "required profile display name")
	}
	if command == "create-profile" {
		fs.StringVar(&controlOrigin, "control-origin", "", "required HTTPS control origin")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if command == "local-forget" && !confirmed {
		return fmt.Errorf("local-forget requires --confirm-local-forget")
	}
	if fs.NArg() != 0 || !nativeRequestUUID(*requestID) || strings.TrimSpace(*instance) == "" || *revision == 0 || (command != "create-profile" && strings.TrimSpace(profile) == "") {
		return fmt.Errorf("service %s requires --request-id UUID, --expected-instance-id and --expected-revision; non-create commands also require --profile-id; no positional arguments are allowed", command)
	}
	if (command == "create-profile" || command == "rename-profile") && strings.TrimSpace(displayName) == "" {
		return fmt.Errorf("--display-name is required")
	}
	if command == "create-profile" && strings.TrimSpace(controlOrigin) == "" {
		return fmt.Errorf("--control-origin is required")
	}
	if command != "connect" && command != "disconnect" && command != "logout" && command != "create-profile" && command != "select-profile" && command != "rename-profile" && command != "remove-profile" && command != "local-forget" {
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
	ref := &ipc.ProfileRef{ProfileId: profile}
	var message proto.Message
	switch command {
	case "local-forget":
		response, callErr := consumer.ForgetLocalEnrollment(ctx, connect.NewRequest(&ipc.ForgetLocalEnrollmentRequest{Mutation: mutation, Profile: ref, Confirmed: confirmed}))
		err = callErr
		if err == nil {
			message = response.Msg
		}
	case "create-profile":
		response, callErr := consumer.CreateProfile(ctx, connect.NewRequest(&ipc.CreateProfileRequest{Mutation: mutation, DisplayName: displayName, ControlOrigin: controlOrigin}))
		err = callErr
		if err == nil {
			message = response.Msg
		}
	case "select-profile":
		response, callErr := consumer.SelectProfile(ctx, connect.NewRequest(&ipc.SelectProfileRequest{Mutation: mutation, Profile: ref}))
		err = callErr
		if err == nil {
			message = response.Msg
		}
	case "rename-profile":
		response, callErr := consumer.RenameProfile(ctx, connect.NewRequest(&ipc.RenameProfileRequest{Mutation: mutation, Profile: ref, DisplayName: displayName}))
		err = callErr
		if err == nil {
			message = response.Msg
		}
	case "remove-profile":
		response, callErr := consumer.RemoveProfile(ctx, connect.NewRequest(&ipc.RemoveProfileRequest{Mutation: mutation, Profile: ref}))
		err = callErr
		if err == nil {
			message = response.Msg
		}
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
