package connectivity

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
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

func mapResponse(request *http.Request, body io.ReadCloser) *http.Response {
	header := make(http.Header)
	header.Set("Content-Type", "application/x-ndjson")
	header.Set("X-EndlessNet-Map-Protocol", "3")
	header.Set("X-EndlessNet-Map-Capabilities", "snapshot,signed-delta,checkpoint,resync,heartbeat")
	return &http.Response{StatusCode: 200, Request: request, Body: body, Header: header}
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

func TestTransportStreamsMapsWithoutPromotingHeartbeatReplayAndPreservesRevocation(t *testing.T) {
	f := newMapFixture(t)
	v, err := NewVerifier(f.profile, f.config)
	if err != nil {
		t.Fatal(err)
	}
	first := f.sign(t, nil, f.now, f.now.Add(time.Minute))
	cursor := state.MapCursor{Revision: f.value.Revision, PayloadHash: f.value.MapSignature.PayloadHash, IssuedAt: f.now}
	heartbeat, _ := json.Marshal(coordinator.MapStreamEvent{Type: "heartbeat", ProtocolVersion: 3, Capabilities: []string{"snapshot", "signed-delta", "checkpoint", "resync", "heartbeat"}, EventID: "heartbeat", From: cursor.Revision, To: cursor.Revision})
	f.now = f.now.Add(time.Second)
	f.value.Revision.Network++
	f.value.Network.Revision = f.value.Revision.Network
	f.value.Node.Status = "revoked"
	f.value.Peers = []coordinator.Peer{}
	last := f.sign(t, &cursor, f.now, f.now.Add(time.Minute))
	body := bytes.Join([][]byte{first, heartbeat, first, last}, []byte{'\n'})
	body = append(body, '\n')
	calls := 0
	transport := transportFixture(t, func(r *http.Request) (*http.Response, error) {
		calls++
		assertNodeRequest(t, r, f)
		if r.Method != http.MethodGet || r.URL.Path != "/maps/node/stream" || r.URL.Query().Get("from_network_revision") != "0" || len(r.URL.Query()) != 4 || r.Header.Get("Accept") != "application/x-ndjson" {
			t.Fatal("stream surface/cursor changed")
		}
		return mapResponse(r, io.NopCloser(bytes.NewReader(body))), nil
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
		if r.URL.Query().Get("from_network_revision") != "9007199254740993" || r.URL.Query().Get("from_map_hash") != cursor.PayloadHash || r.URL.Query().Get("from_global_revision") != "0" {
			t.Fatal("durable cursor rounded or omitted")
		}
		return mapResponse(r, &closeReader{Reader: bytes.NewReader(raw), close: func() { closed = true }}), nil
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

type blockingBody struct {
	closed chan struct{}
	once   sync.Once
}

func (b *blockingBody) Read([]byte) (int, error) { <-b.closed; return 0, io.ErrClosedPipe }
func (b *blockingBody) Close() error             { b.once.Do(func() { close(b.closed) }); return nil }

func TestTransportRejectsBadStreamsAndBoundsIdleRecordsAndCapacity(t *testing.T) {
	for _, name := range []string{"protocol", "capabilities", "media", "encoding", "oversize", "truncated", "denied", "outage", "records", "capacity", "idle", "header-timeout"} {
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
				response := mapResponse(r, io.NopCloser(bytes.NewReader(raw)))
				switch name {
				case "protocol":
					response.Header.Set("X-EndlessNet-Map-Protocol", "2")
				case "capabilities":
					response.Header.Del("X-EndlessNet-Map-Capabilities")
				case "media":
					response.Header.Set("Content-Type", "application/json")
				case "encoding":
					response.Header.Set("Content-Encoding", "gzip")
				case "oversize":
					response.Body = io.NopCloser(strings.NewReader(strings.Repeat("x", 65537) + "\n"))
				case "truncated":
					response.Body = io.NopCloser(bytes.NewReader(bytes.TrimSuffix(raw, []byte{'\n'})))
				case "denied":
					response.StatusCode = http.StatusForbidden
				case "records":
					event, _ := json.Marshal(coordinator.MapStreamEvent{Type: "heartbeat", ProtocolVersion: 3, Capabilities: []string{"snapshot", "signed-delta", "checkpoint", "resync", "heartbeat"}, EventID: "heartbeat"})
					response.Body = io.NopCloser(bytes.NewReader(bytes.Repeat(append(event, '\n'), 10)))
				case "idle":
					response.Body = &blockingBody{closed: make(chan struct{})}
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
			if name == "outage" || name == "records" || name == "capacity" || name == "idle" || name == "header-timeout" {
				want = ErrUnavailable
			}
			if !errors.Is(err, want) || received != 0 || strings.Contains(err.Error(), "secret") {
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
				if r.Method != http.MethodPatch || r.URL.Path != "/nodes/node/endpoint" || r.URL.RawQuery != "" {
					t.Fatal("endpoint route changed")
				}
				raw, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(raw, f.profile.Connection.EndpointRequest.Request) {
					t.Fatal("frozen endpoint bytes were reencoded")
				}
				request, err := coordinator.DecodeUpdateNodeEndpointRequest(raw, 65536)
				if err != nil || request.Generation != input.Generation || request.Status != status {
					t.Fatal("endpoint immutable request changed", err)
				}
				if name == "outage" {
					return nil, errors.New("secret-network-diagnostic")
				}
				ack := coordinator.UpdateNodeEndpointResponse{SchemaVersion: 2, NodeID: "node", NetworkID: "network", Endpoint: input.Endpoint, Generation: input.Generation, Online: status == "online", UpdatedAt: f.now, ExpiresAt: f.now.Add(30 * time.Second), Revision: coordinator.MapRevision{Network: 9007199254740994}}
				if name == "foreign" {
					ack.NetworkID = "other"
				}
				raw, _ = json.Marshal(ack)
				if name == "malformed" {
					raw = []byte(`{"credential":"secret-credential"}`)
				}
				if name == "oversize" {
					raw = bytes.Repeat([]byte{'x'}, 65537)
				}
				response := &http.Response{StatusCode: 200, Request: r, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(raw))}
				switch name {
				case "denied":
					response.StatusCode = 403
				case "stale":
					response.StatusCode = 409
				case "redirect":
					response.StatusCode = 307
					response.Header.Set("Location", "https://other.example/nodes/node/endpoint")
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
