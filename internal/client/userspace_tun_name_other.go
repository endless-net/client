//go:build !darwin

package client

func platformUserspaceTUNName(requested string) string { return requested }
