package rpc

import (
	"strings"
	"testing"

	clientipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestMutationKindsAndOwnershipClaimAnnotations(t *testing.T) {
	methods := clientipc.File_client_v0_service_proto.Services().ByName("ClientService").Methods()
	seen := map[clientipc.OperationKind]string{}
	claims := map[string]bool{}
	for i := 0; i < methods.Len(); i++ {
		method := methods.Get(i)
		name := string(method.Name())
		mutation := method.Input().Fields().ByName("mutation") != nil
		kind := proto.GetExtension(method.Options(), clientipc.E_OperationKind).(clientipc.OperationKind)
		claim := proto.GetExtension(method.Options(), clientipc.E_AllowsInitialOwnershipClaim).(bool)
		if claim {
			claims[name] = true
		}
		if !mutation {
			if kind != clientipc.OperationKind_OPERATION_KIND_UNSPECIFIED || claim {
				t.Errorf("read RPC %s has mutation annotations", name)
			}
			continue
		}
		if kind == clientipc.OperationKind_OPERATION_KIND_UNSPECIFIED {
			t.Errorf("%s has no kind", name)
		}
		if strings.ToLower(strings.ReplaceAll(strings.TrimPrefix(kind.String(), "OPERATION_KIND_"), "_", "")) != strings.ToLower(name) {
			t.Errorf("%s has mismatched operation kind %s", name, kind)
		}
		if previous, ok := seen[kind]; ok {
			t.Errorf("%s and %s share kind %v", previous, name, kind)
		}
		seen[kind] = name
		outcome := method.Output().Fields().ByName("operation")
		if outcome == nil || outcome.Message().FullName() != "client.v0.Operation" {
			t.Errorf("%s lacks typed operation", name)
		}
		if claim {
			access := proto.GetExtension(method.Options(), clientipc.E_Access).(clientipc.Access)
			if access != clientipc.Access_ACCESS_OWNER {
				t.Errorf("claim bypasses owner policy on %s", name)
			}
		}
	}
	if len(claims) != 2 || !claims["Enroll"] || !claims["CreateProfile"] {
		t.Fatalf("unexpected claim surface: %v", claims)
	}
	kinds := clientipc.OperationKind_OPERATION_KIND_UNSPECIFIED.Descriptor().Values()
	for i := 1; i < kinds.Len(); i++ {
		if _, ok := seen[clientipc.OperationKind(kinds.Get(i).Number())]; !ok {
			t.Errorf("orphan operation kind %s", kinds.Get(i).Name())
		}
	}
}

func TestNewFieldsSurviveWireRoundTrip(t *testing.T) {
	operation := &clientipc.Operation{Id: "op", ProfileId: "inactive-profile", Kind: clientipc.OperationKind_OPERATION_KIND_RENEW_SESSION, State: clientipc.OperationState_OPERATION_STATE_RUNNING}
	exit := &clientipc.ExitNodeStatus{
		RequestedExitNodeId: proto.String("exit"), RequestedFamilyMode: clientipc.ExitFamilyMode_EXIT_FAMILY_MODE_DUAL_STACK,
		ApplyState: clientipc.ApplyState_APPLY_STATE_FAILED,
		Ipv4:       &clientipc.ExitFamilyStatus{RequestedExitNodeId: proto.String("exit"), EffectiveExitNodeId: proto.String("exit"), ApplyState: clientipc.ApplyState_APPLY_STATE_APPLIED, FailClosed: true},
		Ipv6:       &clientipc.ExitFamilyStatus{RequestedExitNodeId: proto.String("exit"), ApplyState: clientipc.ApplyState_APPLY_STATE_FAILED, FailClosed: true, Failure: &clientipc.Failure{Code: clientipc.ErrorCode_ERROR_CODE_APPLY_FAILED}},
	}
	for _, message := range []proto.Message{
		operation,
		&clientipc.GetOperationResponse{Operation: operation},
		&clientipc.Status{CurrentOperations: []*clientipc.Operation{operation}},
		&clientipc.WatchEventsResponse{Event: &clientipc.WatchEventsResponse_OperationChanged{OperationChanged: operation}},
		exit,
		&clientipc.ExitNode{AllowedFamilyModes: []clientipc.ExitFamilyMode{clientipc.ExitFamilyMode_EXIT_FAMILY_MODE_IPV4_ONLY, clientipc.ExitFamilyMode_EXIT_FAMILY_MODE_DUAL_STACK}},
		&clientipc.SelectExitNodeRequest{FamilyMode: clientipc.ExitFamilyMode_EXIT_FAMILY_MODE_DUAL_STACK},
	} {
		data, err := proto.Marshal(message)
		if err != nil {
			t.Fatal(err)
		}
		decoded := message.ProtoReflect().New().Interface()
		if err := proto.Unmarshal(data, decoded); err != nil {
			t.Fatal(err)
		}
		if !proto.Equal(message, decoded) {
			t.Fatalf("lost fields for %T", message)
		}
	}
	// Presence must distinguish a missing effective exit on IPv6 from success.
	if exit.Ipv6.ProtoReflect().Has(exit.Ipv6.ProtoReflect().Descriptor().Fields().ByName(protoreflect.Name("effective_exit_node_id"))) {
		t.Fatal("failed IPv6 unexpectedly has effective selection")
	}
}
