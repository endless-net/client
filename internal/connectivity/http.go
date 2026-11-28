package connectivity

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/internal/control"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	wire "github.com/endless-net/coordinator/api/publicquery/v1"
	"github.com/endless-net/coordinator/api/publicquery/v1/publicqueryv1connect"
	"google.golang.org/protobuf/types/known/durationpb"
)

var (
	ErrUnavailable       = errors.New("connectivity control plane unavailable")
	ErrStreamRotation    = errors.New("scheduled map stream rotation")
	ErrProtocol          = errors.New("connectivity protocol or configuration rejected")
	ErrDenied            = errors.New("connectivity access denied")
	ErrRevoked           = errors.New("connectivity access explicitly revoked")
	ErrEndpointUncertain = errors.New("endpoint announcement outcome uncertain")
)

// MapStreamFailure contains only locally classified diagnostics and numeric revisions.
type MapStreamFailure struct {
	cause                          error
	Step, RPCCode                  string
	FullSnapshot                   bool
	RequestRevision, EventRevision uint64
	Records                        int
}

func (e *MapStreamFailure) Error() string { return e.cause.Error() }
func (e *MapStreamFailure) Unwrap() error { return e.cause }

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
	streams, err := control.RestrictedHTTPClient(calls, c.StreamDuration)
	if err != nil {
		return nil, ErrConfig
	}
	// The request context owns the stream deadline and distinguishes rotation
	// from cancellation, credential expiry and transport failures.
	streams.Timeout = 0
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

func connectivityCallError(err error, mutation bool) error {
	if !mutation && control.RejectedControlTransport(err) {
		return ErrTrust
	}
	switch connect.CodeOf(err) {
	case connect.CodeUnauthenticated, connect.CodePermissionDenied:
		return ErrDenied
	case connect.CodeAborted:
		return ErrControlConflict
	case connect.CodeInvalidArgument:
		if connect.IsWireError(err) {
			return ErrProtocol
		}
		if !mutation {
			return ErrMap
		}
	case connect.CodeUnimplemented, connect.CodeFailedPrecondition:
		return ErrProtocol
	case connect.CodeDataLoss:
		if !mutation {
			return ErrProtocol
		}
	case connect.CodeResourceExhausted:
		if !mutation && !connect.IsWireError(err) {
			return ErrMap
		}
	}
	if mutation {
		return ErrEndpointUncertain
	}
	return ErrUnavailable
}

func (t *Transport) client(httpClient connect.HTTPClient, origin string) publicqueryv1connect.ConnectivityServiceClient {
	return publicqueryv1connect.NewConnectivityServiceClient(httpClient, origin,
		connect.WithReadMaxBytes(t.config.MaxResponseBytes), connect.WithSendMaxBytes(t.config.MaxRequestBytes))
}
func validAnnouncement(p state.Profile, r coordinator.UpdateNodeEndpointRequest) bool {
	if coordinator.ValidateNodePostureMetadata(r.Posture) != nil || (r.Posture != nil && r.Status != "online") || p.Connection == nil || r.Generation == 0 || r.Generation != p.Connection.EndpointGeneration || len(r.Candidates) != 0 {
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
	request := connect.NewRequest(control.EndpointRequest(binding.node, input))
	request.Header().Set("X-EndlessNet-Node-Credential", binding.credential)
	response, err := t.client(t.calls, binding.origin).UpdateNodeEndpoint(ctx, request)
	if err != nil {
		return empty, connectivityCallError(err, true)
	}
	if ctx.Err() != nil {
		return empty, ErrEndpointUncertain
	}
	result, err := control.EndpointReceipt(response.Msg)
	// This acknowledgement reports discovery state. Its timestamp uses the
	// server clock and never supplies authority to start or extend traffic.
	if err != nil || coordinator.ValidateEndpointReceipt(input, result, binding.node, binding.network) != nil || result.ExpiresAt.After(binding.credentialEnds) {
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
// ErrStreamRotation asks the caller to start a replacement immediately, with
// no change to the existing signed lease.
func (t *Transport) Stream(parent context.Context, v *Verifier, previous *state.MapCursor, receive func(context.Context, Map) error) error {
	return t.stream(parent, v, previous, receive, false)
}

// StreamSnapshot requests a full snapshot without deleting the verifier's floor.
// This is also the explicit resynchronization path after an unsupported delta.
func (t *Transport) StreamSnapshot(parent context.Context, v *Verifier, previous *state.MapCursor, receive func(context.Context, Map) error) error {
	return t.stream(parent, v, previous, receive, true)
}

func (t *Transport) stream(parent context.Context, v *Verifier, previous *state.MapCursor, receive func(context.Context, Map) error, full bool) (failure error) {
	detail := MapStreamFailure{FullSnapshot: full, Step: "prepare"}
	defer func() {
		if failure != nil {
			detail.cause = failure
			failure = &detail
		}
	}()
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
	detail.Step = "credential_window"
	duration := t.config.StreamDuration
	deadlineCause := ErrStreamRotation
	if remaining := v.credentialEnds.Sub(v.config.Now()); remaining <= duration {
		duration = remaining
		deadlineCause = context.DeadlineExceeded
	}
	if duration < time.Millisecond {
		return ErrDenied
	}
	timed, stop := context.WithTimeoutCause(parent, duration, deadlineCause)
	defer stop()
	ctx, abort := context.WithCancelCause(timed)
	defer abort(nil)
	detail.Step = "verify_trust"
	if _, err := v.currentTrust(ctx, "node:map"); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var challengeBytes [32]byte
	if _, err := rand.Read(challengeBytes[:]); err != nil {
		return ErrUnavailable
	}
	challenge := mapTimeChallenge{nonce: base64.RawURLEncoding.EncodeToString(challengeBytes[:])}
	message := &wire.WatchMapsRequest{NodeId: v.node, Timeout: durationpb.New(duration), Nonce: challenge.nonce}
	if cursor != nil && !full {
		detail.RequestRevision = cursor.Revision.Network
		message.Cursor = &wire.MapCursor{Revision: &wire.MapRevision{Network: cursor.Revision.Network}, MapHash: cursor.PayloadHash}
	}
	request := connect.NewRequest(message)
	request.Header().Set("X-EndlessNet-Node-Credential", v.credential)
	challenge.sent, err = v.config.Clock.Sample()
	if err != nil || !challenge.sent.valid() {
		return ErrClock
	}
	headerTimer := time.AfterFunc(t.config.CallTimeout, func() { abort(ErrUnavailable) })
	defer headerTimer.Stop()
	meter := &streamReadMeter{client: t.streams}
	detail.Step = "open_rpc"
	stream, err := t.client(meter, v.origin).WatchMaps(ctx, request)
	if err != nil {
		if ctx.Err() != nil {
			return streamContextError(ctx)
		}
		detail.RPCCode = connect.CodeOf(err).String()
		return connectivityCallError(err, false)
	}
	defer func() { _ = stream.Close() }()
	receivedMap := false
	for count := 0; count < t.config.MaxRecords; count++ {
		if ctx.Err() != nil {
			return streamContextError(ctx)
		}
		var timer *time.Timer
		if deadline, _ := ctx.Deadline(); t.config.ReadIdleTimeout < time.Until(deadline) {
			timer = time.AfterFunc(t.config.ReadIdleTimeout, func() { abort(ErrUnavailable) })
		}
		detail.Step = "receive_rpc"
		before := meter.bytes
		hasMessage := stream.Receive()
		if timer != nil {
			timer.Stop()
		}
		headerTimer.Stop()
		if ctx.Err() != nil {
			if receivedMap && !hasMessage && meter.bytes == before && context.Cause(ctx) == ErrStreamRotation &&
				(connect.CodeOf(stream.Err()) == connect.CodeDeadlineExceeded || connect.CodeOf(stream.Err()) == connect.CodeCanceled) {
				return ErrStreamRotation
			}
			return streamContextError(ctx)
		}
		if !hasMessage {
			if stream.Err() != nil {
				detail.RPCCode = connect.CodeOf(stream.Err()).String()
			} else {
				detail.RPCCode = "eof"
			}
			return connectivityCallError(stream.Err(), false)
		}
		detail.Records++
		detail.Step = "decode_event"
		event, err := control.MapUpdate(stream.Msg())
		if err != nil || !pathID(event.EventID) {
			return ErrMapEvent
		}
		detail.Step = "verify_event"
		detail.EventRevision = event.To.Network
		var m Map
		if count == 0 {
			m, err = v.verifyEvent(ctx, event, cursor, full, &challenge)
		} else {
			if err = t.waitSnapshotIssuedAt(ctx, v, event); err != nil {
				return err
			}
			m, err = v.verifyEvent(ctx, event, cursor, full, nil)
		}
		if ctx.Err() != nil {
			return streamContextError(ctx)
		}
		if errors.Is(err, ErrReplay) {
			// A nonce-bound opening snapshot may match the durable cursor.
			// Keep reading without granting or extending packet authority.
			receivedMap = true
			continue
		}
		if errors.Is(err, ErrNoMap) {
			if count == 0 {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return streamContextError(ctx)
		}
		detail.Step = "apply_event"
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
		receivedMap = true
		next := m.Cursor()
		cursor = &next
	}
	return ErrUnavailable
}

// A server clock can be slightly ahead without granting authority yet. Wait
// within the existing call budget, then run the unchanged verifier against the
// actual clock. Untrusted timestamps never reach receive or extend a deadline.
func (t *Transport) waitSnapshotIssuedAt(ctx context.Context, v *Verifier, event coordinator.MapStreamEvent) error {
	if event.TimeProof != nil {
		return ErrMap
	}
	if event.Type != "snapshot" && event.Type != "resync" || event.Snapshot == nil || event.Snapshot.MapSignature == nil {
		return nil // The verifier owns malformed records and other event types.
	}
	delay := event.Snapshot.MapSignature.IssuedAt.Sub(v.config.Now())
	if delay <= 0 {
		return nil
	}
	if delay > t.config.CallTimeout {
		return ErrMap
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func streamContextError(ctx context.Context) error {
	if errors.Is(context.Cause(ctx), ErrUnavailable) {
		return ErrUnavailable
	}
	return ctx.Err()
}

type streamReadMeter struct {
	client *http.Client
	bytes  int64
}

func (m *streamReadMeter) Do(request *http.Request) (*http.Response, error) {
	response, err := m.client.Do(request)
	if err == nil {
		response.Body = &measuredStreamBody{ReadCloser: response.Body, meter: m}
	}
	return response, err
}

type measuredStreamBody struct {
	io.ReadCloser
	meter *streamReadMeter
}

func (b *measuredStreamBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.meter.bytes += int64(n)
	return n, err
}
