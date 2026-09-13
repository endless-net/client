package testserver

import (
	"context"
	"errors"
	"testing"
	"time"

	pb "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestWaitIdleTracksHandlerCompletionWithoutWeakeningVerification(t *testing.T) {
	s := New()
	for range 2 {
		expect(t, s, Step{Method: "GetRuntimeInfo", Request: &pb.GetRuntimeInfoRequest{}, Responses: []proto.Message{&pb.GetRuntimeInfoResponse{}}})
		if err := s.WaitIdle(t.Context()); err != nil {
			t.Fatal(err)
		}
		if s.Verify() == nil {
			t.Fatal("idle accepted an unconsumed expectation")
		}
		if _, err := s.take("GetRuntimeInfo", &pb.GetRuntimeInfoRequest{}); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if !errors.Is(s.WaitIdle(ctx), context.Canceled) || s.Verify() == nil {
			t.Fatal("cancelled wait hid an active handler")
		}
		ctx, cancel = context.WithTimeout(t.Context(), time.Second)
		done := make(chan error, 1)
		go func() { done <- s.WaitIdle(ctx) }()
		s.finish()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		cancel()
		if err := s.Verify(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.take("GetRuntimeInfo", &pb.GetRuntimeInfoRequest{}); err == nil {
		t.Fatal("unexpected call accepted")
	}
	if err := s.WaitIdle(t.Context()); err != nil || s.Verify() == nil {
		t.Fatal("idle erased unexpected-call failure")
	}
}
