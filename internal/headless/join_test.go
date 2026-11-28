package headless

import (
	"bytes"
	"strings"
	"testing"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"google.golang.org/protobuf/proto"
)

func TestJoinSubmitsOnceAndRecoversOnlySameTarget(t *testing.T) {
	for _, test := range []struct {
		name    string
		lost    bool
		network string
		wantOK  bool
	}{{"ack", false, "n", true}, {"lost ack", true, "n", true}, {"foreign receipt", true, "other", false}} {
		t.Run(test.name, func(t *testing.T) {
			s := snapshot(clientv0.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED, 7, clientv0.DesiredState_DESIRED_STATE_DISCONNECTED)
			a := upAttempt()
			a.AccountId, a.NetworkId = proto.String("a"), proto.String(test.network)
			a.Phase = clientv0.EnrollmentPhase_ENROLLMENT_PHASE_REGISTERING
			f := &upFake{fakeService: &fakeService{snapshots: []*clientv0.Status{s}}, result: a, lost: test.lost}
			var out bytes.Buffer
			err := EnrollWithKey(t.Context(), f, "a", "n", strings.NewReader("fixture-secret\r\n"), &out)
			if (err == nil) != test.wantOK || f.started.GetJoinTokenEnrollment().GetJoinToken() != "fixture-secret" || f.connectCalls != 0 {
				t.Fatal("unexpected admission outcome", err)
			}
			if (len(f.lookups) != 0) != test.lost || test.lost && (len(f.lookups) != 1 || f.lookups[0].GetRequestId() != f.started.RequestId) {
				t.Fatal("unexpected recovery lookup")
			}
			if strings.Contains(out.String(), "fixture-secret") || !test.wantOK && out.Len() != 0 {
				t.Fatal("unexpected console output")
			}
		})
	}
}

func TestJoinReportsSavedQuotaDenialAfterLostReply(t *testing.T) {
	s := snapshot(clientv0.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED, 7, clientv0.DesiredState_DESIRED_STATE_DISCONNECTED)
	a := upAttempt()
	a.AccountId, a.NetworkId = proto.String("a"), proto.String("n")
	a.Phase = clientv0.EnrollmentPhase_ENROLLMENT_PHASE_DENIED
	a.Restriction = &clientv0.Restriction{ReasonKey: "client.registration.quota_exhausted"}
	f := &upFake{fakeService: &fakeService{snapshots: []*clientv0.Status{s}}, result: a, lost: true}
	var out bytes.Buffer
	err := EnrollWithKey(t.Context(), f, "a", "n", strings.NewReader("fixture-key"), &out)
	if err == nil || !strings.Contains(err.Error(), "Квота устройств исчерпана") || out.Len() != 0 || len(f.lookups) != 1 || f.lookups[0].GetRequestId() != f.started.RequestId {
		t.Fatal("ожидался подтверждённый отказ исходной заявки", err)
	}
}

func TestJoinRejectsMalformedInputBeforeIPC(t *testing.T) {
	for _, input := range []string{"", " ", "one\ntwo", "one\n\r\n", "one\r", strings.Repeat("x", 65537)} {
		f := &upFake{fakeService: &fakeService{}}
		var out bytes.Buffer
		if err := EnrollWithKey(t.Context(), f, "a", "n", strings.NewReader(input), &out); err == nil || f.reads != 0 || f.started != nil || out.Len() != 0 {
			t.Fatal("invalid input was accepted")
		}
	}
}
