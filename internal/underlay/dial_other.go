//go:build !linux && !windows

package underlay

import (
	"net"
	"time"
)

func Dialer(timeout time.Duration) *net.Dialer {
	return &net.Dialer{Timeout: timeout}
}
