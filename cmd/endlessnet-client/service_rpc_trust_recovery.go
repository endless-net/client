package main

import (
	"context"

	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
)

func agentRPCTrustRecovery(ctx context.Context, cfg client.Config) (client.ClientRPCTrustRecoveryResult, error) {
	verified, err := inspectEnrollmentRecovery(ctx, cfg)
	if ctx.Err() != nil {
		return client.ClientRPCTrustRecoveryResult{}, ctx.Err()
	}
	progress := verified.Progress
	if err == nil && progress.Completed && !progress.Terminal && verified.NetworkMap != nil {
		if err := cacheNetworkMapChecked(&cfg, *verified.NetworkMap); err != nil {
			return client.ClientRPCTrustRecoveryResult{}, err
		}
		cfg.NodeCredential = verified.NetworkMap.NodeCredential
		cfg.NodeCredentialSigningTrust = verified.CredentialTrust
		return client.ClientRPCTrustRecoveryResult{Configuration: &cfg}, nil
	}
	code := ipc.ErrorCode_ERROR_CODE_APPLY_FAILED
	switch {
	case progress.Terminal:
		code = ipc.ErrorCode_ERROR_CODE_NEEDS_ENROLLMENT
	case progress.Retryable:
		code = ipc.ErrorCode_ERROR_CODE_UNAVAILABLE
	case progress.Phase == client.RecoveryPhaseNeedsLogin:
		code = ipc.ErrorCode_ERROR_CODE_NEEDS_LOGIN
	case progress.Phase == client.RecoveryPhasePolicyBlocked:
		code = ipc.ErrorCode_ERROR_CODE_POLICY_BLOCKED
	}
	phase := progress.Phase
	if phase == "" {
		phase = client.RecoveryPhaseBlocked
	}
	reason := progress.ErrorCode
	if reason == "" {
		reason = recoveryErrorLocalValidation
	}
	return client.ClientRPCTrustRecoveryResult{Failure: &ipc.Failure{Code: code, ReasonKey: reason, Retryable: progress.Retryable, ControlRequestId: progress.RequestID}, Phase: phase, RequiresEnrollment: progress.Terminal}, nil
}
