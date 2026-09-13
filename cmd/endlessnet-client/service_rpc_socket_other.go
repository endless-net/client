//go:build !linux && !darwin

package main

func recoverAgentRPCSocket(string) error { return nil }
