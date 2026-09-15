package client

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
)

func TestFlowConfigurationRestartsWorkerAfterStorageFailure(t *testing.T) {
	// A nonempty directory at the queue path cannot be loaded or discarded.
	// This makes the real worker fail before any control-plane network request.
	path := filepath.Join(t.TempDir(), "queue")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "occupied"), []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	engine := &WireGuardEngine{flows: &flowCollector{}}
	engine.opts.FlowSpoolPath = path
	cfg := Config{NodeCredential: "test-credential", ControlPlaneURLs: []string{"https://control.example"}}
	var network clientapi.RegisterNodeResponse
	network.Node.ID = "test-node"
	defer func() {
		if engine.flowCancel != nil {
			engine.flowCancel()
			<-engine.flowDone
		}
	}()
	var previousDone chan struct{}
	var scope string
	for attempt := uint64(1); attempt <= 2; attempt++ {
		engine.configureFlowLocked(cfg, network)
		if engine.flowDone == nil || engine.flowDone == previousDone {
			t.Fatal("terminated worker was reused")
		}
		select {
		case <-engine.flowDone:
		case <-time.After(5 * time.Second):
			t.Fatal("storage failure did not terminate worker")
		}
		engine.flows.mu.Lock()
		failures := engine.flows.storageFailures
		engine.flows.mu.Unlock()
		if failures != attempt {
			t.Fatalf("storage retries=%d, want %d", failures, attempt)
		}
		if scope != "" && engine.flowKey != scope {
			t.Fatal("worker retry changed consent scope")
		}
		scope = engine.flowKey
		previousDone = engine.flowDone
		engine.flowTransport.mu.RLock()
		closed := engine.flowTransport.client == nil
		engine.flowTransport.mu.RUnlock()
		if !closed {
			t.Fatal("failed worker retained HTTP transport")
		}
	}
}
