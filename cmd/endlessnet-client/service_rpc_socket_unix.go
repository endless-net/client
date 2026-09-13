//go:build linux || darwin

package main

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// recoverAgentRPCSocket requires the agent's lifetime lock. Only an owned,
// refused socket in a protected owned directory is eligible for recovery.
func recoverAgentRPCSocket(endpoint string) error {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return nil
	}
	if !filepath.IsAbs(endpoint) {
		return errors.New("native IPC recovery requires an absolute socket path")
	}
	info, err := os.Lstat(endpoint)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return errors.New("native IPC recovery refuses a non-socket path")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(endpoint))
	if err != nil {
		return err
	}
	fd, err := unix.Open(parent, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer func() { _ = unix.Close(fd) }()
	var directory, before unix.Stat_t
	if err := unix.Fstat(fd, &directory); err != nil {
		return err
	}
	if directory.Uid != uint32(os.Geteuid()) || directory.Mode&0022 != 0 {
		return errors.New("native IPC recovery requires a protected owned directory")
	}
	name := filepath.Base(endpoint)
	if err := unix.Fstatat(fd, name, &before, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if before.Mode&unix.S_IFMT != unix.S_IFSOCK || before.Uid != uint32(os.Geteuid()) {
		return errors.New("native IPC recovery refuses an unowned or non-socket endpoint")
	}
	connection, err := net.DialTimeout("unix", filepath.Join(parent, name), 200*time.Millisecond)
	if err == nil {
		_ = connection.Close()
		return errors.New("native IPC endpoint is already serving")
	}
	if !errors.Is(err, unix.ECONNREFUSED) {
		return errors.New("native IPC endpoint was not proven stale")
	}
	var after unix.Stat_t
	if err := unix.Fstatat(fd, name, &after, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if before.Dev != after.Dev || before.Ino != after.Ino || before.Mode != after.Mode || before.Uid != after.Uid {
		return errors.New("native IPC endpoint changed during recovery")
	}
	return unix.Unlinkat(fd, name, 0)
}
