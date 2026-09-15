package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/endless-net/client/internal/rpcutil"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func cmdManagedUp(args []string) error {
	fs := flag.NewFlagSet("up", flag.ContinueOnError)
	pipe, socket := serviceIPCTransportFlags(fs)
	profile := fs.String("profile-id", "", "required active profile")
	instance := fs.String("expected-instance-id", "", "required snapshot instance")
	revision := fs.Uint64("expected-revision", 0, "required snapshot revision")
	enrollID := fs.String("enroll-request-id", "", "optional enrollment UUID; omit for an already enrolled profile")
	connectID := fs.String("connect-request-id", "", "required connection UUID; retain both IDs for recovery")
	mode := fs.String("mode", "", "enrollment mode: workstation, server, subnet-router or interactive")
	hostname := fs.String("hostname", "", "optional enrollment hostname")
	tokenFile := fs.String("enrollment-token-file", "", "enrollment token file, or '-' for stdin")
	browser := fs.Bool("browser-login", false, "request browser approval")
	timeoutValue := fs.String("timeout", "10m", "total enrollment and connection wait timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || *profile == "" || *instance == "" || *revision == 0 || !rpcutil.ValidUUID(*connectID) || (*enrollID != "" && (!rpcutil.ValidUUID(*enrollID) || strings.EqualFold(*enrollID, *connectID))) {
		return fmt.Errorf("up requires a profile, snapshot CAS and distinct valid operation request UUIDs")
	}
	timeout, err := parsePositiveServiceIPCTimeout(*timeoutValue)
	if err != nil {
		return err
	}
	endpoint, err := nativeServiceEndpoint(*pipe, *socket)
	if err != nil {
		return err
	}
	ref := &ipc.ProfileRef{ProfileId: *profile}
	connection := &ipc.ConnectRequest{Mutation: &ipc.MutationContext{RequestId: *connectID, ExpectedInstanceId: *instance, ExpectedRevision: *revision}, Profile: ref}
	var enrollment *ipc.EnrollRequest
	if *enrollID != "" {
		if *browser && *tokenFile != "" {
			return fmt.Errorf("browser login and enrollment token are mutually exclusive")
		}
		token, err := secretFlagValue("enrollment-token", "", *tokenFile)
		if err != nil {
			return err
		}
		enrollment, err = nativeCLIEnrollment(*mode, *hostname, token, *browser)
		if err != nil {
			return err
		}
		enrollment.Mutation = &ipc.MutationContext{RequestId: *enrollID, ExpectedInstanceId: *instance, ExpectedRevision: *revision}
		enrollment.Profile = ref
	} else if *mode != "" || *hostname != "" || *browser || *tokenFile != "" {
		return fmt.Errorf("enrollment options require --enroll-request-id")
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
	return runManagedUp(ctx, consumer, enrollment, connection, 250*time.Millisecond, func(op *ipc.Operation) error {
		encoded, err := protojson.Marshal(&ipc.GetOperationResponse{Operation: op})
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(os.Stdout, string(encoded))
		return err
	})
}

type managedRPCClient interface {
	serviceOperationReader
	Enroll(context.Context, *connect.Request[ipc.EnrollRequest]) (*connect.Response[ipc.EnrollResponse], error)
	Connect(context.Context, *connect.Request[ipc.ConnectRequest]) (*connect.Response[ipc.ConnectResponse], error)
}

func runManagedUp(ctx context.Context, consumer managedRPCClient, enrollment *ipc.EnrollRequest, connection *ipc.ConnectRequest, interval time.Duration, report func(*ipc.Operation) error) error {
	validContext := func(m *ipc.MutationContext, p *ipc.ProfileRef) bool {
		return m.GetRequestId() != "" && m.GetExpectedInstanceId() != "" && m.GetExpectedRevision() > 0 && p.GetProfileId() != ""
	}
	if interval <= 0 || report == nil || !validContext(connection.GetMutation(), connection.GetProfile()) ||
		(enrollment != nil && (!validContext(enrollment.Mutation, enrollment.Profile) ||
			enrollment.Profile.ProfileId != connection.Profile.ProfileId ||
			enrollment.Mutation.ExpectedInstanceId != connection.Mutation.ExpectedInstanceId ||
			strings.EqualFold(enrollment.Mutation.RequestId, connection.Mutation.RequestId))) {
		return fmt.Errorf("managed up requires bound profile/instance contexts and distinct requests")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	connection = proto.Clone(connection).(*ipc.ConnectRequest)
	if enrollment != nil {
		enrollment = proto.Clone(enrollment).(*ipc.EnrollRequest)
		accepted, err := consumer.Enroll(ctx, connect.NewRequest(enrollment))
		if err != nil {
			return fmt.Errorf("enrollment: %w; inspect service operation --request-id %s", err, enrollment.Mutation.RequestId)
		}
		if accepted == nil || accepted.Msg == nil {
			return fmt.Errorf("enrollment response missing; inspect service operation --request-id %s", enrollment.Mutation.RequestId)
		}
		terminal, err := waitManagedOperation(ctx, consumer, accepted.Msg.Operation, enrollment.Mutation, enrollment.Profile, ipc.OperationKind_OPERATION_KIND_ENROLL, interval, report)
		if err != nil {
			return fmt.Errorf("enrollment: %w; inspect service operation --request-id %s", err, enrollment.Mutation.RequestId)
		}
		connection.Mutation.ExpectedInstanceId = terminal.Metadata.GetInstanceId()
		connection.Mutation.ExpectedRevision = terminal.Metadata.GetRevision()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	accepted, err := consumer.Connect(ctx, connect.NewRequest(connection))
	if err == nil {
		if accepted == nil || accepted.Msg == nil {
			err = fmt.Errorf("connection response missing")
		} else {
			_, err = waitManagedOperation(ctx, consumer, accepted.Msg.Operation, connection.Mutation, connection.Profile, ipc.OperationKind_OPERATION_KIND_CONNECT, interval, report)
		}
	}
	if err != nil {
		return fmt.Errorf("connection: %w; inspect service operation --request-id %s", err, connection.Mutation.RequestId)
	}
	return nil
}

// Validate every reported revision before it can become the next mutation CAS.
func waitManagedOperation(ctx context.Context, reader serviceOperationReader, initial *ipc.Operation, mutation *ipc.MutationContext, profile *ipc.ProfileRef, kind ipc.OperationKind, interval time.Duration, report func(*ipc.Operation) error) (*ipc.Operation, error) {
	minimumRevision := mutation.ExpectedRevision
	return waitServiceOperation(ctx, reader, initial, interval, func(op *ipc.Operation) error {
		if op.RequestId != mutation.RequestId || op.Kind != kind || op.ProfileId != profile.ProfileId ||
			op.Metadata.GetInstanceId() != mutation.ExpectedInstanceId || op.Metadata.GetRevision() < minimumRevision {
			return fmt.Errorf("operation does not match the retained mutation context")
		}
		if kind == ipc.OperationKind_OPERATION_KIND_ENROLL && op.State == ipc.OperationState_OPERATION_STATE_SUCCEEDED &&
			(op.GetEnrollment().GetProfileId() != profile.ProfileId || op.GetEnrollment().GetNodeId() == "") {
			return fmt.Errorf("enrollment completed without a matching enrolled node")
		}
		minimumRevision = op.Metadata.Revision
		return report(op)
	})
}
