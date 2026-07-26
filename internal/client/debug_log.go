package client

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	DefaultDebugLogDir = `~\.endlessnet\logs`
	debugLogMaxBytes   = 10 << 20
	debugLogBackups    = 3
)

type DebugLogHandle struct {
	previousFlags int
	previousOut   io.Writer
	file          *os.File
}

func ConfigureDebugLogger(component, dir string) (*DebugLogHandle, error) {
	component = safeDebugLogComponent(component)
	if strings.TrimSpace(dir) == "" {
		dir = DefaultDebugLogDir
	}
	resolvedDir, err := ResolveDebugLogDir(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(resolvedDir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(resolvedDir, component+".log")
	if err := rotateDebugLog(path); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	handle := &DebugLogHandle{
		previousFlags: log.Flags(),
		previousOut:   log.Writer(),
		file:          file,
	}
	_, _ = file.WriteString(time.Now().UTC().Format(time.RFC3339Nano) + " debug logger opened component=" + component + " path=" + path + "\n")
	_ = file.Sync()
	log.SetFlags(log.LstdFlags | log.Lmicroseconds | log.LUTC | log.Lshortfile)
	log.SetOutput(bestEffortDebugLogWriter{
		primary:   redactingDebugLogWriter{w: file},
		secondary: handle.previousOut,
	})
	log.Printf("debug logging enabled component=%s path=%s", component, path)
	_ = file.Sync()
	return handle, nil
}

func (h *DebugLogHandle) Close() error {
	if h == nil {
		return nil
	}
	log.SetFlags(h.previousFlags)
	log.SetOutput(h.previousOut)
	if h.file == nil {
		return nil
	}
	return h.file.Close()
}

func ResolveDebugLogDir(dir string) (string, error) {
	dir = strings.TrimSpace(os.ExpandEnv(dir))
	if dir == "" {
		dir = DefaultDebugLogDir
	}
	if filepath.Separator != '\\' {
		dir = strings.ReplaceAll(dir, `\`, string(filepath.Separator))
	}
	if dir == "~" || strings.HasPrefix(dir, `~\`) || strings.HasPrefix(dir, "~/") {
		home, err := debugLogUserHome()
		if err != nil {
			return "", err
		}
		if dir == "~" {
			return filepath.Clean(home), nil
		}
		return filepath.Clean(filepath.Join(home, dir[2:])), nil
	}
	return filepath.Abs(dir)
}

func safeDebugLogComponent(component string) string {
	component = strings.TrimSpace(strings.ToLower(component))
	if component == "" {
		component = "endlessnet"
	}
	var b strings.Builder
	for _, r := range component {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func rotateDebugLog(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Size() < debugLogMaxBytes {
		return nil
	}
	for i := debugLogBackups - 1; i >= 1; i-- {
		from := fmt.Sprintf("%s.%d", path, i)
		to := fmt.Sprintf("%s.%d", path, i+1)
		if _, err := os.Stat(from); err == nil {
			_ = os.Rename(from, to)
		}
	}
	return os.Rename(path, path+".1")
}

type redactingDebugLogWriter struct {
	w io.Writer
}

func (w redactingDebugLogWriter) Write(p []byte) (int, error) {
	if w.w == nil {
		return len(p), nil
	}
	_, err := w.w.Write([]byte(redactDebugLogText(string(p))))
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

type bestEffortDebugLogWriter struct {
	primary   io.Writer
	secondary io.Writer
}

func (w bestEffortDebugLogWriter) Write(p []byte) (int, error) {
	if w.primary != nil {
		_, _ = w.primary.Write(p)
	}
	if w.secondary != nil {
		_, _ = w.secondary.Write(p)
	}
	return len(p), nil
}

var debugLogRedactors = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(token|session|secret|credential|private[_-]?key|join[_-]?token|enrollment[_-]?token)(["'=:\s]+)([^"'\s,;}]+)`),
	regexp.MustCompile(`(?i)(Bearer\s+)[A-Za-z0-9._~+/=-]+`),
}

func redactDebugLogText(value string) string {
	out := value
	for _, re := range debugLogRedactors {
		out = re.ReplaceAllStringFunc(out, func(match string) string {
			parts := re.FindStringSubmatch(match)
			if len(parts) >= 4 {
				return parts[1] + parts[2] + "[redacted]"
			}
			if len(parts) >= 2 {
				return "[redacted]"
			}
			return "[redacted]"
		})
	}
	return out
}
