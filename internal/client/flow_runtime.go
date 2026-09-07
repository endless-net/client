package client

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/url"
	"time"

	"connectrpc.com/connect"
	clientapi "github.com/endless-net/client-api/clientapi/v1"
	coordinatorapi "github.com/endless-net/coordinator/coordinatorapi/v1"
	"github.com/endless-net/coordinator/coordinatorapi/v1/coordinatorapiconnect"
)

func (e *WireGuardEngine) configureFlowLocked(cfg Config, network clientapi.RegisterNodeResponse) {
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%q", []string{network.Node.ID, cfg.NodeCredential, fmt.Sprint(cfg.ControlURLs())}))))
	if e.flowCancel != nil && e.flowKey == key {
		return
	}
	if e.flowCancel != nil {
		e.flowCancel()
		<-e.flowDone
		e.flowCancel = nil
	}
	e.flows.stop()
	if cfg.NodeCredential == "" || network.Node.ID == "" {
		e.discardFlowSpoolLocked()
		return
	}
	var clients []coordinatorapiconnect.FlowLogServiceClient
	for _, endpoint := range cfg.ControlURLs() {
		base, err := url.Parse(endpoint)
		if err != nil || base.Host == "" || base.User != nil || base.Scheme != "https" {
			continue
		}
		base.Path, base.RawPath, base.RawQuery, base.Fragment = "", "", "", ""
		clients = append(clients, coordinatorapiconnect.NewFlowLogServiceClient(applicationHTTPClient(), base.String()))
	}
	if len(clients) == 0 {
		e.discardFlowSpoolLocked()
		return
	}
	var spool *flowSpool
	if e.opts.FlowSpoolPath != "" {
		var err error
		spool, err = newFlowSpool(e.opts.FlowSpoolPath, cfg.NodeCredential, key)
		if err != nil {
			return
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.flowCancel = cancel
	e.flowKey = key
	e.flowDone = make(chan struct{})
	done := e.flowDone
	go func() {
		defer close(done)
		runFlowLogs(ctx, e.flows, clients, network.Node.ID, cfg.NodeCredential, spool)
	}()
}

func runFlowLogs(ctx context.Context, collector *flowCollector, clients []coordinatorapiconnect.FlowLogServiceClient, node, credential string, spool *flowSpool) {
	persistence, err := openFlowPersistence(spool, collector, time.Now())
	if err != nil {
		collector.stop()
		logFlowStatus(slog.Default(), collector.status(time.Now()))
		return
	}
	defer func() { _ = persistence.purge(collector); logFlowStatus(slog.Default(), collector.status(time.Now())) }()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var nextPolicy, nextSend time.Time
	var nextStatus time.Time
	var previousStatus FlowLogStatus
	delay := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		now := time.Now()
		if !now.Before(nextPolicy) {
			for _, client := range clients {
				request := connect.NewRequest(&coordinatorapi.GetFlowLogPolicyRequest{NodeId: node})
				request.Header().Set("Authorization", "Bearer "+credential)
				call, cancel := context.WithTimeout(ctx, 5*time.Second)
				response, err := client.GetFlowLogPolicy(call, request)
				cancel()
				if ctx.Err() != nil {
					return
				}
				if err == nil {
					collector.policy(response.Msg, time.Now())
					persistence.acceptPolicy(collector, time.Now())
					break
				}
				collector.policyFailed()
				if terminalFlowError(err) {
					if persistence.purge(collector) != nil {
						return
					}
					break
				}
			}
			nextPolicy = time.Now().Add(15 * time.Second)
		}
		for range 16 {
			window, version := collector.next(time.Now())
			if window == nil || time.Now().Before(nextSend) {
				break
			}
			if persistence.checkpoint(collector, time.Now()) != nil {
				return
			}
			current, currentVersion := collector.next(time.Now())
			if current == nil || currentVersion != version || current.GetWindowId() != window.GetWindowId() {
				break
			}
			acknowledged := false
			for _, client := range clients {
				request := connect.NewRequest(&coordinatorapi.ReportFlowLogRequest{NodeId: node, ConsentVersion: version, Window: window})
				request.Header().Set("Authorization", "Bearer "+credential)
				call, cancel := context.WithTimeout(ctx, 5*time.Second)
				response, err := client.ReportFlowLog(call, request)
				cancel()
				collector.reportResult(err == nil && response.Msg.GetWindowId() == window.GetWindowId())
				if ctx.Err() != nil {
					return
				}
				if err == nil && response.Msg.GetWindowId() == window.GetWindowId() {
					collector.acknowledge(window.GetWindowId(), version)
					if persistence.checkpoint(collector, time.Now()) != nil {
						return
					}
					acknowledged = true
					break
				}
				if terminalFlowError(err) {
					if persistence.purge(collector) != nil {
						return
					}
					nextPolicy = time.Time{}
					break
				}
			}
			if !acknowledged {
				nextSend = time.Now().Add(delay + time.Duration(rand.Int64N(int64(delay/2))))
				delay = min(delay*2, 15*time.Second)
				break
			}
			delay = time.Second
			nextSend = time.Time{}
		}
		if persistence.checkpoint(collector, time.Now()) != nil {
			return
		}
		if !time.Now().Before(nextStatus) {
			status := collector.status(time.Now())
			if status != previousStatus {
				logFlowStatus(slog.Default(), status)
				previousStatus = status
			}
			nextStatus = time.Now().Add(15 * time.Second)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func terminalFlowError(err error) bool {
	switch connect.CodeOf(err) {
	case connect.CodeUnauthenticated, connect.CodePermissionDenied, connect.CodeFailedPrecondition, connect.CodeInvalidArgument, connect.CodeAlreadyExists:
		return true
	}
	return false
}
