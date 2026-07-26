package client

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestResolveDebugLogDirExpandsHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	got, err := ResolveDebugLogDir(`~\.endlessnet\logs`)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".endlessnet", "logs")
	if runtime.GOOS == "windows" && strings.Contains(strings.ToLower(home), `\system32\config\systemprofile`) {
		t.Skip("Windows LocalSystem home may resolve to the active console user instead")
	}
	if got != filepath.Clean(want) {
		t.Fatalf("ResolveDebugLogDir = %q, want %q", got, filepath.Clean(want))
	}
}

func TestResolveDebugLogDirAcceptsWindowsSeparators(t *testing.T) {
	root := t.TempDir()
	got, err := ResolveDebugLogDir(root + `\endlessnet\logs`)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "endlessnet", "logs")
	if got != filepath.Clean(want) {
		t.Fatalf("ResolveDebugLogDir = %q, want %q", got, filepath.Clean(want))
	}
}

func TestDebugLogRedaction(t *testing.T) {
	input := `token=abc, Authorization: Bearer secret-token, private_key=value`
	got := redactDebugLogText(input)
	for _, leak := range []string{"abc", "secret-token", "value"} {
		if strings.Contains(got, leak) {
			t.Fatalf("redacted log still contains %q: %s", leak, got)
		}
	}
	if !strings.Contains(got, "[redacted]") {
		t.Fatalf("redacted log missing marker: %s", got)
	}
}

func TestBestEffortDebugLogWriterKeepsPrimaryWhenSecondaryFails(t *testing.T) {
	var primary bytes.Buffer
	w := bestEffortDebugLogWriter{
		primary:   redactingDebugLogWriter{w: &primary},
		secondary: failingDebugLogWriter{},
	}
	input := []byte("debug token=secret\n")
	n, err := w.Write(input)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(input) {
		t.Fatalf("Write returned %d bytes, want %d", n, len(input))
	}
	got := primary.String()
	if strings.Contains(got, "secret") {
		t.Fatalf("primary log was not redacted: %s", got)
	}
	if !strings.Contains(got, "[redacted]") {
		t.Fatalf("primary log missing redaction marker: %s", got)
	}
}

type failingDebugLogWriter struct{}

func (failingDebugLogWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}
