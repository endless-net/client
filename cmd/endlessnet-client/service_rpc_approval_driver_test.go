package main

import (
	"strings"
	"testing"

	"connectrpc.com/connect"
	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
)

func TestNativeProfileDriverRejectsRestrictedEnrollmentBeforeApply(t *testing.T) {
	for _, source := range []string{"configuration", "verified-map", "configuration-without-map"} {
		for _, approval := range []string{api.NodeApprovalPending, api.NodeApprovalRejected} {
			t.Run(source+"/"+approval, func(t *testing.T) {
				fixture := newRecoveryTestFixture(t, "https://control.example.test")
				cfg, err := client.LoadConfig(fixture.ConfigPath)
				if err != nil {
					t.Fatal(err)
				}
				cfg.EnrollmentRecovery = nil
				cfg.MapSigningTrust = testSigningTrustBundle(t, testMapSigningPublicKey(t, cfg.CachedMap.MapSignature))
				if source == "verified-map" {
					cfg.CachedMap.Node.ApprovalState = approval
					cfg.CachedMap.MapSignature, err = api.SignNetworkMap(fixture.OldSigningKey, *cfg.CachedMap)
					if err != nil {
						t.Fatal(err)
					}
				} else {
					cfg.NodeApprovalState = " " + strings.ToUpper(approval) + " "
					if source == "configuration-without-map" {
						cfg.CachedMap = nil
					}
				}
				wg := &testAgentWireGuard{}
				wake := make(chan struct{}, 1)
				err = agentRPCProfileDriver(agentIPCOptions{WireGuard: wg, SyncWake: wake}).Start(t.Context(), cfg)
				want, transport := ipc.ErrorCode_ERROR_CODE_APPROVAL_REQUIRED, connect.CodeFailedPrecondition
				if approval == api.NodeApprovalRejected {
					want, transport = ipc.ErrorCode_ERROR_CODE_APPROVAL_REJECTED, connect.CodePermissionDenied
				}
				if rpc.FailureFromError(err).GetCode() != want || connect.CodeOf(err) != transport || wg.configureCalls != 0 || len(wake) != 0 {
					t.Fatal("restricted enrollment reached native apply or lost approval failure", err)
				}
			})
		}
	}
}
