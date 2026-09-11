package testcontrol

import (
	"errors"
	"time"

	rpc "github.com/endless-net/client-api/clientapi/v1/clientrpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type flowState struct {
	policy  *rpc.GetFlowLogPolicyResponse
	windows map[string]*rpc.FlowWindow
}

// SetFlowConsent grants a fixed test consent interval; no production policy is inferred.
func (s *Server) SetFlowConsent(id string, from, until time.Time) error {
	if !until.After(from) {
		return errors.New("invalid consent interval")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.nodes[id] == nil {
		return errors.New("unknown node")
	}
	if s.flows == nil {
		s.flows = map[string]*flowState{}
	}
	s.flows[id] = &flowState{policy: &rpc.GetFlowLogPolicyResponse{ConsentVersion: 1, CollectionNotBefore: timestamppb.New(from), CollectionExpiresAt: timestamppb.New(until)}, windows: map[string]*rpc.FlowWindow{}}
	return nil
}

func (s *Server) flowPolicyLocked(id string) *rpc.GetFlowLogPolicyResponse {
	f := s.flows[id]
	if f == nil {
		return &rpc.GetFlowLogPolicyResponse{}
	}
	return proto.Clone(f.policy).(*rpc.GetFlowLogPolicyResponse)
}

// RevokeFlowConsent changes the next public policy response to collection-off.
func (s *Server) RevokeFlowConsent(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.nodes[id] == nil {
		return errors.New("unknown node")
	}
	if f := s.flows[id]; f != nil {
		f.policy = &rpc.GetFlowLogPolicyResponse{}
	}
	return nil
}

// FlowReports returns captured authenticated RPC bodies, including rejected
// attempts. These are wire observations, not database or client-state access;
// Authorization headers and credentials are never captured.
func (s *Server) FlowReports() []*rpc.ReportFlowLogRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	reports := make([]*rpc.ReportFlowLogRequest, len(s.flowReports))
	for i, report := range s.flowReports {
		reports[i] = proto.Clone(report).(*rpc.ReportFlowLogRequest)
	}
	return reports
}

// LoseNextFlowAcknowledgement accepts one valid window, then returns a
// retryable RPC error instead of its acknowledgement. Acceptance remains
// observable and a retry must preserve the same immutable wire content.
func (s *Server) LoseNextFlowAcknowledgement() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loseFlowAcknowledgement = true
}

func (s *Server) acceptFlowLocked(r *rpc.ReportFlowLogRequest) error {
	f := s.flows[r.NodeId]
	w := r.Window
	now := time.Now()
	if f == nil || w == nil || r.ConsentVersion != f.policy.ConsentVersion || now.Before(f.policy.CollectionNotBefore.AsTime()) || !now.Before(f.policy.CollectionExpiresAt.AsTime()) {
		return errors.New("collection consent is not active")
	}
	if w.WindowId == "" || w.WindowStart == nil || w.WindowEnd == nil || w.WindowStart.CheckValid() != nil || w.WindowEnd.CheckValid() != nil || w.WindowStart.AsTime().Before(f.policy.CollectionNotBefore.AsTime()) || w.WindowEnd.AsTime().After(f.policy.CollectionExpiresAt.AsTime()) || w.WindowEnd.AsTime().Before(w.WindowStart.AsTime()) || w.WindowEnd.AsTime().After(now) {
		return errors.New("invalid flow window interval")
	}
	if old := f.windows[w.WindowId]; old != nil {
		if !proto.Equal(old, w) {
			return errors.New("flow retry changed the accepted window")
		}
		return nil
	}
	f.windows[w.WindowId] = proto.Clone(w).(*rpc.FlowWindow)
	s.recordLocked(Event{Kind: "flow-accepted", NodeID: r.NodeId, Path: w.WindowId})
	return nil
}
