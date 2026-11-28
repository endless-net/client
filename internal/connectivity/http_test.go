package connectivity

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/internal/state"
	"github.com/endless-net/client/internal/testwire"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	wire "github.com/endless-net/coordinator/api/publicquery/v1"
	"github.com/endless-net/coordinator/api/publicquery/v1/publicqueryv1connect"
	"google.golang.org/protobuf/proto"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func transportFixture(t *testing.T, roundTrip roundTripFunc) *Transport {
	t.Helper()
	transport, err := NewTransport(&http.Client{Transport: roundTrip}, HTTPConfig{CallTimeout: time.Second, StreamDuration: time.Second, ReadIdleTimeout: time.Second, MaxRequestBytes: 65536, MaxResponseBytes: 65536, MaxRecords: 8, MaxCalls: 1, MaxStreams: 1})
	if err != nil {
		t.Fatal(err)
	}
	return transport
}

func watchRequest(t *testing.T, r *http.Request) *wire.WatchMapsRequest {
	t.Helper()
	message, err := testwire.ReadWatch(r)
	if err != nil || r.URL.Path != publicqueryv1connect.ConnectivityServiceWatchMapsProcedure || message.NodeId != "node" ||
		message.Timeout.CheckValid() != nil || coordinator.ValidateMapStreamNonce(message.Nonce) != nil {
		t.Fatal("invalid generated WatchMaps request", err)
	}
	return message
}

func fixtureFrames(t *testing.T, records ...[]byte) []byte {
	t.Helper()
	var frames []byte
	for _, record := range records {
		var event coordinator.MapStreamEvent
		if err := json.Unmarshal(record, &event); err != nil {
			t.Fatal(err)
		}
		frame, err := testwire.Frame(testwire.Update(event))
		if err != nil {
			t.Fatal(err)
		}
		frames = append(frames, frame...)
	}
	return frames
}
func assertNodeRequest(t *testing.T, r *http.Request, f *mapFixture) {
	t.Helper()
	if r.URL.Scheme != "https" || r.URL.Host != "control.example" || r.Header.Get("X-EndlessNet-Node-Credential") != f.profile.Secrets.NodeCredential || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
		t.Fatal("node request changed origin/credential class")
	}
	if _, ok := r.Context().Deadline(); !ok {
		t.Fatal("unbounded node request")
	}
}

func TestTransportStreamsMapsWithoutPromotingReplayAndPreservesRevocation(t *testing.T) {
	f := newMapFixture(t)
	v, err := NewVerifier(f.profile, f.config)
	if err != nil {
		t.Fatal(err)
	}
	first := f.sign(t, nil, f.now, f.now.Add(time.Minute))
	cursor := state.MapCursor{Revision: f.value.Revision, PayloadHash: f.value.MapSignature.PayloadHash, IssuedAt: f.now}
	f.now = f.now.Add(time.Second)
	f.value.Revision.Network++
	f.value.Network.Revision = f.value.Revision.Network
	f.value.Node.Status = "revoked"
	f.value.Peers = []coordinator.Peer{}
	last := f.sign(t, &cursor, f.now, f.now.Add(time.Minute))
	calls := 0
	transport := transportFixture(t, func(r *http.Request) (*http.Response, error) {
		calls++
		assertNodeRequest(t, r, f)
		request := watchRequest(t, r)
		if request.Cursor != nil {
			t.Fatal("initial stream unexpectedly bound a cursor")
		}
		opening := f.openingRecord(t, first, request.Nonce)
		heartbeat, err := json.Marshal(coordinator.MapStreamEvent{Type: "heartbeat", EventID: "heartbeat", From: cursor.Revision})
		if err != nil {
			t.Fatal(err)
		}
		body := fixtureFrames(t, opening, heartbeat, first, last)
		return testwire.StreamResponse(r, io.NopCloser(bytes.NewReader(body))), nil
	})
	var received []Map
	err = transport.Stream(context.Background(), v, nil, func(_ context.Context, m Map) error {
		received = append(received, m)
		if m.Denied() {
			return state.ErrUncertain
		}
		return nil
	})
	if !errors.Is(err, ErrRevoked) || !errors.Is(err, state.ErrUncertain) || calls != 1 || len(received) != 2 || received[0].Denied() || !received[1].Denied() || len(transport.activeStreams) != 0 {
		t.Fatal("stream lost revoke/callback uncertainty or promoted replay", err, len(received))
	}
}

func TestFullStreamReplayWaitsForNewMapWithoutRenewing(t *testing.T) {
	f := newMapFixture(t)
	v, err := NewVerifier(f.profile, f.config)
	if err != nil {
		t.Fatal(err)
	}
	raw := f.sign(t, nil, f.now, f.now.Add(time.Minute))
	first, err := v.Verify(t.Context(), raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	cursor := first.Cursor()
	f.value.Network.Revision++
	f.value.Revision.Network++
	next := f.sign(t, &cursor, f.now, f.now.Add(time.Minute))
	tr := transportFixture(t, func(r *http.Request) (*http.Response, error) {
		opening := f.openingRecord(t, raw, watchRequest(t, r).Nonce)
		return testwire.StreamResponse(r, io.NopCloser(bytes.NewReader(fixtureFrames(t, opening, next)))), nil
	})
	stop := errors.New("received replacement")
	err = tr.StreamSnapshot(t.Context(), v, &cursor, func(_ context.Context, m Map) error {
		if m.Cursor().Revision.Network != cursor.Revision.Network+1 {
			t.Fatal("replayed map reached native apply")
		}
		return stop
	})
	if !errors.Is(err, stop) {
		t.Fatal(err)
	}
}

func TestTransportResumesExactUint64CursorAndClosesOnCancellation(t *testing.T) {
	f := newMapFixture(t)
	v, err := NewVerifier(f.profile, f.config)
	if err != nil {
		t.Fatal(err)
	}
	_, err = v.Verify(context.Background(), f.sign(t, nil, f.now, f.now.Add(time.Minute)), nil)
	if err != nil {
		t.Fatal(err)
	}
	cursor := state.MapCursor{Revision: f.value.Revision, PayloadHash: f.value.MapSignature.PayloadHash, IssuedAt: f.now}
	f.now = f.now.Add(time.Second)
	end := f.now.Add(time.Minute)
	f.value.Peers[0].EndpointExpiresAt = &end
	raw := append(f.sign(t, &cursor, f.now, end), '\n')
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	closed := false
	received := 0
	transport := transportFixture(t, func(r *http.Request) (*http.Response, error) {
		assertNodeRequest(t, r, f)
		request := watchRequest(t, r)
		if request.Cursor.GetRevision().GetNetwork() != 9007199254740993 || request.Cursor.GetMapHash() != cursor.PayloadHash || request.Cursor.GetRevision().GetGlobal() != 0 {
			t.Fatal("durable cursor rounded or omitted")
		}
		opening := f.openingRecord(t, raw, request.Nonce)
		return testwire.StreamResponse(r, &closeReader{Reader: bytes.NewReader(fixtureFrames(t, opening)), close: func() { closed = true }}), nil
	})
	err = transport.Stream(ctx, v, &cursor, func(context.Context, Map) error { received++; cancel(); return nil })
	if !errors.Is(err, context.Canceled) || received != 1 || !closed || len(transport.activeStreams) != 0 {
		t.Fatal("cancellation continued/left body or capacity open", err)
	}
}

type closeReader struct {
	io.Reader
	close func()
}

func (b *closeReader) Close() error { b.close(); return nil }

type streamDeadlineReader struct{ ctx context.Context }

func (r streamDeadlineReader) Read([]byte) (int, error) {
	<-r.ctx.Done()
	return 0, r.ctx.Err()
}

func TestStreamRotationRequiresOwnDeadlineAndVerifiedMap(t *testing.T) {
	for _, name := range []string{"scheduled", "parent-deadline", "credential-deadline", "early-eof", "idle", "empty-update", "partial-record"} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f := newMapFixture(t)
				expiry := f.now.Add(time.Minute)
				if name == "credential-deadline" {
					expiry = f.profile.Attempt.CredentialEnds
					f.now = expiry.Add(-500 * time.Millisecond)
					f.value.Peers[0].EndpointExpiresAt = &expiry
				}
				v, err := NewVerifier(f.profile, f.config)
				if err != nil {
					t.Fatal(err)
				}
				raw := append(f.sign(t, nil, f.now, expiry), '\n')
				transport := transportFixture(t, func(r *http.Request) (*http.Response, error) {
					opening := f.openingRecord(t, raw, watchRequest(t, r).Nonce)
					body := io.MultiReader(bytes.NewReader(fixtureFrames(t, opening)), streamDeadlineReader{ctx: r.Context()})
					switch name {
					case "early-eof":
						body = bytes.NewReader(fixtureFrames(t, opening))
					case "idle":
						return testwire.StreamResponse(r, io.NopCloser(streamDeadlineReader{ctx: r.Context()})), nil
					case "empty-update":
						event, err := json.Marshal(coordinator.MapStreamEvent{Type: "heartbeat", ProtocolVersion: 3, Capabilities: []string{"snapshot", "signed-delta", "checkpoint", "resync", "heartbeat"}, EventID: "heartbeat"})
						if err != nil {
							t.Fatal(err)
						}
						body = io.MultiReader(bytes.NewReader(fixtureFrames(t, event)), streamDeadlineReader{ctx: r.Context()})
					case "partial-record":
						body = io.MultiReader(bytes.NewReader(fixtureFrames(t, opening)), strings.NewReader("{"), streamDeadlineReader{ctx: r.Context()})
					}
					return testwire.StreamResponse(r, io.NopCloser(body)), nil
				})
				ctx := context.Background()
				want := ErrUnavailable
				switch name {
				case "scheduled":
					want = ErrStreamRotation
				case "parent-deadline":
					parent, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
					defer cancel()
					ctx = parent
					want = context.DeadlineExceeded
				case "credential-deadline", "partial-record":
					want = context.DeadlineExceeded
				case "idle":
					transport.config.ReadIdleTimeout = 500 * time.Millisecond
				case "empty-update":
					want = ErrMap
				}
				received := 0
				err = transport.StreamSnapshot(ctx, v, nil, func(context.Context, Map) error { received++; return nil })
				wantReceived := 1
				if name == "idle" || name == "empty-update" {
					wantReceived = 0
				}
				if !errors.Is(err, want) || received != wantReceived || len(transport.activeStreams) != 0 {
					t.Fatalf("stream result = %v, maps = %d, active streams = %d; want %v, %d, 0", err, received, len(transport.activeStreams), want, wantReceived)
				}
			})
		})
	}
}

func TestTransportRejectsBadStreamsAndBoundsIdleRecordsAndCapacity(t *testing.T) {
	for _, name := range []string{"media", "oversize", "truncated", "denied", "outage", "records", "capacity", "idle", "header-timeout"} {
		t.Run(name, func(t *testing.T) {
			f := newMapFixture(t)
			v, err := NewVerifier(f.profile, f.config)
			if err != nil {
				t.Fatal(err)
			}
			raw := append(f.sign(t, nil, f.now, f.now.Add(time.Minute)), '\n')
			calls, received := 0, 0
			transport := transportFixture(t, func(r *http.Request) (*http.Response, error) {
				calls++
				if name == "outage" {
					return nil, errors.New("secret-network-diagnostic")
				}
				if name == "header-timeout" {
					<-r.Context().Done()
					return nil, r.Context().Err()
				}
				opening := f.openingRecord(t, raw, watchRequest(t, r).Nonce)
				frame := fixtureFrames(t, opening)
				response := testwire.StreamResponse(r, io.NopCloser(bytes.NewReader(frame)))
				switch name {
				case "media":
					response.Header.Set("Content-Type", "application/json")
				case "oversize":
					prefix := []byte{0, 0, 1, 0, 1}
					response.Body = io.NopCloser(io.MultiReader(bytes.NewReader(prefix), strings.NewReader(strings.Repeat("x", 65537))))
				case "truncated":
					response.Body = io.NopCloser(bytes.NewReader(frame[:len(frame)-1]))
				case "denied":
					response.StatusCode = http.StatusForbidden
					response.Header.Set("Content-Type", "application/json")
					response.Body = io.NopCloser(strings.NewReader(`{"code":"permission_denied"}`))
				case "records":
					frame = append(frame, bytes.Repeat(fixtureFrames(t, raw), 10)...)
					response.Body = io.NopCloser(bytes.NewReader(frame))
				case "idle":
					response.Body = io.NopCloser(streamDeadlineReader{ctx: r.Context()})
				}
				return response, nil
			})
			if name == "capacity" {
				transport.activeStreams <- struct{}{}
				defer func() { <-transport.activeStreams }()
			}
			if name == "idle" {
				transport.config.ReadIdleTimeout = 10 * time.Millisecond
			}
			if name == "header-timeout" {
				transport.config.CallTimeout = 10 * time.Millisecond
			}
			err = transport.Stream(context.Background(), v, nil, func(context.Context, Map) error { received++; return nil })
			want := ErrMap
			if name == "denied" {
				want = ErrDenied
			}
			if name == "media" || name == "outage" || name == "records" || name == "capacity" || name == "idle" || name == "header-timeout" {
				want = ErrUnavailable
			}
			wantReceived := 0
			if name == "records" {
				wantReceived = 1
			}
			if !errors.Is(err, want) || received != wantReceived || strings.Contains(err.Error(), "secret") {
				t.Fatal("unsafe stream accepted or diagnostic leaked", err, received)
			}
			if name == "capacity" && calls != 0 {
				t.Fatal("over-capacity stream sent request")
			}
		})
	}
}

func TestEndpointTransportUsesNodeClassExactACKAndNeverRetriesUncertainMutation(t *testing.T) {
	for _, name := range []string{"valid", "offline", "outage", "malformed", "foreign", "oversize", "denied", "stale", "wrong-intent", "wrong-generation", "redirect"} {
		t.Run(name, func(t *testing.T) {
			f := newMapFixture(t)
			f.profile.Connection.EndpointGeneration = 9007199254740993
			status := "online"
			if name == "offline" {
				_, _ = f.profile.Connection.AcceptIntent(state.IntentDisconnected, f.now)
				status = "offline"
			}
			input := coordinator.UpdateNodeEndpointRequest{Endpoint: "192.0.2.1:51820", Generation: f.profile.Connection.EndpointGeneration, Status: status, TTL: "30s"}
			calls := 0
			transport := transportFixture(t, func(r *http.Request) (*http.Response, error) {
				calls++
				assertNodeRequest(t, r, f)
				if r.Method != http.MethodPost || r.URL.Path != publicqueryv1connect.ConnectivityServiceUpdateNodeEndpointProcedure || r.URL.RawQuery != "" {
					t.Fatal("endpoint route changed")
				}
				raw, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatal(err)
				}
				request, err := testwire.EndpointRequest(raw)
				if err != nil || request.Generation != input.Generation || request.Status != status || request.Endpoint != input.Endpoint || request.TTL != input.TTL {
					t.Fatal("endpoint immutable request changed", err)
				}
				if name == "outage" {
					return nil, errors.New("secret-network-diagnostic")
				}
				ack := coordinator.UpdateNodeEndpointResponse{SchemaVersion: 2, NodeID: "node", NetworkID: "network", Endpoint: input.Endpoint, Generation: input.Generation, Online: status == "online", UpdatedAt: f.now, ExpiresAt: f.now.Add(30 * time.Second), Revision: coordinator.MapRevision{Network: 9007199254740994}}
				if name == "foreign" {
					ack.NetworkID = "other"
				}
				raw, _ = proto.Marshal(testwire.EndpointReceipt(ack))
				if name == "malformed" {
					raw = []byte(`{"credential":"secret-credential"}`)
				}
				if name == "oversize" {
					raw = bytes.Repeat([]byte{'x'}, 65537)
				}
				response := &http.Response{StatusCode: 200, Request: r, Header: http.Header{"Content-Type": {"application/proto"}}, Body: io.NopCloser(bytes.NewReader(raw))}
				switch name {
				case "denied":
					response.StatusCode = 403
					response.Header.Set("Content-Type", "application/json")
					response.Body = io.NopCloser(strings.NewReader(`{"code":"permission_denied"}`))
				case "stale":
					response.StatusCode = 409
					response.Header.Set("Content-Type", "application/json")
					response.Body = io.NopCloser(strings.NewReader(`{"code":"aborted"}`))
				case "redirect":
					response.StatusCode = 307
					response.Header.Set("Location", "https://other.example"+publicqueryv1connect.ConnectivityServiceUpdateNodeEndpointProcedure)
				}
				return response, nil
			})
			if name == "wrong-intent" {
				input.Status = "offline"
			}
			if name == "wrong-generation" {
				input.Generation++
			}
			raw, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			if name == "valid" {
				raw = append([]byte(" \n"), raw...)
			}
			f.profile.Connection.EndpointRequest = &state.EndpointRequest{IntentGeneration: f.profile.Connection.Generation, Request: raw, Sent: true}
			result, err := transport.UpdateEndpoint(context.Background(), f.profile, f.config)
			if name == "valid" || name == "offline" {
				if err != nil || result.Generation != input.Generation || result.Online != (status == "online") || calls != 1 {
					t.Fatal("valid exact endpoint ACK rejected", err)
				}
				return
			}
			want := ErrEndpointUncertain
			if name == "denied" {
				want = ErrDenied
			}
			if name == "stale" {
				want = ErrStale
			}
			if name == "wrong-intent" || name == "wrong-generation" {
				want = ErrConfig
				if calls != 0 {
					t.Fatal("invalid endpoint sent")
				}
			} else if calls != 1 {
				t.Fatal("endpoint request retried or followed redirect", calls)
			}
			if !errors.Is(err, want) || result.Generation != 0 || strings.Contains(err.Error(), "secret") {
				t.Fatal("uncertain endpoint accepted or diagnostic leaked", err)
			}
		})
	}
}

func TestTransportRequiresBoundedPublicHTTPPolicy(t *testing.T) {
	c := HTTPConfig{CallTimeout: time.Second, StreamDuration: time.Second, ReadIdleTimeout: time.Second, MaxRequestBytes: 1024, MaxResponseBytes: 1024, MaxRecords: 2, MaxCalls: 1, MaxStreams: 1}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, client := range []*http.Client{nil, {Jar: jar}, {Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}, {Transport: &http.Transport{TLSClientConfig: &tls.Config{Certificates: []tls.Certificate{{}}}}}} {
		if _, err := NewTransport(client, c); !errors.Is(err, ErrConfig) {
			t.Fatal("unsafe HTTP source accepted")
		}
	}
	if _, err := NewTransport(&http.Client{}, HTTPConfig{}); !errors.Is(err, ErrConfig) {
		t.Fatal("unbounded config accepted")
	}
}

func TestOpeningMapUsesSignedTimeAndRejectsInvalidAuthority(t *testing.T) {
	for _, name := range []string{"ahead", "behind", "resync", "missing", "nonce", "hash", "proof-signature", "map-signature", "rollback", "delayed-expiry", "cancel"} {
		t.Run(name, func(t *testing.T) {
			f := newMapFixture(t)
			clock := &leaseClock{sample: ClockSample{Wall: f.now, Uptime: time.Hour, UpperUptime: time.Hour, Epoch: "fixture-runtime"}}
			f.config.Clock = clock
			issued := f.now.Add(1200 * time.Millisecond)
			if name == "behind" {
				issued = f.now.Add(-1200 * time.Millisecond)
			}
			expires := issued.Add(30 * time.Second)
			raw := f.sign(t, nil, issued, expires)
			if name == "rollback" {
				f.profile.Connection.MapCursor = &state.MapCursor{
					Revision: coordinator.MapRevision{Network: f.value.Revision.Network + 1}, PayloadHash: strings.Repeat("a", 64), IssuedAt: issued,
				}
			}
			if name == "resync" {
				raw = bytes.ReplaceAll(raw, []byte(`"type":"snapshot"`), []byte(`"type":"resync"`))
			}
			v, err := NewVerifier(f.profile, f.config)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			requests, received := 0, 0
			transport := transportFixture(t, func(r *http.Request) (*http.Response, error) {
				requests++
				nonce := watchRequest(t, r).Nonce
				if coordinator.ValidateMapStreamNonce(nonce) != nil {
					t.Fatal("invalid outgoing nonce")
				}
				if name == "nonce" {
					nonce = strings.Repeat("A", 43)
				}
				opening := f.openingRecord(t, raw, nonce)
				switch name {
				case "missing":
					opening = append(bytes.Clone(raw), '\n')
				case "hash":
					opening = bytes.Replace(opening, []byte(`"map_hash":"`+f.value.MapSignature.PayloadHash), []byte(`"map_hash":"`+strings.Repeat("a", 64)), 1)
				case "proof-signature":
					var event coordinator.MapStreamEvent
					if err := json.Unmarshal(opening, &event); err != nil {
						t.Fatal(err)
					}
					opening = bytes.Replace(opening, []byte(event.TimeProof.Signature), []byte(strings.Repeat("A", 86)), 1)
				case "map-signature":
					opening = bytes.ReplaceAll(opening, []byte(f.value.MapSignature.Signature), []byte(strings.Repeat("A", 86)))
				case "delayed-expiry":
					clock.advance(0, 30*time.Second)
				case "cancel":
					cancel()
				}
				return testwire.StreamResponse(r, io.NopCloser(bytes.NewReader(fixtureFrames(t, opening)))), nil
			})
			done := errors.New("received verified map")
			err = transport.StreamSnapshot(ctx, v, nil, func(_ context.Context, m Map) error {
				received++
				if m.Cursor().IssuedAt != issued || m.timeProof.serverTime != issued {
					t.Fatal("signed times were altered")
				}
				return done
			})
			want, wantReceived := ErrMap, 0
			switch name {
			case "ahead", "behind", "resync":
				want, wantReceived = done, 1
			case "rollback":
				want = ErrStale
			case "delayed-expiry":
				want = ErrExpired
			case "cancel":
				want = context.Canceled
			}
			if !errors.Is(err, want) || received != wantReceived || requests != 1 {
				t.Fatalf("stream = %v, maps = %d, requests = %d; want %v, %d, 1", err, received, requests, want, wantReceived)
			}
		})
	}
}

func TestOpeningMapLeaseCountsRoundTripSleepAndRestart(t *testing.T) {
	f := newMapFixture(t)
	clock := &leaseClock{sample: ClockSample{Wall: f.now, Uptime: time.Hour, UpperUptime: time.Hour, Epoch: "fixture-runtime"}}
	f.config.Clock = clock
	v, err := NewVerifier(f.profile, f.config)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := NewLease(t.Context(), clock, f.profile.ID, f.profile.Connection.Generation)
	if err != nil {
		t.Fatal(err)
	}
	issued := f.now.Add(1200 * time.Millisecond)
	expires := issued.Add(30 * time.Second)
	raw := f.sign(t, nil, issued, expires)
	var firstNonce string
	var first []byte
	var received int
	var calls int
	transport := transportFixture(t, func(r *http.Request) (*http.Response, error) {
		calls++
		nonce := watchRequest(t, r).Nonce
		if calls == 1 {
			firstNonce = nonce
			first = f.openingRecord(t, raw, nonce)
			clock.advance(0, 2*time.Second)
		} else if nonce == firstNonce {
			t.Fatal("reconnect reused challenge")
		}
		return testwire.StreamResponse(r, io.NopCloser(bytes.NewReader(fixtureFrames(t, first)))), nil
	})
	var accepted Map
	applied := errors.New("applied")
	err = transport.Stream(t.Context(), v, nil, func(_ context.Context, m Map) error {
		accepted = m
		if err := lease.Confirm(m); err != nil {
			return err
		}
		return applied
	})
	if !errors.Is(err, applied) {
		t.Fatal("opening map was not applied", err)
	}
	nestedEnd := issued.Add(5 * time.Second)
	if !lease.WindowCurrent(accepted, issued, nestedEnd, f.now) || !lease.Permit(false) {
		t.Fatal("signed interval start was not accepted")
	}
	clock.advance(0, 3*time.Second)
	if lease.WindowCurrent(accepted, issued, nestedEnd, f.now) || !lease.Permit(true) {
		t.Fatal("shorter interval did not expire independently of map")
	}
	lease.Unavailable()
	clock.advance(0, 25*time.Second)
	if lease.Permit(true) {
		t.Fatal("sleep or elapsed exchange extended offline permission")
	}
	// A later map can restore the lease while the original Relay credential
	// remains expired on its original time bound.
	f.now = f.now.Add(2 * time.Second)
	clock.advance(2*time.Second, 0)
	cursor := accepted.Cursor()
	updated, err := v.Verify(t.Context(), f.sign(t, &cursor, f.now, f.now.Add(20*time.Second)), &cursor)
	if err != nil || lease.Confirm(updated) != nil || !lease.Permit(true) || lease.OriginalExpiryCurrent(accepted, nestedEnd, f.now) {
		t.Fatal("map refresh renewed the original Relay credential", err)
	}
	err = transport.Stream(t.Context(), v, nil, func(context.Context, Map) error {
		received++
		return applied
	})
	if !errors.Is(err, ErrMap) || received != 0 || calls != 2 {
		t.Fatal("old proof was accepted in a new stream", err)
	}
	restarted := &leaseClock{sample: ClockSample{Wall: f.now, Uptime: time.Hour, UpperUptime: time.Hour, Epoch: "restarted-runtime"}}
	next, err := NewLease(t.Context(), restarted, f.profile.ID, f.profile.Connection.Generation)
	if err != nil || !errors.Is(next.Confirm(accepted), ErrClock) || next.Permit(true) {
		t.Fatal("restart restored authority from old proof", err)
	}
}

func TestOpeningTimeProofCannotAuthorizeLaterStreamRecords(t *testing.T) {
	for _, test := range []struct {
		name   string
		replay bool
	}{{"later issued time", false}, {"repeated proof", true}} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f := newMapFixture(t)
				started := time.Now()
				f.config.Now = func() time.Time { return f.now.Add(time.Since(started)) }
				f.config.Clock = clockFunc(func() (ClockSample, error) {
					uptime := time.Hour + time.Since(started)
					return ClockSample{Wall: f.config.Now(), Uptime: uptime, UpperUptime: uptime, Epoch: "fixture-runtime"}, nil
				})
				first := f.sign(t, nil, f.now, f.now.Add(time.Minute))
				cursor := state.MapCursor{Revision: f.value.Revision, PayloadHash: f.value.MapSignature.PayloadHash, IssuedAt: f.now}
				issued := f.now.Add(50 * time.Millisecond)
				later := f.sign(t, &cursor, issued, f.now.Add(time.Minute))
				v, err := NewVerifier(f.profile, f.config)
				if err != nil {
					t.Fatal(err)
				}
				transport := transportFixture(t, func(r *http.Request) (*http.Response, error) {
					opening := f.openingRecord(t, first, watchRequest(t, r).Nonce)
					second := append(bytes.Clone(later), '\n')
					if test.replay {
						second = opening
					}
					return testwire.StreamResponse(r, io.NopCloser(bytes.NewReader(fixtureFrames(t, opening, second)))), nil
				})
				received := 0
				finished := errors.New("received later record")
				err = transport.Stream(t.Context(), v, nil, func(context.Context, Map) error {
					received++
					if received == 2 {
						if f.config.Now().Before(issued) {
							t.Fatal("unattested map applied before issued_at")
						}
						return finished
					}
					return nil
				})
				want, wantReceived := finished, 2
				if test.replay {
					want, wantReceived = ErrMap, 1
				}
				if !errors.Is(err, want) || received != wantReceived {
					t.Fatal("later record violated its authorization boundary", err, received)
				}
			})
		})
	}
}

func TestMapStreamFailureClassification(t *testing.T) {
	for _, tc := range []struct {
		name      string
		err, want error
	}{
		{"missing procedure", connect.NewWireError(connect.CodeUnimplemented, nil), ErrProtocol},
		{"damaged upstream frame", connect.NewWireError(connect.CodeDataLoss, nil), ErrProtocol},
		{"invalid configuration", connect.NewWireError(connect.CodeFailedPrecondition, nil), ErrProtocol},
		{"TLS hostname rejected", &url.Error{Err: x509.HostnameError{Certificate: &x509.Certificate{}}}, ErrTrust},
		{"server limit", connect.NewWireError(connect.CodeResourceExhausted, nil), ErrUnavailable},
		{"local size limit", connect.NewError(connect.CodeResourceExhausted, nil), ErrMap},
		{"outage", connect.NewWireError(connect.CodeUnavailable, nil), ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := connectivityCallError(tc.err, false); !errors.Is(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
