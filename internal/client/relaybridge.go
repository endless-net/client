package client

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	clientapi "github.com/unng-lab/endlessnet/clientapi/v1"

	relay "github.com/unng-lab/endlessnet-relay/protocol/v1"
)

type RelayDataplaneBridgeOptions struct {
	NetworkMap             clientapi.RegisterNodeResponse
	Relays                 []relay.Endpoint
	Credential             relay.Credential
	LocalEndpointBase      string
	WireGuardListenAddr    string
	Timeout                time.Duration
	TLSConfig              *tls.Config
	Dialer                 *net.Dialer
	HeartbeatInterval      time.Duration
	Ready                  func(RelayDataplaneBridgeStatus)
	MaxDatagramPayloadSize int
}

type RelayDataplaneBridgeStatus struct {
	Relay         RelayDialResult   `json:"relay"`
	PeerEndpoints map[string]string `json:"peer_endpoints"`
}

type relayDataplaneBinding struct {
	peer     clientapi.Peer
	endpoint string
	conn     *net.UDPConn
}

func RunRelayDataplaneBridge(ctx context.Context, opts RelayDataplaneBridgeOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(opts.WireGuardListenAddr) == "" {
		return errors.New("wireguard listen address is required")
	}
	relays := opts.Relays
	if len(relays) == 0 {
		relays = opts.NetworkMap.Relays
	}
	if len(relays) == 0 {
		return errors.New("relay endpoint list is empty")
	}
	credential := opts.Credential
	if strings.TrimSpace(credential.NodeID) == "" && opts.NetworkMap.RelayCredential != nil {
		credential = *opts.NetworkMap.RelayCredential
	}
	if strings.TrimSpace(credential.NodeID) == "" {
		return errors.New("relay credential is missing")
	}
	wgAddr, err := net.ResolveUDPAddr("udp", opts.WireGuardListenAddr)
	if err != nil {
		return fmt.Errorf("resolve WireGuard listen address: %w", err)
	}
	dialTimeout := opts.Timeout
	if dialTimeout <= 0 {
		dialTimeout = DefaultRelayDialTimeout
	}
	conn, relayResult, err := DialRelay(ctx, relays, credential, RelayDialOptions{
		Timeout:           dialTimeout,
		TLSConfig:         opts.TLSConfig,
		Dialer:            opts.Dialer,
		HeartbeatInterval: opts.HeartbeatInterval,
	})
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	bridgeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var bindings []relayDataplaneBinding
	if strings.TrimSpace(opts.LocalEndpointBase) == "" {
		bindings, err = listenDynamicRelayDataplaneBindings(opts.NetworkMap.Peers, wgAddr)
	} else {
		peerEndpoints, endpointErr := RelayDataplaneEndpointOverrides(opts.NetworkMap.Peers, opts.LocalEndpointBase)
		if endpointErr != nil {
			return endpointErr
		}
		bindings, err = listenRelayDataplaneBindings(opts.NetworkMap.Peers, peerEndpoints)
	}
	if err != nil {
		return err
	}
	if len(bindings) == 0 {
		return errors.New("relay dataplane bridge requires at least one peer")
	}
	defer func() {
		for _, binding := range bindings {
			_ = binding.conn.Close()
		}
	}()

	status := RelayDataplaneBridgeStatus{Relay: relayResult, PeerEndpoints: relayDataplaneBindingEndpoints(bindings)}
	if opts.Ready != nil {
		opts.Ready(status)
	}

	errCh := make(chan error, len(bindings)+1)
	var writeMu sync.Mutex
	encoder := json.NewEncoder(conn)
	maxPayload := opts.MaxDatagramPayloadSize
	if maxPayload <= 0 || maxPayload > relay.MaxFramePayloadBytes {
		maxPayload = relay.MaxFramePayloadBytes
	}
	for _, binding := range bindings {
		binding := binding
		go relayDataplaneForwardUDPToRelay(bridgeCtx, binding, maxPayload, &writeMu, encoder, errCh)
	}
	go relayDataplaneForwardRelayToUDP(bridgeCtx, conn, bindingsByPeerID(bindings), wgAddr, errCh)

	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		cancel()
		for _, binding := range bindings {
			_ = binding.conn.Close()
		}
		_ = conn.Close()
		if err == nil || relayDataplaneContextDone(ctx, err) {
			return nil
		}
		return err
	}
}

func listenDynamicRelayDataplaneBindings(peers []clientapi.Peer, wgAddr *net.UDPAddr) ([]relayDataplaneBinding, error) {
	network := "udp4"
	loopback := net.IPv4(127, 0, 0, 1)
	if wgAddr != nil && wgAddr.IP.To4() == nil {
		network = "udp6"
		loopback = net.IPv6loopback
	}
	bindings := make([]relayDataplaneBinding, 0, len(peers))
	for _, peer := range peers {
		if strings.TrimSpace(peer.ID) == "" {
			closeRelayDataplaneBindings(bindings)
			return nil, errors.New("relay dataplane peer id is missing")
		}
		conn, err := net.ListenUDP(network, &net.UDPAddr{IP: loopback})
		if err != nil {
			closeRelayDataplaneBindings(bindings)
			return nil, fmt.Errorf("listen dynamic relay dataplane endpoint for %s: %w", peer.Hostname, err)
		}
		bindings = append(bindings, relayDataplaneBinding{peer: peer, endpoint: conn.LocalAddr().String(), conn: conn})
	}
	return bindings, nil
}

func relayDataplaneBindingEndpoints(bindings []relayDataplaneBinding) map[string]string {
	out := make(map[string]string, len(bindings))
	for _, binding := range bindings {
		out[binding.peer.ID] = binding.endpoint
	}
	return out
}

func listenRelayDataplaneBindings(peers []clientapi.Peer, endpoints map[string]string) ([]relayDataplaneBinding, error) {
	bindings := make([]relayDataplaneBinding, 0, len(endpoints))
	for _, peer := range peers {
		endpoint := strings.TrimSpace(endpoints[peer.ID])
		if endpoint == "" {
			continue
		}
		addr, err := net.ResolveUDPAddr("udp", endpoint)
		if err != nil {
			closeRelayDataplaneBindings(bindings)
			return nil, fmt.Errorf("resolve relay dataplane endpoint for %s: %w", peer.Hostname, err)
		}
		conn, err := net.ListenUDP("udp", addr)
		if err != nil {
			closeRelayDataplaneBindings(bindings)
			return nil, fmt.Errorf("listen relay dataplane endpoint %s for %s: %w", endpoint, peer.Hostname, err)
		}
		bindings = append(bindings, relayDataplaneBinding{peer: peer, endpoint: endpoint, conn: conn})
	}
	return bindings, nil
}

func closeRelayDataplaneBindings(bindings []relayDataplaneBinding) {
	for _, binding := range bindings {
		_ = binding.conn.Close()
	}
}

func bindingsByPeerID(bindings []relayDataplaneBinding) map[string]relayDataplaneBinding {
	out := make(map[string]relayDataplaneBinding, len(bindings))
	for _, binding := range bindings {
		out[binding.peer.ID] = binding
	}
	return out
}

func relayDataplaneForwardUDPToRelay(ctx context.Context, binding relayDataplaneBinding, maxPayload int, writeMu *sync.Mutex, encoder *json.Encoder, errCh chan<- error) {
	buf := make([]byte, maxPayload)
	for {
		n, _, err := binding.conn.ReadFromUDP(buf)
		if err != nil {
			if relayDataplaneContextDone(ctx, err) {
				return
			}
			errCh <- fmt.Errorf("read relay dataplane UDP for %s: %w", binding.peer.Hostname, err)
			return
		}
		if n == 0 {
			continue
		}
		payload := append([]byte(nil), buf[:n]...)
		writeMu.Lock()
		err = encoder.Encode(relay.ClientFrame{Type: relay.MessageClientFrame, ProtocolVersion: relay.Version, PeerID: binding.peer.ID, Payload: payload})
		writeMu.Unlock()
		if err != nil {
			errCh <- fmt.Errorf("write relay frame for %s: %w", binding.peer.Hostname, err)
			return
		}
	}
}

func relayDataplaneForwardRelayToUDP(ctx context.Context, conn net.Conn, bindings map[string]relayDataplaneBinding, wgAddr *net.UDPAddr, errCh chan<- error) {
	reader := bufio.NewReader(conn)
	for {
		line, err := readBoundedLine(reader, MaxRelayControlLineBytes, "relay dataplane frame")
		if err != nil {
			if relayDataplaneContextDone(ctx, err) {
				return
			}
			errCh <- fmt.Errorf("read relay frame: %w", err)
			return
		}
		messageType, err := relayResponseType(line)
		if err != nil {
			errCh <- fmt.Errorf("decode relay frame envelope: %w", err)
			return
		}
		if messageType == relay.MessageError {
			var errorMessage relay.Error
			if err := relay.DecodeStrict(line, &errorMessage); err != nil || errorMessage.Type != relay.MessageError || errorMessage.ProtocolVersion != relay.Version || errorMessage.Error == "" {
				errCh <- errors.New("decode relay error response")
				return
			}
			// Relay errors after authentication describe a rejected individual
			// frame (for example, a peer that is temporarily disconnected). They
			// must not tear down the shared bridge used by every WireGuard peer.
			continue
		}
		if messageType == relay.MessageHeartbeat {
			var heartbeat relay.Heartbeat
			if err := relay.DecodeStrict(line, &heartbeat); err != nil || heartbeat.Type != relay.MessageHeartbeat || heartbeat.ProtocolVersion != relay.Version || heartbeat.Time == "" {
				errCh <- errors.New("decode relay heartbeat response")
				return
			}
			continue
		}
		if messageType != relay.MessageServerFrame {
			errCh <- fmt.Errorf("unexpected relay dataplane message %q", messageType)
			return
		}
		var frame relay.ServerFrame
		if err := relay.DecodeStrict(line, &frame); err != nil {
			errCh <- fmt.Errorf("decode relay frame: %w", err)
			return
		}
		if frame.Type != relay.MessageServerFrame || frame.ProtocolVersion != relay.Version || frame.FromNodeID == "" || frame.FromNodeID != strings.TrimSpace(frame.FromNodeID) || len(frame.Payload) == 0 || len(frame.Payload) > relay.MaxFramePayloadBytes {
			errCh <- errors.New("invalid relay frame response")
			return
		}
		binding, ok := bindings[frame.FromNodeID]
		if !ok {
			continue
		}
		if _, err := binding.conn.WriteToUDP(frame.Payload, wgAddr); err != nil {
			if relayDataplaneContextDone(ctx, err) {
				return
			}
			errCh <- fmt.Errorf("write relay datagram from %s to WireGuard: %w", binding.peer.Hostname, err)
			return
		}
	}
}

func relayDataplaneContextDone(ctx context.Context, err error) bool {
	if ctx != nil && ctx.Err() != nil {
		return true
	}
	return errors.Is(err, net.ErrClosed)
}
