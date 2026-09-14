package client

import (
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

// Called under mutations.mu. Readiness is volatile; authority and the active
// renewal identity come only from protected, context-bound durable state.
func (m *ClientRPCMutations) projectSessionRenewalLocked(session *ipc.Session, cfg Config, profileID string) {
	if session == nil {
		return
	}
	session.Renewal = &ipc.Restriction{Availability: ipc.Availability_AVAILABILITY_TEMPORARILY_UNAVAILABLE, ReasonKey: "session_renewal_worker_unavailable"}
	if cfg.RPCState == nil {
		return
	}
	if plan := cfg.RPCState.SessionRenewal; plan != nil && plan.ProfileID == profileID && sessionRenewalBound(&cfg, plan) {
		session.State = ipc.SessionState_SESSION_STATE_RENEWING
		session.RenewalOperationId = plan.OperationID
		session.Renewal.ReasonKey = "session_renewal_in_progress"
		return
	}
	w := m.capabilityWorkers[ipc.Capability_CAPABILITY_SESSION_RENEWAL]
	if w == nil || w.ctx.Err() != nil {
		return
	}
	if profileID != cfg.RPCState.ActiveProfileID {
		session.Renewal.ReasonKey = "session_renewal_requires_active_profile"
		return
	}
	stored := cfg.UserSession
	if stored == nil || stored.TokenBinding != sessionTokenBinding(cfg.Token) || stored.ControlOrigin != cfg.RPCState.Profiles[profileID].ControlOrigin ||
		stored.Response == nil || !validRetainedSessionGrant(stored.RenewalGrant, stored.Response.Session, m.now()) {
		session.Renewal.ReasonKey = "session_renewal_requires_login"
		return
	}
	for _, record := range cfg.RPCState.Operations {
		op := new(ipc.Operation)
		if proto.Unmarshal(record.Operation, op) != nil || !rpcOperationTerminal(op.State) {
			session.Renewal.ReasonKey = "session_renewal_busy"
			return
		}
	}
	session.Renewal = &ipc.Restriction{Availability: ipc.Availability_AVAILABILITY_AVAILABLE}
}
