package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/endless-net/client/internal/client"
)

const (
	diagnosticsBundleMaxFileBytes = 5 << 20
	diagnosticsBundleMaxFiles     = 10
	diagnosticsBundleMaxTotal     = 50 << 20
	diagnosticsBundleRetention    = 7 * 24 * time.Hour
	diagnosticsBundleReuseWindow  = time.Minute
)

var diagnosticsBundleNamePattern = regexp.MustCompile(`^diagnostics-[0-9]{8}T[0-9]{6}\.[0-9]{9}Z-[0-9a-f]{16}\.json$`)

type diagnosticsStore struct {
	dir string
	mu  sync.Mutex
}

type diagnosticsBundleInfo struct {
	Path      string
	CreatedAt time.Time
	ExpiresAt time.Time
	SizeBytes int64
	Reused    bool
}

type diagnosticsBundleFile struct {
	name    string
	path    string
	size    int64
	created time.Time
}

func newDiagnosticsStore(dir string) *diagnosticsStore {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return &diagnosticsStore{}
	}
	dir = filepath.Clean(dir)
	if absolute, err := filepath.Abs(dir); err == nil {
		dir = absolute
	}
	return &diagnosticsStore{dir: dir}
}

func (s *diagnosticsStore) Prune() error {
	if s == nil || strings.TrimSpace(s.dir) == "" || s.dir == "." {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.pruneLocked(time.Now().UTC(), 0)
	return err
}

func (s *diagnosticsStore) Write(payload any) (diagnosticsBundleInfo, error) {
	if s == nil || strings.TrimSpace(s.dir) == "" || s.dir == "." {
		return diagnosticsBundleInfo{}, errors.New("diagnostics bundle directory is not configured")
	}
	raw, err := json.MarshalIndent(sanitizeDiagnosticsAnyForJSON(payload), "", "  ")
	if err != nil {
		return diagnosticsBundleInfo{}, err
	}
	raw = append(raw, '\n')
	if len(raw) > diagnosticsBundleMaxFileBytes {
		return diagnosticsBundleInfo{}, fmt.Errorf("diagnostics bundle exceeds %d bytes", diagnosticsBundleMaxFileBytes)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	files, err := s.pruneLocked(now, 0)
	if err != nil {
		return diagnosticsBundleInfo{}, err
	}
	if len(files) > 0 {
		latest := files[len(files)-1]
		if age := now.Sub(latest.created); age >= 0 && age <= diagnosticsBundleReuseWindow {
			return diagnosticsBundleInfo{
				Path: latest.path, CreatedAt: latest.created, ExpiresAt: latest.created.Add(diagnosticsBundleRetention),
				SizeBytes: latest.size, Reused: true,
			}, nil
		}
	}
	if _, err := s.pruneLocked(now, int64(len(raw))); err != nil {
		return diagnosticsBundleInfo{}, err
	}

	name, err := newDiagnosticsBundleName(now)
	if err != nil {
		return diagnosticsBundleInfo{}, err
	}
	path := filepath.Join(s.dir, name)
	if filepath.Dir(path) != s.dir {
		return diagnosticsBundleInfo{}, errors.New("diagnostics bundle path escapes its store")
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		if err == nil {
			return diagnosticsBundleInfo{}, errors.New("diagnostics bundle name collision")
		}
		return diagnosticsBundleInfo{}, err
	}
	if err := client.WriteFileAtomic(path, raw, 0o600); err != nil {
		return diagnosticsBundleInfo{}, err
	}
	if err := secureDiagnosticsBundleFile(path); err != nil {
		_ = os.Remove(path)
		return diagnosticsBundleInfo{}, fmt.Errorf("secure diagnostics bundle file: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return diagnosticsBundleInfo{}, err
	}
	if !info.Mode().IsRegular() || isDiagnosticsReparsePoint(info) || !diagnosticsFilePermissionsSecure(path, info) || info.Size() != int64(len(raw)) {
		_ = os.Remove(path)
		return diagnosticsBundleInfo{}, errors.New("diagnostics bundle store created an unsafe file")
	}
	// Reuse and retention read the persisted file timestamp, which may differ
	// from the pre-write clock (including filesystem timestamp precision).
	created := info.ModTime().UTC()
	return diagnosticsBundleInfo{
		Path: path, CreatedAt: created, ExpiresAt: created.Add(diagnosticsBundleRetention), SizeBytes: info.Size(),
	}, nil
}

func (s *diagnosticsStore) pruneLocked(now time.Time, requiredBytes int64) ([]diagnosticsBundleFile, error) {
	if err := validateExistingDiagnosticsPath(s.dir); err != nil {
		return nil, err
	}
	_, beforeCreateErr := os.Lstat(s.dir)
	directoryCreated := os.IsNotExist(beforeCreateErr)
	if beforeCreateErr != nil && !directoryCreated {
		return nil, beforeCreateErr
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return nil, err
	}
	if directoryCreated {
		if err := secureNewDiagnosticsDirectory(s.dir); err != nil {
			return nil, fmt.Errorf("secure diagnostics bundle directory: %w", err)
		}
	}
	if err := validateExistingDiagnosticsPath(s.dir); err != nil {
		return nil, err
	}
	dirInfo, err := os.Lstat(s.dir)
	if err != nil {
		return nil, err
	}
	if !dirInfo.IsDir() || isDiagnosticsReparsePoint(dirInfo) || !diagnosticsDirectoryPermissionsSecure(s.dir, dirInfo) {
		return nil, errors.New("diagnostics bundle store must be a protected local directory without links, reparse points, or untrusted access")
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	files := make([]diagnosticsBundleFile, 0, len(entries))
	for _, entry := range entries {
		if !diagnosticsBundleNamePattern.MatchString(entry.Name()) {
			continue
		}
		path := filepath.Join(s.dir, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || isDiagnosticsReparsePoint(info) || !diagnosticsFilePermissionsSecure(path, info) {
			return nil, fmt.Errorf("diagnostics bundle %s is not a regular owned file", entry.Name())
		}
		if info.Size() > diagnosticsBundleMaxFileBytes || now.Sub(info.ModTime()) > diagnosticsBundleRetention {
			if err := os.Remove(path); err != nil {
				return nil, err
			}
			continue
		}
		files = append(files, diagnosticsBundleFile{name: entry.Name(), path: path, size: info.Size(), created: info.ModTime().UTC()})
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].created.Equal(files[j].created) {
			return files[i].name < files[j].name
		}
		return files[i].created.Before(files[j].created)
	})
	total := int64(0)
	for _, file := range files {
		total += file.size
	}
	extraFile := 0
	if requiredBytes > 0 {
		extraFile = 1
	}
	for len(files) > 0 && (len(files)+extraFile > diagnosticsBundleMaxFiles || total+requiredBytes > diagnosticsBundleMaxTotal) {
		oldest := files[0]
		if err := os.Remove(oldest.path); err != nil {
			return nil, err
		}
		total -= oldest.size
		files = files[1:]
	}
	if requiredBytes > diagnosticsBundleMaxTotal {
		return nil, errors.New("diagnostics bundle cannot fit within the store quota")
	}
	return files, nil
}

func validateExistingDiagnosticsPath(path string) error {
	path = filepath.Clean(path)
	volume := filepath.VolumeName(path)
	remainder := strings.TrimPrefix(path, volume)
	current := volume + string(filepath.Separator)
	for _, component := range strings.FieldsFunc(remainder, func(r rune) bool { return r == '/' || r == '\\' }) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		trustedAlias := isTrustedDiagnosticsPathAlias(current, info)
		if isDiagnosticsReparsePoint(info) && !trustedAlias {
			return fmt.Errorf("diagnostics bundle path component %s is a link or reparse point", current)
		}
		if !info.IsDir() && !trustedAlias && current != path {
			return fmt.Errorf("diagnostics bundle path component %s is not a directory", current)
		}
	}
	return nil
}

func newDiagnosticsBundleName(now time.Time) (string, error) {
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("diagnostics-%s-%s.json", now.UTC().Format("20060102T150405.000000000Z"), hex.EncodeToString(nonce[:])), nil
}
