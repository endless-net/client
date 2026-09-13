package main

import (
	"context"
	"errors"
	"os"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
)

// Native enrollment invokes the typed workflow directly, without CLI arguments
// or v2 DTOs. The coordinator owns every config save and the operation lifecycle.
func agentRPCEnroll(ctx context.Context, cfg client.Config, input client.ClientRPCEnrollmentInput, save func(client.Config) error) (*ipc.UserAction, error) {
	hostname := input.Hostname
	if cfg.PendingDirectRegistration != nil {
		hostname = cfg.PendingDirectRegistration.Request.Hostname
	} else if cfg.EnrollmentRequest != nil {
		hostname = cfg.EnrollmentRequest.Hostname
	}
	if hostname == "" {
		var err error
		hostname, err = os.Hostname()
		if err != nil {
			return nil, err
		}
	}
	mode := map[ipc.EnrollmentMode]string{
		ipc.EnrollmentMode_ENROLLMENT_MODE_WORKSTATION:   "workstation",
		ipc.EnrollmentMode_ENROLLMENT_MODE_SERVER:        "server",
		ipc.EnrollmentMode_ENROLLMENT_MODE_SUBNET_ROUTER: "subnet-router",
		ipc.EnrollmentMode_ENROLLMENT_MODE_INTERACTIVE:   "interactive",
	}[input.Mode]
	if mode == "" || input.Browser == (input.Token != "") {
		return nil, errors.New("invalid native enrollment input")
	}
	var action *ipc.UserAction
	err := enrollConfiguredClient(ctx, cfg, clientEnrollmentOptions{
		Save: save, JoinToken: input.Token, IdempotencyKey: input.OperationID,
		Hostname: hostname, HostnameExplicit: input.Hostname != "", Network: defaultNetworkName, Tags: []string{"mode:" + mode},
		ApprovalNotice: func(notice enrollmentApprovalRequiredError) error {
			action = &ipc.UserAction{Kind: ipc.UserAction_KIND_OPEN_BROWSER, BrowserUrl: notice.ApprovalURL, ReasonKey: "approve_enrollment"}
			return nil
		},
		Report: func(response clientapi.RegisterNodeResponse) {
			if response.Node.ApprovalState == clientapi.NodeApprovalPending {
				action = &ipc.UserAction{Kind: ipc.UserAction_KIND_WAIT_FOR_APPROVAL, ReasonKey: "node_approval_pending"}
			}
		},
	})
	var approval enrollmentApprovalRequiredError
	if errors.As(err, &approval) {
		return &ipc.UserAction{Kind: ipc.UserAction_KIND_OPEN_BROWSER, BrowserUrl: approval.ApprovalURL, ReasonKey: "approve_enrollment"}, nil
	}
	return action, err
}
