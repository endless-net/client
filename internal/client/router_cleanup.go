package client

import (
	"context"
	"errors"
	"net"
)

// A cleanup may partially succeed. Retain only failed/unattempted steps so a
// retry does not repeat successful destructive commands or discard failures.
type routerCleanupPlan struct {
	pending []func(context.Context) error
}

func (p *routerCleanupPlan) run(ctx context.Context) error {
	var failures error
	remaining := p.pending[:0]
	for i, step := range p.pending {
		if err := ctx.Err(); err != nil {
			remaining = append(remaining, p.pending[i:]...)
			p.pending = remaining
			return errors.Join(failures, err)
		}
		err := step(ctx)
		if err != nil {
			remaining = append(remaining, step)
			failures = errors.Join(failures, err)
		}
	}
	p.pending = remaining
	return failures
}

func routerInterfacePresent(name string) (bool, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return false, err
	}
	for _, iface := range interfaces {
		if iface.Name == name {
			return true, nil
		}
	}
	return false, nil
}

// Interface-bound kernel state disappears with the interface. After a failed
// removal, absence is a verified postcondition; an enumeration error is not.
func routerInterfaceCleanup(name string, present func(string) (bool, error), remove func(context.Context) error) func(context.Context) error {
	return func(ctx context.Context) error {
		err := remove(ctx)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return errors.Join(err, ctx.Err())
		}
		if present == nil {
			present = routerInterfacePresent
		}
		exists, inspectErr := present(name)
		if inspectErr != nil {
			return errors.Join(err, inspectErr)
		}
		if !exists {
			return nil
		}
		return err
	}
}
