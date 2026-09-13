package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestNativeExitSelectionRequiresExplicitParametersBeforeConnecting(t *testing.T) {
	base := []string{"--profile-id", "profile", "--request-id", "b10b8dab-f1a2-46a0-b489-0e151c2bcc51", "--expected-instance-id", "instance", "--expected-revision", "7"}
	for _, args := range [][]string{
		nil,
		{"--family-mode", "dual-stack", "--lan-access", "block"},
		{"--exit-node-id", "exit", "--lan-access", "block"},
		{"--exit-node-id", "exit", "--family-mode", "dual-stack"},
		{"--exit-node-id", "exit", "--family-mode", "none", "--lan-access", "block"},
		{"--exit-node-id", "exit", "--family-mode", "4", "--lan-access", "block"},
		{"--exit-node-id", "exit", "--family-mode", "dual-stack", "--lan-access", "default"},
	} {
		var output bytes.Buffer
		err := cmdServiceRPCMutation("select-exit-node", append(append([]string(nil), base...), args...), &output)
		if err == nil || !strings.Contains(err.Error(), "select-exit-node requires") || output.Len() != 0 {
			t.Fatal("missing or ambiguous selection parameters reached transport")
		}
	}
	for _, command := range []string{"select-exit-node", "clear-exit-node"} {
		var output bytes.Buffer
		args := []string{}
		if command == "select-exit-node" {
			args = []string{"--exit-node-id", "exit", "--family-mode", "dual-stack", "--lan-access", "block"}
		}
		if err := cmdServiceRPCMutation(command, args, &output); err == nil || !strings.Contains(err.Error(), "--request-id UUID") || output.Len() != 0 {
			t.Fatal("exit mutation accepted missing durable context")
		}
	}
}
