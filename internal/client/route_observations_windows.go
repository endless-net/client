package client

import (
	"os/exec"
	"syscall"
)

func hideRouteObservationWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
