package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/endless-net/client/internal/testcontrol"
)

func TestDeniedJoinTokenCanBeReplacedWithoutForgettingEnrollment(t *testing.T) {
	setInstallationStateDirForTest(t, t.TempDir())
	s := testcontrol.New(t)
	network, token, err := s.AddNetwork("token-recovery", "100.95.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := s.RotateJoinToken(token)
	if err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(t.TempDir(), "client.json")
	args := []string{"--config", config, "--server", s.URL(), "--network", network.Name, "--hostname", "retry-node", "--join-token"}
	if err := cmdUp(append(args, token)); err == nil {
		t.Fatal("retired token authorized registration")
	}
	if _, err := captureStdout(t, func() error { return cmdUp(append(args, replacement)) }); err != nil {
		t.Fatal("replacement token could not recover the same client configuration")
	}
	registered := 0
	for _, event := range s.Events() {
		if event.Kind == "registered" {
			registered++
		}
	}
	if registered != 1 {
		t.Fatal("replacement token did not complete exactly one enrollment")
	}
}

func TestInvalidBrowserInputCanBeCorrected(t *testing.T) {
	for _, tc := range []struct{ flag, invalid, valid string }{
		{"--advertise", "not-a-cidr", "192.0.2.0/24"},
		{"--hostname", "invalid hostname", "route-node"},
		{"--endpoint", "not-an-endpoint", "127.0.0.1:51820"},
	} {
		t.Run(strings.TrimPrefix(tc.flag, "--"), func(t *testing.T) {
			setInstallationStateDirForTest(t, t.TempDir())
			s := testcontrol.New(t)
			network, _, err := s.AddNetwork("browser-advertisement", "100.95.0.0/24")
			if err != nil {
				t.Fatal(err)
			}
			config := filepath.Join(t.TempDir(), "client.json")
			args := []string{"--config", config, "--server", s.URL(), "--network", network.Name, "--hostname", "route-node", "--approval-timeout", "0", tc.flag}
			_, err = captureStdout(t, func() error { return cmdUp(append(args, tc.invalid)) })
			if err == nil {
				t.Fatal("invalid browser input was accepted")
			}
			for _, event := range s.Events() {
				if event.Kind == "request" && event.Path == "POST /nodes/enrollment-requests" {
					t.Fatal("invalid browser input was sent to enrollment")
				}
			}
			_, err = captureStdout(t, func() error { return cmdUp(append(args, tc.valid)) })
			var approval enrollmentApprovalRequiredError
			if !errors.As(err, &approval) || approval.RequestID == "" {
				t.Fatal("corrected browser input did not reach approval")
			}
			created := 0
			for _, event := range s.Events() {
				if event.Kind == "enrollment" {
					created++
				}
			}
			if created != 1 {
				t.Fatal("corrected browser input did not create exactly one approval request")
			}
			if err := s.DecideEnrollment(approval.RequestID, true); err != nil {
				t.Fatal(err)
			}
			if _, err := captureStdout(t, func() error { return cmdUp(append(args, tc.valid)) }); err != nil {
				t.Fatal("corrected browser input could not complete approved enrollment")
			}
			registered := 0
			for _, event := range s.Events() {
				if event.Kind == "registered" {
					registered++
				}
			}
			if registered != 1 {
				t.Fatal("approved corrected input did not register exactly one node")
			}
		})
	}
}

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
