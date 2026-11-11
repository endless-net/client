package connectivity

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/endless-net/client/internal/control"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

var (
	ErrUnavailable       = errors.New("connectivity control plane unavailable")
	ErrDenied            = errors.New("connectivity access denied")
	ErrRevoked           = errors.New("connectivity access explicitly revoked")
	ErrEndpointUncertain = errors.New("endpoint announcement outcome uncertain")
)

type HTTPConfig struct {
	CallTimeout, StreamDuration, ReadIdleTimeout                        time.Duration
	MaxRequestBytes, MaxResponseBytes, MaxRecords, MaxCalls, MaxStreams int
}

type Transport struct {
	calls, streams             *http.Client
	config                     HTTPConfig
	activeCalls, activeStreams chan struct{}
}

// NewTransport reuses the public control HTTP policy, without private Service
// Kit. Separate finite capacities keep an open map stream from consuming all
// endpoint call slots. Parent cancellation owns shutdown of active operations.
func NewTransport(client *http.Client, c HTTPConfig) (*Transport, error) {
	if c.Validate() != nil {
		return nil, ErrConfig
	}
	calls, err := control.RestrictedHTTPClient(client, c.CallTimeout)
	if err != nil {
		return nil, ErrConfig
	}
	streamInput := *calls
	streamInput.Timeout = c.StreamDuration
	streams, err := control.RestrictedHTTPClient(&streamInput, c.StreamDuration)
	if err != nil {
		return nil, ErrConfig
	}
	return &Transport{calls: calls, streams: streams, config: c, activeCalls: make(chan struct{}, c.MaxCalls), activeStreams: make(chan struct{}, c.MaxStreams)}, nil
}

// Validate is pure and can reject application configuration before acquisition.
func (c HTTPConfig) Validate() error {
	if c.CallTimeout < time.Millisecond || c.CallTimeout > 30*time.Second || c.StreamDuration < time.Millisecond || c.StreamDuration > 5*time.Minute || c.ReadIdleTimeout < time.Millisecond || c.ReadIdleTimeout > 30*time.Second || c.ReadIdleTimeout > c.StreamDuration || c.MaxRequestBytes < 1 || c.MaxResponseBytes < 1 || c.MaxRequestBytes > 1<<20 || c.MaxResponseBytes > 1<<20 || c.MaxRecords < 1 || c.MaxCalls < 1 || c.MaxStreams < 1 {
		return ErrConfig
	}
	return nil
}

func (t *Transport) CloseIdleConnections() {
	t.calls.CloseIdleConnections()
	t.streams.CloseIdleConnections()
}

func pathID(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, c := range value {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', strings.ContainsRune("_.:-", c):
		default:
			return false
		}
	}
	return true
}

func headerOne(h http.Header, key string) string {
	var values []string
	for name, value := range h {
		if strings.EqualFold(name, key) {
			values = append(values, value...)
		}
	}
	if len(values) != 1 {
		return ""
	}
	return values[0]
}

func responseStatus(status int, mutation bool) error {
	switch status {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrDenied
	case http.StatusConflict:
		return ErrStale
	case http.StatusBadRequest:
		return ErrMap
	}
	if mutation {
		return ErrEndpointUncertain
	}
	return ErrUnavailable
}

func validAnnouncement(p state.Profile, r coordinator.UpdateNodeEndpointRequest) bool {
	if p.Connection == nil || r.Generation == 0 || r.Generation != p.Connection.EndpointGeneration || len(r.Candidates) != 0 {
		return false
	}
	if r.Transport != nil && (r.Transport.IntentGeneration != p.Connection.Generation || coordinator.ValidateTransportAdvertisement(*r.Transport) != nil) {
		return false
	}
	want := "offline"
	if p.Connection.Intent == state.IntentConnected {
		want = "online"
	}
	ttl, err := time.ParseDuration(r.TTL)
	if err != nil || ttl <= 0 || ttl > 300*time.Second || r.Status != want {
		return false
	}
	a, err := netip.ParseAddrPort(r.Endpoint)
	return err == nil && a.Addr().Is4() && !a.Addr().IsUnspecified() && !a.Addr().IsLoopback() && !a.Addr().IsMulticast() && a.Port() != 0 && a.String() == r.Endpoint
}

// UpdateEndpoint sends once. The caller must first persist the exact generation
// and request. Lost/malformed replies remain uncertain; the ACK grants no traffic
// authority. A disconnected intent can publish only an offline announcement.
func (t *Transport) UpdateEndpoint(parent context.Context, p state.Profile, c VerifierConfig) (coordinator.UpdateNodeEndpointResponse, error) {
	var empty coordinator.UpdateNodeEndpointResponse
	if parent.Err() != nil {
		return empty, parent.Err()
	}
	if p.Connection == nil || p.Connection.EndpointRequest == nil || !p.Connection.EndpointRequest.Sent || p.Connection.EndpointRequest.Receipt != nil || p.Connection.EndpointRequest.IntentGeneration != p.Connection.Generation {
		return empty, ErrConfig
	}
	raw := append([]byte(nil), p.Connection.EndpointRequest.Request...)
	input, err := coordinator.DecodeUpdateNodeEndpointRequest(raw, t.config.MaxRequestBytes)
	if err != nil || !validAnnouncement(p, input) {
		return empty, ErrConfig
	}
	binding, err := newBinding(p, c, false)
	if err != nil {
		return empty, err
	}
	select {
	case t.activeCalls <- struct{}{}:
		defer func() { <-t.activeCalls }()
	default:
		return empty, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(parent, t.config.CallTimeout)
	defer cancel()
	if _, err := binding.currentTrust(ctx, "node:endpoint"); err != nil {
		return empty, err
	}
	if ctx.Err() != nil {
		return empty, ctx.Err()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPatch, binding.origin+"/nodes/"+binding.node+"/endpoint", bytes.NewReader(raw))
	if err != nil {
		return empty, ErrConfig
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-EndlessNet-Node-Credential", binding.credential)
	response, err := t.calls.Do(request)
	if err != nil {
		return empty, ErrEndpointUncertain
	}
	defer func() { _ = response.Body.Close() }()
	if err := responseStatus(response.StatusCode, true); err != nil {
		return empty, err
	}
	media, _, err := mime.ParseMediaType(headerOne(response.Header, "Content-Type"))
	if err != nil || media != "application/json" || response.ContentLength > int64(t.config.MaxResponseBytes) {
		return empty, ErrEndpointUncertain
	}
	raw, err = io.ReadAll(io.LimitReader(response.Body, int64(t.config.MaxResponseBytes)+1))
	if err != nil || ctx.Err() != nil || len(raw) > t.config.MaxResponseBytes {
		return empty, ErrEndpointUncertain
	}
	result, err := coordinator.DecodeUpdateNodeEndpointResponse(raw, t.config.MaxResponseBytes)
	if err != nil || coordinator.ValidateEndpointReceipt(input, result, binding.node, binding.network) != nil || result.UpdatedAt.After(c.Now()) || result.ExpiresAt.After(binding.credentialEnds) {
		return empty, ErrEndpointUncertain
	}
	return result, nil
}

func copyCursor(cursor *state.MapCursor) (*state.MapCursor, error) {
	if cursor == nil {
		return nil, nil
	}
	hash, err := hex.DecodeString(cursor.PayloadHash)
	if err != nil || len(hash) != 32 || hex.EncodeToString(hash) != cursor.PayloadHash || cursor.Revision.Network == 0 || cursor.Revision.Global != 0 || cursor.IssuedAt.IsZero() {
		return nil, ErrConfig
	}
	copyValue := *cursor
	return &copyValue, nil
}

// Stream processes one record at a time without an unbounded queue or retry.
// Receive must persist/fence the cursor before apply and respect its context.
// A callback error closes this stream; heartbeat/replay never invoke Receive.
func (t *Transport) Stream(parent context.Context, v *Verifier, previous *state.MapCursor, receive func(context.Context, Map) error) error {
	return t.stream(parent, v, previous, receive, false)
}

// StreamSnapshot requests a full snapshot without deleting the verifier's floor.
// This is also the explicit resynchronization path after an unsupported delta.
func (t *Transport) StreamSnapshot(parent context.Context, v *Verifier, previous *state.MapCursor, receive func(context.Context, Map) error) error {
	return t.stream(parent, v, previous, receive, true)
}

func (t *Transport) stream(parent context.Context, v *Verifier, previous *state.MapCursor, receive func(context.Context, Map) error, full bool) error {
	if parent.Err() != nil {
		return parent.Err()
	}
	if v == nil || receive == nil {
		return ErrConfig
	}
	cursor, err := v.previous(previous)
	if err != nil {
		return err
	}
	select {
	case t.activeStreams <- struct{}{}:
		defer func() { <-t.activeStreams }()
	default:
		return ErrUnavailable
	}
	duration := t.config.StreamDuration
	if remaining := v.credentialEnds.Sub(v.config.Now()); remaining < duration {
		duration = remaining
	}
	if duration < time.Millisecond {
		return ErrDenied
	}
	timed, stop := context.WithTimeout(parent, duration)
	defer stop()
	ctx, abort := context.WithCancelCause(timed)
	defer abort(nil)
	if _, err := v.currentTrust(ctx, "node:map"); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	query := url.Values{"from_network_revision": {"0"}, "from_global_revision": {"0"}, "from_map_hash": {""}, "timeout": {duration.String()}}
	if cursor != nil && !full {
		query.Set("from_network_revision", strconv.FormatUint(cursor.Revision.Network, 10))
		query.Set("from_map_hash", cursor.PayloadHash)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, v.origin+"/maps/"+v.node+"/stream?"+query.Encode(), nil)
	if err != nil {
		return ErrConfig
	}
	request.Header.Set("Accept", "application/x-ndjson")
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("X-EndlessNet-Node-Credential", v.credential)
	headerTimer := time.AfterFunc(t.config.CallTimeout, func() { abort(ErrUnavailable) })
	response, err := t.streams.Do(request)
	headerTimer.Stop()
	if err != nil {
		if ctx.Err() != nil {
			return streamContextError(ctx)
		}
		return ErrUnavailable
	}
	response.Body = &streamBody{ReadCloser: response.Body}
	defer func() { _ = response.Body.Close() }()
	closeBody := func() { _ = response.Body.Close() }
	if err := responseStatus(response.StatusCode, false); err != nil {
		return err
	}
	media, _, err := mime.ParseMediaType(headerOne(response.Header, "Content-Type"))
	if err != nil || media != "application/x-ndjson" || headerOne(response.Header, "X-EndlessNet-Map-Protocol") != "3" || headerOne(response.Header, "X-EndlessNet-Map-Capabilities") != "snapshot,signed-delta,checkpoint,resync,heartbeat" || response.Header.Get("Content-Encoding") != "" {
		return ErrMap
	}
	reader := bufio.NewReaderSize(response.Body, t.config.MaxResponseBytes+1)
	for count := 0; count < t.config.MaxRecords; count++ {
		if ctx.Err() != nil {
			return streamContextError(ctx)
		}
		timer := time.AfterFunc(t.config.ReadIdleTimeout, func() { abort(ErrUnavailable); closeBody() })
		raw, readErr := reader.ReadSlice('\n')
		timer.Stop()
		if ctx.Err() != nil {
			return streamContextError(ctx)
		}
		if len(raw) > t.config.MaxResponseBytes || errors.Is(readErr, bufio.ErrBufferFull) {
			return ErrMap
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) && len(raw) > 0 {
				return ErrMap
			}
			return ErrUnavailable
		}
		var m Map
		var err error
		if full {
			m, err = v.VerifySnapshot(ctx, raw, cursor)
		} else {
			m, err = v.Verify(ctx, raw, cursor)
		}
		if ctx.Err() != nil {
			return streamContextError(ctx)
		}
		if errors.Is(err, ErrNoMap) || errors.Is(err, ErrReplay) {
			continue
		}
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return streamContextError(ctx)
		}
		callbackErr := receive(ctx, m)
		if m.Denied() {
			if callbackErr != nil {
				return errors.Join(ErrRevoked, callbackErr)
			}
			return ErrRevoked
		}
		if callbackErr != nil {
			return callbackErr
		}
		next := m.Cursor()
		cursor = &next
	}
	return ErrUnavailable
}

func streamContextError(ctx context.Context) error {
	if errors.Is(context.Cause(ctx), ErrUnavailable) {
		return ErrUnavailable
	}
	return ctx.Err()
}

type streamBody struct {
	io.ReadCloser
	once sync.Once
}

func (b *streamBody) Close() (err error) {
	b.once.Do(func() { err = b.ReadCloser.Close() })
	return err
}
