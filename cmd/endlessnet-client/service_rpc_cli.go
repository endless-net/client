package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"runtime"
	"strings"

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
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("service %s does not accept positional arguments", command)
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
