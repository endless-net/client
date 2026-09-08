package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/internal/client"
)

type terminalAgentCredential struct {
	code               api.ErrorCode
	nodeID, credential string
}

func (e *terminalAgentCredential) Error() string { return "node enrollment ended: " + string(e.code) }

// The public SDK preserves error bodies but not their response headers. Inspect
// the original response here so destructive recovery requires the full current
// error contract, including content type, HTTP status and matching request ID.
type agentCredentialTransport struct {
	base               http.RoundTripper
	nodeID, credential string
	terminal           *terminalAgentCredential
}

func (t *agentCredentialTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(req)
	if err != nil {
		return resp, err
	}
	nodePath := url.PathEscape(t.nodeID)
	endpointRequest := req.Method == http.MethodPatch && req.URL.EscapedPath() == "/nodes/"+nodePath+"/endpoint"
	mapRequest := req.Method == http.MethodGet && req.URL.EscapedPath() == "/maps/"+nodePath+"/stream"
	if resp.StatusCode != http.StatusUnauthorized || req.Header.Get(nodeCredentialHeader) != t.credential || t.credential == "" || (!endpointRequest && !mapRequest) {
		return resp, nil
	}
	if requireJSONContentType(resp.Header.Get("Content-Type")) != nil {
		return resp, nil
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4097))
	resp.Body = &restoredResponseBody{Reader: io.MultiReader(bytes.NewReader(body), resp.Body), Closer: resp.Body}
	if readErr != nil || len(body) > 4096 {
		return resp, nil
	}
	public, decodeErr := api.DecodePublicError(bytes.NewReader(body))
	if decodeErr == nil && public.ValidateHTTPResponse(resp.StatusCode, resp.Header.Get(controlRequestIDHeader)) == nil && public.ErrorCode.RequiresReEnrollment() {
		t.terminal = &terminalAgentCredential{code: public.ErrorCode, nodeID: t.nodeID, credential: t.credential}
	}
	return resp, nil
}

type restoredResponseBody struct {
	io.Reader
	io.Closer
}

// Called while the agent operation lock is held. Never erase a replacement
// credential installed after the failed request, or state while teardown fails.
func handleAgentTerminalCredential(ctx context.Context, opts agentIPCOptions, failure error) (bool, error) {
	var terminal *terminalAgentCredential
	if !errors.As(failure, &terminal) {
		return false, nil
	}
	store, err := agentServiceIPCConfigStore(opts)
	if err != nil {
		return true, err
	}
	current := store.Read()
	if current.NodeID != terminal.nodeID || current.NodeCredential != terminal.credential {
		return true, errors.New("node enrollment changed during failed sync")
	}
	if _, err = downAgentWireGuard(ctx, opts); err != nil {
		return true, fmt.Errorf("stop revoked tunnel: %w", err)
	}
	if err = store.Update(func(cfg *client.Config) error {
		if cfg.NodeID != terminal.nodeID || cfg.NodeCredential != terminal.credential {
			return errors.New("node enrollment changed during cleanup")
		}
		return client.ApplyTerminalRecoveryCleanup(cfg)
	}); err != nil {
		return true, err
	}
	if path := strings.TrimSpace(opts.StateOutput); path != "" {
		if err = os.Remove(path); err != nil && !os.IsNotExist(err) {
			return true, err
		}
	}
	return true, nil
}
