package client

import (
	"context"
	"errors"
	"sync"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
)

type exitLANTestStream struct {
	changed chan struct{}
	once    sync.Once
	err     error
	closed  bool
}

func (s *exitLANTestStream) Changed() <-chan struct{} { return s.changed }
func (s *exitLANTestStream) Err() error               { return s.err }
func (s *exitLANTestStream) Close() error {
	s.closed = true
	s.once.Do(func() { close(s.changed) })
	return nil
}

func exitLANTestLifetime(t *testing.T) *exitLANSourceLifetime {
	t.Helper()
	stream := &exitLANTestStream{changed: make(chan struct{})}
	t.Cleanup(func() { _ = stream.Close() })
	return &exitLANSourceLifetime{ctx: t.Context(), stream: stream}
}

func TestExitLANWatchCaptureOwnershipAndInvalidation(t *testing.T) {
	for _, scenario := range []string{"stable", "before", "during", "lost", "capture_error", "wrong_scope", "wrong_family", "cancelled", "open_error", "no_stream"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			stream := &exitLANTestStream{changed: make(chan struct{})}
			opened, captured := false, false
			open := func(context.Context) (exitLANChangeStream, error) {
				opened = true
				if scenario == "before" {
					_ = stream.Close()
				}
				if scenario == "open_error" {
					return stream, errExitLANSource
				}
				if scenario == "no_stream" {
					return nil, nil
				}
				return stream, nil
			}
			capture := func(_ context.Context, own string, family api.ExitFamilyMode) (*exitLANSource, error) {
				if !opened {
					t.Fatal("snapshot preceded subscription")
				}
				captured = true
				switch scenario {
				case "during":
					_ = stream.Close()
				case "lost":
					stream.err = errors.New("receive loss")
				case "capture_error":
					return nil, errExitLANSource
				case "wrong_scope":
					own = "foreign"
				case "wrong_family":
					family = api.ExitFamilyIPv4Only
				case "cancelled":
					cancel()
				}
				return &exitLANSource{OwnInterface: own, Family: family}, nil
			}
			source, err := captureWatchedExitLANSource(ctx, "endlessnet", api.ExitFamilyDualStack, open, capture)
			if scenario != "stable" {
				if err == nil || source != nil {
					t.Fatal("invalid observation survived", err)
				}
				if scenario != "no_stream" && !stream.closed {
					t.Fatal("abandoned subscription leaked")
				}
				if (scenario == "before" || scenario == "open_error" || scenario == "no_stream") && captured {
					t.Fatal("snapshot ran without live subscription")
				}
				return
			}
			if err != nil || !source.lifetime.current() || stream.closed {
				t.Fatal("stable receipt lost", err)
			}
			copy := *source
			if err := source.close(); err != nil || copy.lifetime.current() {
				t.Fatal("copy outlived subscription", err)
			}
			source.lifetime = exitLANTestLifetime(t)
			if copy.lifetime.current() {
				t.Fatal("replacement revived original receipt")
			}
		})
	}
}
