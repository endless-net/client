package clientv0_test

import (
	"bytes"
	"encoding/hex"
	"testing"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestDescriptorAccessAndInitialClaim(t *testing.T) {
	service := clientv0.File_client_v0_service_proto.Services().ByName("ClientService")
	if service.Methods().Len() != 39 {
		t.Fatalf("unexpected RPC inventory: %d", service.Methods().Len())
	}
	claims := map[string]bool{"CreateProfile": true, "Enroll": true}
	for i := 0; i < service.Methods().Len(); i++ {
		method := service.Methods().Get(i)
		if !proto.HasExtension(method.Options(), clientv0.E_Access) {
			t.Fatalf("%s lacks access declaration", method.Name())
		}
		role := proto.GetExtension(method.Options(), clientv0.E_Access).(clientv0.Access)
		if role == clientv0.Access_ACCESS_UNSPECIFIED {
			t.Fatalf("%s unspecified role", method.Name())
		}
		initial := proto.GetExtension(method.Options(), clientv0.E_AllowsInitialOwnershipClaim).(bool)
		if initial != claims[string(method.Name())] {
			t.Fatalf("%s initial claim=%v", method.Name(), initial)
		}
		if method.IsStreamingClient() || method.IsStreamingServer() != (method.Name() == "WatchEvents") {
			t.Fatalf("%s changed streaming shape", method.Name())
		}
	}
}

func TestMutationPresenceAndUnknownWire(t *testing.T) {
	absent := &clientv0.PreferencesPatch{}
	explicit := &clientv0.PreferencesPatch{AcceptDns: proto.Bool(false)}
	a, err := proto.Marshal(absent)
	if err != nil {
		t.Fatal(err)
	}
	b, err := proto.Marshal(explicit)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, b) {
		t.Fatal("absent and explicit false collapsed")
	}
	var decoded clientv0.PreferencesPatch
	if err := proto.Unmarshal(b, &decoded); err != nil || decoded.AcceptDns == nil || decoded.GetAcceptDns() {
		t.Fatalf("false presence lost: %v %v", &decoded, err)
	}
	wire := []byte{0x98, 0x06, 0x01} // Future field 99, value 1.
	var context clientv0.MutationContext
	if err := proto.Unmarshal(wire, &context); err != nil {
		t.Fatal(err)
	}
	result, err := proto.Marshal(&context)
	if err != nil || !bytes.Equal(wire, result) {
		t.Fatalf("unknown bytes lost: %x %v", result, err)
	}
	// Preservation is a serialization property, not permission for a boundary to accept an unknown mutation.
	for _, bad := range [][]byte{{0x0a, 0x80}, {0xff}, {0x0a, 0x02, 0x61}} {
		if err := proto.Unmarshal(bad, &clientv0.MutationContext{}); err == nil {
			t.Fatalf("malformed wire accepted: %x", bad)
		}
	}
}

func TestSharedDartExitSelectionVector(t *testing.T) {
	request := &clientv0.SelectExitNodeRequest{Mutation: &clientv0.MutationContext{ExpectedInstanceId: "instance-fixture", ExpectedRevision: 7}, Profile: &clientv0.ProfileRef{ProfileId: "profile-fixture"}, ExitNodeId: "exit-fixture", LanAccess: clientv0.LanAccess_LAN_ACCESS_BLOCK, FamilyMode: clientv0.ExitFamilyMode_EXIT_FAMILY_MODE_DUAL_STACK}
	wire, err := proto.MarshalOptions{Deterministic: true}.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	const expected = "0a140a10696e7374616e63652d66697874757265100712110a0f70726f66696c652d666978747572651a0c657869742d6669787475726520012804"
	if hex.EncodeToString(wire) != expected {
		t.Fatalf("Go/Dart fixture changed: %x", wire)
	}
	var decoded clientv0.SelectExitNodeRequest
	if err := proto.Unmarshal(wire, &decoded); err != nil || !proto.Equal(request, &decoded) {
		t.Fatalf("roundtrip: %v", err)
	}
}

func TestNoPrivateKeyExportField(t *testing.T) {
	files := []protoreflect.FileDescriptor{clientv0.File_client_v0_common_proto, clientv0.File_client_v0_runtime_proto, clientv0.File_client_v0_features_proto, clientv0.File_client_v0_service_proto}
	for _, file := range files {
		for i := 0; i < file.Messages().Len(); i++ {
			message := file.Messages().Get(i)
			for j := 0; j < message.Fields().Len(); j++ {
				field := message.Fields().Get(j)
				if field.Name() == "private_key" || field.Name() == "tunnel_private_key" {
					t.Fatalf("exportable secret field: %s", field.FullName())
				}
			}
		}
	}
}
