package v2

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const maxResponseBodyBytes = 1 << 20

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		BaseURL:    DefaultLocalEndpoint,
		HTTPClient: httpClient,
	}
}

func (c *Client) Request(ctx context.Context, method, path string, in any, out any) error {
	if c == nil {
		return fmt.Errorf("service IPC client is nil")
	}
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	baseURL := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if baseURL == "" {
		baseURL = DefaultLocalEndpoint
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	setRequestHeaders(req)
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodyBytes+1))
	if err != nil {
		return err
	}
	if len(raw) > maxResponseBodyBytes {
		return fmt.Errorf("service IPC response is too large")
	}
	if err := validateResponseMetadata(raw, resp.StatusCode >= 200 && resp.StatusCode < 300); err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var payload struct {
			ErrorCode string `json:"error_code"`
			Error     string `json:"error"`
			RequestID string `json:"request_id"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			return NewError(resp.StatusCode, ErrorRequestFailed, fmt.Errorf("%s %s failed: %s", method, path, resp.Status))
		}
		return NewErrorWithRequestID(resp.StatusCode, payload.ErrorCode, payload.RequestID, fmt.Errorf("%s", firstNonEmptyIPCString(payload.Error, resp.Status)))
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	return decodeJSON(raw, out)
}

func (c *Client) Stream(ctx context.Context, method, path string, in any, onEvent func(Event) error) error {
	if c == nil {
		return fmt.Errorf("service IPC client is nil")
	}
	if onEvent == nil {
		return fmt.Errorf("service IPC stream event callback is required")
	}
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	baseURL := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if baseURL == "" {
		baseURL = DefaultLocalEndpoint
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	setRequestHeaders(req)
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodyBytes+1))
		if readErr != nil {
			return readErr
		}
		if len(raw) > maxResponseBodyBytes {
			return fmt.Errorf("service IPC response is too large")
		}
		if err := validateResponseMetadata(raw, false); err != nil {
			return err
		}
		var payload struct {
			ErrorCode string `json:"error_code"`
			Error     string `json:"error"`
			RequestID string `json:"request_id"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			return NewError(resp.StatusCode, ErrorRequestFailed, fmt.Errorf("%s %s failed: %s", method, path, resp.Status))
		}
		return NewErrorWithRequestID(resp.StatusCode, payload.ErrorCode, payload.RequestID, fmt.Errorf("%s", firstNonEmptyIPCString(payload.Error, resp.Status)))
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), maxResponseBodyBytes)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var event Event
		if err := decodeJSON(line, &event); err != nil {
			return err
		}
		if err := validateMetadata(event.Metadata, true); err != nil {
			return err
		}
		if err := onEvent(event); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func decodeJSON(raw []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("service IPC response contains trailing JSON")
		}
		return err
	}
	return nil
}

func setRequestHeaders(req *http.Request) {
	req.Header.Set(ProtocolHeader, Protocol)
	req.Header.Set(VersionHeader, fmt.Sprintf("%d", Version))
	req.Header.Set(MinVersionHeader, fmt.Sprintf("%d", MinSupportedVersion))
}

func validateResponseMetadata(raw []byte, requireNegotiated bool) error {
	var envelope Metadata
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("decode service IPC protocol metadata: %w", err)
	}
	return validateMetadata(envelope, requireNegotiated)
}

func validateMetadata(metadata Metadata, requireNegotiated bool) error {
	if metadata.IPCProtocol != Protocol {
		return NewError(http.StatusUpgradeRequired, ErrorProtocolUnsupported, fmt.Errorf("unsupported service IPC response protocol %q", metadata.IPCProtocol))
	}
	if metadata.IPCVersion <= 0 || metadata.IPCMinSupported <= 0 || metadata.IPCMinSupported > metadata.IPCVersion {
		return NewError(http.StatusBadRequest, ErrorInvalidVersionRange, fmt.Errorf("service IPC response version range is invalid"))
	}
	lower := max(MinSupportedVersion, metadata.IPCMinSupported)
	upper := min(Version, metadata.IPCVersion)
	if lower > upper {
		return NewError(http.StatusUpgradeRequired, ErrorVersionUnsupported, fmt.Errorf("service IPC client and server version ranges do not overlap"))
	}
	if requireNegotiated && metadata.IPCNegotiatedVersion != upper {
		return NewError(http.StatusUpgradeRequired, ErrorVersionUnsupported, fmt.Errorf("service IPC response negotiated version %d, want %d", metadata.IPCNegotiatedVersion, upper))
	}
	if metadata.IPCNegotiatedVersion != 0 && (metadata.IPCNegotiatedVersion < lower || metadata.IPCNegotiatedVersion > upper) {
		return NewError(http.StatusUpgradeRequired, ErrorVersionUnsupported, fmt.Errorf("service IPC response negotiated version is outside the compatible range"))
	}
	return nil
}

func firstNonEmptyIPCString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
