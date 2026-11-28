package control

import (
	"bytes"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/state"
	contract "github.com/endless-net/coordinator/api/enrollment"
	wire "github.com/endless-net/coordinator/api/publicquery/v1"
	"github.com/endless-net/coordinator/api/publicquery/v1/publicqueryv1connect"
	sessionv1 "github.com/endless-net/identity/api/session/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRevocationTransportRequiresCurrentRevisionAndNeverReplaysMutation(t *testing.T) {
	for _, mode := range []string{"valid", "missing-revision", "unknown-field", "foreign-node", "lost-answer", "false-enforced", "expired-session", "renewed-session"} {
		t.Run(mode, func(t *testing.T) {
			f := newRegistrationFixture(t)
			f.p.Attempt.Phase = enrollment.Registered
			f.p.Attempt.NodeID = "available"
			f.p.Secrets.UserID = "human"
			access := f.p.Secrets.Access
			if mode == "renewed-session" {
				f.p.Secrets.SessionID = "initial-session"
				f.p.Secrets.SessionExpiresAt = f.now.Add(-time.Second)
				access = strings.Repeat("n", 43)
				f.p.ActiveSession = renewedRevocationSession(t, f.p, f.now, access)
			}
			req := contract.RevokeNodeRequest{AccountID: "account", NetworkID: "network", NodeID: "lost", CommandID: "c086b75d-a2a6-4d4e-9bb8-6ce3bc0b674e", ExpectedRevision: 7}
			command := state.RemoteRevocation{CallerSID: "owner", UserID: "human", ControlOrigin: f.p.ControlOrigin, CreatedAt: f.now, Request: req}
			f.p.RemoteRevocations = map[string]state.RemoteRevocation{req.CommandID: command}
			calls := 0
			r, err := NewRevocations(&http.Client{Transport: transport(func(request *http.Request) (*http.Response, error) {
				calls++
				if _, ok := request.Context().Deadline(); !ok || request.Header.Get("Authorization") != "Bearer "+access || request.Header.Get("Cookie") != "" || request.Header.Get("X-EndlessNet-Node-Credential") != "" || request.Header.Get("Idempotency-Key") != "" || request.Header.Get("X-Idempotency-Key") != "" {
					t.Fatal("wrong credentials or replayable request")
				}
				if request.URL.Path == publicqueryv1connect.NodeManagementServiceListNodesProcedure {
					input := &wire.ListNodesRequest{}
					readRPC(t, request, input)
					if input.NetworkId != req.NetworkID {
						t.Fatal("list changed the network selector")
					}
					result := &wire.ListNodesResponse{NetworkRevision: 7, Nodes: []*wire.MapNode{{Id: "lost", NetworkId: "network", Hostname: "same-name", Status: "offline"}}}
					switch mode {
					case "foreign-node":
						result.Nodes[0].NetworkId = "foreign"
					case "missing-revision":
						result.NetworkRevision = 0
					case "unknown-field":
						result.ProtoReflect().SetUnknown([]byte{0x78, 0x01})
					}
					return rpcResponse(t, request, result), nil
				}
				input := &wire.RevokeNodeRequest{}
				body := readRPC(t, request, input)
				actual := contract.RevokeNodeRequest{AccountID: input.AccountId, NetworkID: input.NetworkId, NodeID: input.NodeId,
					CommandID: input.CommandId, ExpectedRevision: input.ExpectedRevision}
				if request.URL.Path != publicqueryv1connect.NodeManagementServiceRevokeNodeProcedure || actual != req ||
					bytes.Contains(body, []byte(access)) {
					t.Fatal("command changed or exported a secret")
				}
				if mode == "lost-answer" {
					return nil, errors.New("private transport diagnostic")
				}
				receipt := &wire.NodeRevocationReceipt{AccountId: "account", NetworkId: "network", NodeId: "lost", CommandId: req.CommandID,
					Status: "accepted", Revision: 8, AcceptedAt: timestamppb.New(f.now), EnforceBy: timestamppb.New(f.now.Add(300 * time.Second))}
				if mode == "false-enforced" {
					receipt.Status = "enforced"
				}
				return rpcResponse(t, request, &wire.RevokeNodeResponse{Receipt: receipt}), nil
			})}, staticAnnouncement{f.announcement}, RevocationConfig{MaxBytes: 65536, MaxTargets: 10, Now: func() time.Time { return f.now }})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "expired-session" {
				f.p.Secrets.SessionExpiresAt = f.now
			}
			targets, err := r.List(t.Context(), f.p)
			if mode == "missing-revision" || mode == "unknown-field" || mode == "foreign-node" || mode == "expired-session" {
				if err == nil || targets != nil {
					t.Fatal("unverified target/current revision accepted")
				}
				return
			}
			if err != nil || len(targets) != 1 || targets[0].NetworkRevision != 7 || targets[0].Status != "offline" {
				t.Fatal("current target missing", err)
			}
			before := calls
			result, err := r.Revoke(t.Context(), f.p, command)
			if calls != before+1 {
				t.Fatal("hidden mutation replay")
			}
			if mode == "lost-answer" || mode == "false-enforced" {
				if err == nil || result.CommandID != "" {
					t.Fatal("unknown/false enforcement became success")
				}
			} else if err != nil || result.Match(req) != nil {
				t.Fatal("original accepted receipt rejected", err)
			}
		})
	}
}

func renewedRevocationSession(t *testing.T, p state.Profile, now time.Time, access string) *state.ActiveSession {
	t.Helper()
	const requestID = "00000000-0000-4000-8000-000000000001"
	raw, err := proto.Marshal(&sessionv1.SessionRenewal{
		OperationId: "renewal-operation", RequestId: requestID, PreviousSessionId: p.Secrets.SessionID,
		State: sessionv1.SessionRenewalState_SESSION_RENEWAL_STATE_SUCCEEDED, ReplayExpiresAt: timestamppb.New(now.Add(time.Minute)),
		Outcome: &sessionv1.SessionRenewal_Result{Result: &sessionv1.SessionRenewalResult{
			Session: &sessionv1.UserSession{
				SessionId: "rotated-session", UserId: p.Secrets.UserID, State: sessionv1.UserSessionState_USER_SESSION_STATE_ACTIVE,
				ExpiresAt: timestamppb.New(now.Add(time.Hour)), RenewalSupported: true,
			},
			AccessBearer: access,
			RenewalAuthorization: &sessionv1.SessionRenewalAuthorization{
				Bearer: strings.Repeat("g", 43), ExpiresAt: timestamppb.New(now.Add(24 * time.Hour)),
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return &state.ActiveSession{
		RequestID: requestID, PreviousSessionID: p.Secrets.SessionID,
		PreviousRenewalSHA256: state.CredentialDigest(p.Secrets.Renewal), Operation: raw,
	}
}
