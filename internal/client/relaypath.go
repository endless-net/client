package client

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	relay "github.com/endless-net/relay/protocol/v1"
)

const DefaultRelayDialTimeout = 2 * time.Second

type RelayDialOptions struct {
	Timeout           time.Duration
	TLSConfig         *tls.Config
	Dialer            *net.Dialer
	HeartbeatInterval time.Duration
}

type RelayDialAttempt struct {
	ID       string        `json:"id"`
	Addr     string        `json:"addr"`
	Protocol string        `json:"protocol"`
	Priority int           `json:"priority,omitempty"`
	Duration time.Duration `json:"duration"`
	Error    string        `json:"error,omitempty"`
}

type RelayDialResult struct {
	Selected *relay.Endpoint    `json:"selected,omitempty"`
	Attempts []RelayDialAttempt `json:"attempts"`
	Duration time.Duration      `json:"duration"`
}

type RelayWatchEvent struct {
	Type     string    `json:"type"`
	RelayID  string    `json:"relay_id,omitempty"`
	Addr     string    `json:"addr,omitempty"`
	Protocol string    `json:"protocol,omitempty"`
	Error    string    `json:"error,omitempty"`
	At       time.Time `json:"at"`
}

type RelayWatchResult struct {
	Initial     *relay.Endpoint    `json:"initial,omitempty"`
	Current     *relay.Endpoint    `json:"current,omitempty"`
	Reconnected bool               `json:"reconnected"`
	Switched    bool               `json:"switched"`
	Attempts    []RelayDialAttempt `json:"attempts"`
	Events      []RelayWatchEvent  `json:"events,omitempty"`
	Duration    time.Duration      `json:"duration"`
}

func DialRelay(ctx context.Context, endpoints []relay.Endpoint, credential relay.Credential, opts RelayDialOptions) (net.Conn, RelayDialResult, error) {
	started := time.Now()
	result := RelayDialResult{Attempts: []RelayDialAttempt{}}
	if len(endpoints) == 0 {
		return nil, result, errors.New("relay endpoint list is empty")
	}
	if err := (relay.EndpointSnapshot{Version: 1, Endpoints: endpoints}).Validate(); err != nil {
		return nil, result, fmt.Errorf("invalid relay endpoint list: %w", err)
	}
	if opts.HeartbeatInterval < 0 {
		return nil, result, errors.New("relay heartbeat interval must not be negative")
	}
	if opts.HeartbeatInterval > 0 {
		heartbeatMS := int(opts.HeartbeatInterval.Round(time.Millisecond) / time.Millisecond)
		if heartbeatMS < relay.MinHeartbeatIntervalMS || heartbeatMS > relay.MaxHeartbeatIntervalMS {
			return nil, result, errors.New("relay heartbeat interval is out of range")
		}
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultRelayDialTimeout
	}
	dialer := opts.Dialer
	if dialer == nil {
		dialer = &net.Dialer{}
	}
	var failures []string
	ordered := orderedRelayEndpoints(endpoints)
	for groupStart := 0; groupStart < len(ordered); {
		priority := RelayEndpointPriority(ordered[groupStart])
		groupEnd := groupStart + 1
		for groupEnd < len(ordered) && RelayEndpointPriority(ordered[groupEnd]) == priority {
			groupEnd++
		}
		candidates := measureRelayTLSGroup(ctx, dialer, ordered[groupStart:groupEnd], timeout, opts.TLSConfig)
		attemptOffset := len(result.Attempts)
		for _, candidate := range candidates {
			result.Attempts = append(result.Attempts, candidate.attempt)
			if candidate.err != nil {
				failures = append(failures, fmt.Sprintf("%s/%s: %v", candidate.endpoint.ID, candidate.endpoint.Addr, candidate.err))
			}
		}
		orderedCandidates := orderMeasuredRelayCandidates(candidates, credential.NodeID)
		for _, candidateIndex := range orderedCandidates {
			candidate := &candidates[candidateIndex]
			authStarted := time.Now()
			err := authenticateRelayConnWithHeartbeat(candidate.conn, credential, opts.HeartbeatInterval)
			result.Attempts[attemptOffset+candidateIndex].Duration += time.Since(authStarted)
			if err != nil {
				_ = candidate.conn.Close()
				candidate.conn = nil
				result.Attempts[attemptOffset+candidateIndex].Error = err.Error()
				failures = append(failures, fmt.Sprintf("%s/%s: %v", candidate.endpoint.ID, candidate.endpoint.Addr, err))
				continue
			}
			for index := range candidates {
				if index != candidateIndex && candidates[index].conn != nil {
					_ = candidates[index].conn.Close()
				}
			}
			selected := candidate.endpoint
			result.Selected = &selected
			result.Duration = time.Since(started)
			return candidate.conn, result, nil
		}
		for index := range candidates {
			if candidates[index].conn != nil {
				_ = candidates[index].conn.Close()
			}
		}
		groupStart = groupEnd
	}
	result.Duration = time.Since(started)
	if len(failures) == 0 {
		return nil, result, errors.New("relay endpoint list contains no usable endpoints")
	}
	return nil, result, fmt.Errorf("all relay endpoints failed: %s", strings.Join(failures, "; "))
}

func orderedRelayEndpoints(endpoints []relay.Endpoint) []relay.Endpoint {
	ordered := append([]relay.Endpoint(nil), endpoints...)
	sort.SliceStable(ordered, func(i, j int) bool {
		left := RelayEndpointPriority(ordered[i])
		right := RelayEndpointPriority(ordered[j])
		return left < right
	})
	return ordered
}

func RelayEndpointPriority(endpoint relay.Endpoint) int {
	return endpoint.Priority
}

type measuredRelayCandidate struct {
	endpoint relay.Endpoint
	attempt  RelayDialAttempt
	conn     net.Conn
	err      error
}

func measureRelayTLSGroup(ctx context.Context, dialer *net.Dialer, endpoints []relay.Endpoint, timeout time.Duration, tlsConfig *tls.Config) []measuredRelayCandidate {
	results := make(chan struct {
		index     int
		candidate measuredRelayCandidate
	}, len(endpoints))
	for index, rawEndpoint := range endpoints {
		go func(index int, endpoint relay.Endpoint) {
			started := time.Now()
			candidate := measuredRelayCandidate{
				endpoint: endpoint,
				attempt:  RelayDialAttempt{ID: endpoint.ID, Addr: endpoint.Addr, Protocol: endpoint.Protocol, Priority: RelayEndpointPriority(endpoint)},
			}
			switch {
			case endpoint.ID == "" || endpoint.ID != strings.TrimSpace(endpoint.ID) || endpoint.Addr == "" || endpoint.Addr != strings.TrimSpace(endpoint.Addr):
				candidate.err = errors.New("relay endpoint id and addr are required")
			case endpoint.Priority < 0:
				candidate.err = errors.New("relay endpoint priority must not be negative")
			case endpoint.Protocol != relay.EndpointProtocolTLS:
				candidate.err = fmt.Errorf("unsupported relay protocol %q", endpoint.Protocol)
			default:
				attemptCtx, cancel := context.WithTimeout(ctx, timeout)
				candidate.conn, candidate.err = dialTLSRelayEndpoint(attemptCtx, dialer, endpoint.Addr, tlsConfig)
				cancel()
			}
			candidate.attempt.Duration = time.Since(started)
			if candidate.err != nil {
				candidate.attempt.Error = candidate.err.Error()
			}
			results <- struct {
				index     int
				candidate measuredRelayCandidate
			}{index: index, candidate: candidate}
		}(index, rawEndpoint)
	}
	candidates := make([]measuredRelayCandidate, len(endpoints))
	for range endpoints {
		measured := <-results
		candidates[measured.index] = measured.candidate
	}
	return candidates
}

func orderMeasuredRelayCandidates(candidates []measuredRelayCandidate, nodeID string) []int {
	ordered := make([]int, 0, len(candidates))
	var minimumRTT time.Duration
	for index, candidate := range candidates {
		if candidate.err != nil || candidate.conn == nil {
			continue
		}
		ordered = append(ordered, index)
		if minimumRTT == 0 || candidate.attempt.Duration < minimumRTT {
			minimumRTT = candidate.attempt.Duration
		}
	}
	const rendezvousRTTWindow = 10 * time.Millisecond
	sort.SliceStable(ordered, func(i, j int) bool {
		left := candidates[ordered[i]]
		right := candidates[ordered[j]]
		leftNear := left.attempt.Duration < minimumRTT+rendezvousRTTWindow
		rightNear := right.attempt.Duration < minimumRTT+rendezvousRTTWindow
		if leftNear != rightNear {
			return leftNear
		}
		if leftNear {
			leftScore := relayRendezvousScore(nodeID, left.endpoint.ID)
			rightScore := relayRendezvousScore(nodeID, right.endpoint.ID)
			if leftScore != rightScore {
				return leftScore > rightScore
			}
		}
		if left.attempt.Duration != right.attempt.Duration {
			return left.attempt.Duration < right.attempt.Duration
		}
		return left.endpoint.ID < right.endpoint.ID
	})
	return ordered
}

func relayRendezvousScore(nodeID, relayID string) uint64 {
	sum := sha256.Sum256([]byte(strings.TrimSpace(nodeID) + "\x00" + strings.TrimSpace(relayID)))
	return binary.BigEndian.Uint64(sum[:8])
}

func dialTLSRelayEndpoint(ctx context.Context, dialer *net.Dialer, addr string, tlsConfig *tls.Config) (net.Conn, error) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS13}
	if tlsConfig != nil {
		cfg = tlsConfig.Clone()
		if cfg.MinVersion == 0 || cfg.MinVersion < tls.VersionTLS13 {
			cfg.MinVersion = tls.VersionTLS13
		}
	}
	if cfg.ServerName == "" {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		cfg.ServerName = host
	}
	raw, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	conn := tls.Client(raw, cfg)
	if err := conn.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func authenticateRelayConnWithHeartbeat(conn net.Conn, credential relay.Credential, heartbeatInterval time.Duration) error {
	if err := conn.SetDeadline(time.Now().Add(DefaultRelayDialTimeout)); err != nil {
		return err
	}
	defer func() {
		_ = conn.SetDeadline(time.Time{})
	}()
	auth := relay.ClientHello{Type: relay.MessageClientHello, ProtocolVersion: relay.Version, Credential: credential}
	if heartbeatInterval > 0 {
		auth.HeartbeatIntervalMS = int(heartbeatInterval.Round(time.Millisecond) / time.Millisecond)
		if auth.HeartbeatIntervalMS < relay.MinHeartbeatIntervalMS || auth.HeartbeatIntervalMS > relay.MaxHeartbeatIntervalMS {
			return errors.New("relay heartbeat interval is out of range")
		}
	}
	if err := json.NewEncoder(conn).Encode(auth); err != nil {
		return err
	}
	line, err := readBoundedLine(bufio.NewReader(conn), MaxRelayControlLineBytes, "relay control line")
	if err != nil {
		return err
	}
	messageType, err := relayResponseType(line)
	if err != nil {
		return err
	}
	switch messageType {
	case relay.MessageError:
		var message relay.Error
		if err := relay.DecodeStrict(line, &message); err != nil {
			return err
		}
		if message.ProtocolVersion != relay.Version || message.Type != relay.MessageError || message.Error == "" {
			return errors.New("invalid relay error response")
		}
		return errors.New(message.Error)
	case relay.MessageReady:
		var ready relay.Ready
		if err := relay.DecodeStrict(line, &ready); err != nil {
			return err
		}
		if ready.ProtocolVersion != relay.Version || ready.Type != relay.MessageReady || !ready.Ready {
			return errors.New("invalid relay ready response")
		}
		return nil
	default:
		return fmt.Errorf("unexpected relay authentication message %q", messageType)
	}
}

func WatchRelayFailover(ctx context.Context, endpoints []relay.Endpoint, credential relay.Credential, opts RelayDialOptions) (RelayWatchResult, error) {
	started := time.Now()
	watch := RelayWatchResult{
		Attempts: []RelayDialAttempt{},
		Events:   []RelayWatchEvent{},
	}
	heartbeatInterval := opts.HeartbeatInterval
	if heartbeatInterval <= 0 {
		heartbeatInterval = time.Second
	}
	opts.HeartbeatInterval = heartbeatInterval
	conn, selected, attempts, err := dialRelayForWatch(ctx, endpoints, credential, opts)
	watch.Attempts = append(watch.Attempts, attempts...)
	if err != nil {
		watch.Duration = time.Since(started)
		return watch, err
	}
	defer func() {
		if conn != nil {
			_ = conn.Close()
		}
	}()
	watch.Initial = selected
	watch.Current = selected
	watch.Events = append(watch.Events, relayWatchEvent("connected", selected, ""))
	reader := bufio.NewReader(conn)
	for {
		if ctx.Err() != nil {
			watch.Duration = time.Since(started)
			return watch, nil
		}
		if err := conn.SetReadDeadline(time.Now().Add(relayWatchReadTimeout(heartbeatInterval))); err != nil {
			watch.Duration = time.Since(started)
			return watch, err
		}
		messageType, err := readRelayWatchMessage(reader)
		if err == nil {
			if messageType == "heartbeat" {
				watch.Events = append(watch.Events, relayWatchEvent("heartbeat", selected, ""))
			}
			continue
		}
		if ctx.Err() != nil {
			watch.Duration = time.Since(started)
			return watch, nil
		}
		watch.Events = append(watch.Events, relayWatchEvent("disconnected", selected, err.Error()))
		_ = conn.Close()
		conn, selected, attempts, err = dialRelayForWatch(ctx, endpoints, credential, opts)
		watch.Attempts = append(watch.Attempts, attempts...)
		if err != nil {
			watch.Duration = time.Since(started)
			return watch, err
		}
		watch.Reconnected = true
		if watch.Current == nil || watch.Current.ID != selected.ID || watch.Current.Addr != selected.Addr || watch.Current.Protocol != selected.Protocol {
			watch.Switched = true
		}
		watch.Current = selected
		watch.Events = append(watch.Events, relayWatchEvent("reconnected", selected, ""))
		watch.Duration = time.Since(started)
		return watch, nil
	}
}

func dialRelayForWatch(ctx context.Context, endpoints []relay.Endpoint, credential relay.Credential, opts RelayDialOptions) (net.Conn, *relay.Endpoint, []RelayDialAttempt, error) {
	conn, result, err := DialRelay(ctx, endpoints, credential, opts)
	if err != nil {
		return nil, nil, result.Attempts, err
	}
	if result.Selected == nil {
		if conn != nil {
			_ = conn.Close()
		}
		return nil, nil, result.Attempts, errors.New("relay did not select an endpoint")
	}
	selected := *result.Selected
	return conn, &selected, result.Attempts, nil
}

func readRelayWatchMessage(reader *bufio.Reader) (string, error) {
	line, err := readBoundedLine(reader, MaxRelayControlLineBytes, "relay watch line")
	if err != nil {
		return "", err
	}
	messageType, err := relayResponseType(line)
	if err != nil {
		return "", err
	}
	switch messageType {
	case relay.MessageError:
		var message relay.Error
		if err := relay.DecodeStrict(line, &message); err != nil {
			return "", err
		}
		if message.ProtocolVersion != relay.Version || message.Type != relay.MessageError || message.Error == "" {
			return "", errors.New("invalid relay error response")
		}
		return "", errors.New(message.Error)
	case relay.MessageHeartbeat:
		var heartbeat relay.Heartbeat
		if err := relay.DecodeStrict(line, &heartbeat); err != nil {
			return "", err
		}
		if heartbeat.ProtocolVersion != relay.Version || heartbeat.Type != relay.MessageHeartbeat || heartbeat.Time == "" {
			return "", errors.New("invalid relay heartbeat response")
		}
		return "heartbeat", nil
	default:
		return "", fmt.Errorf("unexpected relay watch message %q", messageType)
	}
}

func relayResponseType(raw []byte) (string, error) {
	var envelope struct {
		Type            string `json:"type"`
		ProtocolVersion int    `json:"protocol_version"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return "", err
	}
	if envelope.Type == "" || envelope.ProtocolVersion != relay.Version {
		return "", errors.New("unsupported relay response protocol")
	}
	return envelope.Type, nil
}

func relayWatchReadTimeout(heartbeatInterval time.Duration) time.Duration {
	timeout := heartbeatInterval * 3
	if timeout < time.Second {
		return time.Second
	}
	if timeout > 10*time.Second {
		return 10 * time.Second
	}
	return timeout
}

func relayWatchEvent(eventType string, endpoint *relay.Endpoint, err string) RelayWatchEvent {
	event := RelayWatchEvent{
		Type:  eventType,
		Error: err,
		At:    time.Now().UTC(),
	}
	if endpoint != nil {
		event.RelayID = endpoint.ID
		event.Addr = endpoint.Addr
		event.Protocol = endpoint.Protocol
	}
	return event
}

func NewRelayTLSConfig(rootCAs *x509.CertPool) *tls.Config {
	return &tls.Config{
		RootCAs:    rootCAs,
		MinVersion: tls.VersionTLS13,
	}
}
