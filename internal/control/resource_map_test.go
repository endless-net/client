package control

import (
	"reflect"
	"testing"
	"time"

	"github.com/endless-net/client/internal/testwire"
	contract "github.com/endless-net/coordinator/api/enrollment"
)

func TestResourceMapPreservesSignedPathFields(t *testing.T) {
	host := contract.ServiceHost{NodeID: "router", PublicKey: "router-key"}
	resource := contract.Application{
		ID: "db", TargetType: "ip", Target: "10.20.0.7", Connectors: []contract.ServiceHost{host},
		AllowedPorts: []contract.ServicePort{{Protocol: "tcp", Port: 443}},
		Routes:       []contract.ApplicationRoute{{Connector: host, CIDRs: []string{"10.20.0.7/32"}, ExpiresAt: time.Unix(100, 0).UTC(), Metric: 15, Masquerade: true}},
	}
	snapshot := contract.NetworkMapSnapshot{Network: contract.Network{Applications: []contract.Application{resource}}}
	got, err := MapSnapshot(testwire.Map(snapshot))
	if err != nil || !reflect.DeepEqual(got.Network.Applications, snapshot.Network.Applications) {
		t.Fatalf("decoded resource: %+v, %v", got.Network.Applications, err)
	}
}
