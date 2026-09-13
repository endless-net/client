package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"

	"connectrpc.com/connect"
	clientapi "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/clientipc/rpc"
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
	var retryableTransport bool
	err := enrollConfiguredClient(ctx, cfg, clientEnrollmentOptions{
		RequestOutcome: func(status int, err error) {
			retryableTransport = retryableEnrollmentHTTPStatus(status) || retryableRPCEnrollmentError(err)
		},
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
	if err != nil && (retryableTransport || retryableRPCEnrollmentError(err)) {
		return nil, rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	}
	return action, err
}

func retryableEnrollmentHTTPStatus(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// Retry transport outages and explicitly temporary HTTP responses, not malformed
// registration, rejected identity, invalid signatures or authorization failures.
func retryableRPCEnrollmentError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var network *net.OpError
	if errors.As(err, &network) {
		return true
	}
	for _, status := range []int{http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
		if clientapi.IsControlPlaneStatus(err, status) {
			return true
		}
	}
	return false
}
