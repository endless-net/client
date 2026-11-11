package dataplane

import (
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/endless-net/client/internal/connectivity"
)

func TestAssignedCipherFactoryFencesMapAndOwnsWholeIsolatedChannel(t *testing.T) {
	f := newCandidateFixture(t)
	sink := func([]byte) error { return nil }
	if _, err := newAssignedCipher(t.Context(), connectivity.Map{}, f.lease, "peer", "fixture-profile", f.profile.Keys.WireGuardPrivate, 1280, f.clock, sink); err == nil {
		t.Fatal("factory accepted unsigned map")
	}
	socket := newCandidateSocketFixture()
	channel, err := buildAssignedCipher(t.Context(), f.verified, f.lease, "peer", "fixture-profile", f.profile.Keys.WireGuardPrivate, 1280, f.clock, sink, func(uint16) (candidateSocket, error) { return socket, nil })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(channel.Close)
	for _, rendered := range []string{fmt.Sprint(channel), fmt.Sprintf("%#v", channel)} {
		if rendered != "[assigned encrypted peer channel]" {
			t.Fatal("unsafe channel formatting")
		}
	}
	if port, err := channel.cipher.BoundPort(); port != 51821 || err != nil {
		t.Fatal("factory did not use acknowledged candidate socket", port, err)
	}
	cursor := f.verified.Cursor()
	f.clock.advance(time.Millisecond, time.Millisecond)
	f.refresh(t, &cursor)
	if err := channel.Update(t.Context(), f.verified); err != nil {
		t.Fatal("verified renewal failed", err)
	}
	if channel.bind.socket != socket {
		t.Fatal("renewal replaced prepared listener")
	}
	channel.Close()
	select {
	case <-socket.done:
	default:
		t.Fatal("channel close left socket open")
	}
	select {
	case <-channel.cipher.port.done:
	default:
		t.Fatal("channel close left virtual TUN open")
	}
	if err := channel.Update(t.Context(), f.verified); err != ErrClosed {
		t.Fatal("closed channel updated", err)
	}
}

type closingCallbackDevice struct {
	wireGuardDevice
	callback func()
}

func (*closingCallbackDevice) IpcSetOperation(io.Reader) error { return ErrEngine }
func (d *closingCallbackDevice) Close()                        { d.callback(); d.wireGuardDevice.Close() }

func TestAssignedUpdateFailureDoesNotHoldSinkLockWhileClosingEngine(t *testing.T) {
	channel, f, _, _, _ := pathChallengeFixture(t)
	local, peer, err := f.assignment(t).addresses()
	if err != nil {
		t.Fatal(err)
	}
	packet, err := controlPacket(peer, local, replyControl(challengeControl(t, f), "echo"))
	if err != nil {
		t.Fatal(err)
	}
	channel.cipher.mu.Lock()
	original := channel.cipher.device
	channel.cipher.device = &closingCallbackDevice{wireGuardDevice: original, callback: func() { _ = channel.receivePlaintext(packet) }}
	channel.cipher.mu.Unlock()
	// An unchanged endpoint is intentionally not sent through crypto IPC.
	// Change the actual signed endpoint to exercise the failing IPC/Close path.
	cursor := f.verified.Cursor()
	f.clock.advance(time.Millisecond, time.Millisecond)
	f.snapshot.Peers[0].Transport.Profiles[0].PeerEndpoint = "192.0.2.21:51820"
	f.snapshot.Peers[0].Transport.PeerAdvertisement.Endpoints[0].Endpoint = "192.0.2.21:51820"
	f.refresh(t, &cursor)
	done := make(chan error, 1)
	go func() { done <- channel.Update(t.Context(), f.verified) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("injected IPC failure accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("Update held sink lock during engine shutdown")
	}
}
