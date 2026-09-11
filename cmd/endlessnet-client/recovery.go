package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
	wgkeys "github.com/endless-net/client-api/clientapi/wireguard"
	"github.com/endless-net/client/internal/client"
)

const (
	recoveryErrorProtocol        = "recovery_protocol_error"
	recoveryErrorLocalValidation = "recovery_local_validation_failed"
	recoveryErrorStateConflict   = "recovery_state_conflict"
	controlRequestIDHeader       = "X-Request-ID"
	nodeCredentialHeader         = "X-EndlessNet-Node-Credential"
)

type recoveryAttemptResult struct {
	OperationID string
	Phase       client.RecoveryPhase
	ErrorCode   string
	RequestID   string
	Retryable   bool
	Completed   bool
	Terminal    bool
}

type recoveryControlResult struct {
	Response    *clientapi.RegisterNodeResponse
	PublicError *clientapi.PublicError
	FailureCode string
	Retryable   bool
	Err         error
}

type remoteCleanupError struct {
	RequestID string
	cause     error
}

func (e remoteCleanupError) Error() string { return "remote cleanup was not confirmed" }

func (e remoteCleanupError) Unwrap() error { return e.cause }

func continueEnrollmentRecovery(ctx context.Context, configPath string) (recoveryAttemptResult, error) {
	store, err := client.OpenConfigStore(configPath)
	if err != nil {
		return recoveryAttemptResult{}, err
	}
	cfg := store.Read()
	if cfg.EnrollmentRecovery == nil {
		return recoveryAttemptResult{Completed: true}, nil
	}
	recovery := *cfg.EnrollmentRecovery
	if err := recovery.Validate(); err != nil {
		return persistRecoveryFailure(store, recovery, client.RecoveryPhaseBlocked, recoveryErrorLocalValidation, "", false, err)
	}
	req, err := credentialRenewalRequest(cfg, recovery)
	if err != nil {
		return persistRecoveryFailure(store, recovery, client.RecoveryPhaseBlocked, recoveryErrorLocalValidation, "", false, err)
	}
	control := registerNodeRecovery(ctx, apiFromConfig(cfg), req)
	if control.PublicError != nil {
		publicError := *control.PublicError
		if publicError.ErrorCode.RequiresReEnrollment() {
			if err := store.Update(func(current *client.Config) error {
				if err := requireSameRecovery(current, recovery, cfg.NodeCredential); err != nil {
					return err
				}
				return client.ApplyTerminalRecoveryCleanup(current)
			}); err != nil {
				return recoveryAttemptResult{}, err
			}
			return recoveryAttemptResult{
				OperationID: recovery.OperationID,
				ErrorCode:   string(publicError.ErrorCode),
				RequestID:   publicError.RequestID,
				Completed:   true,
				Terminal:    true,
			}, nil
		}
		phase, retryable := recoveryPhaseForPublicError(publicError.ErrorCode)
		return persistRecoveryFailure(store, recovery, phase, string(publicError.ErrorCode), publicError.RequestID, retryable, nil)
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
		return persistRecoveryFailure(store, recovery, phase, code, "", control.Retryable, control.Err)
	}

	response := *control.Response
	networkMap := response
	if err := verifyNetworkMap(&cfg, networkMap); err != nil {
		return persistRecoveryFailure(store, recovery, client.RecoveryPhaseBlocked, recoveryErrorLocalValidation, "", false, fmt.Errorf("verify recovery network map: %w", err))
	}
	if err := verifyRegistrationNodeCredential(apiFromConfig(cfg), networkMap, response.NodeCredential); err != nil {
		return persistRecoveryFailure(store, recovery, client.RecoveryPhaseBlocked, recoveryErrorLocalValidation, "", false, err)
	}
	if err := store.Update(func(current *client.Config) error {
		if err := requireSameRecovery(current, recovery, cfg.NodeCredential); err != nil {
			return err
		}
		if err := cacheNetworkMapChecked(current, networkMap); err != nil {
			return err
		}
		current.NodeCredential = response.NodeCredential
		current.EnrollmentRequestID = ""
		current.EnrollmentPollToken = ""
		current.ApprovalURL = ""
		current.EnrollmentRequest = nil
		current.PendingDirectRegistration = nil
		current.EnrollmentRecovery = nil
		return nil
	}); err != nil {
		return recoveryAttemptResult{}, err
	}
	return recoveryAttemptResult{OperationID: recovery.OperationID, Completed: true}, nil
}

func credentialRenewalRequest(cfg client.Config, recovery client.EnrollmentRecovery) (clientapi.RegisterNodeRequest, error) {
	if cfg.CachedMap == nil {
		return clientapi.RegisterNodeRequest{}, errors.New("credential recovery requires the last validated network map")
	}
	if strings.TrimSpace(cfg.NodeCredential) == "" || strings.TrimSpace(cfg.NetworkID) == "" {
		return clientapi.RegisterNodeRequest{}, errors.New("credential recovery requires existing node enrollment")
	}
	publicKey, err := wgkeys.PublicKey(cfg.PrivateKey)
	if err != nil {
		return clientapi.RegisterNodeRequest{}, fmt.Errorf("derive WireGuard public key: %w", err)
	}
	identityPublicKey, err := client.IdentityPublicKey(cfg.IdentityPrivateKey)
	if err != nil {
		return clientapi.RegisterNodeRequest{}, fmt.Errorf("derive identity public key: %w", err)
	}
	node := cfg.CachedMap.Node
	if err := validateNodeIdentityBinding(node, publicKey, identityPublicKey, cfg.DeviceFingerprint); err != nil {
		return clientapi.RegisterNodeRequest{}, err
	}
	if strings.TrimSpace(cfg.CachedMap.RegistrationBinding) == "" {
		return clientapi.RegisterNodeRequest{}, errors.New("credential recovery requires the prior registration binding")
	}
	tags := append([]string(nil), node.RequestedTags...)
	if len(tags) == 0 {
		tags = append(tags, node.Tags...)
	}
	req := clientapi.RegisterNodeRequest{
		SchemaVersion:       clientapi.SchemaVersion,
		IdempotencyID:       recovery.IdempotencyID,
		NetworkID:           cfg.NetworkID,
		NodeCredential:      cfg.NodeCredential,
		RegistrationBinding: cfg.CachedMap.RegistrationBinding,
		Hostname:            node.Hostname,
		ClientVersion:       strings.TrimSpace(version),
		IdentityPublicKey:   identityPublicKey,
		PublicKey:           publicKey,
		DeviceFingerprint:   cfg.DeviceFingerprint,
		Endpoint:            node.Endpoint,
		EndpointGeneration:  node.EndpointGeneration,
		EndpointCandidates:  append([]string(nil), node.EndpointCandidates...),
		AdvertisedIPs:       append([]string(nil), node.AdvertisedIPs...),
		Tags:                tags,
	}
	req.IdentitySignature, err = client.SignIdentity(cfg.IdentityPrivateKey, clientapi.RegistrationIdentityProofPayload(req))
	if err != nil {
		return clientapi.RegisterNodeRequest{}, err
	}
	if err := req.Validate(); err != nil {
		return clientapi.RegisterNodeRequest{}, err
	}
	return req, nil
}

func registerNodeRecovery(ctx context.Context, api *clientapi.API, req clientapi.RegisterNodeRequest) recoveryControlResult {
	if api == nil || api.HTTPClient == nil {
		return recoveryControlResult{FailureCode: recoveryErrorLocalValidation, Err: errors.New("control-plane client is required")}
	}
	raw, err := clientapi.MarshalRegisterNodeRequest(req)
	if err != nil {
		return recoveryControlResult{FailureCode: recoveryErrorLocalValidation, Err: err}
	}
	baseURLs := clientapi.NormalizeControlPlaneURLs(append([]string{api.BaseURL}, api.BaseURLs...)...)
	if len(baseURLs) == 0 {
		return recoveryControlResult{FailureCode: recoveryErrorLocalValidation, Err: errors.New("control-plane URL is required")}
	}
	var lastUnavailable *clientapi.PublicError
	var lastTransport error
	for _, baseURL := range baseURLs {
		endpoint, err := recoveryControlEndpoint(baseURL)
		if err != nil {
			return recoveryControlResult{FailureCode: recoveryErrorLocalValidation, Err: err}
		}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
		if err != nil {
			return recoveryControlResult{FailureCode: recoveryErrorLocalValidation, Err: err}
		}
		httpReq.Header.Set("Accept", "application/json")
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set(nodeCredentialHeader, req.NodeCredential)
		resp, err := api.HTTPClient.Do(httpReq)
		if err != nil {
			lastTransport = err
			continue
		}
		result := decodeRegisterNodeRecoveryResponse(resp, req)
		_ = resp.Body.Close()
		if result.PublicError != nil && result.PublicError.ErrorCode == clientapi.ErrorCodeTemporarilyUnavailable {
			lastUnavailable = result.PublicError
			continue
		}
		return result
	}
	if lastUnavailable != nil {
		return recoveryControlResult{PublicError: lastUnavailable}
	}
	return recoveryControlResult{
		FailureCode: string(clientapi.ErrorCodeTemporarilyUnavailable),
		Retryable:   true,
		Err:         fmt.Errorf("credential recovery transport unavailable: %w", lastTransport),
	}
}

func revokeNode(ctx context.Context, api *clientapi.API, nodeID string) error {
	if api == nil || api.HTTPClient == nil {
		return remoteCleanupError{cause: errors.New("control-plane client is required")}
	}
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" || strings.ContainsAny(nodeID, `/\?#`) {
		return remoteCleanupError{cause: errors.New("node ID is required")}
	}
	baseURLs := clientapi.NormalizeControlPlaneURLs(append([]string{api.BaseURL}, api.BaseURLs...)...)
	var lastErr error
	for _, baseURL := range baseURLs {
		base, err := recoveryControlEndpoint(baseURL)
		if err != nil {
			return remoteCleanupError{cause: err}
		}
		parsed, _ := url.Parse(base)
		parsed.Path = path.Join(path.Dir(parsed.Path), nodeID)
		req, err := http.NewRequestWithContext(ctx, http.MethodDelete, parsed.String(), nil)
		if err != nil {
			return remoteCleanupError{cause: err}
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set(nodeCredentialHeader, api.NodeCredential)
		resp, err := api.HTTPClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
			_ = resp.Body.Close()
			return nil
		}
		if err := requireJSONContentType(resp.Header.Get("Content-Type")); err != nil {
			_ = resp.Body.Close()
			return remoteCleanupError{cause: err}
		}
		publicError, err := clientapi.DecodePublicError(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return remoteCleanupError{cause: err}
		}
		if err := publicError.ValidateHTTPResponse(resp.StatusCode, strings.TrimSpace(resp.Header.Get(controlRequestIDHeader))); err != nil {
			return remoteCleanupError{cause: err}
		}
		if publicError.ErrorCode == clientapi.ErrorCodeTemporarilyUnavailable {
			lastErr = remoteCleanupError{RequestID: publicError.RequestID, cause: errors.New(string(publicError.ErrorCode))}
			continue
		}
		return remoteCleanupError{RequestID: publicError.RequestID, cause: errors.New(string(publicError.ErrorCode))}
	}
	var typed remoteCleanupError
	if errors.As(lastErr, &typed) {
		return typed
	}
	return remoteCleanupError{cause: lastErr}
}

func decodeRegisterNodeRecoveryResponse(resp *http.Response, req clientapi.RegisterNodeRequest) recoveryControlResult {
	if resp == nil || resp.Body == nil {
		return recoveryControlResult{FailureCode: recoveryErrorProtocol, Err: errors.New("control-plane response is missing")}
	}
	if err := requireJSONContentType(resp.Header.Get("Content-Type")); err != nil {
		return recoveryControlResult{FailureCode: recoveryErrorProtocol, Err: err}
	}
	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		response, err := clientapi.DecodeRegisterNodeResponse(resp.Body)
		if err != nil {
			return recoveryControlResult{FailureCode: recoveryErrorProtocol, Err: err}
		}
		if err := response.ValidateForRequest(req); err != nil {
			return recoveryControlResult{FailureCode: recoveryErrorProtocol, Err: err}
		}
		return recoveryControlResult{Response: &response}
	}
	publicError, err := clientapi.DecodePublicError(resp.Body)
	if err != nil {
		return recoveryControlResult{FailureCode: recoveryErrorProtocol, Err: err}
	}
	if err := publicError.ValidateHTTPResponse(resp.StatusCode, strings.TrimSpace(resp.Header.Get(controlRequestIDHeader))); err != nil {
		return recoveryControlResult{FailureCode: recoveryErrorProtocol, Err: err}
	}
	return recoveryControlResult{PublicError: &publicError}
}

func recoveryControlEndpoint(baseURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("invalid control-plane URL")
	}
	if parsed.Scheme != "https" && (parsed.Scheme != "http" || !isLoopbackControlHost(parsed.Hostname())) {
		return "", errors.New("control-plane URL must use HTTPS outside loopback development")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/nodes/register"
	return parsed.String(), nil
}

func isLoopbackControlHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func requireJSONContentType(value string) error {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(value))
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		return errors.New("control-plane recovery response must use application/json")
	}
	return nil
}

func recoveryPhaseForPublicError(code clientapi.ErrorCode) (client.RecoveryPhase, bool) {
	switch code {
	case clientapi.ErrorCodeAuthenticationRequired:
		return client.RecoveryPhaseNeedsLogin, false
	case clientapi.ErrorCodeAuthorizationDenied:
		return client.RecoveryPhasePolicyBlocked, false
	case clientapi.ErrorCodeTemporarilyUnavailable:
		return client.RecoveryPhaseRecovering, true
	default:
		return client.RecoveryPhaseBlocked, false
	}
}

func persistRecoveryFailure(store *client.ConfigStore, recovery client.EnrollmentRecovery, phase client.RecoveryPhase, code, requestID string, retryable bool, cause error) (recoveryAttemptResult, error) {
	if err := store.Update(func(current *client.Config) error {
		if err := requireSameRecovery(current, recovery, ""); err != nil {
			return err
		}
		failed := current.EnrollmentRecovery.WithFailure(phase, code, requestID, retryable, time.Now())
		current.EnrollmentRecovery = &failed
		return nil
	}); err != nil {
		return recoveryAttemptResult{}, err
	}
	return recoveryAttemptResult{
		OperationID: recovery.OperationID,
		Phase:       phase,
		ErrorCode:   code,
		RequestID:   requestID,
		Retryable:   retryable,
	}, cause
}

func requireSameRecovery(current *client.Config, expected client.EnrollmentRecovery, oldCredential string) error {
	if current == nil || current.EnrollmentRecovery == nil ||
		current.EnrollmentRecovery.OperationID != expected.OperationID ||
		current.EnrollmentRecovery.IdempotencyID != expected.IdempotencyID {
		return errors.New(recoveryErrorStateConflict)
	}
	if oldCredential != "" && current.NodeCredential != oldCredential {
		return errors.New(recoveryErrorStateConflict)
	}
	return nil
}
