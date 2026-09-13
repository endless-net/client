package client

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func rpcTestLog(message string) *ipc.LogEntry {
	return &ipc.LogEntry{Timestamp: timestamppb.New(time.Unix(100, 0)), Message: message}
}

func TestRPCRecentLogsPaginationAndIsolation(t *testing.T) {
	m, peer, profile := rpcConnectFixture(t)
	s := NewClientRPCService(m, nil)
	entries := []*ipc.LogEntry{rpcTestLog("first"), rpcTestLog("join_token=synthetic-private-value"), rpcTestLog("third")}
	s.RecentLogsProvider = func(_ context.Context, id string) ([]*ipc.LogEntry, error) {
		if id != profile.ProfileId {
			t.Fatal("provider profile changed")
		}
		return entries, nil
	}
	req := &ipc.ListRecentLogsRequest{Profile: profile, Page: &ipc.PageRequest{PageSize: 1}}
	first, err := s.recentLogsAs(t.Context(), peer, req)
	if err != nil || len(first.GetLogs()) != 1 || first.Logs[0].Message != "first" || first.GetPage().GetNextPageToken() == "" {
		t.Fatal("missing first page", err)
	}
	if first.Page.Metadata.InstanceId != m.instanceID || first.Page.Metadata.Revision != m.store.Read().RPCState.Revision {
		t.Fatal("wrong metadata")
	}
	first.Logs[0].Message = "consumer change"
	if entries[0].Message != "first" {
		t.Fatal("provider snapshot was aliased")
	}
	req.Page.PageToken = first.Page.NextPageToken
	second, err := s.recentLogsAs(t.Context(), peer, req)
	if err != nil || second.Logs[0].Message != "[redacted token line]" || entries[1].Message != "join_token=synthetic-private-value" {
		t.Fatal("redaction or snapshot ownership failed", err)
	}
	_, err = s.recentLogsAs(t.Context(), local.Peer{Identity: "other-admin", Administrator: true}, req)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	req.Page.PageSize = 2
	_, err = s.recentLogsAs(t.Context(), peer, req)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	req.Page.PageSize = 1
	entries[0] = rpcTestLog("rotated")
	_, err = s.recentLogsAs(t.Context(), peer, req)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	entries[0] = rpcTestLog("first")
	req.Page.PageToken = second.Page.NextPageToken
	last, err := s.recentLogsAs(t.Context(), peer, req)
	if err != nil || len(last.Logs) != 1 || last.Logs[0].Message != "third" || last.Page.NextPageToken != "" {
		t.Fatal("wrong terminal page", err)
	}
	if err := m.store.Update(func(cfg *Config) error { cfg.RPCState.Revision++; return nil }); err != nil {
		t.Fatal(err)
	}
	_, err = s.recentLogsAs(t.Context(), peer, req)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
}

func TestRPCRecentLogsAdmissionAndProviderFailures(t *testing.T) {
	for _, test := range []struct {
		name   string
		code   ipc.ErrorCode
		change func(*ClientRPCService, *ipc.ListRecentLogsRequest, *local.Peer)
	}{
		{"unsupported", ipc.ErrorCode_ERROR_CODE_UNSUPPORTED, func(s *ClientRPCService, _ *ipc.ListRecentLogsRequest, _ *local.Peer) { s.RecentLogsProvider = nil }},
		{"anonymous", ipc.ErrorCode_ERROR_CODE_UNAUTHENTICATED, func(_ *ClientRPCService, _ *ipc.ListRecentLogsRequest, p *local.Peer) { p.Identity = "" }},
		{"observer", ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED, func(_ *ClientRPCService, _ *ipc.ListRecentLogsRequest, p *local.Peer) { p.Identity = "other" }},
		{"missing-profile", ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, func(_ *ClientRPCService, r *ipc.ListRecentLogsRequest, _ *local.Peer) { r.Profile = nil }},
		{"unknown-profile", ipc.ErrorCode_ERROR_CODE_NOT_FOUND, func(_ *ClientRPCService, r *ipc.ListRecentLogsRequest, _ *local.Peer) {
			r.Profile = &ipc.ProfileRef{ProfileId: "missing"}
		}},
		{"page-size", ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, func(_ *ClientRPCService, r *ipc.ListRecentLogsRequest, _ *local.Peer) {
			r.Page = &ipc.PageRequest{PageSize: 501}
		}},
		{"oversized-token", ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, func(_ *ClientRPCService, r *ipc.ListRecentLogsRequest, _ *local.Peer) {
			r.Page = &ipc.PageRequest{PageToken: strings.Repeat("x", 2049)}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			m, peer, profile := rpcConnectFixture(t)
			s := NewClientRPCService(m, nil)
			s.RecentLogsProvider = func(context.Context, string) ([]*ipc.LogEntry, error) {
				t.Fatal("provider called before admission")
				return nil, nil
			}
			req := &ipc.ListRecentLogsRequest{Profile: profile}
			test.change(s, req, &peer)
			_, err := s.recentLogsAs(t.Context(), peer, req)
			assertRPCFailure(t, err, test.code)
		})
	}
	for _, test := range []struct {
		name    string
		entries []*ipc.LogEntry
		code    ipc.ErrorCode
	}{
		{"nil-entry", []*ipc.LogEntry{nil}, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE},
		{"missing-time", []*ipc.LogEntry{{Message: "message"}}, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE},
		{"invalid-time", []*ipc.LogEntry{{Timestamp: &timestamppb.Timestamp{Seconds: 253402300800}}}, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE},
		{"invalid-text", []*ipc.LogEntry{rpcTestLog(string([]byte{255}))}, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE},
		{"unordered", []*ipc.LogEntry{rpcTestLog("new"), {Timestamp: timestamppb.New(time.Unix(99, 0)), Message: "old"}}, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE},
		{"large-message", []*ipc.LogEntry{rpcTestLog(strings.Repeat("x", maxRPCLogMessageBytes+1))}, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED},
		{"large-snapshot", make([]*ipc.LogEntry, maxRPCRecentLogs+1), ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED},
	} {
		t.Run(test.name, func(t *testing.T) {
			m, peer, profile := rpcConnectFixture(t)
			s := NewClientRPCService(m, nil)
			s.RecentLogsProvider = func(context.Context, string) ([]*ipc.LogEntry, error) { return test.entries, nil }
			_, err := s.recentLogsAs(t.Context(), peer, &ipc.ListRecentLogsRequest{Profile: profile})
			assertRPCFailure(t, err, test.code)
		})
	}
}

func TestRPCRecentLogsRechecksContextAndOwnership(t *testing.T) {
	for _, mode := range []string{"cancel-before", "cancel-during", "revision", "owner", "provider-error", "empty", "unknown-fields"} {
		t.Run(mode, func(t *testing.T) {
			m, peer, profile := rpcConnectFixture(t)
			s := NewClientRPCService(m, nil)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "cancel-before" {
				cancel()
			}
			s.RecentLogsProvider = func(context.Context, string) ([]*ipc.LogEntry, error) {
				switch mode {
				case "cancel-before":
					t.Fatal("cancelled query called provider")
				case "cancel-during":
					cancel()
				case "revision", "owner":
					if err := m.store.Update(func(cfg *Config) error {
						if mode == "owner" {
							cfg.LocalOwnerID = "replacement"
						} else {
							cfg.RPCState.Revision++
						}
						return nil
					}); err != nil {
						t.Fatal(err)
					}
				case "provider-error":
					return nil, errors.New("synthetic private provider detail")
				case "unknown-fields":
					entry := rpcTestLog("safe")
					entry.ProtoReflect().SetUnknown([]byte{0x1a, 0x06, 's', 'e', 'c', 'r', 'e', 't'})
					return []*ipc.LogEntry{entry}, nil
				}
				return nil, nil
			}
			response, err := s.recentLogsAs(ctx, peer, &ipc.ListRecentLogsRequest{Profile: profile})
			switch mode {
			case "cancel-before", "cancel-during":
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			case "revision":
				assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
			case "owner":
				assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED)
			case "provider-error":
				assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
				if strings.Contains(err.Error(), "private") {
					t.Fatal("provider error leaked")
				}
			case "empty":
				if err != nil || response.Page.Metadata == nil || len(response.Logs) != 0 {
					t.Fatal(response, err)
				}
			case "unknown-fields":
				if err != nil || !proto.Equal(response.Logs[0], rpcTestLog("safe")) {
					t.Fatal("unknown fields leaked", err)
				}
			}
		})
	}
}
