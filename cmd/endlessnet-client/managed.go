package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/endless-net/client/internal/client"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
)

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

func waitForBrowserEnrollmentApproval(ctx context.Context, api *clientapi.API, cfg *client.Config, save func(client.Config) error, req *clientapi.RegisterNodeRequest, timeout time.Duration, notice func(enrollmentApprovalRequiredError) error) (clientapi.RegisterNodeResponse, error) {
	if err := ctx.Err(); err != nil {
		return clientapi.RegisterNodeResponse{}, err
	}
	if save == nil {
		return clientapi.RegisterNodeResponse{}, errors.New("enrollment persistence is required")
	}
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
		if err := save(*cfg); err != nil {
			return clientapi.RegisterNodeResponse{}, err
		}
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
				if err := clearBrowserEnrollmentRequest(cfg, save); err != nil {
					return clientapi.RegisterNodeResponse{}, err
				}
				return waitForBrowserEnrollmentApproval(ctx, api, cfg, save, req, timeout, notice)
			}
			if ctx.Err() != nil {
				return clientapi.RegisterNodeResponse{}, ctx.Err()
			}
			if timeout == 0 {
				if notice != nil {
					if err := notice(enrollmentApprovalRequiredError{RequestID: requestID, ApprovalURL: approvalURL}); err != nil {
						return clientapi.RegisterNodeResponse{}, err
					}
				}
				return clientapi.RegisterNodeResponse{}, enrollmentApprovalRequiredError{RequestID: requestID, ApprovalURL: approvalURL}
			}
			return clientapi.RegisterNodeResponse{}, err
		}
		if currentApprovalURL := strings.TrimSpace(status.Request.ApprovalURL); currentApprovalURL != "" {
			approvalURL = currentApprovalURL
			cfg.ApprovalURL = approvalURL
			if err := save(*cfg); err != nil {
				return clientapi.RegisterNodeResponse{}, err
			}
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
			if err := clearBrowserEnrollmentRequest(cfg, save); err != nil {
				return clientapi.RegisterNodeResponse{}, err
			}
			return clientapi.RegisterNodeResponse{}, errors.New("enrollment request was rejected")
		case clientapi.NodeEnrollmentRequestExpired:
			// Expiry ends the old operation. Reusing its idempotency ID would
			// replay the expired request even after clearing local poll state.
			replacement := *req
			replacement.IdempotencyID, err = clientapi.NewRegistrationIdempotencyID()
			if err != nil {
				return clientapi.RegisterNodeResponse{}, err
			}
			replacement.IdentitySignature, err = client.SignIdentity(cfg.IdentityPrivateKey, clientapi.RegistrationIdentityProofPayload(replacement))
			if err != nil {
				return clientapi.RegisterNodeResponse{}, err
			}
			if err := clearBrowserEnrollmentRequest(cfg, save); err != nil {
				return clientapi.RegisterNodeResponse{}, err
			}
			*req = replacement
			return waitForBrowserEnrollmentApproval(ctx, api, cfg, save, req, timeout, notice)
		}
	}
	if notice != nil {
		if err := notice(enrollmentApprovalRequiredError{RequestID: requestID, ApprovalURL: approvalURL}); err != nil {
			return clientapi.RegisterNodeResponse{}, err
		}
	}
	if timeout == 0 {
		return clientapi.RegisterNodeResponse{}, enrollmentApprovalRequiredError{
			RequestID:   requestID,
			ApprovalURL: approvalURL,
		}
	}
	deadline := time.Now().Add(timeout)
	for {
		if err := ctx.Err(); err != nil {
			return clientapi.RegisterNodeResponse{}, err
		}
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
			timer := time.NewTimer(sleep)
			select {
			case <-ctx.Done():
				timer.Stop()
				return clientapi.RegisterNodeResponse{}, ctx.Err()
			case <-timer.C:
			}
		}
	}
}

func clearBrowserEnrollmentRequest(cfg *client.Config, save func(client.Config) error) error {
	cfg.EnrollmentRequestID = ""
	cfg.EnrollmentPollToken = ""
	cfg.ApprovalURL = ""
	cfg.EnrollmentRequest = nil
	cfg.PendingDirectRegistration = nil
	cfg.NodeApprovalState = ""
	return save(*cfg)
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
