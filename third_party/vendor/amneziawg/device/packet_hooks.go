// SPDX-License-Identifier: MIT
// EndlessNet adapter hooks; cryptographic and wire algorithms are unchanged.
package device

// PacketHooks annotates outgoing plaintext and admits authenticated incoming
// plaintext with its original encrypted datagram size. Callbacks must not keep
// the plaintext slice or call device configuration/Close. A guard travels with
// its packet through staging/encryption and is called at the bind's send boundary.
// Hook errors drop that packet; they never authorize packets or create sessions.
type PacketHooks struct {
	Outbound func([]byte) func(int) error
	Inbound  func([]byte, int) error
}

// SetPacketHooks installs an immutable callback snapshot. Nil restores the
// upstream annotation path for future packets; already queued guards remain.
// The owner configures hooks before Up.
func (device *Device) SetPacketHooks(hooks *PacketHooks) {
	if hooks == nil {
		device.packetHooks.Store(nil)
		return
	}
	copy := *hooks
	device.packetHooks.Store(&copy)
}

func (device *Device) packetSendGuard(plain []byte) func(int) error {
	hooks := device.packetHooks.Load()
	if hooks != nil && hooks.Outbound != nil {
		return hooks.Outbound(plain)
	}
	return nil
}

func (device *Device) packetReceiveAllowed(plain []byte, wireBytes int) bool {
	hooks := device.packetHooks.Load()
	return hooks == nil || hooks.Inbound == nil || hooks.Inbound(plain, wireBytes) == nil
}
