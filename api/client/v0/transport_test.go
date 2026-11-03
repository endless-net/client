package clientv0_test

import (
	"strings"
	"testing"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"google.golang.org/protobuf/proto"
)

func TestWorkingRelayWithBackgroundDirectSearch(t *testing.T) {
	status := &clientv0.Status{ServiceState: clientv0.ServiceState_SERVICE_STATE_CONNECTED, ConnectionPhase: clientv0.ConnectionPhase_CONNECTION_PHASE_CONNECTED, PeerTransports: []*clientv0.PeerTransportStatus{{PeerId: "peer", SelectedPath: clientv0.PathKind_PATH_KIND_RELAY, Engine: "wireguard", ProtocolVersion: "1", TransportProfileId: "wg", TransportProfileRevision: 9007199254740993, SearchState: clientv0.TransportSearchState_TRANSPORT_SEARCH_STATE_SEARCHING, ReasonKey: "client.transport.searching"}}}
	raw, err := proto.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	var decoded clientv0.Status
	if err := proto.Unmarshal(raw, &decoded); err != nil || !proto.Equal(status, &decoded) {
		t.Fatal("working Relay/search projection changed", err)
	}
	if decoded.PeerTransports[0].TransportProfileRevision != 9007199254740993 {
		t.Fatal("profile revision lost uint64 precision")
	}
	fields := decoded.PeerTransports[0].ProtoReflect().Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {
		name := string(fields.Get(i).Name())
		for _, secret := range []string{"key", "nonce", "fingerprint", "template", "parameters", "profile_digest"} {
			if name != "reason_key" && strings.Contains(name, secret) {
				t.Fatal("sensitive transport field in public projection", name)
			}
		}
	}
}
