package dataplane

import (
	"bytes"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	awgconn "github.com/amnezia-vpn/amneziawg-go/v3/conn"
	"github.com/endless-net/client/api/transport"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

type relayCryptoFixtureBind struct {
	*cipherFixtureBind
	emit func([]byte)
}

func (b *relayCryptoFixtureBind) Send(packets [][]byte, _ awgconn.Endpoint) error {
	for _, raw := range packets {
		b.emit(raw)
	}
	return nil
}

func TestActualRelayWireGuardControlAndApplicationStaySeparatedFromDirect(t *testing.T) {
	mux, f, sent, inject := relayMuxFixture(t)
	var applications atomic.Int32
	channel, err := newRelayCipher(t.Context(), mux, f.verified, f.lease, "peer", f.profile.Keys.WireGuardPrivate, 576, func([]byte) error { applications.Add(1); return nil })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(channel.Close)
	for _, rendered := range []string{fmt.Sprint(channel), fmt.Sprintf("%#v", channel)} {
		if rendered != "[encrypted Relay peer channel]" {
			t.Fatal("unsafe Relay cipher formatting")
		}
	}
	a := f.assignment(t)
	localIP, peerIP, err := a.addresses()
	if err != nil {
		t.Fatal(err)
	}
	wire := &memoryWireBind{address: a.peer.Endpoint}
	peerBind := &relayCryptoFixtureBind{cipherFixtureBind: &cipherFixtureBind{memoryWireBind: wire}, emit: func(raw []byte) { inject("peer", raw) }}
	var other *peerCipher
	appReceived := make(chan struct{}, 1)
	port, err := newPacketPort(t.Context(), 576, f.lease.Permissions, func(raw []byte) error {
		value, claimed, err := decodeControlPacket(raw, localIP, peerIP)
		if err != nil {
			return err
		}
		if claimed {
			kind := transport.Echo
			if value.Kind == transport.Prepare {
				kind = transport.Prepared
			}
			reply, err := controlPacket(peerIP, localIP, replyControl(value, kind))
			if err != nil {
				return err
			}
			return other.Queue(reply, true)
		}
		select {
		case appReceived <- struct{}{}:
		default:
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	other, err = newPeerCipher(t.Context(), coordinator.Node{ID: a.peer.ID, PublicKey: a.peer.PublicKey, AssignedIP: peerIP.String()}, coordinator.Peer{ID: a.local.ID, PublicKey: a.local.PublicKey, AllowedIPs: []string{localIP.String() + "/32"}, ACLRestricted: true, Endpoint: a.offer.LocalEndpoint}, bytes.Repeat([]byte{84}, 32), relayBaseline(), port, peerBind)
	if err != nil {
		_ = port.Close()
		t.Fatal(err)
	}
	t.Cleanup(other.Close)
	go func() {
		for {
			select {
			case <-t.Context().Done():
				return
			case <-mux.ctx.Done():
				return
			case frame := <-sent:
				socket := wire.socket.Load()
				if socket == nil {
					continue
				}
				select {
				case <-socket.done:
					return
				case socket.packets <- memoryWirePacket{data: bytes.Clone(frame.Payload), source: a.offer.LocalEndpoint}:
				}
			}
		}
	}()
	cycle, err := newProbeCycle(t.Context(), f.clock, f.profile.ID, 1, "112233445566778899aabbccddeeff00")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cycle.cancel)
	health, err := channel.BeginHealth(cycle)
	if err != nil {
		t.Fatal(err)
	}
	if !mux.transport.Ready() {
		t.Fatal("fixture Relay TLS not ready")
	}
	if _, err := health.Evidence(); err == nil {
		t.Fatal("TLS Ready alone confirmed peer Relay path")
	}
	if err := health.Send(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-health.done:
	case <-time.After(time.Second):
		t.Fatal("actual WG Relay own-nonce roundtrip failed")
	}
	if _, err := health.Evidence(); err != nil {
		t.Fatal("actual Relay proof not eligible", err)
	}
	select {
	case <-channel.Events():
		t.Fatal("baseline health escaped into Direct negotiation events")
	default:
	}
	request := challengeControl(t, f)
	request.Kind = transport.Prepare
	if err := channel.QueueControl(request); err != nil {
		t.Fatal(err)
	}
	select {
	case reply := <-channel.Events():
		if !transport.MatchesReply(request, reply) {
			t.Fatal("actual encrypted Relay prepare reply mismatched")
		}
	case <-time.After(time.Second):
		t.Fatal("real baseline WG over producer Relay frames failed")
	}
	if applications.Load() != 0 {
		t.Fatal("Relay prepare reached application")
	}
	// This tests ciphertext transport, not native directed application ACL.
	if err := channel.QueueApplication(cipherIPv4(localIP.String(), peerIP.String(), 9)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-appReceived:
	case <-time.After(time.Second):
		t.Fatal("Relay encrypted app frame not delivered to peer fixture")
	}
	raw, err := controlPacket(localIP, peerIP, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := channel.QueueApplication(raw); err == nil {
		t.Fatal("application entrypoint admitted reserved control")
	}
	channel.Close()
	select {
	case _, open := <-channel.Events():
		if open {
			t.Fatal("closed engine left control events open")
		}
	default:
		t.Fatal("control event lifecycle not closed")
	}
}

func TestRelayCipherControlScopeQueueAndRevocation(t *testing.T) {
	mux, f, _, _ := relayMuxFixture(t)
	channel, err := newRelayCipher(t.Context(), mux, f.verified, f.lease, "peer", f.profile.Keys.WireGuardPrivate, 1280, func([]byte) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(channel.Close)
	request := challengeControl(t, f)
	request.SourceGeneration++
	if err := channel.QueueControl(request); err == nil {
		t.Fatal("wrong protected generation sent control")
	}
	request = challengeControl(t, f)
	request.ProfileDigest = fmt.Sprintf("%064x", 1)
	if err := channel.QueueControl(request); err == nil {
		t.Fatal("unassigned profile sent Relay negotiation")
	}
	request = challengeControl(t, f)
	localIP, peerIP, err := f.assignment(t).addresses()
	if err != nil {
		t.Fatal(err)
	}
	inbound := replyControl(request, transport.Echo)
	raw, err := controlPacket(peerIP, localIP, inbound)
	if err != nil {
		t.Fatal(err)
	}
	for range 17 {
		if err := channel.receivePlaintext(raw); err != nil {
			t.Fatal(err)
		}
	}
	if len(channel.Events()) != 16 {
		t.Fatal("supervisor control queue not bounded", len(channel.Events()))
	}
	f.lease.Revoke()
	if err := channel.QueueControl(request); err == nil {
		t.Fatal("revoked peer sent control")
	}
	if err := channel.QueueApplication(cipherIPv4(localIP.String(), peerIP.String(), 1)); err == nil {
		t.Fatal("revoked peer queued application data")
	}
}
