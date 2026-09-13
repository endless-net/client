package main

import (
	"fmt"
	"strings"

	ipc "github.com/endless-net/client/clientipc/v0"
)

func nativeResourceKinds(raw string) ([]ipc.ResourceKind, error) {
	if raw == "" {
		return nil, nil
	}
	invalid := func() ([]ipc.ResourceKind, error) {
		return nil, fmt.Errorf("--kinds requires unique host,subnet,service,application names")
	}
	if len(raw) > 64 {
		return invalid()
	}
	names := strings.Split(raw, ",")
	if len(names) > 4 {
		return invalid()
	}
	known := map[string]ipc.ResourceKind{"host": ipc.ResourceKind_RESOURCE_KIND_HOST, "subnet": ipc.ResourceKind_RESOURCE_KIND_SUBNET, "service": ipc.ResourceKind_RESOURCE_KIND_SERVICE, "application": ipc.ResourceKind_RESOURCE_KIND_APPLICATION}
	seen := map[ipc.ResourceKind]bool{}
	var kinds []ipc.ResourceKind
	for _, name := range names {
		kind, ok := known[strings.TrimSpace(name)]
		if !ok || seen[kind] {
			return invalid()
		}
		seen[kind] = true
		kinds = append(kinds, kind)
	}
	return kinds, nil
}
