package clientv0_test

import (
	"strings"
	"testing"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

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
