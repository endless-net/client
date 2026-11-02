package clientv0_test

import (
	"strings"
	"testing"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestRemoteRevocationDoesNotInferEnforcementOrExportCredentials(t *testing.T) {
	visited := map[protoreflect.FullName]bool{}
	var inspect func(protoreflect.MessageDescriptor)
	inspect = func(message protoreflect.MessageDescriptor) {
		if visited[message.FullName()] {
			return
		}
		visited[message.FullName()] = true
		for i := 0; i < message.Fields().Len(); i++ {
			field := message.Fields().Get(i)
			for _, forbidden := range []string{"private_key", "token", "bearer", "authorization", "secret", "proof", "cleaned", "enforced"} {
				if strings.Contains(string(field.Name()), forbidden) {
					t.Fatalf("unsafe remote revoke field: %s", field.FullName())
				}
			}
			if field.Message() != nil {
				inspect(field.Message())
			}
		}
	}
	inspect((&clientv0.NodeRevocationResult{}).ProtoReflect().Descriptor())
	inspect((&clientv0.ListNodeRevocationTargetsResponse{}).ProtoReflect().Descriptor())
	phase := clientv0.NodeRevocationPhase_NODE_REVOCATION_PHASE_ACCEPTED.Descriptor()
	if phase.Values().Len() != 3 {
		t.Fatal("new state requires an explicit evidence contract")
	}
	unknown := &clientv0.NodeRevocationResult{CommandId: "original", Phase: clientv0.NodeRevocationPhase_NODE_REVOCATION_PHASE_UNKNOWN}
	if clone := proto.Clone(unknown).(*clientv0.NodeRevocationResult); clone.Phase != unknown.Phase || clone.AcceptedAt != nil || clone.EnforceBy != nil || clone.CommittedNetworkRevision != 0 {
		t.Fatal("unknown result invented a receipt")
	}
	service := clientv0.File_client_v0_service_proto.Services().ByName("ClientService")
	for _, name := range []protoreflect.Name{"ListNodeRevocationTargets", "RevokeNodeAccess", "GetNodeRevocation"} {
		method := service.Methods().ByName(name)
		if method == nil || proto.GetExtension(method.Options(), clientv0.E_Access) != clientv0.Access_ACCESS_OWNER || proto.GetExtension(method.Options(), clientv0.E_AllowsInitialOwnershipClaim).(bool) {
			t.Fatal("remote revoke bypassed existing owner IPC boundary", name)
		}
	}
}
