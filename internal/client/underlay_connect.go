package client

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Keep transport errors inspectable without exposing endpoints in diagnostics.
type underlayConnectError struct{ cause error }

func (e *underlayConnectError) Error() string { return "authorized underlay endpoint is unreachable" }
func (e *underlayConnectError) Unwrap() error { return e.cause }

// The caller supplies the total deadline. Individual candidates cannot monopolize
// it, and at most two sockets are connecting at once. A dial callback must honor
// its context; even a late success after cancellation is closed by its owner.
func dialUnderlayAddresses(ctx context.Context, network, port string, addresses []netip.Addr, dial func(context.Context, string, string) (net.Conn, error), current func(context.Context) error) (net.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch network {
	case "tcp", "tcp4", "tcp6", "udp", "udp4", "udp6":
	default:
		return nil, errors.New("unsupported underlay transport")
	}
	portNumber, err := strconv.ParseUint(port, 10, 16)
	if err != nil || portNumber == 0 || dial == nil || len(addresses) == 0 || len(addresses) > 256 {
		return nil, errors.New("invalid underlay connection candidates")
	}
	var first, second []netip.Addr
	seen := make(map[netip.Addr]bool, len(addresses))
	var firstIs4 bool
	for _, ip := range addresses {
		if !ip.IsValid() {
			return nil, errors.New("invalid underlay connection candidate")
		}
		ip = ip.Unmap()
		if strings.HasSuffix(network, "4") && !ip.Is4() || strings.HasSuffix(network, "6") && !ip.Is6() || seen[ip] {
			continue
		}
		seen[ip] = true
		if len(first) == 0 {
			firstIs4 = ip.Is4()
		}
		if ip.Is4() == firstIs4 {
			first = append(first, ip)
		} else {
			second = append(second, ip)
		}
	}
	candidates := make([]netip.Addr, 0, len(seen))
	for i := 0; i < len(first) || i < len(second); i++ {
		if i < len(first) {
			candidates = append(candidates, first[i])
		}
		if i < len(second) {
			candidates = append(candidates, second[i])
		}
	}
	if len(candidates) == 0 {
		return nil, errors.New("no underlay candidates for transport family")
	}
	ctx, cancel := context.WithCancel(ctx)
	var attempts sync.WaitGroup
	defer func() { cancel(); attempts.Wait() }()
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if current != nil {
			if err := current(ctx); err != nil {
				return err
			}
		}
		return ctx.Err()
	}
	type result struct {
		conn net.Conn
		err  error
	}
	// Unbuffered ownership transfer ensures a success cannot sit in a queue
	// after the caller returns: the cancelled worker closes that connection.
	results := make(chan result)
	next, active := 0, 0
	launch := func() error {
		if err := check(); err != nil {
			return err
		}
		address := net.JoinHostPort(candidates[next].String(), port)
		next++
		active++
		attempts.Add(1)
		go func() {
			defer attempts.Done()
			attempt, stop := context.WithTimeout(ctx, 2*time.Second)
			conn, err := dial(attempt, network, address)
			if attempt.Err() != nil {
				err = attempt.Err()
			}
			stop()
			if err == nil && conn == nil {
				err = errors.New("underlay dial returned no connection")
			}
			if err != nil && conn != nil {
				_ = conn.Close()
				conn = nil
			}
			select {
			case results <- result{conn, err}:
			case <-ctx.Done():
				if conn != nil {
					_ = conn.Close()
				}
			}
		}()
		return nil
	}
	if err := launch(); err != nil {
		return nil, err
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	var failures []error
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case outcome := <-results:
			active--
			if outcome.err == nil {
				if err := check(); err != nil {
					_ = outcome.conn.Close()
					return nil, err
				}
				return outcome.conn, nil
			}
			failures = append(failures, outcome.err)
			if next < len(candidates) {
				if err := launch(); err != nil {
					return nil, err
				}
			} else if active == 0 {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				return nil, &underlayConnectError{cause: errors.Join(failures...)}
			}
		case <-ticker.C:
			if active < 2 && next < len(candidates) {
				if err := launch(); err != nil {
					return nil, err
				}
			}
		}
	}
}
