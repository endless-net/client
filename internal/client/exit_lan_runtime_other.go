//go:build !linux

package client

func newPlatformExitLANRuntime(*ConfigStore) *exitLANRuntime { return nil }
