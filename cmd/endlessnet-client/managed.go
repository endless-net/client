package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/endless-net/client/internal/client"
	ipc "github.com/endless-net/client/ipc/v1"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
)

func cmdManagedUp(args []string) error {
	fs := flag.NewFlagSet("up", flag.ContinueOnError)
	ipcPipe, ipcSocket := serviceIPCTransportFlags(fs)
	serverURL := fs.String("server", defaultPublicServerURL, "EndlessNet server URL")
	joinToken := fs.String("join-token", "", "one-time node join token")
	joinTokenFile := fs.String("join-token-file", "", "read one-time node join token from this file, or '-' for stdin")
	mode := fs.String("mode", "server", "device mode: workstation, server, or subnet-router")
	hostname := fs.String("hostname", mustHostname(), "hostname to register for this device")
	idempotencyKey := fs.String("idempotency-key", "", "registration retry idempotency key")
	approvalTimeoutValue := fs.String("approval-timeout", "10m", "maximum time to wait for browser approval; 0 saves the request without waiting")
	if err := fs.Parse(args); err != nil {
		return err
	}
	approvalTimeout, err := parseOptionalDuration("approval-timeout", *approvalTimeoutValue)
	if err != nil {
		return err
	}
	effectiveJoinToken, err := secretFlagValue("join-token", *joinToken, *joinTokenFile)
	if err != nil {
		return err
	}

	ctx := context.Background()
	cancel := func() {}
	if approvalTimeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, approvalTimeout)
	}
	defer cancel()
	return runManagedUp(
		ctx,
		newServiceIPCClientForLocalTransport(*ipcPipe, *ipcSocket),
		effectiveJoinToken,
		*serverURL,
		*mode,
		*hostname,
		*idempotencyKey,
		approvalTimeout > 0,
		managedEnrollmentPollInterval,
	)
}

func runManagedUp(ctx context.Context, ipcClient *ipc.Client, token, serverURL, mode, hostname, idempotencyKey string, waitForApproval bool, pollInterval time.Duration) error {
	if pollInterval <= 0 {
		pollInterval = managedEnrollmentPollInterval
	}
	printedApprovalURL := ""
	for {
		requestCtx, cancel := context.WithTimeout(ctx, managedServiceIPCStartupTimeout)
		payload, err := serviceEnrollViaIPCWithRetry(requestCtx, ipcClient, token, serverURL, mode, hostname, idempotencyKey)
		cancel()
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return managedEnrollmentContextError(ctxErr, printedApprovalURL)
			}
			return fmt.Errorf("contact EndlessNet system service: %w", err)
		}

		state := payload.State
		if state == ipc.StateNeedsApproval || payload.ControlState == ipc.ControlStatePendingApproval {
			approvalURL := strings.TrimSpace(payload.ApprovalURL)
			if approvalURL != "" && approvalURL != printedApprovalURL {
				fmt.Printf("Open this URL to approve the device:\n%s\n", approvalURL)
				printedApprovalURL = approvalURL
			}
			if !waitForApproval {
				fmt.Println("Enrollment request saved; run endlessnet up after approving the device.")
				return nil
			}
			select {
			case <-ctx.Done():
				return managedEnrollmentContextError(ctx.Err(), printedApprovalURL)
			case <-time.After(pollInterval):
				continue
			}
		}

		if payload.WireGuardApply == nil {
			if state == "" {
				state = ipc.ServiceState("unknown")
			}
			return fmt.Errorf("EndlessNet system service returned state %s without a WireGuard apply result", state)
		}
		if !payload.WireGuardApply.OK {
			return errors.New("EndlessNet system service returned an unsuccessful WireGuard apply result")
		}
		fmt.Println("EndlessNet connected.")
		if overlayIP := strings.TrimSpace(payload.OverlayIP); overlayIP != "" {
			fmt.Printf("IP: %s\n", overlayIP)
		}
		printManagedUpHealthWarning(payload.ControlState)
		return nil
	}
}

func printManagedUpHealthWarning(controlState ipc.ControlState) {
	switch controlState {
	case ipc.ControlStateReady, ipc.ControlStateRegistered:
		return
	}
	state := strings.TrimSpace(string(controlState))
	if state == "" {
		state = "unknown"
	}
	fmt.Printf("Warning: the EndlessNet tunnel is running, but service/control health is %s.\n", state)
}

func managedEnrollmentContextError(err error, approvalURL string) error {
	if errors.Is(err, context.DeadlineExceeded) {
		if strings.TrimSpace(approvalURL) != "" {
			return fmt.Errorf("device approval timed out; open %s and run endlessnet up again", approvalURL)
		}
		return errors.New("timed out waiting for the EndlessNet system service")
	}
	return err
}

type enrollmentApprovalRequiredError struct {
	RequestID   string
	ApprovalURL string
}

func (e enrollmentApprovalRequiredError) Error() string {
	if strings.TrimSpace(e.ApprovalURL) == "" {
		return "browser approval is required"
	}
	return "browser approval is required: " + e.ApprovalURL
}

func waitForBrowserEnrollmentApproval(api *clientapi.API, cfg *client.Config, configPath string, req *clientapi.RegisterNodeRequest, timeout time.Duration) (clientapi.RegisterNodeResponse, error) {
	requestID := strings.TrimSpace(cfg.EnrollmentRequestID)
	pollToken := strings.TrimSpace(cfg.EnrollmentPollToken)
	approvalURL := strings.TrimSpace(cfg.ApprovalURL)
	if requestID == "" || pollToken == "" {
		created, err := api.CreateNodeEnrollmentRequest(*req)
		if err != nil {
			return clientapi.RegisterNodeResponse{}, err
		}
		requestID = strings.TrimSpace(created.Request.ID)
		pollToken = strings.TrimSpace(created.PollToken)
		approvalURL = strings.TrimSpace(created.Request.ApprovalURL)
		if requestID == "" || pollToken == "" {
			return clientapi.RegisterNodeResponse{}, errors.New("enrollment request response is missing id or poll token")
		}
		cfg.EnrollmentRequestID = requestID
		cfg.EnrollmentPollToken = pollToken
		cfg.ApprovalURL = approvalURL
		savedRequest := *req
		cfg.EnrollmentRequest = &savedRequest
		cfg.NodeApprovalState = clientapi.NodeEnrollmentRequestPending
		if err := client.SaveConfig(configPath, *cfg); err != nil {
			return clientapi.RegisterNodeResponse{}, err
		}
		fmt.Printf("Open this URL to approve the device:\n%s\n", created.Request.ApprovalURL)
	} else {
		if cfg.EnrollmentRequest != nil {
			savedRequest, err := reusableBrowserEnrollmentRequest(*req, *cfg.EnrollmentRequest)
			if err != nil {
				return clientapi.RegisterNodeResponse{}, err
			}
			*req = savedRequest
		}
		status, err := api.NodeEnrollmentRequestStatus(requestID, pollToken)
		if err != nil {
			if clientapi.IsControlPlaneStatus(err, http.StatusNotFound) {
				if err := clearBrowserEnrollmentRequest(cfg, configPath); err != nil {
					return clientapi.RegisterNodeResponse{}, err
				}
				return waitForBrowserEnrollmentApproval(api, cfg, configPath, req, timeout)
			}
			if timeout == 0 {
				return clientapi.RegisterNodeResponse{}, enrollmentApprovalRequiredError{RequestID: requestID, ApprovalURL: approvalURL}
			}
			return clientapi.RegisterNodeResponse{}, err
		}
		if currentApprovalURL := strings.TrimSpace(status.Request.ApprovalURL); currentApprovalURL != "" {
			approvalURL = currentApprovalURL
			cfg.ApprovalURL = approvalURL
			if err := client.SaveConfig(configPath, *cfg); err != nil {
				return clientapi.RegisterNodeResponse{}, err
			}
			fmt.Printf("Open this URL to approve the device:\n%s\n", approvalURL)
		}
		switch status.Request.Status {
		case clientapi.NodeEnrollmentRequestApproved, clientapi.NodeEnrollmentRequestEnrolled:
			completed, err := api.CompleteNodeEnrollmentRequest(requestID, pollToken)
			if err != nil {
				return clientapi.RegisterNodeResponse{}, err
			}
			if completed.Registration == nil {
				return clientapi.RegisterNodeResponse{}, errors.New("completed enrollment response is missing registration")
			}
			return *completed.Registration, nil
		case clientapi.NodeEnrollmentRequestRejected:
			if err := clearBrowserEnrollmentRequest(cfg, configPath); err != nil {
				return clientapi.RegisterNodeResponse{}, err
			}
			return clientapi.RegisterNodeResponse{}, errors.New("enrollment request was rejected")
		case clientapi.NodeEnrollmentRequestExpired:
			if err := clearBrowserEnrollmentRequest(cfg, configPath); err != nil {
				return clientapi.RegisterNodeResponse{}, err
			}
			return waitForBrowserEnrollmentApproval(api, cfg, configPath, req, timeout)
		}
	}
	if timeout == 0 {
		fmt.Println("Enrollment request saved; rerun the command after approving the device.")
		return clientapi.RegisterNodeResponse{}, enrollmentApprovalRequiredError{
			RequestID:   requestID,
			ApprovalURL: approvalURL,
		}
	}
	deadline := time.Now().Add(timeout)
	for {
		status, err := api.NodeEnrollmentRequestStatus(requestID, pollToken)
		if err != nil {
			return clientapi.RegisterNodeResponse{}, err
		}
		switch status.Request.Status {
		case clientapi.NodeEnrollmentRequestApproved, clientapi.NodeEnrollmentRequestEnrolled:
			completed, err := api.CompleteNodeEnrollmentRequest(requestID, pollToken)
			if err != nil {
				return clientapi.RegisterNodeResponse{}, err
			}
			if completed.Registration == nil {
				return clientapi.RegisterNodeResponse{}, errors.New("completed enrollment response is missing registration")
			}
			return *completed.Registration, nil
		case clientapi.NodeEnrollmentRequestRejected:
			return clientapi.RegisterNodeResponse{}, errors.New("enrollment request was rejected")
		case clientapi.NodeEnrollmentRequestExpired:
			return clientapi.RegisterNodeResponse{}, errors.New("enrollment request expired")
		}
		if time.Now().After(deadline) {
			return clientapi.RegisterNodeResponse{}, fmt.Errorf("enrollment approval timed out; open %s and rerun the command after approving", status.Request.ApprovalURL)
		}
		sleep := time.Duration(status.PollAfterSeconds) * time.Second
		if sleep <= 0 || sleep > 30*time.Second {
			sleep = nodeEnrollmentPollDefaultInterval
		}
		if remaining := time.Until(deadline); remaining < sleep {
			sleep = remaining
		}
		if sleep > 0 {
			time.Sleep(sleep)
		}
	}
}

func clearBrowserEnrollmentRequest(cfg *client.Config, configPath string) error {
	cfg.EnrollmentRequestID = ""
	cfg.EnrollmentPollToken = ""
	cfg.ApprovalURL = ""
	cfg.EnrollmentRequest = nil
	cfg.NodeApprovalState = ""
	return client.SaveConfig(configPath, *cfg)
}

func reusableBrowserEnrollmentRequest(current, saved clientapi.RegisterNodeRequest) (clientapi.RegisterNodeRequest, error) {
	if strings.TrimSpace(saved.JoinToken) != "" || strings.TrimSpace(saved.NodeCredential) != "" || strings.TrimSpace(saved.SessionTokenBinding) != "" {
		return clientapi.RegisterNodeRequest{}, errors.New("saved browser enrollment request contains unexpected authorization data")
	}
	if strings.TrimSpace(saved.IdentityPublicKey) != strings.TrimSpace(current.IdentityPublicKey) ||
		strings.TrimSpace(saved.PublicKey) != strings.TrimSpace(current.PublicKey) ||
		strings.TrimSpace(saved.DeviceFingerprint) != strings.TrimSpace(current.DeviceFingerprint) {
		return clientapi.RegisterNodeRequest{}, errors.New("saved browser enrollment request does not match this device identity")
	}
	if err := clientapi.VerifyRegisterNodeIdentityProof(saved); err != nil {
		return clientapi.RegisterNodeRequest{}, fmt.Errorf("verify saved browser enrollment request: %w", err)
	}
	return saved, nil
}
