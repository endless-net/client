package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"runtime"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func nativeServiceEndpoint(pipe, socket string) (string, error) {
	pipe, socket = strings.TrimSpace(pipe), strings.TrimSpace(socket)
	if (pipe != "" && socket != "") || (runtime.GOOS == "windows" && socket != "") || (runtime.GOOS != "windows" && pipe != "") {
		return "", fmt.Errorf("service requires exactly one platform-local endpoint")
	}
	if pipe != "" {
		return pipe, nil
	}
	if socket != "" {
		return socket, nil
	}
	switch runtime.GOOS {
	case "windows":
		return local.DefaultWindowsPipe, nil
	case "darwin":
		return local.DefaultDarwinSocket, nil
	case "linux":
		return local.DefaultUnixSocket, nil
	default:
		return "", fmt.Errorf("local service RPC is unsupported on %s", runtime.GOOS)
	}
}

func cmdServiceRPCQuery(command string, args []string, output io.Writer) error {
	fs := flag.NewFlagSet("service "+command, flag.ContinueOnError)
	pipe, socket := serviceIPCTransportFlags(fs)
	timeoutValue := fs.String("timeout", "30s", "maximum time for native service RPC")
	var operationID, requestID string
	var profileID string
	requiresProfile := command == "preferences" || command == "managed-settings" || command == "session" || command == "server-identity" || command == "networks" || command == "peers" || command == "diagnostics" || command == "logs-recent"
	if requiresProfile {
		fs.StringVar(&profileID, "profile-id", "", "required target profile")
	}
	var wait bool
	var pageSize uint
	var pageToken string
	if command == "profiles" || command == "networks" || command == "peers" || command == "logs-recent" {
		fs.UintVar(&pageSize, "page-size", 0, "page size; 0 uses 100, maximum 500")
		fs.StringVar(&pageToken, "page-token", "", "opaque token from the previous page")
	}
	var search string
	if command == "peers" {
		fs.StringVar(&search, "search", "", "filter peers by ID, hostname or overlay address")
	}
	if command == "operation" {
		fs.BoolVar(&wait, "wait", false, "emit operation changes until terminal state or timeout; never repeat the mutation")
		fs.StringVar(&operationID, "operation-id", "", "accepted operation UUID")
		fs.StringVar(&requestID, "request-id", "", "original mutation request UUID for lost-response recovery")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("service %s does not accept positional arguments", command)
	}
	if requiresProfile && strings.TrimSpace(profileID) == "" {
		return fmt.Errorf("--profile-id is required")
	}
	if pageSize > 500 {
		return fmt.Errorf("--page-size cannot exceed 500")
	}
	var lookup *ipc.GetOperationRequest
	if command == "operation" {
		operationID, requestID = strings.TrimSpace(operationID), strings.TrimSpace(requestID)
		if (operationID == "") == (requestID == "") {
			return fmt.Errorf("service operation requires exactly one of --operation-id or --request-id")
		}
		lookup = &ipc.GetOperationRequest{}
		if operationID != "" {
			lookup.Lookup = &ipc.GetOperationRequest_OperationId{OperationId: operationID}
		} else {
			lookup.Lookup = &ipc.GetOperationRequest_RequestId{RequestId: requestID}
		}
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
	info, err := consumer.Bootstrap(ctx)
	if err != nil {
		return err
	}
	var message proto.Message
	switch command {
	case "preferences":
		response, err := consumer.GetPreferences(ctx, connect.NewRequest(&ipc.GetPreferencesRequest{Profile: &ipc.ProfileRef{ProfileId: profileID}}))
		if err != nil {
			return err
		}
		message = response.Msg
	case "managed-settings":
		response, err := consumer.ListManagedSettings(ctx, connect.NewRequest(&ipc.ListManagedSettingsRequest{Profile: &ipc.ProfileRef{ProfileId: profileID}}))
		if err != nil {
			return err
		}
		message = response.Msg
	case "session":
		response, err := consumer.GetSession(ctx, connect.NewRequest(&ipc.GetSessionRequest{Profile: &ipc.ProfileRef{ProfileId: profileID}}))
		if err != nil {
			return err
		}
		message = response.Msg
	case "peers":
		response, err := consumer.ListPeers(ctx, connect.NewRequest(&ipc.ListPeersRequest{Profile: &ipc.ProfileRef{ProfileId: profileID}, Search: search, Page: &ipc.PageRequest{PageSize: uint32(pageSize), PageToken: pageToken}}))
		if err != nil {
			return err
		}
		message = response.Msg
	case "networks":
		response, err := consumer.ListNetworks(ctx, connect.NewRequest(&ipc.ListNetworksRequest{Profile: &ipc.ProfileRef{ProfileId: profileID}, Page: &ipc.PageRequest{PageSize: uint32(pageSize), PageToken: pageToken}}))
		if err != nil {
			return err
		}
		message = response.Msg
	case "diagnostics":
		response, err := consumer.GetDiagnostics(ctx, connect.NewRequest(&ipc.GetDiagnosticsRequest{Profile: &ipc.ProfileRef{ProfileId: profileID}}))
		if err != nil {
			return err
		}
		message = response.Msg
	case "logs-recent":
		response, err := consumer.ListRecentLogs(ctx, connect.NewRequest(&ipc.ListRecentLogsRequest{Profile: &ipc.ProfileRef{ProfileId: profileID}, Page: &ipc.PageRequest{PageSize: uint32(pageSize), PageToken: pageToken}}))
		if err != nil {
			return err
		}
		message = response.Msg
	case "server-identity":
		response, err := consumer.GetServerIdentity(ctx, connect.NewRequest(&ipc.GetServerIdentityRequest{Profile: &ipc.ProfileRef{ProfileId: profileID}}))
		if err != nil {
			return err
		}
		message = response.Msg
	case "profiles":
		response, err := consumer.ListProfiles(ctx, connect.NewRequest(&ipc.ListProfilesRequest{Page: &ipc.PageRequest{PageSize: uint32(pageSize), PageToken: pageToken}}))
		if err != nil {
			return err
		}
		message = response.Msg
	case "operation":
		response, err := consumer.GetOperation(ctx, connect.NewRequest(lookup))
		if err != nil {
			return err
		}
		if wait {
			_, err := waitServiceOperation(ctx, consumer, response.Msg.Operation, 250*time.Millisecond, func(op *ipc.Operation) error {
				encoded, err := protojson.Marshal(&ipc.GetOperationResponse{Operation: op})
				if err != nil {
					return err
				}
				_, err = fmt.Fprintln(output, string(encoded))
				return err
			})
			return err
		}
		message = response.Msg
	case "runtime-info":
		message = &ipc.GetRuntimeInfoResponse{Runtime: info}
	case "events":
		stream, err := consumer.WatchEvents(ctx, connect.NewRequest(&ipc.WatchEventsRequest{}))
		if err != nil {
			return err
		}
		defer func() { _ = stream.Close() }()
		return writeServiceRPCEvents(ctx, stream, info.InstanceId, output)
	case "status":
		response, err := consumer.GetStatus(ctx, connect.NewRequest(&ipc.GetStatusRequest{}))
		if err != nil {
			return err
		}
		message = response.Msg
	case "support-info":
		response, err := consumer.GetSupportInfo(ctx, connect.NewRequest(&ipc.GetSupportInfoRequest{}))
		if err != nil {
			return err
		}
		message = response.Msg
	default:
		return fmt.Errorf("unknown native service query %q", command)
	}
	encoded, err := protojson.Marshal(message)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, string(encoded))
	return err
}
