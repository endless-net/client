package client

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	"connectrpc.com/connect"
	clientapi "github.com/endless-net/client-api/clientapi/v1"
	clientrpc "github.com/endless-net/client-api/clientapi/v1/clientrpc"
	"github.com/endless-net/client-api/clientapi/v1/clientrpc/clientrpcconnect"
)

func (e *WireGuardEngine) configureFlowLocked(cfg Config, network clientapi.RegisterNodeResponse) {
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%q", []string{network.Node.ID, cfg.NodeCredential, fmt.Sprint(cfg.ControlURLs())}))))
	if e.flowCancel != nil && e.flowKey == key {
		select {
		case <-e.flowDone:
			// A terminated worker cannot service a transport replacement.
		default:
			if e.flowMark != e.routerCfg.FirewallMark || e.flowDNS != underlayDNSSourceIdentity(e.underlayDNS) || e.flowLease != e.underlayLease {
				client, err := e.newUnderlayHTTPClientLocked(cfg.ControlURLs(), e.routerCfg.FirewallMark)
				if err != nil {
					return
				}
				e.flowTransport.replace(client)
				e.flowMark = e.routerCfg.FirewallMark
				e.flowDNS = underlayDNSSourceIdentity(e.underlayDNS)
				e.flowLease = e.underlayLease
			}
			return
		}
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
	httpClient, err := e.newUnderlayHTTPClientLocked(cfg.ControlURLs(), e.routerCfg.FirewallMark)
	if err != nil {
		e.discardFlowSpoolLocked()
		return
	}
	transport := newRotatingControlClient(httpClient)
	var clients []clientrpcconnect.FlowLogServiceClient
	for _, endpoint := range cfg.ControlURLs() {
		origin, _ := rpcProfileOrigin(endpoint) // Validated by the transport constructor.
		clients = append(clients, clientrpcconnect.NewFlowLogServiceClient(transport, origin))
	}
	var spool *flowSpool
	if e.opts.FlowSpoolPath != "" {
		var err error
		spool, err = newFlowSpool(e.opts.FlowSpoolPath, cfg.NodeCredential, key)
		if err != nil {
			transport.replace(nil)
			return
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.flowCancel = cancel
	e.flowKey = key
	e.flowMark = e.routerCfg.FirewallMark
	e.flowDNS = underlayDNSSourceIdentity(e.underlayDNS)
	e.flowLease = e.underlayLease
	e.flowTransport = transport
	e.flowDone = make(chan struct{})
	done := e.flowDone
	go func() {
		defer close(done)
		defer transport.replace(nil)
		runFlowLogs(ctx, e.flows, clients, network.Node.ID, cfg.NodeCredential, spool)
	}()
}

func runFlowLogs(ctx context.Context, collector *flowCollector, clients []clientrpcconnect.FlowLogServiceClient, node, credential string, spool *flowSpool) {
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
				request := connect.NewRequest(&clientrpc.GetFlowLogPolicyRequest{NodeId: node})
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
				request := connect.NewRequest(&clientrpc.ReportFlowLogRequest{NodeId: node, ConsentVersion: version, Window: window})
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
