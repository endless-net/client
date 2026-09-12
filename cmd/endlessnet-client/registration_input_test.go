package main

import (
	"path/filepath"
	"strings"
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

func TestSentRegistrationInputRemainsImmutableAfterResponseLoss(t *testing.T) {
	setInstallationStateDirForTest(t, t.TempDir())
	s := testcontrol.New(t)
	network, token, err := s.AddNetwork("replay", "100.95.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(t.TempDir(), "client.json")
	args := []string{"--config", config, "--server", s.URL(), "--network", network.Name, "--join-token", token, "--hostname", "route-node", "--advertise"}
	s.SetRegistrationResponsesDropped(true)
	if err := cmdUp(append(args, "192.0.2.0/24")); err == nil {
		t.Fatal("lost response unexpectedly completed enrollment")
	}
	s.SetRegistrationResponsesDropped(false)
	before := len(s.Events())
	if err := cmdUp(append(args, "198.51.100.0/24")); err == nil || !strings.Contains(err.Error(), "pending registration requires unchanged input") {
		t.Fatal("sent registration accepted changed input or failed for another reason")
	}
	for _, event := range s.Events()[before:] {
		if event.Kind == "request" && event.Path == "POST /nodes/register" {
			t.Fatal("changed input reached registration")
		}
	}
	if _, err := captureStdout(t, func() error { return cmdUp(append(args, "192.0.2.0/24")) }); err != nil {
		t.Fatal("original registration did not recover")
	}
	registered, dropped, attempts := 0, 0, 0
	operation, hash := "", ""
	for _, event := range s.Events() {
		switch event.Kind {
		case "registered":
			registered++
		case "registration-response-dropped":
			dropped++
		case "registration-request":
			if attempts == 0 {
				operation, hash = event.OperationID, event.RequestHash
			}
			if event.OperationID != operation || event.RequestHash != hash {
				t.Fatal("retry changed the sent registration")
			}
			attempts++
		}
	}
	if registered != 1 || dropped == 0 || attempts < 2 {
		t.Fatal("response loss did not recover exactly one enrollment")
	}
}
