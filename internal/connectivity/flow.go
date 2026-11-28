package connectivity

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	wire "github.com/endless-net/coordinator/api/public/v1"
	"github.com/endless-net/coordinator/api/public/v1/publicv1connect"
)

var ErrFlowUnconfirmed = errors.New("flow window delivery unconfirmed")

// FlowPolicy belongs to the verifier that received it. Delivery and collection
// must also be stopped by the connection lifetime when the intent changes.
type FlowPolicy struct {
	verifier       *Verifier
	version        uint64
	start, end     time.Time
	received       ClockSample
	opening        mapTimeAuthority
	openingReceipt ClockSample
}

func (t *Transport) flowRPC(parent context.Context, v *Verifier, call func(context.Context, publicv1connect.FlowLogServiceClient) error) error {
	if v == nil {
		return ErrConfig
	}
	if err := parent.Err(); err != nil {
		return err
	}
	select {
	case t.activeCalls <- struct{}{}:
		defer func() { <-t.activeCalls }()
	default:
		return ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(parent, t.config.CallTimeout)
	defer cancel()
	if _, err := v.currentTrust(ctx, coordinator.NodeFlowScope); err != nil {
		return err
	}
	sdk := publicv1connect.NewFlowLogServiceClient(t.calls, v.origin, connect.WithReadMaxBytes(t.config.MaxResponseBytes), connect.WithSendMaxBytes(t.config.MaxRequestBytes))
	return call(ctx, sdk)
}

func (t *Transport) ReadFlowPolicy(parent context.Context, v *Verifier) (*FlowPolicy, error) {
	var policy *FlowPolicy
	err := t.flowRPC(parent, v, func(ctx context.Context, sdk publicv1connect.FlowLogServiceClient) error {
		req := connect.NewRequest(&wire.GetFlowLogPolicyRequest{NodeId: v.node})
		req.Header().Set("X-EndlessNet-Node-Credential", v.credential)
		response, err := sdk.GetFlowLogPolicy(ctx, req)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return connectivityCallError(err, false)
		}
		if response == nil || response.Msg == nil || len(response.Msg.ProtoReflect().GetUnknown()) != 0 {
			return ErrProtocol
		}
		p := response.Msg
		if p.CollectionNotBefore == nil && p.CollectionExpiresAt == nil {
			policy = &FlowPolicy{verifier: v, version: p.ConsentVersion}
			return nil
		}
		if p.ConsentVersion == 0 || p.CollectionNotBefore.CheckValid() != nil || p.CollectionExpiresAt.CheckValid() != nil || len(p.CollectionNotBefore.ProtoReflect().GetUnknown()) != 0 || len(p.CollectionExpiresAt.ProtoReflect().GetUnknown()) != 0 {
			return ErrProtocol
		}
		if v.config.Clock == nil {
			return ErrClock
		}
		sample, sampleErr := v.config.Clock.Sample()
		if sampleErr != nil || !sample.valid() {
			return ErrClock
		}
		now := sample.Wall
		start, end := p.CollectionNotBefore.AsTime(), p.CollectionExpiresAt.AsTime()
		if !end.After(start) || end.After(v.credentialEnds) {
			return ErrProtocol
		}
		if now.Before(start) || !now.Before(end) {
			return ErrExpired
		}
		policy = &FlowPolicy{verifier: v, version: p.ConsentVersion, start: start, end: end, received: sample}
		return nil
	})
	return policy, err
}

// ReportFlowWindow submits exactly one observation. Only a matching receipt
// permits removing the window as delivered. Management arbitrates conflicts.
func (t *Transport) ReportFlowWindow(parent context.Context, v *Verifier, p *FlowPolicy, w *wire.FlowWindow) error {
	if p == nil || v == nil || p.verifier != v || p.version == 0 || p.end.IsZero() {
		return ErrDenied
	}
	if w == nil || !pathID(w.WindowId) || w.WindowStart.CheckValid() != nil || w.WindowEnd.CheckValid() != nil || !w.WindowEnd.AsTime().After(w.WindowStart.AsTime()) || w.WindowStart.AsTime().Before(p.start) || w.WindowEnd.AsTime().After(p.end) || w.Direction != wire.FlowDirection_FLOW_DIRECTION_INGRESS && w.Direction != wire.FlowDirection_FLOW_DIRECTION_EGRESS {
		return ErrProtocol
	}
	return t.reportObservation(parent, v, p, w.WindowEnd.AsTime(), func(ctx context.Context, sdk publicv1connect.FlowLogServiceClient) (bool, error) {
		req := connect.NewRequest(&wire.ReportFlowLogRequest{NodeId: v.node, ConsentVersion: p.version, Window: w})
		req.Header().Set("X-EndlessNet-Node-Credential", v.credential)
		ack, err := sdk.ReportFlowLog(ctx, req)
		return ack != nil && ack.Msg != nil && len(ack.Msg.ProtoReflect().GetUnknown()) == 0 && ack.Msg.WindowId == w.WindowId, err
	})
}

func (t *Transport) reportCoverage(parent context.Context, v *Verifier, p *FlowPolicy, report *wire.ReportFlowCoverageRequest) error {
	if p == nil || p.verifier != v || report.NodeId != v.node || report.ConsentVersion != p.version || p.end.IsZero() {
		return ErrDenied
	}
	if report.StartInclusive.AsTime().Before(p.start) || report.EndExclusive.AsTime().After(p.end) || !report.StartInclusive.AsTime().Before(report.EndExclusive.AsTime()) {
		return ErrProtocol
	}
	return t.reportObservation(parent, v, p, report.EndExclusive.AsTime(), func(ctx context.Context, sdk publicv1connect.FlowLogServiceClient) (bool, error) {
		req := connect.NewRequest(report)
		req.Header().Set("X-EndlessNet-Node-Credential", v.credential)
		ack, err := sdk.ReportFlowCoverage(ctx, req)
		return ack != nil && ack.Msg != nil && len(ack.Msg.ProtoReflect().GetUnknown()) == 0 && ack.Msg.ReportId == report.ReportId, err
	})
}

func (t *Transport) reportObservation(parent context.Context, v *Verifier, p *FlowPolicy, end time.Time, send func(context.Context, publicv1connect.FlowLogServiceClient) (bool, error)) error {
	return t.flowRPC(parent, v, func(ctx context.Context, sdk publicv1connect.FlowLogServiceClient) error {
		sample, err := v.config.Clock.Sample()
		if err != nil {
			return ErrClock
		}
		remaining := p.remaining(sample)
		if remaining <= 0 || sample.Wall.Before(p.start) || end.After(sample.Wall) {
			return ErrExpired
		}
		ctx, cancel := context.WithTimeout(ctx, remaining)
		defer cancel()
		accepted, err := send(ctx, sdk)
		if err != nil {
			if connect.IsWireError(err) {
				switch connect.CodeOf(err) {
				case connect.CodePermissionDenied, connect.CodeUnauthenticated, connect.CodeFailedPrecondition:
					return ErrDenied
				case connect.CodeAlreadyExists, connect.CodeInvalidArgument:
					return ErrProtocol
				}
			}
			return ErrFlowUnconfirmed
		}
		if ctx.Err() != nil || !accepted {
			return ErrFlowUnconfirmed
		}
		return nil
	})
}
