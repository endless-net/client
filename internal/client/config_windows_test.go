package client

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

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
