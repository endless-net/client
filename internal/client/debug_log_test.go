package client

import (
	"bytes"
	"errors"
	"log"
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

func TestConfigureDebugLoggerRedactsFileAndFallback(t *testing.T) {
	var fallback bytes.Buffer
	previousOutput := log.Writer()
	previousFlags := log.Flags()
	log.SetOutput(&fallback)
	t.Cleanup(func() {
		log.SetOutput(previousOutput)
		log.SetFlags(previousFlags)
	})
	dir := t.TempDir()
	handle, err := ConfigureDebugLogger("agent", dir)
	if err != nil {
		t.Fatal(err)
	}
	log.Print("debug token=synthetic-secret")
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join(dir, "agent.log"))
	if err != nil {
		t.Fatal(err)
	}
	for name, output := range map[string]string{"file": string(contents), "fallback": fallback.String()} {
		if strings.Contains(output, "synthetic-secret") || !strings.Contains(output, "[redacted") {
			t.Fatalf("%s output was not redacted: %s", name, output)
		}
	}
}

func TestDebugLogRotatesWhileRunning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.log")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	writer := &rotatingDebugLogWriter{path: path, file: file, maxBytes: 12}
	for _, line := range []string{"first\n", "second\n", "third\n", "fourth\n", "fifth\n"} {
		if _, err := writer.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"agent.log": "fifth\n", "agent.log.1": "fourth\n", "agent.log.2": "third\n", "agent.log.3": "second\n"} {
		contents, err := os.ReadFile(filepath.Join(filepath.Dir(path), name))
		if err != nil || string(contents) != want {
			t.Fatalf("%s = %q, %v; want %q", name, contents, err, want)
		}
	}
	if _, err := os.Stat(path + ".4"); !os.IsNotExist(err) {
		t.Fatalf("old backup was not removed: %v", err)
	}
}

type failingDebugLogWriter struct{}

func (failingDebugLogWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}
