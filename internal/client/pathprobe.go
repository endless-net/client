package client

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	clientapi "github.com/endless-net/client-api/clientapi/v1"

	"golang.org/x/crypto/curve25519"
)

func peerDirectEndpoints(peer clientapi.Peer) []string {
	seen := map[string]bool{}
	endpoints := make([]string, 0, len(peer.EndpointCandidates)+1)
	for _, endpoint := range orderedPeerEndpointCandidates(peer) {
		endpoint = strings.TrimSpace(endpoint)
		if endpoint == "" || seen[endpoint] {
			continue
		}
		host, port, err := net.SplitHostPort(endpoint)
		if err != nil || strings.TrimSpace(host) == "" {
			continue
		}
		parsedPort, err := strconv.Atoi(port)
		if err != nil || parsedPort <= 0 || parsedPort > 65535 {
			continue
		}
		seen[endpoint] = true
		endpoints = append(endpoints, endpoint)
	}
	return endpoints
}

const (
	pathProbeRequestType   = byte(1)
	pathProbeResponseType  = byte(2)
	pathProbePacketSize    = 8 + 1 + 32 + 16 + 8 + sha256.Size
	pathProbeFreshness     = 30 * time.Second
	pathProbeAttempts      = 3
	maxPathProbeCandidates = 32
)

var pathProbeMagic = [8]byte{'E', 'N', 'D', 'L', 'P', 'T', 'H', 1}

type DirectEndpointProbe struct {
	PeerID           string
	Endpoint         string
	ObservedEndpoint string
	Reachable        bool
	RTT              time.Duration
	CheckedAt        time.Time
	Error            string
}

type magicBindPathProbeWaiter struct {
	peerPublic [32]byte
	key        [32]byte
	response   chan magicBindPathProbePacket
}

type magicBindPathProbePacket struct {
	remote *net.UDPAddr
}

// ConfigurePathProbing derives a distinct authenticated discovery key for
// every signed WireGuard peer. Probe packets prove possession of the peer's
// WireGuard private key before the data path is moved away from relay.
func (b *MagicBind) ConfigurePathProbing(privateKey, publicKey string, peers []clientapi.Peer) error {
	privateRaw, err := decodePathProbeKey(privateKey)
	if err != nil {
		return fmt.Errorf("path probe private key: %w", err)
	}
	publicRaw, err := decodePathProbeKey(publicKey)
	if err != nil {
		return fmt.Errorf("path probe public key: %w", err)
	}
	derivedPublic, err := curve25519.X25519(privateRaw[:], curve25519.Basepoint)
	if err != nil {
		return fmt.Errorf("derive path probe public key: %w", err)
	}
	if subtle.ConstantTimeCompare(derivedPublic, publicRaw[:]) != 1 {
		return errors.New("path probe private key does not match node public key")
	}

	keys := make(map[[32]byte][32]byte, len(peers))
	for _, peer := range peers {
		peerPublic, decodeErr := decodePathProbeKey(peer.PublicKey)
		if decodeErr != nil {
			return fmt.Errorf("path probe peer %s public key: %w", firstNonEmptyString(peer.Hostname, peer.ID), decodeErr)
		}
		shared, deriveErr := curve25519.X25519(privateRaw[:], peerPublic[:])
		if deriveErr != nil {
			return fmt.Errorf("derive path probe key for %s: %w", firstNonEmptyString(peer.Hostname, peer.ID), deriveErr)
		}
		keys[peerPublic] = derivePathProbeKey(shared)
	}

	b.mu.Lock()
	b.pathKeys = keys
	b.pathLocalPublic = publicRaw
	b.pathProbeEnabled = true
	for nonce := range b.pathWaiters {
		delete(b.pathWaiters, nonce)
	}
	b.mu.Unlock()
	return nil
}

func decodePathProbeKey(value string) ([32]byte, error) {
	var out [32]byte
	raw, err := base64.StdEncoding.Strict().DecodeString(strings.TrimSpace(value))
	if err != nil {
		return out, err
	}
	if len(raw) != len(out) {
		return out, fmt.Errorf("key is %d bytes, want %d", len(raw), len(out))
	}
	copy(out[:], raw)
	return out, nil
}

func derivePathProbeKey(shared []byte) [32]byte {
	mac := hmac.New(sha256.New, shared)
	_, _ = mac.Write([]byte("endlessnet/path-probe/v1"))
	var out [32]byte
	copy(out[:], mac.Sum(nil))
	return out
}

func (b *MagicBind) dispatchPathProbe(payload []byte, remote *net.UDPAddr, socket *net.UDPConn) bool {
	if len(payload) < len(pathProbeMagic) || subtle.ConstantTimeCompare(payload[:len(pathProbeMagic)], pathProbeMagic[:]) != 1 {
		return false
	}
	if len(payload) != pathProbePacketSize || remote == nil || socket == nil {
		return true
	}
	messageType := payload[8]
	var senderPublic [32]byte
	copy(senderPublic[:], payload[9:41])
	var nonce [16]byte
	copy(nonce[:], payload[41:57])
	timestamp := int64(binary.BigEndian.Uint64(payload[57:65]))

	b.mu.RLock()
	key, knownPeer := b.pathKeys[senderPublic]
	localPublic := b.pathLocalPublic
	enabled := b.pathProbeEnabled
	waiter, waiting := b.pathWaiters[nonce]
	b.mu.RUnlock()
	if !enabled || !knownPeer || !verifyPathProbePacket(payload, key) {
		return true
	}

	switch messageType {
	case pathProbeRequestType:
		sentAt := time.UnixMilli(timestamp)
		age := time.Since(sentAt)
		if age < -pathProbeFreshness || age > pathProbeFreshness {
			return true
		}
		response := buildPathProbePacket(pathProbeResponseType, localPublic, nonce, timestamp, key)
		_, _ = socket.WriteToUDP(response, remote)
	case pathProbeResponseType:
		if !waiting || waiter.peerPublic != senderPublic || waiter.key != key {
			return true
		}
		select {
		case waiter.response <- magicBindPathProbePacket{remote: remote}:
		default:
		}
	}
	return true
}

func buildPathProbePacket(messageType byte, senderPublic [32]byte, nonce [16]byte, timestamp int64, key [32]byte) []byte {
	packet := make([]byte, pathProbePacketSize)
	copy(packet[:8], pathProbeMagic[:])
	packet[8] = messageType
	copy(packet[9:41], senderPublic[:])
	copy(packet[41:57], nonce[:])
	binary.BigEndian.PutUint64(packet[57:65], uint64(timestamp))
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write(packet[:65])
	copy(packet[65:], mac.Sum(nil))
	return packet
}

func verifyPathProbePacket(packet []byte, key [32]byte) bool {
	if len(packet) != pathProbePacketSize {
		return false
	}
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write(packet[:65])
	return hmac.Equal(packet[65:], mac.Sum(nil))
}

// ProbePeerEndpoints sends authenticated discovery packets from the exact UDP
// socket used by WireGuard. Successful replies both validate the peer and open
// the relevant NAT mappings before endpoint selection changes.
func (b *MagicBind) ProbePeerEndpoints(ctx context.Context, peer clientapi.Peer, timeout time.Duration) []DirectEndpointProbe {
	endpoints := peerDirectEndpoints(peer)
	if len(endpoints) > maxPathProbeCandidates {
		endpoints = endpoints[:maxPathProbeCandidates]
	}
	results := make([]DirectEndpointProbe, len(endpoints))
	if len(endpoints) == 0 {
		return results
	}
	if timeout <= 0 {
		timeout = 750 * time.Millisecond
	}
	var wg sync.WaitGroup
	for index, endpoint := range endpoints {
		index, endpoint := index, endpoint
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[index] = b.probePeerEndpoint(ctx, peer, endpoint, timeout)
		}()
	}
	wg.Wait()
	return results
}

func (b *MagicBind) probePeerEndpoint(ctx context.Context, peer clientapi.Peer, endpoint string, timeout time.Duration) DirectEndpointProbe {
	result := DirectEndpointProbe{
		PeerID:    strings.TrimSpace(peer.ID),
		Endpoint:  strings.TrimSpace(endpoint),
		CheckedAt: time.Now().UTC(),
	}
	peerPublic, err := decodePathProbeKey(peer.PublicKey)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	remote, err := net.ResolveUDPAddr("udp", result.Endpoint)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		result.Error = err.Error()
		return result
	}

	b.mu.Lock()
	session := b.session
	key, knownPeer := b.pathKeys[peerPublic]
	localPublic := b.pathLocalPublic
	enabled := b.pathProbeEnabled
	if session == nil || !enabled || !knownPeer {
		b.mu.Unlock()
		result.Error = "authenticated path probing is not configured"
		return result
	}
	socket := session.v4
	if remote.IP.To4() == nil {
		socket = session.v6
	}
	if socket == nil {
		b.mu.Unlock()
		result.Error = fmt.Sprintf("UDP address family for %s is unavailable", result.Endpoint)
		return result
	}
	waiter := magicBindPathProbeWaiter{
		peerPublic: peerPublic,
		key:        key,
		response:   make(chan magicBindPathProbePacket, 1),
	}
	b.pathWaiters[nonce] = waiter
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.pathWaiters, nonce)
		b.mu.Unlock()
	}()

	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	retryDelay := timeout / pathProbeAttempts
	if retryDelay < 100*time.Millisecond {
		retryDelay = 100 * time.Millisecond
	}
	var lastSent time.Time
	for attempt := 0; attempt < pathProbeAttempts; attempt++ {
		timestamp := time.Now().UTC().UnixMilli()
		packet := buildPathProbePacket(pathProbeRequestType, localPublic, nonce, timestamp, key)
		lastSent = time.Now()
		if _, err := socket.WriteToUDP(packet, remote); err != nil {
			result.Error = err.Error()
			return result
		}
		timer := time.NewTimer(retryDelay)
		select {
		case response := <-waiter.response:
			timer.Stop()
			result.Reachable = true
			result.RTT = time.Since(lastSent)
			if result.RTT <= 0 {
				result.RTT = time.Microsecond
			}
			result.CheckedAt = time.Now().UTC()
			if response.remote != nil {
				result.ObservedEndpoint = response.remote.String()
			}
			return result
		case <-probeCtx.Done():
			timer.Stop()
			result.CheckedAt = time.Now().UTC()
			result.Error = fmt.Sprintf("authenticated path probe to %s timed out", result.Endpoint)
			return result
		case <-session.done:
			timer.Stop()
			result.CheckedAt = time.Now().UTC()
			result.Error = net.ErrClosed.Error()
			return result
		case <-timer.C:
		}
	}
	result.CheckedAt = time.Now().UTC()
	result.Error = fmt.Sprintf("authenticated path probe to %s timed out", result.Endpoint)
	return result
}
