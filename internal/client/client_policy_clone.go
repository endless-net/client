package client

import (
	"slices"

	api "github.com/endless-net/client-api/clientapi/v1"
)

// Runtime plans and rollback snapshots own every mutable policy field.
func cloneClientPolicy(value *api.ClientPolicy) *api.ClientPolicy {
	if value == nil {
		return nil
	}
	copy := *value
	copy.Resources = slices.Clone(value.Resources)
	copy.Settings = slices.Clone(value.Settings)
	for i := range copy.Settings {
		setting := &copy.Settings[i]
		if setting.BooleanValue != nil {
			v := *setting.BooleanValue
			setting.BooleanValue = &v
		}
		if setting.LifecycleValue != nil {
			v := *setting.LifecycleValue
			setting.LifecycleValue = &v
		}
	}
	copy.ExitNodes = slices.Clone(value.ExitNodes)
	for i := range copy.ExitNodes {
		copy.ExitNodes[i].AllowedFamilyModes = slices.Clone(value.ExitNodes[i].AllowedFamilyModes)
		copy.ExitNodes[i].AllowedLANAccess = slices.Clone(value.ExitNodes[i].AllowedLANAccess)
	}
	return &copy
}
