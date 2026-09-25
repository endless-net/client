//go:build linux

package testclient

import (
	"bufio"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"

	"github.com/godbus/dbus/v5"
)

const (
	testLogindName      = "org.freedesktop.login1"
	testLogindPath      = dbus.ObjectPath("/org/freedesktop/login1")
	testLogindInterface = "org.freedesktop.login1.Manager"
)

type testLogindBus struct {
	mu         sync.Mutex
	inhibitors []*os.File
	once       sync.Once
}

type testLogindProperties struct{}

type testLogindSession struct {
	ID   string
	UID  uint32
	User string
	Seat string
	Path dbus.ObjectPath
}

func newTestLogindBus() (string, func(), error) {
	cmd := exec.Command("dbus-daemon", "--session", "--nofork", "--print-address=1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", nil, err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return "", nil, err
	}
	address, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return "", nil, err
	}
	address = address[:len(address)-1]
	conn, err := dbus.Connect(address)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return "", nil, err
	}
	service := &testLogindBus{}
	if err := conn.Export(service, testLogindPath, testLogindInterface); err != nil {
		_ = conn.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return "", nil, err
	}
	if err := conn.Export(testLogindProperties{}, testLogindPath, "org.freedesktop.DBus.Properties"); err != nil {
		_ = conn.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return "", nil, err
	}
	if reply, err := conn.RequestName(testLogindName, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		_ = conn.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if err == nil {
			err = errors.New("test logind service name is unavailable")
		}
		return "", nil, err
	}
	cleanup := func() {
		service.once.Do(func() {
			service.mu.Lock()
			for _, inhibitor := range service.inhibitors {
				_ = inhibitor.Close()
			}
			service.mu.Unlock()
			_ = conn.Close()
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		})
	}
	return address, cleanup, nil
}

func (s *testLogindBus) Inhibit(string, string, string, string) (dbus.UnixFD, *dbus.Error) {
	reader, writer, err := os.Pipe()
	if err != nil {
		return 0, dbus.MakeFailedError(err)
	}
	s.mu.Lock()
	s.inhibitors = append(s.inhibitors, reader, writer)
	s.mu.Unlock()
	return dbus.UnixFD(reader.Fd()), nil
}

func (*testLogindBus) ListSessions() ([]testLogindSession, *dbus.Error) {
	return []testLogindSession{}, nil
}

func (testLogindProperties) Get(_, _ string) (dbus.Variant, *dbus.Error) {
	return dbus.MakeVariant(uint64(5_000_000)), nil
}
