package client

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestConfigReadersDuringAtomicReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.json")
	store, err := OpenConfigStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(func(cfg *Config) error {
		cfg.NodeID = "atomic-reader-initial"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	start, stop := make(chan struct{}), make(chan struct{})
	failures := make(chan error, 4)
	var readers sync.WaitGroup
	for range 4 {
		readers.Go(func() {
			<-start
			for {
				select {
				case <-stop:
					return
				default:
				}
				// Bypass the process cache, as each fresh CLI process does.
				resolved, err := resolveConfigPath(path)
				if err == nil {
					var cfg Config
					cfg, err = loadConfigFile(resolved)
					if err == nil && !strings.HasPrefix(cfg.NodeID, "atomic-reader-") {
						t.Error("atomic replacement exposed missing or partial state")
						return
					}
				}
				if err != nil {
					failures <- err
					return
				}
			}
		})
	}
	close(start)
	for i := range 100 {
		if err := store.Update(func(cfg *Config) error {
			cfg.NodeID = "atomic-reader-" + strconv.Itoa(i)
			return nil
		}); err != nil {
			t.Error(err)
			break
		}
	}
	close(stop)
	readers.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
}

func TestWriteFileAtomicRetriesWindowsSharingViolation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "client.json")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	reader, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	readerClosed := make(chan error, 1)
	go func() {
		time.Sleep(100 * time.Millisecond)
		readerClosed <- reader.Close()
	}()

	if err := WriteFileAtomic(path, []byte("new"), 0o600); err != nil {
		t.Fatalf("WriteFileAtomic while target is briefly open: %v", err)
	}
	if err := <-readerClosed; err != nil {
		t.Fatalf("close target reader: %v", err)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != "new" {
		t.Fatalf("written content = %q, want new", written)
	}
}
