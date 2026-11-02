//go:build windows

package wintundriver

import "syscall"

func callSetLogger(address uintptr) {
	_, _, _ = syscall.SyscallN(address, 0)
}
