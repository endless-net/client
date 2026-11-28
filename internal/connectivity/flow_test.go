package connectivity

import (
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/endless-net/client/internal/testwire"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	wire "github.com/endless-net/coordinator/api/public/v1"
	"github.com/endless-net/coordinator/api/public/v1/publicv1connect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestNodeReadsBoundedFlowConsent(t *testing.T) {
	for _, tc := range []struct {
		name string
		want error
	}{
		{"active", nil}, {"disabled", nil}, {"unscoped", ErrCredentialInvalid}, {"missing-start", ErrProtocol}, {"no-version", ErrProtocol}, {"expired", ErrExpired}, {"future", ErrExpired}, {"beyond-credential", ErrProtocol},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var scopes []string
			if tc.name != "unscoped" {
				scopes = append(scopes, coordinator.NodeFlowScope)
			}
			f := newMapFixture(t, scopes...)
			v, err := NewVerifier(f.profile, f.config)
			if err != nil {
				t.Fatal(err)
			}
			p := &wire.GetFlowLogPolicyResponse{ConsentVersion: 9, CollectionNotBefore: timestamppb.New(f.now.Add(-time.Second)), CollectionExpiresAt: timestamppb.New(f.now.Add(time.Minute))}
			switch tc.name {
			case "disabled":
				p.CollectionNotBefore = nil
				p.CollectionExpiresAt = nil
			case "missing-start":
				p.CollectionNotBefore = nil
			case "no-version":
				p.ConsentVersion = 0
			case "expired":
				p.CollectionExpiresAt = timestamppb.New(f.now)
			case "future":
				p.CollectionNotBefore = timestamppb.New(f.now.Add(time.Second))
			case "beyond-credential":
				p.CollectionExpiresAt = timestamppb.New(v.credentialEnds.Add(time.Nanosecond))
			}
			calls := 0
			transport := transportFixture(t, func(r *http.Request) (*http.Response, error) {
				calls++
				assertNodeRequest(t, r, f)
				b, err := io.ReadAll(r.Body)
				req := new(wire.GetFlowLogPolicyRequest)
				if err != nil || proto.Unmarshal(b, req) != nil || req.NodeId != v.node || r.URL.Path != publicv1connect.FlowLogServiceGetFlowLogPolicyProcedure {
					t.Fatal("incorrect flow request")
				}
				return testwire.UnaryResponse(r, p)
			})
			got, err := transport.ReadFlowPolicy(t.Context(), v)
			if !errors.Is(err, tc.want) || tc.want == nil && (got == nil || got.verifier != v || got.version != p.ConsentVersion || (p.CollectionExpiresAt != nil && !got.end.Equal(p.CollectionExpiresAt.AsTime())) || (p.CollectionExpiresAt == nil && !got.end.IsZero())) || tc.want != nil && got != nil {
				t.Fatalf("policy accepted incorrectly: %v", err)
			}
			if tc.name == "unscoped" && calls != 0 || tc.name != "unscoped" && calls != 1 {
				t.Fatal("unexpected flow RPC count", calls)
			}
		})
	}
}

func TestNodeReportsOnlyItsLiveImmutableFlowWindow(t *testing.T) {
	f := newMapFixture(t, coordinator.NodeFlowScope)
	v, err := NewVerifier(f.profile, f.config)
	if err != nil {
		t.Fatal(err)
	}
	w := &wire.FlowWindow{WindowId: "id-1", Source: "10.0.0.1", Destination: "10.0.0.2", DestinationPort: 443, Protocol: "tcp", Decision: "deny", Direction: wire.FlowDirection_FLOW_DIRECTION_INGRESS, Bytes: proto.Uint64(120), Packets: proto.Uint64(2), WindowStart: timestamppb.New(f.now), WindowEnd: timestamppb.New(f.now.Add(time.Second))}
	p := &wire.GetFlowLogPolicyResponse{ConsentVersion: 7, CollectionNotBefore: timestamppb.New(f.now), CollectionExpiresAt: timestamppb.New(f.now.Add(time.Minute))}
	reports := 0
	transport := transportFixture(t, func(r *http.Request) (*http.Response, error) {
		assertNodeRequest(t, r, f)
		if r.URL.Path == publicv1connect.FlowLogServiceGetFlowLogPolicyProcedure {
			return testwire.UnaryResponse(r, p)
		}
		b, err := io.ReadAll(r.Body)
		req := new(wire.ReportFlowLogRequest)
		if err != nil || proto.Unmarshal(b, req) != nil || req.NodeId != v.node || req.ConsentVersion != 7 || !proto.Equal(req.Window, w) || r.URL.Path != publicv1connect.FlowLogServiceReportFlowLogProcedure {
			t.Fatal("window changed across report")
		}
		reports++
		switch reports {
		case 1:
			return nil, io.ErrUnexpectedEOF
		case 2:
			return testwire.UnaryResponse(r, &wire.ReportFlowLogResponse{WindowId: "other"})
		}
		return testwire.UnaryResponse(r, &wire.ReportFlowLogResponse{WindowId: w.WindowId})
	})
	grant, err := transport.ReadFlowPolicy(t.Context(), v)
	if err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Second)
	other, err := NewVerifier(f.profile, f.config)
	if err != nil {
		t.Fatal(err)
	}
	if err := transport.ReportFlowWindow(t.Context(), other, grant, w); !errors.Is(err, ErrDenied) {
		t.Fatal("grant transferred to another verifier", err)
	}
	for i, want := range []error{ErrFlowUnconfirmed, ErrFlowUnconfirmed, nil} {
		if err := transport.ReportFlowWindow(t.Context(), v, grant, w); !errors.Is(err, want) || reports != i+1 {
			t.Fatal("incorrect window receipt or retry", err, reports)
		}
	}
	f.now = p.CollectionExpiresAt.AsTime()
	if err := transport.ReportFlowWindow(t.Context(), v, grant, w); !errors.Is(err, ErrExpired) || reports != 3 {
		t.Fatal("expired report sent", err, reports)
	}
}
