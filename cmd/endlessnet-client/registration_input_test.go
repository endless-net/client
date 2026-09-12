package main

import (
	"path/filepath"
	"testing"

	"github.com/endless-net/client/internal/testcontrol"
)

func TestInvalidAdvertisementCanBeCorrectedWithoutForgettingEnrollment(t *testing.T) {
	setInstallationStateDirForTest(t, t.TempDir())
	s := testcontrol.New(t)
	network, token, err := s.AddNetwork("advertisement", "100.95.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(t.TempDir(), "client.json")
	args := []string{"--config", config, "--server", s.URL(), "--network", network.Name, "--join-token", token, "--hostname", "route-node", "--advertise"}
	if err := cmdUp(append(args, "not-a-cidr")); err == nil {
		t.Fatal("invalid advertisement was accepted")
	}
	for _, event := range s.Events() {
		if event.Kind == "request" && event.Path == "POST /nodes/register" {
			t.Fatal("invalid advertisement was sent to registration")
		}
	}
	if _, err := captureStdout(t, func() error { return cmdUp(append(args, "192.0.2.0/24")) }); err != nil {
		t.Fatal("correcting invalid advertisement did not recover enrollment")
	}
	registered := 0
	for _, event := range s.Events() {
		if event.Kind == "registered" {
			registered++
		}
	}
	if registered != 1 {
		t.Fatal("corrected input did not produce exactly one enrollment")
	}
}
