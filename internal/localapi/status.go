package localapi

import (
	"context"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/localipc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (c *Controller) GetStatus(ctx context.Context, _ *clientv0.GetStatusRequest) (*clientv0.GetStatusResponse, error) {
	p, err := localipc.Caller(ctx)
	if err != nil {
		return nil, err
	}
	return c.snapshot(ctx, p)
}

func (c *Controller) snapshot(ctx context.Context, caller localipc.Principal) (*clientv0.GetStatusResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	s, err := c.store.Load(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	defer clearProfileKeys(s)
	// A registration or durable intent alone never confirms native application.
	r := &clientv0.Status{Metadata: &clientv0.SnapshotMetadata{InstanceId: c.config.RuntimeID, Revision: s.Revision, GeneratedAt: timestamppb.New(c.config.Now())}, ServiceState: clientv0.ServiceState_SERVICE_STATE_DISCONNECTED, ConnectionPhase: clientv0.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED, Intent: &clientv0.ConnectionIntent{DesiredState: clientv0.DesiredState_DESIRED_STATE_DISCONNECTED}}
	if s.OwnerSID == "" {
		r.ServiceState = clientv0.ServiceState_SERVICE_STATE_NEEDS_ENROLLMENT
		r.ControlState = clientv0.ControlState_CONTROL_STATE_NOT_REGISTERED
	}
	if s.OwnerSID == "" || s.OwnerSID != caller.SID {
		return &clientv0.GetStatusResponse{Status: r}, nil
	}
	profile := s.Profiles[s.ActiveProfileID]
	r.ActiveProfileId = profile.ID
	r.Hostname = profile.Hostname
	r.ServiceState = clientv0.ServiceState_SERVICE_STATE_NEEDS_ENROLLMENT
	r.ControlState = clientv0.ControlState_CONTROL_STATE_NOT_REGISTERED
	session, _ := profile.CurrentSession()
	credential, credentialEnds, _ := profile.CurrentNodeCredential()
	r.StoredState = &clientv0.StoredStatePresence{IdentityPrivateKeyPresent: len(profile.Keys.IdentityPrivate) != 0, TunnelPrivateKeyPresent: len(profile.Keys.WireGuardPrivate) != 0, TokenPresent: session.Access != ""}
	r.StoredState.NodeCredentialPresent = credential != ""
	if profile.Attempt != nil && credential != "" {
		r.Credential = &clientv0.CredentialStatus{State: clientv0.CredentialState_CREDENTIAL_STATE_UNSPECIFIED}
		if !credentialEnds.IsZero() {
			r.Credential.ExpiresAt = timestamppb.New(credentialEnds)
			r.Credential.State = clientv0.CredentialState_CREDENTIAL_STATE_VALID
			if !c.config.Now().Before(credentialEnds) {
				r.Credential.State = clientv0.CredentialState_CREDENTIAL_STATE_EXPIRED
			}
		}
	}
	r.StoredState.DeviceFingerprintPresent = profile.Attempt != nil && profile.Attempt.DeviceKeyHash != [32]byte{}
	if session.Access != "" && !session.ExpiresAt.IsZero() {
		r.Session = &clientv0.Session{State: clientv0.SessionState_SESSION_STATE_ACTIVE, ExpiresAt: timestamppb.New(session.ExpiresAt)}
		if !c.config.Now().Before(session.ExpiresAt) {
			r.Session.State = clientv0.SessionState_SESSION_STATE_EXPIRED
		}
	}
	if profile.Attempt != nil {
		a := profile.Attempt
		switch a.Phase {
		case enrollment.AwaitingLogin:
			r.ServiceState = clientv0.ServiceState_SERVICE_STATE_NEEDS_LOGIN
			r.ControlState = clientv0.ControlState_CONTROL_STATE_NEEDS_LOGIN
		case enrollment.ChoosingContext:
			r.ServiceState = clientv0.ServiceState_SERVICE_STATE_NEEDS_ENROLLMENT
			r.ControlState = clientv0.ControlState_CONTROL_STATE_NOT_REGISTERED
		case enrollment.Registering, enrollment.Completing:
			r.ServiceState = clientv0.ServiceState_SERVICE_STATE_RECOVERING
			r.ControlState = clientv0.ControlState_CONTROL_STATE_RECOVERING
		case enrollment.PendingApproval:
			r.ServiceState = clientv0.ServiceState_SERVICE_STATE_NEEDS_APPROVAL
			r.ControlState = clientv0.ControlState_CONTROL_STATE_PENDING_APPROVAL
		case enrollment.Registered:
			r.ServiceState = clientv0.ServiceState_SERVICE_STATE_DISCONNECTED
			r.ControlState = clientv0.ControlState_CONTROL_STATE_REGISTERED
			r.AccountId = a.AccountID
			r.NodeId = a.NodeID
			r.Network = &clientv0.Network{Id: a.NetworkID}
		}
		if a.Uncertain {
			r.ServiceState = clientv0.ServiceState_SERVICE_STATE_RECOVERING
			r.ControlState = clientv0.ControlState_CONTROL_STATE_RECOVERING
			if !c.config.Now().Before(a.RecoveryUntil) {
				r.ServiceState = clientv0.ServiceState_SERVICE_STATE_RECOVERY_BLOCKED
				r.ControlState = clientv0.ControlState_CONTROL_STATE_RECOVERY_BLOCKED
			}
		}
	}
	c.projectConnection(r, profile)
	return &clientv0.GetStatusResponse{Status: r}, nil
}
