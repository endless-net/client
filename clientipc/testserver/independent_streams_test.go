package testserver

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	pb "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestResponseGatesAndCancellationAreIsolatedAcrossStreams(t *testing.T) {
	server := New()
	firstGate, secondGate := make(chan struct{}), make(chan struct{})
	for _, gate := range []chan struct{}{firstGate, secondGate} {
		expect(t, server, Step{Method: "WatchEvents", Request: &pb.WatchEventsRequest{},
			Responses:       []proto.Message{&pb.WatchEventsResponse{Sequence: 1}, &pb.WatchEventsResponse{Sequence: 2}},
			ResponseRelease: []<-chan struct{}{nil, gate}})
	}
	consumer := start(t, server)
	ctx, stop := context.WithTimeout(t.Context(), 5*time.Second)
	defer stop()
	firstCtx, cancelFirst := context.WithCancel(ctx)
	defer cancelFirst()
	first, err := consumer.WatchEvents(firstCtx, connect.NewRequest(&pb.WatchEventsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Close() }()
	// Receiving the opening event establishes admission order without sleeps.
	if !first.Receive() || first.Msg().Sequence != 1 {
		t.Fatal("first stream did not open", first.Err())
	}
	second, err := consumer.WatchEvents(ctx, connect.NewRequest(&pb.WatchEventsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Close() }()
	if !second.Receive() || second.Msg().Sequence != 1 {
		t.Fatal("blocked first stream prevented second admission", second.Err())
	}
	cancelFirst()
	if first.Receive() || connect.CodeOf(first.Err()) != connect.CodeCanceled {
		t.Fatal("first stream cancellation released a gated event", first.Err())
	}
	_ = first.Close()
	if server.Verify() == nil {
		t.Fatal("one cancelled stream completed another blocked stream")
	}
	close(secondGate)
	if !second.Receive() || second.Msg().Sequence != 2 {
		t.Fatal("first cancellation prevented independent gated delivery", second.Err())
	}
	if second.Receive() || second.Err() != nil {
		t.Fatal("second stream did not complete independently", second.Err())
	}
	if err := server.WaitIdle(ctx); err != nil {
		t.Fatal(err)
	}
	if err := server.Verify(); err != nil {
		t.Fatal(err)
	}
}
