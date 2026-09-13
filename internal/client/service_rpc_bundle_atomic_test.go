package client

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRPCBundleSecuresEmptyTemporaryFileBeforeWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bundles.state")
	if err := WriteFileAtomic(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("synthetic ACL failure")
	var temporary string
	err := writeFileAtomicSecured(path, []byte("replacement"), 0o600, func(candidate string) error {
		temporary = candidate
		info, err := os.Stat(candidate)
		if err != nil || info.Size() != 0 {
			t.Fatal("payload written before protection", err)
		}
		return failure
	})
	if !errors.Is(err, failure) || temporary == "" {
		t.Fatal("protection failure ignored", err)
	}
	if _, err := os.Stat(temporary); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("temporary file retained", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "original" {
		t.Fatal("failed protection replaced original", err)
	}
}
