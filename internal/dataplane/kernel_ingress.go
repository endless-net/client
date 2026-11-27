package dataplane

import awgconn "github.com/amnezia-vpn/amneziawg-go/v3/conn"

// candidateBind's receive callback has already checked the signed endpoint,
// clock, permission and probe budget for each non-empty result.
func forwardKernelIngress(receive awgconn.ReceiveFunc, write func([]byte) error) error {
	var payload [65535]byte
	var sizes [1]int
	var endpoints [1]awgconn.Endpoint
	packets := [][]byte{payload[:]}
	for {
		n, err := receive(packets, sizes[:], endpoints[:])
		if err != nil {
			return err
		}
		if n == 0 {
			continue
		}
		if n != 1 || sizes[0] <= 0 || sizes[0] > 65507 || endpoints[0] == nil {
			return ErrPacketIO
		}
		if err := write(payload[:sizes[0]]); err != nil {
			return err
		}
	}
}
