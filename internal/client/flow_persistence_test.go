package client

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
)

func TestFlowPersistenceCrashReplayRequiresFreshConsentAndPersistsACK(t *testing.T) {
	now := time.Now().UTC()
	spool, err := newFlowSpool(filepath.Join(t.TempDir(), "queue"), "credential", "node")
	if err != nil {
		t.Fatal(err)
	}
	c := &flowCollector{}
	c.policy(flowPolicy(now, 7), now)
	c.observe(applicationTCPPacket("100.64.0.1", "100.64.0.2", 1234, 443), true, now)
	window, version := c.next(now.Add(11 * time.Second))
	p, err := openFlowPersistence(spool, c, now)
	if err != nil {
		t.Fatal(err)
	}
	p.acceptPolicy(c, now)
	if err := p.checkpoint(c, now.Add(11*time.Second)); err != nil {
		t.Fatal(err)
	}
	// Simulate a process lost after the remote commit but before local ACK.
	restarted := &flowCollector{}
	replay, err := openFlowPersistence(spool, restarted, now.Add(12*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if next, _ := restarted.next(now.Add(12 * time.Second)); next != nil {
		t.Fatal("replayed before fresh consent")
	}
	restarted.policy(flowPolicy(now.Add(12*time.Second), 7), now.Add(12*time.Second))
	replay.acceptPolicy(restarted, now.Add(12*time.Second))
	actual, actualVersion := restarted.next(now.Add(13 * time.Second))
	if actualVersion != version || !proto.Equal(actual, window) {
		t.Fatal("crash replay changed identity/payload")
	}
	restarted.acknowledge(actual.GetWindowId(), actualVersion)
	if err := replay.checkpoint(restarted, now.Add(13*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, _, windows, err := spool.load(now.Add(14 * time.Second)); err != nil || len(windows) != 0 {
		t.Fatal("ACK did not remove durable record")
	}
	if restarted.status(now.Add(14*time.Second)).RestoredWindows != 1 {
		t.Fatal("restore not counted")
	}
}

func TestFlowPersistenceChangedConsentAndStorageFailure(t *testing.T) {
	now := time.Now().UTC()
	path := filepath.Join(t.TempDir(), "queue")
	spool, err := newFlowSpool(path, "credential", "node")
	if err != nil {
		t.Fatal(err)
	}
	c := &flowCollector{}
	c.policy(flowPolicy(now, 7), now)
	c.observe(applicationTCPPacket("100.64.0.1", "100.64.0.2", 1234, 443), true, now)
	c.next(now.Add(11 * time.Second))
	p, err := openFlowPersistence(spool, c, now)
	if err != nil {
		t.Fatal(err)
	}
	p.acceptPolicy(c, now)
	if err := p.checkpoint(c, now.Add(11*time.Second)); err != nil {
		t.Fatal(err)
	}
	restarted := &flowCollector{}
	replay, err := openFlowPersistence(spool, restarted, now.Add(12*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	restarted.policy(flowPolicy(now.Add(12*time.Second), 8), now.Add(12*time.Second))
	replay.acceptPolicy(restarted, now.Add(12*time.Second))
	if next, _ := restarted.next(now.Add(12 * time.Second)); next != nil {
		t.Fatal("old consent restored")
	}
	if err := replay.checkpoint(restarted, now.Add(12*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("revoked ciphertext retained")
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	// A non-empty directory cannot be replaced by a queue file.
	if err := os.WriteFile(filepath.Join(path, "occupied"), []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	p.saved = false
	if err := p.checkpoint(c, now.Add(13*time.Second)); err == nil {
		t.Fatal("storage failure ignored")
	}
	if c.status(now.Add(13*time.Second)).StorageFailures == 0 {
		t.Fatal("storage failure not observable")
	}
}
