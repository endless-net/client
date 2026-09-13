package main

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestNativeResourceMutationRequiresExplicitIntentAndContext(t *testing.T) {
	base := []string{"--profile-id", "profile", "--request-id", "b10b8dab-f1a2-46a0-b489-0e151c2bcc51", "--expected-instance-id", "instance", "--expected-revision", "7"}
	for _, args := range [][]string{nil, {"--resource-id", "resource"}, {"--enabled=false"}, {"--resource-id", " ", "--enabled=true"}} {
		var output bytes.Buffer
		err := cmdServiceRPCMutation("set-resource-enabled", append(append([]string(nil), base...), args...), &output)
		if err == nil || !strings.Contains(err.Error(), "requires --resource-id and explicit") || output.Len() != 0 {
			t.Fatal("resource mutation reached transport without explicit intent")
		}
	}
	var output bytes.Buffer
	if err := cmdServiceRPCMutation("set-resource-enabled", []string{"--resource-id", "resource", "--enabled=false"}, &output); err == nil || !strings.Contains(err.Error(), "--request-id UUID") || output.Len() != 0 {
		t.Fatal("resource mutation accepted missing durable context")
	}
}

func TestNativeResourceCatalogKindsAreBoundedAndExplicit(t *testing.T) {
	want := []ipc.ResourceKind{ipc.ResourceKind_RESOURCE_KIND_APPLICATION, ipc.ResourceKind_RESOURCE_KIND_HOST, ipc.ResourceKind_RESOURCE_KIND_SUBNET, ipc.ResourceKind_RESOURCE_KIND_SERVICE}
	got, err := nativeResourceKinds("application, host,subnet,service")
	if err != nil || !slices.Equal(got, want) {
		t.Fatal("resource kinds changed order or meaning")
	}
	if got, err := nativeResourceKinds(""); err != nil || len(got) != 0 {
		t.Fatal("absent filter invented kinds")
	}
	for _, raw := range []string{"unspecified", "1", "host,host", "host,", ",host", " ", "HOST", "host,subnet,service,application,host", strings.Repeat("host", 17)} {
		var output bytes.Buffer
		err := cmdServiceRPCQuery("resources", []string{"--profile-id", "profile", "--kinds", raw}, &output)
		if err == nil || !strings.Contains(err.Error(), "--kinds requires") || output.Len() != 0 {
			t.Fatal("invalid filter reached transport")
		}
	}
	for _, args := range [][]string{nil, {"--profile-id", "profile", "--page-size", "501"}} {
		var output bytes.Buffer
		if err := cmdServiceRPCQuery("resources", args, &output); err == nil || output.Len() != 0 {
			t.Fatal("resource catalog accepted missing profile or oversized page")
		}
	}
}
