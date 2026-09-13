package main

import (
	"context"
	"errors"
	"strings"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/internal/client"
)

// verifiedEnrollmentRecovery is a private provider result, not an IPC payload.
// Only the operation executor may commit it after rechecking local authority.
type verifiedEnrollmentRecovery struct {
	Progress        recoveryAttemptResult
	NetworkMap      *clientapi.RegisterNodeResponse
	CredentialTrust *clientapi.SigningTrustBundle
}

// inspectEnrollmentRecovery performs typed network recovery and cryptographic
// verification without reading/writing files or mutating the supplied config.
func inspectEnrollmentRecovery(ctx context.Context, cfg client.Config) (verifiedEnrollmentRecovery, error) {
	if err := ctx.Err(); err != nil {
		return verifiedEnrollmentRecovery{}, err
	}
	if cfg.EnrollmentRecovery == nil {
		return verifiedEnrollmentRecovery{}, errors.New("recovery plan is required")
	}
	recovery := *cfg.EnrollmentRecovery
	failure := func(phase client.RecoveryPhase, code, requestID string, retryable bool, err error) (verifiedEnrollmentRecovery, error) {
		return verifiedEnrollmentRecovery{Progress: recoveryAttemptResult{OperationID: recovery.OperationID, Phase: phase, ErrorCode: code, RequestID: requestID, Retryable: retryable}}, err
	}
	localFailure := func(err error) (verifiedEnrollmentRecovery, error) {
		return failure(client.RecoveryPhaseBlocked, recoveryErrorLocalValidation, "", false, err)
	}
	if err := recovery.Validate(); err != nil {
		return localFailure(err)
	}
	if err := validateMapSigningEnrollmentURLs(cfg); err != nil {
		return localFailure(err)
	}
	for _, origin := range cfg.ControlURLs() {
		if origin != recovery.ConfirmedControlOrigin {
			return localFailure(errors.New("recovery control origin changed"))
		}
	}
	trust, err := client.SigningTrustBundle(cfg)
	if err != nil {
		return localFailure(err)
	}
	if trust.ActiveKeyID != recovery.ConfirmedKeyID {
		return localFailure(errors.New("recovery signing identity changed"))
	}
	req, err := credentialRenewalRequest(cfg, recovery)
	if err != nil {
		return localFailure(err)
	}
	api := apiFromConfig(cfg)
	api.HTTPClient.Transport = enrollmentContextTransport{lifetime: ctx, base: api.HTTPClient.Transport}
	control := registerNodeRecovery(ctx, api, req)
	if err := ctx.Err(); err != nil {
		return verifiedEnrollmentRecovery{}, err
	}
	if control.PublicError != nil {
		publicError := *control.PublicError
		if publicError.ErrorCode.RequiresReEnrollment() {
			return verifiedEnrollmentRecovery{Progress: recoveryAttemptResult{OperationID: recovery.OperationID, ErrorCode: string(publicError.ErrorCode), RequestID: publicError.RequestID, Completed: true, Terminal: true}}, nil
		}
		phase, retryable := recoveryPhaseForPublicError(publicError.ErrorCode)
		return failure(phase, string(publicError.ErrorCode), publicError.RequestID, retryable, nil)
	}
	if control.Response == nil {
		phase := client.RecoveryPhaseBlocked
		if control.Retryable {
			phase = client.RecoveryPhaseRecovering
		}
		code := strings.TrimSpace(control.FailureCode)
		if code == "" {
			code = recoveryErrorProtocol
		}
		return failure(phase, code, "", control.Retryable, control.Err)
	}
	response := *control.Response
	if err := verifyNetworkMap(&cfg, response); err != nil {
		return localFailure(err)
	}
	credentialTrust, err := verifyRegistrationNodeCredential(api, response, response.NodeCredential)
	if err != nil {
		if ctx.Err() != nil {
			return verifiedEnrollmentRecovery{}, ctx.Err()
		}
		return localFailure(err)
	}
	if err := ctx.Err(); err != nil {
		return verifiedEnrollmentRecovery{}, err
	}
	if cfg.MapRevision != 0 && response.Network.Revision < cfg.MapRevision {
		return localFailure(errors.New("recovery network map is stale"))
	}
	return verifiedEnrollmentRecovery{Progress: recoveryAttemptResult{OperationID: recovery.OperationID, Completed: true}, NetworkMap: &response, CredentialTrust: credentialTrust}, nil
}
