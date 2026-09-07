package client

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	coordinatorapi "github.com/endless-net/coordinator/coordinatorapi/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestFlowSpoolRestartRetainsIdentityAndErasesExpiredLease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue")
	s, err := newFlowSpool(path, "test-credential", "test-producer")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	expiry := now.Add(50 * time.Second)
	window := &coordinatorapi.FlowWindow{WindowId: "immutable-window", Source: "100.64.0.1", Destination: "100.64.0.2", WindowStart: timestamppb.New(now), WindowEnd: timestamppb.New(now), Bytes: 40, Packets: 1}
	if err := s.save(7, expiry, []*coordinatorapi.FlowWindow{window}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"100.64.", "immutable-window", "test-credential", "test-producer"} {
		if bytes.Contains(raw, []byte(forbidden)) {
			t.Fatal("plaintext queue metadata")
		}
	}
	restarted, err := newFlowSpool(path, "test-credential", "test-producer")
	if err != nil {
		t.Fatal(err)
	}
	version, deadline, windows, err := restarted.load(now.Add(time.Second))
	if err != nil || version != 7 || !deadline.Equal(expiry) || len(windows) != 1 || !proto.Equal(windows[0], window) {
		t.Fatalf("restart changed pending identity: %d %v", version, err)
	}
	if _, _, windows, err := restarted.load(expiry); err != nil || len(windows) != 0 {
		t.Fatal("expired queue restored")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("expired ciphertext retained")
	}
}

func TestFlowSpoolRejectsTamperingWrongScopeAndOversize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue")
	now := time.Now().UTC()
	s, err := newFlowSpool(path, "credential", "node-a")
	if err != nil {
		t.Fatal(err)
	}
	w := &coordinatorapi.FlowWindow{WindowId: "w", WindowStart: timestamppb.New(now), WindowEnd: timestamppb.New(now)}
	if err := s.save(1, now.Add(30*time.Second), []*coordinatorapi.FlowWindow{w}); err != nil {
		t.Fatal(err)
	}
	for _, values := range [][2]string{{"other-credential", "node-a"}, {"credential", "node-b"}} {
		other, err := newFlowSpool(path, values[0], values[1])
		if err != nil {
			t.Fatal(err)
		}
		if _, _, windows, err := other.load(now); !errors.Is(err, errInvalidFlowSpool) || len(windows) != 0 {
			t.Fatal("queue escaped identity binding")
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 1
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.load(now); !errors.Is(err, errInvalidFlowSpool) {
		t.Fatal("tampered queue accepted")
	}
	if err := os.WriteFile(path, make([]byte, maxFlowSpoolBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.load(now); !errors.Is(err, errInvalidFlowSpool) {
		t.Fatal("oversized queue accepted")
	}
}
