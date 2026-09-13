//go:build !windows

package client

import "os/exec"

func hideRouteObservationWindow(_ *exec.Cmd) {}
