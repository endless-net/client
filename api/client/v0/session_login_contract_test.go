package clientv0_test

import (
	"bytes"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

const sessionLoginRequestID = "611fb038-329c-4de5-872a-89bcab17287a"

func TestSessionLoginOwnerSurfaceAndCutover(t *testing.T) {
	methods := clientv0.File_client_v0_service_proto.Services().ByName("ClientService").Methods()
	for _, name := range []protoreflect.Name{"GetSession", "StartSessionLogin", "GetSessionLogin"} {
		method := methods.ByName(name)
		if method == nil || proto.GetExtension(method.Options(), clientv0.E_Access).(clientv0.Access) != clientv0.Access_ACCESS_OWNER || proto.GetExtension(method.Options(), clientv0.E_AllowsInitialOwnershipClaim).(bool) {
			t.Fatalf("%s grants initial claim or lacks owner access declaration", name)
		}
	}
	for _, name := range []protoreflect.Name{"RenewSession", "WaitSessionRenewal"} {
		if methods.ByName(name) != nil {
			t.Fatalf("legacy provider-code method retained: %s", name)
		}
	}
	if clientv0.Capability_CAPABILITY_SESSION_LOGIN.Number() != 18 {
		t.Fatal("explicit login capability changed")
	}
	// These declarations do not prove runtime OS authentication/no-side-effects.
}

func validSessionLoginStart() *clientv0.StartSessionLoginRequest {
	return &clientv0.StartSessionLoginRequest{RequestId: sessionLoginRequestID, Mutation: &clientv0.MutationContext{ExpectedInstanceId: "instance-fixture", ExpectedRevision: 7}}
}

func TestSessionLoginStartInputValidation(t *testing.T) {
	for name, change := range map[string]func(*clientv0.StartSessionLoginRequest){
		"missing request":  func(r *clientv0.StartSessionLoginRequest) { r.RequestId = "" },
		"uppercase UUID":   func(r *clientv0.StartSessionLoginRequest) { r.RequestId = strings.ToUpper(sessionLoginRequestID) },
		"nil UUID":         func(r *clientv0.StartSessionLoginRequest) { r.RequestId = "00000000-0000-0000-0000-000000000000" },
		"missing CAS":      func(r *clientv0.StartSessionLoginRequest) { r.Mutation = nil },
		"missing instance": func(r *clientv0.StartSessionLoginRequest) { r.Mutation.ExpectedInstanceId = "" },
		"missing revision": func(r *clientv0.StartSessionLoginRequest) { r.Mutation.ExpectedRevision = 0 },
		"invalid profile": func(r *clientv0.StartSessionLoginRequest) {
			r.Profile = &clientv0.ProfileRef{ProfileId: "foreign-input"}
		},
		"unknown request": func(r *clientv0.StartSessionLoginRequest) { r.ProtoReflect().SetUnknown([]byte{0x98, 0x06, 0x01}) },
		"unknown CAS": func(r *clientv0.StartSessionLoginRequest) {
			r.Mutation.ProtoReflect().SetUnknown([]byte{0x98, 0x06, 0x01})
		},
		"unknown profile": func(r *clientv0.StartSessionLoginRequest) {
			r.Profile = &clientv0.ProfileRef{}
			r.Profile.ProtoReflect().SetUnknown([]byte{0x98, 0x06, 0x01})
		},
	} {
		t.Run(name, func(t *testing.T) {
			request := validSessionLoginStart()
			change(request)
			if !errors.Is(clientv0.ValidateStartSessionLoginRequest(request), clientv0.ErrInvalidSessionLoginInput) {
				t.Fatal("invalid input accepted")
			}
		})
	}
	if clientv0.ValidateStartSessionLoginRequest(nil) == nil {
		t.Fatal("nil request accepted")
	}
	for _, profile := range []*clientv0.ProfileRef{nil, {}, {ProfileId: sessionLoginRequestID}} {
		request := validSessionLoginStart()
		request.Profile = profile
		if err := clientv0.ValidateStartSessionLoginRequest(request); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSessionLoginLookupValidationAndSharedWire(t *testing.T) {
	valid := []*clientv0.GetSessionLoginRequest{
		{Selector: &clientv0.GetSessionLoginRequest_AttemptId{AttemptId: sessionLoginRequestID}},
		{Selector: &clientv0.GetSessionLoginRequest_RequestId{RequestId: sessionLoginRequestID}},
		{Selector: &clientv0.GetSessionLoginRequest_Current{Current: true}},
	}
	for _, request := range valid {
		if err := clientv0.ValidateGetSessionLoginRequest(request); err != nil {
			t.Fatal(err)
		}
	}
	for _, request := range []*clientv0.GetSessionLoginRequest{nil, {},
		{Selector: &clientv0.GetSessionLoginRequest_Current{Current: false}},
		{Selector: &clientv0.GetSessionLoginRequest_AttemptId{}},
		{Selector: &clientv0.GetSessionLoginRequest_RequestId{RequestId: "provider-code"}},
		{Selector: &clientv0.GetSessionLoginRequest_Current{Current: true}, Profile: &clientv0.ProfileRef{ProfileId: "bad"}},
	} {
		if clientv0.ValidateGetSessionLoginRequest(request) == nil {
			t.Fatal("invalid lookup accepted")
		}
	}
	unknown := proto.Clone(valid[2]).(*clientv0.GetSessionLoginRequest)
	unknown.ProtoReflect().SetUnknown([]byte{0x98, 0x06, 0x01})
	if clientv0.ValidateGetSessionLoginRequest(unknown) == nil {
		t.Fatal("unknown lookup input accepted")
	}
	for _, value := range []bool{false, true} {
		wire, err := proto.Marshal(&clientv0.GetSessionLoginRequest{Selector: &clientv0.GetSessionLoginRequest_Current{Current: value}})
		want := []byte{0x20, 0}
		if value {
			want[1] = 1
		}
		if err != nil || !bytes.Equal(wire, want) {
			t.Fatalf("shared Go/Dart current vector changed: %x %v", wire, err)
		}
		var decoded clientv0.GetSessionLoginRequest
		if err := proto.Unmarshal(wire, &decoded); err != nil {
			t.Fatal(err)
		}
		if selected, ok := decoded.Selector.(*clientv0.GetSessionLoginRequest_Current); !ok || selected.Current != value {
			t.Fatal("current=false presence lost")
		}
	}
	// Standard binary last-wins, not strict raw-wire rejection.
	requestWire, err := proto.Marshal(valid[1])
	if err != nil {
		t.Fatal(err)
	}
	var decoded clientv0.GetSessionLoginRequest
	if err := proto.Unmarshal(append([]byte{0x20, 0x01}, requestWire...), &decoded); err != nil || decoded.GetRequestId() != sessionLoginRequestID || decoded.GetCurrent() {
		t.Fatal("binary selector semantics changed", err)
	}
	start, err := proto.MarshalOptions{Deterministic: true}.Marshal(validSessionLoginStart())
	const wantStart = "0a140a10696e7374616e63652d6669787475726510071a2436313166623033382d333239632d346465352d383732612d383962636162313732383761"
	if err != nil || hex.EncodeToString(start) != wantStart {
		t.Fatalf("shared Go/Dart Start vector changed: %x %v", start, err)
	}
}

func TestSessionLoginProjectionHasNoCredentialsOrNodeReceipt(t *testing.T) {
	visited := map[protoreflect.FullName]bool{}
	var inspect func(protoreflect.MessageDescriptor)
	inspect = func(message protoreflect.MessageDescriptor) {
		if visited[message.FullName()] {
			return
		}
		visited[message.FullName()] = true
		for i := 0; i < message.Fields().Len(); i++ {
			field := message.Fields().Get(i)
			for _, forbidden := range []string{"private_key", "bearer", "authorization", "poll_token", "user_code", "verifier", "secret", "proof", "node_credential", "node_id"} {
				if strings.Contains(string(field.Name()), forbidden) {
					t.Fatalf("secret or Node receipt field: %s", field.FullName())
				}
			}
			if field.Message() != nil {
				inspect(field.Message())
			}
		}
	}
	inspect((&clientv0.SessionLoginAttempt{}).ProtoReflect().Descriptor())
	response := (&clientv0.GetSessionResponse{}).ProtoReflect().Descriptor()
	if response.Fields().ByName("login_attempt").Message().FullName() != "client.v0.SessionLoginAttempt" {
		t.Fatal("GetSession cannot recover current login attempt")
	}
}
