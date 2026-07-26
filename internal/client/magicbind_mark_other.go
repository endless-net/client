//go:build !linux

package client

import "net"

func setMagicBindSocketMark(_ *net.UDPConn, _ uint32) error { return nil }
