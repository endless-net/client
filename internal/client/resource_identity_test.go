package client

import (
	"encoding/base64"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestResourceIdentityMatchesCatalogAndRejectsForgedTargets(t *testing.T) {
	m, owner, profile := rpcPreferenceFixture(t)
	s := NewClientRPCService(m, nil)
	response, err := s.resourcesAs(t.Context(), owner, &ipc.ListResourcesRequest{Profile: profile})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Resources) == 0 {
		t.Fatal("empty resource fixture")
	}
	cfg := m.store.Read()
	for _, resource := range response.Resources {
		identity, err := resolveRPCResource(cfg, resource.Id, time.Now())
		if err != nil || identity.Kind != resource.Kind {
			t.Fatal("catalog and mutation identity disagree", resource.Kind, err)
		}
	}
	peer := cfg.CachedMap.Peers[0]
	for _, id := range []string{
		response.Resources[0].Id + "=",
		rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_HOST, "undisclosed"),
		rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_SUBNET, peer.ID+"\x000.0.0.0/0"),
		rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_SUBNET, peer.ID+"\x00"+peer.AllowedIPs[0]),
		rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_SERVICE, "unknown\x00tcp\x00443"),
		base64.RawURLEncoding.EncodeToString([]byte("01\x00" + peer.ID)),
		rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_HOST, peer.ID+"\x00extra"),
	} {
		if _, err := resolveRPCResource(cfg, id, time.Now()); err == nil {
			t.Fatal("forged resource identity accepted", id)
		}
	}
	cfg.CachedMap.Network.Name = "tampered"
	if _, err := resolveRPCResource(cfg, response.Resources[0].Id, time.Now()); err == nil {
		t.Fatal("tampered map authorized resource identity")
	}
}

func TestResourceIdentityManagedSingleIPAndApplicationSource(t *testing.T) {
	for _, connector := range []bool{false, true} {
		cfg, source, key := signedApplicationFixture(t, connector)
		cidr := source.Peers[0].AllowedIPs[0]
		source.Network.ClientPolicy = &api.ClientPolicy{Resources: []api.ManagedResourceSetting{{Kind: api.ManagedResourceSubnet, ID: source.Peers[0].ID, CIDR: cidr, Source: api.ClientPolicyDevice, PolicyID: "subnet", Enabled: true}}}
		resignApplicationMap(t, &source, key)
		cfg.CachedMap, cfg.MapRevision, cfg.MapGlobalRevision = &source, source.Network.Revision, source.Revision.Global
		subnet := rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_SUBNET, source.Peers[0].ID+"\x00"+cidr)
		if value, err := resolveRPCResource(cfg, subnet, time.Now()); err != nil || value.CIDR != cidr {
			t.Fatal("managed single-IP subnet lost", err)
		}
		app := rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_APPLICATION, source.Network.Applications[0].ID)
		_, err := resolveRPCResource(cfg, app, time.Now())
		if (err == nil) == connector {
			t.Fatal("application source identity not enforced", connector, err)
		}
	}
}
