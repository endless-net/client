package dataplane

import (
	"crypto/ed25519"
	"testing"

	"github.com/endless-net/client/internal/control"
	"github.com/endless-net/client/internal/testwire"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	wire "github.com/endless-net/coordinator/api/publicquery/v1"
	"google.golang.org/protobuf/proto"
)

func TestZoneSurvivesNodeMapTransport(t *testing.T) {
	f := newCandidateFixture(t)
	f.resolverDNS = true
	f.snapshot.Network.DNSConfig = &coordinator.DNSConfig{Zones: []coordinator.DNSZone{{Name: "dev.example.", Version: 9007199254740993, Records: []coordinator.DNSRecord{{Name: "api.dev.example.", Type: "A", Value: "192.0.2.9", TTL: 27}}}}}
	f.verify(t, nil)
	raw, err := proto.Marshal(testwire.Map(f.snapshot))
	if err != nil {
		t.Fatal(err)
	}
	var m wire.NetworkMapSnapshot
	if err = proto.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	verify := func() error {
		snapshot, err := control.MapSnapshot(&m)
		if err != nil {
			return err
		}
		return coordinator.VerifyNetworkMap(snapshot, f.mapKey.Public().(ed25519.PublicKey), f.snapshot.Network.AccountID, f.snapshot.Network.ID, f.snapshot.Node.ID, f.snapshot.Node.PublicKey, f.snapshot.MapSignature.IssuedAt)
	}
	if err = verify(); err != nil {
		t.Fatal("decoded zone failed signature verification", err)
	}
	m.Network.DnsConfig.Zones[0].Records[0].Ttl++
	if verify() == nil {
		t.Fatal("modified TTL retained a valid signature")
	}
}
