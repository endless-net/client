package clientv0_test

import (
	"bytes"
	"strings"
	"testing"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestCurrentEnrollmentLookupShapeAndOwnerBoundary(t *testing.T) {
	method := clientv0.File_client_v0_service_proto.Services().ByName("ClientService").Methods().ByName("GetEnrollmentAttempt")
	if proto.GetExtension(method.Options(), clientv0.E_Access).(clientv0.Access) != clientv0.Access_ACCESS_OWNER || proto.GetExtension(method.Options(), clientv0.E_AllowsInitialOwnershipClaim).(bool) {
		t.Fatal("current lookup grants privileges or initial claim")
	}
	field := method.Input().Fields().ByName("current")
	if field == nil || field.Number() != 4 || field.Kind() != protoreflect.BoolKind || field.ContainingOneof() == nil || field.ContainingOneof().Name() != "selector" {
		t.Fatal("current selector contract changed")
	}
	for _, value := range []bool{true, false} {
		lookup := &clientv0.GetEnrollmentAttemptRequest{Selector: &clientv0.GetEnrollmentAttemptRequest_Current{Current: value}}
		raw, err := proto.Marshal(lookup)
		if err != nil {
			t.Fatal(err)
		}
		var decoded clientv0.GetEnrollmentAttemptRequest
		if err := proto.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		selected, ok := decoded.Selector.(*clientv0.GetEnrollmentAttemptRequest_Current)
		if !ok || selected.Current != value || decoded.GetAttemptId() != "" || decoded.GetRequestId() != "" {
			t.Fatal("oneof presence lost")
		}
		want := []byte{0x20, 0}
		if value {
			want[1] = 1
		}
		if !bytes.Equal(raw, want) {
			t.Fatalf("shared Go/Dart current vector: %x", raw)
		}
	}
	if (&clientv0.GetEnrollmentAttemptRequest{}).Selector != nil {
		t.Fatal("absent selector became current")
	}
}

func TestEnrollmentLookupRetainsStandardBinaryOneofLastWins(t *testing.T) {
	current, err := proto.Marshal(&clientv0.GetEnrollmentAttemptRequest{Selector: &clientv0.GetEnrollmentAttemptRequest_Current{Current: true}})
	if err != nil {
		t.Fatal(err)
	}
	request, err := proto.Marshal(&clientv0.GetEnrollmentAttemptRequest{Selector: &clientv0.GetEnrollmentAttemptRequest_RequestId{RequestId: "611fb038-329c-4de5-872a-89bcab17287a"}})
	if err != nil {
		t.Fatal(err)
	}
	var decoded clientv0.GetEnrollmentAttemptRequest
	if err := proto.Unmarshal(append(current, request...), &decoded); err != nil || decoded.GetRequestId() == "" || decoded.GetCurrent() {
		t.Fatal("standard binary last-wins changed", err)
	}
	// Typed/object validation sees exactly one selector. Shape tests do not
	// assert strict raw-wire rejection, runtime owner authorization or no-I/O.
}

func TestEnrollmentAttemptPreservesOriginalReplayInputs(t *testing.T) {
	attempt := &clientv0.EnrollmentAttempt{RequestId: "611fb038-329c-4de5-872a-89bcab17287a", Mode: clientv0.EnrollmentMode_ENROLLMENT_MODE_SERVER, Hostname: "host"}
	raw, err := proto.Marshal(attempt)
	if err != nil {
		t.Fatal(err)
	}
	var decoded clientv0.EnrollmentAttempt
	if err := proto.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	retry := &clientv0.EnrollRequest{RequestId: decoded.GetRequestId(), Mode: decoded.GetMode(), Hostname: decoded.GetHostname()}
	if retry.RequestId != attempt.RequestId || retry.Mode != attempt.Mode || retry.Hostname != attempt.Hostname {
		t.Fatal("original replay binding lost")
	}
	fields := attempt.ProtoReflect().Descriptor().Fields()
	if fields.ByName("mode").Number() != 18 || fields.ByName("hostname").Number() != 19 {
		t.Fatal("original inputs field identity changed")
	}
	vector, err := proto.Marshal(&clientv0.EnrollmentAttempt{Mode: clientv0.EnrollmentMode_ENROLLMENT_MODE_SERVER, Hostname: "host"})
	if err != nil || !bytes.Equal(vector, []byte{0x90, 0x01, 0x02, 0x9a, 0x01, 0x04, 'h', 'o', 's', 't'}) {
		t.Fatalf("Go/Dart original-input vector: %x %v", vector, err)
	}
	missing := &clientv0.EnrollmentAttempt{}
	if missing.GetMode() != clientv0.EnrollmentMode_ENROLLMENT_MODE_UNSPECIFIED || missing.GetHostname() != "" {
		t.Fatal("missing projection guessed original inputs")
	}
	// Rejecting missing/unsupported original inputs for up and enforcing exact
	// retry authority are runtime responsibilities, not serialization claims.
}

// Рекурсивная проверка public projection защищает IPC от случайного экспорта
// native grants. Она не подтверждает runtime redaction или OS caller authority.
func TestEnrollmentProjectionHasNoCredentials(t *testing.T) {
	visited := map[protoreflect.FullName]bool{}
	var inspect func(protoreflect.MessageDescriptor)
	inspect = func(message protoreflect.MessageDescriptor) {
		if visited[message.FullName()] {
			return
		}
		visited[message.FullName()] = true
		for i := 0; i < message.Fields().Len(); i++ {
			field := message.Fields().Get(i)
			name := string(field.Name())
			for _, forbidden := range []string{"private_key", "bearer", "authorization", "poll_token", "user_code", "verifier", "secret", "proof"} {
				if strings.Contains(name, forbidden) {
					t.Fatalf("secret-capable IPC field: %s", field.FullName())
				}
			}
			if field.Message() != nil {
				inspect(field.Message())
			}
		}
	}
	inspect((&clientv0.EnrollmentAttempt{}).ProtoReflect().Descriptor())
	inspect((&clientv0.ListEnrollmentTargetsResponse{}).ProtoReflect().Descriptor())
}

func TestEnrollmentLookupSelectorAndUncertainOutcome(t *testing.T) {
	lookup := &clientv0.GetEnrollmentAttemptRequest{Selector: &clientv0.GetEnrollmentAttemptRequest_RequestId{RequestId: "611fb038-329c-4de5-872a-89bcab17287a"}}
	wire, err := proto.Marshal(lookup)
	if err != nil {
		t.Fatal(err)
	}
	var decoded clientv0.GetEnrollmentAttemptRequest
	if err := proto.Unmarshal(wire, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.GetAttemptId() != "" || decoded.GetRequestId() != lookup.GetRequestId() {
		t.Fatal("recovery selector lost")
	}
	attempt := &clientv0.EnrollmentAttempt{Phase: clientv0.EnrollmentPhase_ENROLLMENT_PHASE_COMPLETING, Certainty: clientv0.EnrollmentCertainty_ENROLLMENT_CERTAINTY_UNCERTAIN}
	clone, ok := proto.Clone(attempt).(*clientv0.EnrollmentAttempt)
	if !ok || clone.GetPhase() == clientv0.EnrollmentPhase_ENROLLMENT_PHASE_REGISTERED || clone.NodeId != nil || clone.GetCertainty() != clientv0.EnrollmentCertainty_ENROLLMENT_CERTAINTY_UNCERTAIN {
		t.Fatal("unknown completion became successful registration")
	}
}
