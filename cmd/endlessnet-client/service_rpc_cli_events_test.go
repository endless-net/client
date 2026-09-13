package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

type serviceRPCEventsFixture struct {
	events []*ipc.WatchEventsResponse
	index  int
	err    error
}

func (s *serviceRPCEventsFixture) Receive() bool                 { s.index++; return s.index <= len(s.events) }
func (s *serviceRPCEventsFixture) Msg() *ipc.WatchEventsResponse { return s.events[s.index-1] }
func (s *serviceRPCEventsFixture) Err() error                    { return s.err }

func TestServiceNativeEventsValidateSubscription(t *testing.T) {
	first := &ipc.WatchEventsResponse{Sequence: 1, Metadata: &ipc.SnapshotMetadata{InstanceId: "instance", Revision: 1},
		Event: &ipc.WatchEventsResponse_Snapshot{Snapshot: &ipc.SnapshotEvent{Runtime: &ipc.RuntimeInfo{InstanceId: "instance"}, Status: &ipc.Status{}}}}
	second := &ipc.WatchEventsResponse{Sequence: 2, Metadata: &ipc.SnapshotMetadata{InstanceId: "instance", Revision: 2},
		Event: &ipc.WatchEventsResponse_StatusChanged{StatusChanged: &ipc.Status{}}}
	for _, scenario := range []string{"timeout", "timeout before snapshot", "EOF", "EOF before snapshot", "status first", "gap", "new instance", "revision regression", "missing event", "missing metadata"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			a, b := proto.Clone(first).(*ipc.WatchEventsResponse), proto.Clone(second).(*ipc.WatchEventsResponse)
			stream := &serviceRPCEventsFixture{events: []*ipc.WatchEventsResponse{a, b}}
			switch scenario {
			case "timeout":
				cancel()
				stream.err = context.Canceled
			case "timeout before snapshot":
				cancel()
				stream.events = nil
				stream.err = context.Canceled
			case "EOF before snapshot":
				stream.events = nil
			case "status first":
				a.Event = b.Event
			case "gap":
				b.Sequence = 3
			case "new instance":
				b.Metadata.InstanceId = "replacement"
			case "revision regression":
				a.Metadata.Revision = 3
			case "missing event":
				a.Event = nil
			case "missing metadata":
				a.Metadata = nil
			}
			var output bytes.Buffer
			err := writeServiceRPCEvents(ctx, stream, "instance", &output)
			if (err == nil) != (scenario == "timeout") {
				t.Fatal("wrong subscription outcome", err)
			}
			if scenario == "timeout" && len(strings.Split(strings.TrimSpace(output.String()), "\n")) != 2 {
				t.Fatal("events not written as NDJSON")
			}
		})
	}
	failure := errors.New("synthetic output failure")
	if err := writeServiceRPCEvents(t.Context(), &serviceRPCEventsFixture{events: []*ipc.WatchEventsResponse{first}}, "instance", serviceRPCFailWriter{failure}); !errors.Is(err, failure) {
		t.Fatal("output failure lost", err)
	}
}

type serviceRPCFailWriter struct{ err error }

func (w serviceRPCFailWriter) Write([]byte) (int, error) { return 0, w.err }
