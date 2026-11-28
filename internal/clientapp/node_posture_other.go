//go:build !windows && !linux

package clientapp

func platformPosture() (string, string, string) { return "", "", "" }

func scanProcesses(func(string, string)) bool { return false }
