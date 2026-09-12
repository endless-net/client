package main

import (
	"bytes"
	"io"
	"net/http"

	api "github.com/endless-net/client-api/clientapi/v1"
)

// Only a complete public rejection can retire the saved registration attempt.
// Transport failures and unvalidated error bodies leave its replay identity intact.
type registrationAttemptTransport struct {
	base   http.RoundTripper
	denied bool
}

func (t *registrationAttemptTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.denied = false
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(req)
	if err != nil || req.Method != http.MethodPost || req.URL.EscapedPath() != "/nodes/register" ||
		resp.StatusCode != http.StatusForbidden || requireJSONContentType(resp.Header.Get("Content-Type")) != nil {
		return resp, err
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4097))
	resp.Body = &restoredResponseBody{Reader: io.MultiReader(bytes.NewReader(body), resp.Body), Closer: resp.Body}
	if readErr == nil && len(body) <= 4096 {
		public, decodeErr := api.DecodePublicError(bytes.NewReader(body))
		t.denied = decodeErr == nil && public.ErrorCode == api.ErrorCodeAuthorizationDenied &&
			public.ValidateHTTPResponse(resp.StatusCode, resp.Header.Get(controlRequestIDHeader)) == nil
	}
	return resp, nil
}
