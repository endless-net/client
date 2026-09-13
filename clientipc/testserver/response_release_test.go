package testserver

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	pb "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestResponseReleaseValidation(t *testing.T) {
	for _, step := range []Step{
		{Method: "GetStatus", Request: &pb.GetStatusRequest{}, Responses: []proto.Message{&pb.GetStatusResponse{}}, ResponseRelease: []<-chan struct{}{nil}},
		{Method: "WatchEvents", Request: &pb.WatchEventsRequest{}, Responses: []proto.Message{&pb.WatchEventsResponse{}}, ResponseRelease: []<-chan struct{}{nil, nil}},
	} {
		if err := New().Expect(step); err == nil {
			t.Fatal("invalid response gates accepted")
		}
	}
}

func TestResponseReleaseStream(t *testing.T) {
	for _, cancelBlocked := range []bool{false, true} {
		t.Run(map[bool]string{false: "release", true: "cancel"}[cancelBlocked], func(t *testing.T) {
			server := New()
			gate := make(chan struct{})
			gates := []<-chan struct{}{nil, gate}
			expect(t, server, Step{
				Method: "WatchEvents", Request: &pb.WatchEventsRequest{},
				Responses:       []proto.Message{&pb.WatchEventsResponse{Sequence: 1}, &pb.WatchEventsResponse{Sequence: 2}},
				ResponseRelease: gates,
			})
			// The queued gate list belongs to the script, not the caller's slice.
			gates[1] = nil
			if server.steps["WatchEvents"][0].ResponseRelease[1] != gate {
				t.Fatal("caller mutation changed the queued gate")
			}
			expect(t, server, Step{Method: "GetStatus", Request: &pb.GetStatusRequest{}, Responses: []proto.Message{&pb.GetStatusResponse{}}})
			client := start(t, server)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			stream, err := client.WatchEvents(ctx, connect.NewRequest(&pb.WatchEventsRequest{}))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = stream.Close() }()
			if !stream.Receive() || stream.Msg().GetSequence() != 1 {
				t.Fatal("initial event missing")
			}
			if _, err := client.GetStatus(ctx, connect.NewRequest(&pb.GetStatusRequest{})); err != nil {
				t.Fatal("blocked stream prevented concurrent unary", err)
			}
			if server.Verify() == nil {
				t.Fatal("blocked stream verified as complete")
			}
			if cancelBlocked {
				cancel()
				_ = stream.Close()
			} else {
				close(gate)
				if !stream.Receive() || stream.Msg().GetSequence() != 2 {
					t.Fatal("released event missing")
				}
				if stream.Receive() || stream.Err() != nil {
					t.Fatal("unexpected stream termination", stream.Err())
				}
			}
			idle, stop := context.WithTimeout(t.Context(), 5*time.Second)
			defer stop()
			if err := server.WaitIdle(idle); err != nil {
				t.Fatal(err)
			}
			if err := server.Verify(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
