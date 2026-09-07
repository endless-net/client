package client

import (
	"context"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
	clientrpc "github.com/endless-net/client-api/clientapi/v1/clientrpc"
	"github.com/endless-net/client-api/clientapi/v1/clientrpc/clientrpcconnect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func flowPolicy(now time.Time, version uint64) *clientrpc.GetFlowLogPolicyResponse {
	return &clientrpc.GetFlowLogPolicyResponse{ConsentVersion: version, CollectionNotBefore: timestamppb.New(now.Add(-time.Minute)), CollectionExpiresAt: timestamppb.New(now.Add(50 * time.Second))}
}

func TestFlowCollectorConsentBoundsAndImmutableRetry(t *testing.T) {
	c := &flowCollector{}
	now := time.Now()
	packet := applicationTCPPacket("100.64.0.1", "100.64.0.2", 40000, 443)
	c.observe(packet, true, now)
	if len(c.active) != 0 {
		t.Fatal("captured without consent")
	}
	c.policy(flowPolicy(now, 7), now)
	c.observe(packet, true, now)
	c.observe(packet, true, now.Add(time.Second))
	first, version := c.next(now.Add(11 * time.Second))
	if first == nil || version != 7 || first.GetPackets() != 2 || first.GetBytes() != uint64(2*len(packet)) || first.GetDecision() != "observed" {
		t.Fatalf("aggregate: %v", first)
	}
	first.Bytes++
	retry, _ := c.next(now.Add(12 * time.Second))
	if retry.GetBytes() == first.GetBytes() || retry.GetWindowId() != first.GetWindowId() {
		t.Fatal("retry was mutable or changed identity")
	}
	c.acknowledge(retry.GetWindowId(), 6)
	if len(c.pending) != 1 {
		t.Fatal("stale ack removed current window")
	}
	c.policy(flowPolicy(now.Add(15*time.Second), 8), now.Add(15*time.Second))
	if len(c.pending) != 0 {
		t.Fatal("policy revision retained old buffer")
	}
	c.observe(packet, false, now.Add(16*time.Second))
	denied, _ := c.next(now.Add(27 * time.Second))
	if denied.GetDecision() != "deny" {
		t.Fatal("denial not recorded")
	}
	c.next(now.Add(66 * time.Second))
	if len(c.active)+len(c.pending) != 0 || c.version != 0 {
		t.Fatal("expired consent retained metadata")
	}
}

func TestFlowCollectorCapacityAndUnsupportedPackets(t *testing.T) {
	c := &flowCollector{}
	now := time.Now()
	c.policy(flowPolicy(now, 1), now)
	for i := 0; i < maxFlowWindows+5; i++ {
		c.observe(applicationTCPPacket("100.64.0.1", "100.64.0.2", 40000, uint16(i)), true, now)
	}
	if len(c.active) != maxFlowWindows {
		t.Fatalf("unbounded buffer: %d", len(c.active))
	}
	c.observe([]byte("payload is not an IP packet"), true, now)
	if len(c.active) != maxFlowWindows {
		t.Fatal("malformed packet retained")
	}
	c.stop()
	if len(c.active) != 0 {
		t.Fatal("stop retained IP metadata")
	}
}

type flowRuntimeServer struct {
	spool *flowSpool
	clientrpcconnect.UnimplementedFlowLogServiceHandler
	reports chan *clientrpc.ReportFlowLogRequest
	count   int
}

func (s *flowRuntimeServer) GetFlowLogPolicy(_ context.Context, r *connect.Request[clientrpc.GetFlowLogPolicyRequest]) (*connect.Response[clientrpc.GetFlowLogPolicyResponse], error) {
	if r.Header().Get("Authorization") != "Bearer credential" {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("credential"))
	}
	return connect.NewResponse(flowPolicy(time.Now(), 7)), nil
}
func (s *flowRuntimeServer) ReportFlowLog(_ context.Context, r *connect.Request[clientrpc.ReportFlowLogRequest]) (*connect.Response[clientrpc.ReportFlowLogResponse], error) {
	if s.spool != nil {
		version, _, windows, err := s.spool.load(time.Now())
		if err != nil || version != r.Msg.GetConsentVersion() || len(windows) != 1 || !proto.Equal(windows[0], r.Msg.GetWindow()) {
			return nil, errors.New("report preceded durable checkpoint")
		}
	}
	s.reports <- proto.Clone(r.Msg).(*clientrpc.ReportFlowLogRequest)
	s.count++
	if s.count == 1 {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("temporary outage"))
	}
	return connect.NewResponse(&clientrpc.ReportFlowLogResponse{WindowId: r.Msg.GetWindow().GetWindowId()}), nil
}

func TestTUNFlowProducerRetriesThroughTLSProtobuf(t *testing.T) {
	spool, err := newFlowSpool(filepath.Join(t.TempDir(), "queue"), "credential", "node")
	if err != nil {
		t.Fatal(err)
	}
	service := &flowRuntimeServer{reports: make(chan *clientrpc.ReportFlowLogRequest, 4), spool: spool}
	_, handler := clientrpcconnect.NewFlowLogServiceHandler(service)
	endpoint := httptest.NewTLSServer(handler)
	defer endpoint.Close()
	client := clientrpcconnect.NewFlowLogServiceClient(endpoint.Client(), endpoint.URL)
	c := &flowCollector{}
	now := time.Now()
	c.policy(flowPolicy(now, 7), now)
	device := &applicationBatchTUN{}
	tun := &applicationTUN{Device: device, filter: newApplicationPacketFilter(), flows: c}
	packet := applicationTCPPacket("100.64.0.1", "100.64.0.2", 40000, 443)
	if _, err := tun.Write([][]byte{packet}, 0); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		runFlowLogs(ctx, c, []clientrpcconnect.FlowLogServiceClient{client}, "node", "credential", spool)
	}()
	defer func() { cancel(); <-done }()
	var first *clientrpc.ReportFlowLogRequest
	for i := 0; i < 2; i++ {
		select {
		case request := <-service.reports:
			if i == 0 {
				first = request
			} else if !proto.Equal(first, request) {
				t.Fatal("retry changed immutable flow")
			}
		case <-ctx.Done():
			t.Fatal("TUN flow was not delivered")
		}
	}
	if first.GetConsentVersion() != 7 || first.GetNodeId() != "node" || first.GetWindow().GetPackets() != 1 || first.GetWindow().GetBytes() != uint64(len(packet)) {
		t.Fatalf("invalid TUN aggregate: %v", first)
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for c.status(time.Now()).AcknowledgedWindows != 1 {
		select {
		case <-ctx.Done():
			t.Fatal("acknowledgement was not committed locally")
		case <-ticker.C:
		}
	}
	// ACK is removed from memory before the atomic checkpoint finishes; wait for
	// the durable queue to become empty without stopping the worker prematurely.
	for {
		_, _, windows, err := spool.load(time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if len(windows) == 0 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("acknowledgement was not persisted")
		case <-ticker.C:
		}
	}
}
