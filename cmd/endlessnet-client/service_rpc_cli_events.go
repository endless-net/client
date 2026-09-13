package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"connectrpc.com/connect"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/encoding/protojson"
)

type serviceRPCEventStream interface {
	Receive() bool
	Msg() *ipc.WatchEventsResponse
	Err() error
}

func writeServiceRPCEvents(ctx context.Context, stream serviceRPCEventStream, instance string, output io.Writer) error {
	var sequence, revision uint64
	for stream.Receive() {
		event := stream.Msg()
		if event == nil || event.Sequence != sequence+1 || event.Event == nil ||
			event.Metadata.GetInstanceId() != instance || instance == "" ||
			event.Metadata.GetRevision() == 0 || event.Metadata.GetRevision() < revision {
			return errors.New("invalid native event sequence or snapshot metadata; resubscribe for a fresh snapshot")
		}
		if sequence == 0 && (event.GetSnapshot() == nil || event.GetSnapshot().GetRuntime().GetInstanceId() != instance || event.GetSnapshot().Status == nil) {
			return errors.New("native event stream must begin with a full snapshot")
		}
		if sequence != 0 && event.GetSnapshot() != nil {
			return errors.New("native event stream repeated its opening snapshot; resubscribe for a fresh snapshot")
		}
		encoded, err := protojson.Marshal(event)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintln(output, string(encoded)); err != nil {
			return err
		}
		sequence, revision = event.Sequence, event.Metadata.Revision
	}
	err := stream.Err()
	if sequence != 0 && ctx.Err() != nil && (errors.Is(err, ctx.Err()) || connect.CodeOf(err) == connect.CodeDeadlineExceeded || connect.CodeOf(err) == connect.CodeCanceled) {
		return nil // The explicitly bounded subscription completed after opening.
	}
	if err != nil {
		return err
	}
	return errors.New("native event stream ended unexpectedly; resubscribe for a fresh snapshot")
}
