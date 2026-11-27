package dataplane

import (
	"io"

	awgconn "github.com/amnezia-vpn/amneziawg-go/v3/conn"
)

// Forward only datagrams accepted by the candidate's bound receiver. Its
// receive callback owns source checking, lease validation and discovery charges.
func (d *linuxPeerDevice) receiveCandidate(receive awgconn.ReceiveFunc) error {
	return forwardKernelIngress(receive, func(packet []byte) error {
		written, err := d.wire.WriteToUDPAddrPort(packet, d.endpoint)
		if err != nil {
			return err
		}
		if written != len(packet) {
			return io.ErrShortWrite
		}
		return nil
	})
}
