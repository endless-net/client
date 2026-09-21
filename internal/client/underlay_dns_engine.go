package client

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"net/url"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
)

func underlayDNSSourceIdentity(source *underlayDNSSource) string {
	if source == nil {
		return ""
	}
	raw, err := json.Marshal(source)
	if err != nil {
		return "invalid"
	}
	sum := sha256.Sum256(raw)
	return string(sum[:])
}

func underlayDNSRequired(cfg Config, network *clientapi.RegisterNodeResponse) bool {
	for _, origin := range cfg.ControlURLs() {
		u, err := url.Parse(origin)
		if err != nil {
			return true
		}
		if _, err := netip.ParseAddr(u.Hostname()); err != nil {
			return true
		}
	}
	if network != nil {
		for _, endpoint := range network.Relays {
			host, _, err := net.SplitHostPort(endpoint.Addr)
			if err != nil {
				return true
			}
			if _, err := netip.ParseAddr(host); err != nil {
				return true
			}
		}
	}
	return false
}

func (e *WireGuardEngine) captureUnderlayDNSLocked(ctx context.Context, required bool) error {
	if e.exitGuard == nil {
		return errors.New("underlay DNS capture requires owned protection")
	}
	if !required {
		e.underlayDNS = nil
		return ctx.Err()
	}
	capture := e.opts.underlayDNSCapture
	if capture == nil {
		capture = func(ctx context.Context, name string) (*underlayDNSSource, error) {
			return captureUnderlayDNSSource(ctx, name, nil)
		}
	}
	source, err := capture(ctx, e.exitGuard.interfaceName)
	if ctx.Err() != nil {
		e.underlayDNS = nil
		return ctx.Err()
	}
	if err != nil || source == nil {
		e.underlayDNS = nil
		return errors.New("underlay DNS source is unavailable")
	}
	if err := ctx.Err(); err != nil {
		e.underlayDNS = nil
		return err
	}
	e.underlayDNS = cloneUnderlayDNSSource(source)
	return nil
}

// A client keeps an immutable source and rechecks it without taking engine.mu.
// The caller already holds the engine/effect lock when constructing it. A
// changed resolver owner, link or DNS configuration invalidates this transport;
// it must never silently adopt a new source while an old request is in flight.
func (e *WireGuardEngine) underlayDNSCurrentLocked() func(context.Context) error {
	if e.underlayDNS == nil || e.exitGuard == nil {
		return nil
	}
	identity := underlayDNSSourceIdentity(e.underlayDNS)
	name := e.exitGuard.interfaceName
	capture := e.opts.underlayDNSCapture
	if capture == nil {
		capture = func(ctx context.Context, name string) (*underlayDNSSource, error) {
			return captureUnderlayDNSSource(ctx, name, nil)
		}
	}
	return func(ctx context.Context) error {
		source, err := capture(ctx, name)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil || source == nil || underlayDNSSourceIdentity(source) != identity {
			return errors.New("underlay DNS source changed")
		}
		return ctx.Err()
	}
}
