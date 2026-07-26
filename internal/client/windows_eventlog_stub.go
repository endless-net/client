//go:build !windows

package client

func ConfigureWindowsEventLogger(source string) (func(), error) {
	return func() {}, nil
}
