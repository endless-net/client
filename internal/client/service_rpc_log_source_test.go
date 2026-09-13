package client

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestRPCNativeLogSourceCommittedOperationsAndProfiles(t *testing.T) {
	m, peer, profile := rpcConnectFixture(t)
	s := NewClientRPCService(m, nil)
	read := func(id string) []*ipc.LogEntry {
		t.Helper()
		result, err := s.recentLogsAs(t.Context(), peer, &ipc.ListRecentLogsRequest{Profile: &ipc.ProfileRef{ProfileId: id}})
		if err != nil {
			t.Fatal(err)
		}
		return result.Logs
	}
	first := read(profile.ProfileId)
	if len(first) != 1 || !strings.Contains(first[0].Message, "OPERATION_KIND_CREATE_PROFILE state=OPERATION_STATE_SUCCEEDED") {
		t.Fatal("default provider did not record committed creation")
	}
	request := rpcCreateRequest(t, m)
	request.ControlOrigin = "https://other.test"
	request.DisplayName = "synthetic-private-display"
	created, err := m.createProfileAs(peer, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.createProfileAs(peer, request); err != nil {
		t.Fatal(err)
	}
	if len(read(profile.ProfileId)) != 1 || len(read(created.ProfileId)) != 1 {
		t.Fatal("cross-profile records or duplicate replay logs")
	}
	if strings.Contains(read(created.ProfileId)[0].Message, "synthetic") || strings.Contains(read(created.ProfileId)[0].Message, "other.test") {
		t.Fatal("private creation fields leaked")
	}
	request.DisplayName = "conflicting replay"
	if _, err := m.createProfileAs(peer, request); err == nil {
		t.Fatal("conflicting replay accepted")
	}
	if len(read(created.ProfileId)) != 1 {
		t.Fatal("rejected mutation logged as committed")
	}
	if _, err := m.removeProfileAs(peer, &ipc.RemoveProfileRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: &ipc.ProfileRef{ProfileId: created.ProfileId}}); err != nil {
		t.Fatal(err)
	}
	deleted, err := m.recentLogsSnapshot(t.Context(), created.ProfileId)
	if err != nil || len(deleted) != 0 {
		t.Fatal("deleted profile retained logs", err)
	}
	_, err = s.recentLogsAs(t.Context(), peer, &ipc.ListRecentLogsRequest{Profile: &ipc.ProfileRef{ProfileId: created.ProfileId}})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_NOT_FOUND)
	restarted, err := NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
	if err != nil {
		t.Fatal(err)
	}
	logs, err := restarted.recentLogsSnapshot(t.Context(), profile.ProfileId)
	if err != nil || len(logs) != 0 {
		t.Fatal("volatile logs restored as durable journal", err)
	}
}

func TestRPCNativeLogSourceObservedStatusAndCancellation(t *testing.T) {
	m, peer, profile := rpcConnectFixture(t)
	s := NewClientRPCService(m, nil)
	status := &ipc.Status{Metadata: m.Metadata(), ActiveProfileId: profile.ProfileId,
		ConnectionPhase: ipc.ConnectionPhase_CONNECTION_PHASE_CONNECTING,
		Hostname:        "synthetic-private-host", PendingAction: &ipc.UserAction{BrowserUrl: "https://private.test"}}
	if err := m.PublishStatus(status); err != nil {
		t.Fatal(err)
	}
	status.Metadata = m.Metadata()
	if err := m.PublishStatus(status); err != nil {
		t.Fatal(err)
	}
	response, err := s.recentLogsAs(t.Context(), peer, &ipc.ListRecentLogsRequest{Profile: profile})
	if err != nil || len(response.GetLogs()) != 2 {
		t.Fatal("missing status or duplicate unchanged observation", err)
	}
	message := response.Logs[1].Message
	if !strings.Contains(message, "CONNECTION_PHASE_CONNECTING") || strings.Contains(message, "private") {
		t.Fatal("unsafe or missing runtime state")
	}
	stale := proto.Clone(status).(*ipc.Status)
	stale.Metadata.Revision--
	stale.ConnectionPhase = ipc.ConnectionPhase_CONNECTION_PHASE_CONNECTED
	if err := m.PublishStatus(stale); err == nil {
		t.Fatal("stale observation accepted")
	}
	logs, err := m.recentLogsSnapshot(t.Context(), profile.ProfileId)
	if err != nil || len(logs) != 2 {
		t.Fatal("stale observation logged", err)
	}
	logs[0].Message = "caller mutation"
	logs[0].Timestamp.Seconds = 0
	again, err := m.recentLogsSnapshot(t.Context(), profile.ProfileId)
	if err != nil || again[0].Message == "caller mutation" || again[0].Timestamp.Seconds == 0 {
		t.Fatal("snapshot aliases source", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := m.recentLogsSnapshot(ctx, profile.ProfileId); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestRPCNativeLogSourceBoundAndClockRollback(t *testing.T) {
	m, peer, profile := rpcConnectFixture(t)
	s := NewClientRPCService(m, nil)
	m.mu.Lock()
	m.recentLogs = nil
	m.now = func() time.Time { return time.Unix(100, 0) }
	cfg := m.store.Read()
	for i := 0; i < maxRPCRecentLogs+10; i++ {
		if i > 0 {
			m.now = func() time.Time { return time.Unix(99, 0) }
		}
		m.recordDiagnosticTransitionLocked(cfg, &ipc.Operation{ProfileId: profile.ProfileId, Kind: ipc.OperationKind_OPERATION_KIND_CONNECT,
			State:      ipc.OperationState_OPERATION_STATE_FAILED,
			UserAction: &ipc.UserAction{BrowserUrl: "https://synthetic-private.test"},
			Outcome:    &ipc.Operation_Failure{Failure: &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_UNAVAILABLE}}})
	}
	count := len(m.recentLogs)
	m.mu.Unlock()
	if count != maxRPCRecentLogs {
		t.Fatal("unbounded log history", count)
	}
	response, err := s.recentLogsAs(t.Context(), peer, &ipc.ListRecentLogsRequest{Profile: profile, Page: &ipc.PageRequest{PageSize: 500}})
	if err != nil || len(response.GetLogs()) != 500 {
		t.Fatal("bounded log snapshot failed", err)
	}
	for _, entry := range response.Logs {
		if entry.Timestamp.Seconds != 100 || strings.Contains(entry.Message, "private") || !strings.Contains(entry.Message, "ERROR_CODE_UNAVAILABLE") {
			t.Fatal("clock rollback or unsafe failure projection")
		}
	}
}
