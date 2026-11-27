package clientv0_test

import (
	"testing"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"google.golang.org/protobuf/proto"
)

func TestDnsApplicationSnapshots(t *testing.T) {
	for name, status := range map[string]*clientv0.DnsApplication{
		"unobserved":                             nil,
		"failed update with previous assignment": {AccountId: "a", NetworkId: "n", NodeId: "d", TargetMapRevision: 9, LastAppliedMapRevision: proto.Uint64(8), State: clientv0.DnsApplyState_DNS_APPLY_STATE_FAILED, Failure: clientv0.DnsApplyFailure_DNS_APPLY_FAILURE_OS_APPLY},
		"cleanup confirmed":                      {AccountId: "a", NetworkId: "n", NodeId: "d", TargetMapRevision: 9, State: clientv0.DnsApplyState_DNS_APPLY_STATE_WITHDRAWN},
	} {
		t.Run(name, func(t *testing.T) {
			want := &clientv0.DnsDiagnostics{Application: status}
			wire, err := proto.Marshal(want)
			if err != nil {
				t.Fatal(err)
			}
			var got clientv0.DnsDiagnostics
			if err := proto.Unmarshal(wire, &got); err != nil {
				t.Fatal(err)
			}
			if !proto.Equal(want, &got) {
				t.Fatalf("round trip: got %v, want %v", &got, want)
			}
		})
	}
}
