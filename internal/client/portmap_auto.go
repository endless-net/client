package client

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"runtime"
	"strings"
	"time"
)

const (
	automaticPortMappingLifetime = 2 * time.Minute
	automaticPortMappingRetry    = 5 * time.Minute
	automaticPortMappingTimeout  = 750 * time.Millisecond
)

type automaticPortMappingState struct {
	port      int
	result    PortMappingResult
	refreshAt time.Time
	expiresAt time.Time
	retryAt   time.Time
	attempts  []PortMappingResult
}

func (e *WireGuardEngine) refreshAutomaticPortMapping(ctx context.Context, stunResults []STUNCheckResult, port int, timeout time.Duration) []PortMappingResult {
	if port <= 0 || port > 65535 {
		return nil
	}
	now := time.Now().UTC()
	e.mu.Lock()
	state := e.portMapping
	if state.port == port && state.result.OK && now.Before(state.refreshAt) && now.Before(state.expiresAt) {
		result := clonePortMappingResult(state.result)
		e.mu.Unlock()
		return []PortMappingResult{result}
	}
	if state.port == port && now.Before(state.retryAt) {
		results := automaticPortMappingResults(state, now)
		e.mu.Unlock()
		return results
	}
	runner := e.opts.Runner
	e.mu.Unlock()

	mappingTimeout := automaticPortMappingTimeout
	if timeout > 0 && timeout < mappingTimeout {
		mappingTimeout = timeout
	}
	if mappingTimeout < 250*time.Millisecond {
		mappingTimeout = 250 * time.Millisecond
	}
	discoveryCtx, cancelDiscovery := context.WithTimeout(ctx, mappingTimeout)
	destinations := publicSTUNDestinations(discoveryCtx, stunResults)
	cancelDiscovery()
	if len(destinations) == 0 {
		return nil
	}

	gateways := []string{}
	seenGateways := map[string]bool{}
	attempts := []PortMappingResult{}
	for _, destination := range destinations {
		routeCtx, cancelRoute := context.WithTimeout(ctx, mappingTimeout)
		gateway, err := routeGatewayForDestination(routeCtx, destination, runner)
		cancelRoute()
		if err != nil {
			attempts = append(attempts, PortMappingResult{Protocol: "auto", Error: err.Error()})
			continue
		}
		if gateway == "" || seenGateways[gateway] {
			continue
		}
		seenGateways[gateway] = true
		gateways = append(gateways, gateway)
	}
	for _, gateway := range gateways {
		server := net.JoinHostPort(gateway, "5351")
		request := PortMappingRequest{
			Gateway:      server,
			InternalPort: port,
			ExternalPort: port,
			Lifetime:     automaticPortMappingLifetime,
			Timeout:      mappingTimeout,
		}
		if state.port == port && state.result.Protocol == "pcp" && state.result.Gateway == server {
			request.ExternalPort = state.result.ExternalPort
			request.pcpNonce = append([]byte(nil), state.result.pcpNonce...)
		}
		pcp := MapPCP(ctx, request)
		attempts = append(attempts, pcp)
		if pcp.OK {
			return e.storeAutomaticPortMapping(port, pcp, attempts, now)
		}
		natPMP := MapNATPMP(ctx, request)
		attempts = append(attempts, natPMP)
		if natPMP.OK {
			return e.storeAutomaticPortMapping(port, natPMP, attempts, now)
		}
	}

	e.mu.Lock()
	if e.portMapping.port != port {
		e.portMapping = automaticPortMappingState{port: port}
	}
	e.portMapping.attempts = clonePortMappingResults(attempts)
	e.portMapping.retryAt = now.Add(automaticPortMappingRetry)
	if e.portMapping.result.OK && now.Before(e.portMapping.expiresAt) && e.portMapping.expiresAt.Before(e.portMapping.retryAt) {
		e.portMapping.retryAt = e.portMapping.expiresAt
	}
	results := automaticPortMappingResults(e.portMapping, now)
	e.mu.Unlock()
	return results
}

func (e *WireGuardEngine) storeAutomaticPortMapping(port int, result PortMappingResult, attempts []PortMappingResult, now time.Time) []PortMappingResult {
	lifetime := time.Duration(result.LifetimeSeconds) * time.Second
	if lifetime <= 0 {
		lifetime = automaticPortMappingLifetime
	}
	state := automaticPortMappingState{
		port:      port,
		result:    clonePortMappingResult(result),
		refreshAt: now.Add(lifetime / 2),
		expiresAt: now.Add(lifetime),
		attempts:  clonePortMappingResults(attempts),
	}
	e.mu.Lock()
	e.portMapping = state
	e.mu.Unlock()
	return automaticPortMappingResults(state, now)
}

func automaticPortMappingResults(state automaticPortMappingState, now time.Time) []PortMappingResult {
	results := clonePortMappingResults(state.attempts)
	if state.result.OK && now.Before(state.expiresAt) {
		for _, result := range results {
			if result.OK && result.Protocol == state.result.Protocol && result.Gateway == state.result.Gateway && result.MappedEndpoint == state.result.MappedEndpoint {
				return results
			}
		}
		results = append([]PortMappingResult{clonePortMappingResult(state.result)}, results...)
	}
	return results
}

func clonePortMappingResults(values []PortMappingResult) []PortMappingResult {
	out := make([]PortMappingResult, len(values))
	for index, value := range values {
		out[index] = clonePortMappingResult(value)
	}
	return out
}

func clonePortMappingResult(value PortMappingResult) PortMappingResult {
	value.pcpNonce = append([]byte(nil), value.pcpNonce...)
	return value
}

func publicSTUNDestinations(ctx context.Context, results []STUNCheckResult) []netip.Addr {
	out := []netip.Addr{}
	seen := map[netip.Addr]bool{}
	for _, result := range results {
		host, _, err := net.SplitHostPort(strings.TrimSpace(result.Addr))
		if err != nil {
			continue
		}
		if addr, parseErr := netip.ParseAddr(host); parseErr == nil {
			addr = addr.Unmap()
			if usablePortMappingDestination(addr) && !seen[addr] {
				seen[addr] = true
				out = append(out, addr)
			}
			continue
		}
		resolved, resolveErr := net.DefaultResolver.LookupNetIP(ctx, "ip4", host)
		if resolveErr != nil {
			continue
		}
		for _, addr := range resolved {
			addr = addr.Unmap()
			if usablePortMappingDestination(addr) && !seen[addr] {
				seen[addr] = true
				out = append(out, addr)
			}
		}
	}
	return out
}

func usablePortMappingDestination(addr netip.Addr) bool {
	return addr.Is4() && addr.IsGlobalUnicast() && !addr.IsPrivate() && !addr.IsLoopback() && !addr.IsLinkLocalUnicast()
}

func routeGatewayForDestination(ctx context.Context, destination netip.Addr, runner CommandRunner) (string, error) {
	destination = destination.Unmap()
	if !usablePortMappingDestination(destination) {
		return "", errors.New("public IPv4 destination is required for port-mapping gateway discovery")
	}
	if runner == nil {
		runner = runCommand
	}
	var output []byte
	var err error
	switch runtime.GOOS {
	case "windows":
		script := strings.Join([]string{
			"$ErrorActionPreference='Stop'",
			fmt.Sprintf("$route=Find-NetRoute -RemoteIPAddress '%s' | Select-Object -First 1", destination),
			"if ($null -eq $route) { throw 'route not found' }",
			"$hop=[string]$route.NextHop",
			"if ($hop -eq '0.0.0.0') { $default=Get-NetRoute -AddressFamily IPv4 -InterfaceIndex $route.InterfaceIndex -DestinationPrefix '0.0.0.0/0' | Sort-Object RouteMetric | Select-Object -First 1; if ($null -ne $default) { $hop=[string]$default.NextHop } }",
			"[Console]::Out.Write($hop)",
		}, "; ")
		output, err = runner(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	case "linux":
		output, err = runner(ctx, "ip", "-4", "route", "get", destination.String())
	case "darwin":
		output, err = runner(ctx, "route", "-n", "get", destination.String())
	default:
		return "", fmt.Errorf("automatic port-mapping gateway discovery is unsupported on %s", runtime.GOOS)
	}
	if err != nil {
		return "", fmt.Errorf("discover route gateway for %s: %s", destination, commandError(err, output))
	}
	gateway := parseRouteGateway(runtime.GOOS, string(output))
	if gateway == "" {
		return "", fmt.Errorf("route to %s does not use an IPv4 gateway", destination)
	}
	return gateway, nil
}

func parseRouteGateway(goos, output string) string {
	if goos == "windows" {
		for _, field := range strings.Fields(output) {
			if addr, err := netip.ParseAddr(strings.TrimSpace(field)); err == nil && usableGatewayAddress(addr) {
				return addr.Unmap().String()
			}
		}
		return ""
	}
	fields := strings.Fields(output)
	for index, field := range fields {
		if goos == "linux" && field == "via" && index+1 < len(fields) {
			if addr, err := netip.ParseAddr(strings.TrimSpace(fields[index+1])); err == nil && usableGatewayAddress(addr) {
				return addr.Unmap().String()
			}
		}
		if goos == "darwin" && strings.TrimSuffix(field, ":") == "gateway" && index+1 < len(fields) {
			if addr, err := netip.ParseAddr(strings.TrimSpace(fields[index+1])); err == nil && usableGatewayAddress(addr) {
				return addr.Unmap().String()
			}
		}
	}
	return ""
}

func usableGatewayAddress(addr netip.Addr) bool {
	addr = addr.Unmap()
	return addr.Is4() && addr.IsGlobalUnicast() && !addr.IsUnspecified() && !addr.IsLoopback() && !addr.IsMulticast()
}
