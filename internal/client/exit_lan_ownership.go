package client

import (
	"math"

	api "github.com/endless-net/client-api/clientapi/v1"
)

// This record addresses previously owned kernel objects for recovery. It is
// neither permission to adopt matching pins nor evidence of live attachment,
// current authority, or a valid lease. Pin names are derived from Scope only.
type exitLANOwnership struct {
	Scope           string                   `json:"scope"`
	BootID          string                   `json:"boot_id"`
	NamespaceDevice uint64                   `json:"namespace_device"`
	NamespaceInode  uint64                   `json:"namespace_inode"`
	Family          api.ExitFamilyMode       `json:"family"`
	MapID           uint32                   `json:"map_id"`
	ProgramID       uint32                   `json:"program_id"`
	Links           []exitLANOwnedLink       `json:"links"`
	Routing         *exitLANRoutingOwnership `json:"routing,omitempty"`
}

type exitLANOwnedLink struct {
	ID        uint32 `json:"id"`
	ProgramID uint32 `json:"program_id"`
	Family    uint32 `json:"family"`
	Hook      uint32 `json:"hook"`
	Priority  int32  `json:"priority"`
}

func validateExitLANOwnership(owned *exitLANOwnership) error {
	if owned == nil {
		return errExitLANBPF
	}
	if _, err := exitLANBPFPinNames(owned.Scope); err != nil {
		return errExitLANBPF
	}
	if !exitLANOwnershipBootID(owned.BootID) || owned.NamespaceDevice == 0 || owned.NamespaceInode == 0 || owned.MapID == 0 || owned.ProgramID == 0 {
		return errExitLANBPF
	}
	if owned.Routing != nil && (validateExitLANRouting(owned.Routing) != nil || owned.Routing.Family != owned.Family) {
		return errExitLANBPF
	}
	families, err := exitLANBPFFamilies(owned.Family)
	if err != nil || len(owned.Links) != len(families) {
		return errExitLANBPF
	}
	for i, link := range owned.Links {
		if link.ID == 0 || link.ProgramID != owned.ProgramID || link.Family != families[i] || link.Hook >= 5 || link.Priority == math.MinInt32 || link.Priority == math.MaxInt32 {
			return errExitLANBPF
		}
		if i != 0 && (link.ID == owned.Links[0].ID || link.Hook != owned.Links[0].Hook || link.Priority != owned.Links[0].Priority) {
			return errExitLANBPF
		}
	}
	return nil
}

func exitLANOwnershipBootID(id string) bool {
	if len(id) != 36 {
		return false
	}
	nonzero := false
	for i := 0; i < len(id); i++ {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if id[i] != '-' {
				return false
			}
			continue
		}
		if (id[i] < '0' || id[i] > '9') && (id[i] < 'a' || id[i] > 'f') {
			return false
		}
		nonzero = nonzero || id[i] != '0'
	}
	return nonzero
}

func cloneExitLANOwnership(owned *exitLANOwnership) *exitLANOwnership {
	if owned == nil {
		return nil
	}
	copy := *owned
	if owned.Links != nil {
		copy.Links = append([]exitLANOwnedLink{}, owned.Links...)
	}
	if owned.Routing != nil {
		routing := *owned.Routing
		routing.Routes = append([]exitLANOwnedRoute(nil), owned.Routing.Routes...)
		copy.Routing = &routing
	}
	return &copy
}
