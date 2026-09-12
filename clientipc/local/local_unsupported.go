//go:build !windows && !linux && !darwin

package local

import (
	"context"
	"errors"
	"net"
)

var errUnsupported = errors.New("Client IPC local transport is unsupported on this OS")

func validateEndpoint(string) (string, error)        { return "", errUnsupported }
func listen(string) (net.Listener, error)            { return nil, errUnsupported }
func dial(context.Context, string) (net.Conn, error) { return nil, errUnsupported }
func identify(net.Conn) (Peer, error)                { return Peer{}, errUnsupported }
