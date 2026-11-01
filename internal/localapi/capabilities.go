package localapi

import (
	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/localipc"
	"github.com/endless-net/client/internal/state"
)

// Projection не вызывает owner(..., true): bootstrap не захватывает installation
// и не подменяет atomic claim Enroll. Global capability — доступность local RPC,
// а не remote membership/quota/admission или право на любой выбранный profile.
func enrollmentCapabilities(s state.State, p localipc.Principal, platform clientv0.Platform) []*clientv0.CapabilityStatus {
	restriction := &clientv0.Restriction{Availability: clientv0.Availability_AVAILABILITY_PERMISSION_REQUIRED, ReasonKey: "client.owner.required", ActionOwner: clientv0.ActionOwner_ACTION_OWNER_USER}
	if p.SID != "" && (s.OwnerSID == p.SID || s.OwnerSID == "") {
		restriction = &clientv0.Restriction{Availability: clientv0.Availability_AVAILABILITY_AVAILABLE}
	}
	return []*clientv0.CapabilityStatus{{Capability: clientv0.Capability_CAPABILITY_ENROLLMENT, Restriction: restriction, Platform: platform}}
}
