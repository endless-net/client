package client

import (
	"context"
	"errors"
	"strings"
	"time"
)

const defaultRouteOutputLimit = 16 * 1024

// ObserveOSDefaultRoute returns an observed presence value only when the
// platform collector can read both address families. An unavailable family
// makes the combined observation unknown rather than silently absent.
func ObserveOSDefaultRoute(ctx context.Context) (present bool, observed bool) {
	return observePlatformOSDefaultRoute(ctx)
}

func observeDefaultRoutes(ctx context.Context, goos string, run CommandRunner) (bool, error) {
	if run == nil {
		return false, errors.New("default route observation unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var present bool
	switch goos {
	case "linux":
		for _, family := range []string{"-4", "-6"} {
			if err := ctx.Err(); err != nil {
				return false, err
			}
			output, err := run(ctx, "ip", "-j", family, "route", "show", "default")
			if err != nil || len(output) > defaultRouteOutputLimit {
				return false, errors.New("default route observation unavailable")
			}
			value, err := underlayDNSJSON(output)
			rows, ok := value.([]any)
			if err != nil || !ok || len(rows) > 4096 {
				return false, errors.New("default route observation invalid")
			}
			for _, raw := range rows {
				row, ok := raw.(map[string]any)
				if !ok {
					return false, errors.New("default route observation invalid")
				}
				destination, ok := row["dst"].(string)
				familyDefault := (family == "-4" && destination == "0.0.0.0/0") || (family == "-6" && destination == "::/0")
				if !ok || (destination != "default" && !familyDefault) {
					return false, errors.New("default route observation invalid")
				}
				if routeType, exists := row["type"]; exists && routeType != "unicast" && routeType != "blackhole" && routeType != "unreachable" && routeType != "prohibit" && routeType != "throw" {
					return false, errors.New("default route observation invalid")
				}
				present = true
			}
		}
		return present, nil
	case "darwin":
		for _, family := range []string{"inet", "inet6"} {
			if err := ctx.Err(); err != nil {
				return false, err
			}
			output, err := run(ctx, "netstat", "-rn", "-f", family)
			if err != nil || len(output) > defaultRouteOutputLimit {
				return false, errors.New("default route observation unavailable")
			}
			familyPresent, err := darwinDefaultRoutePresent(output)
			if err != nil {
				return false, err
			}
			present = present || familyPresent
		}
		return present, nil
	default:
		return false, errors.New("default route observation unsupported")
	}
}

func darwinDefaultRoutePresent(output []byte) (bool, error) {
	if len(output) == 0 || len(output) > defaultRouteOutputLimit {
		return false, errors.New("default route observation invalid")
	}
	lines := strings.Split(string(output), "\n")
	header := false
	present := false
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[0] == "Destination" {
			header = true
			continue
		}
		if fields[0] != "default" && fields[0] != "0/0" && fields[0] != "::/0" {
			continue
		}
		if !header || len(fields) < 4 {
			return false, errors.New("default route observation invalid")
		}
		present = true
	}
	if !header {
		return false, errors.New("default route observation invalid")
	}
	return present, nil
}
