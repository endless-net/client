package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
)

const DefaultLinuxServiceConfigPath = "/var/lib/endlessnet/client.json"

const ClientStateVersionUnsupportedCode = "client_state_version_unsupported"

const ClientStateFormatUnsupportedCode = "client_state_format_unsupported"

type ClientStateFormatUnsupportedError struct {
	Format string
}

func (e ClientStateFormatUnsupportedError) Error() string {
	return fmt.Sprintf("client state format %q is unsupported; format %q is required", e.Format, CurrentConfigStateFormat)
}

func IsClientStateFormatUnsupported(err error) bool {
	var unsupported ClientStateFormatUnsupportedError
	return errors.As(err, &unsupported)
}

type ClientStateVersionUnsupportedError struct {
	Version int
}

func (e ClientStateVersionUnsupportedError) Error() string {
	return fmt.Sprintf("client state version %d is unsupported; version %d is required", e.Version, CurrentConfigStateVersion)
}

func IsClientStateVersionUnsupported(err error) bool {
	var unsupported ClientStateVersionUnsupportedError
	return errors.As(err, &unsupported)
}

type configStoreMetadata struct {
	mu       sync.Mutex
	path     string
	revision uint64
	baseline []byte
}

// ConfigStore is the process-local source of truth for one client state file.
// It keeps the decoded state in memory and serializes every mutation before an
// atomic durable write. Save also performs a top-level three-way merge for
// callers using the LoadConfig/SaveConfig convenience pair, preventing an
// unrelated concurrent mutation from being lost.
type ConfigStore struct {
	path string

	mu       sync.RWMutex
	config   Config
	revision uint64
}

var configStores sync.Map

func OpenConfigStore(path string) (*ConfigStore, error) {
	resolved, err := resolveConfigPath(path)
	if err != nil {
		return nil, err
	}
	if existing, ok := configStores.Load(resolved); ok {
		return existing.(*ConfigStore), nil
	}
	cfg, err := loadConfigFile(resolved)
	if err != nil {
		return nil, err
	}
	store := &ConfigStore{path: resolved, config: cfg, revision: 1}
	actual, loaded := configStores.LoadOrStore(resolved, store)
	if loaded {
		return actual.(*ConfigStore), nil
	}
	return store, nil
}

func (s *ConfigStore) Read() Config {
	if s == nil {
		return Config{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return configForCaller(s.path, s.revision, s.config)
}

func (s *ConfigStore) Update(update func(*Config) error) error {
	if s == nil {
		return errors.New("config store is required")
	}
	if update == nil {
		return errors.New("config update is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := clonePersistentConfig(s.config)
	if err := update(&next); err != nil {
		return err
	}
	next = normalizePersistentConfig(next)
	if err := saveConfigFile(s.path, next); err != nil {
		return err
	}
	s.config = clonePersistentConfig(next)
	s.revision++
	return nil
}

func (s *ConfigStore) Save(cfg Config) error {
	if s == nil {
		return errors.New("config store is required")
	}
	proposed := clonePersistentConfig(cfg)
	proposed = normalizePersistentConfig(proposed)
	metadataPath, metadataRevision, baseline := configMetadataSnapshot(cfg.storeMetadata)

	s.mu.Lock()
	defer s.mu.Unlock()
	next := proposed
	if metadataPath == s.path && metadataRevision != 0 && metadataRevision != s.revision && len(baseline) != 0 {
		merged, err := mergeConfigChanges(baseline, proposed, s.config)
		if err != nil {
			return err
		}
		next = merged
	}
	next = normalizePersistentConfig(next)
	if err := saveConfigFile(s.path, next); err != nil {
		return err
	}
	s.config = clonePersistentConfig(next)
	s.revision++
	updateConfigMetadata(cfg.storeMetadata, s.path, s.revision, proposed)
	return nil
}

func resolveConfigPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		var err error
		path, err = DefaultConfigPath()
		if err != nil {
			return "", err
		}
	}
	if runtime.GOOS == "linux" && filepath.Clean(path) == "/etc/endlessnet/client.json" {
		return "", errors.New("client state path /etc/endlessnet/client.json is unsupported; use /var/lib/endlessnet/client.json")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func loadConfigFile(path string) (Config, error) {
	var cfg Config
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return normalizePersistentConfig(Config{}), nil
		}
		return cfg, err
	}
	raw, err = unprotectConfigState(raw)
	if err != nil {
		return cfg, fmt.Errorf("unprotect client state %s: %w", path, err)
	}
	if err := validateClientStateHeader(raw); err != nil {
		return cfg, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return cfg, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return cfg, fmt.Errorf("client state has trailing JSON: %w", err)
	}
	cfg.ControlPlaneURLs = clientapi.NormalizeControlPlaneURLs(cfg.ControlPlaneURLs...)
	if err := validateConfigFilePermissions(path, cfg); err != nil {
		return cfg, err
	}
	cfg.storeMetadata = nil
	return cfg, nil
}

func validateClientStateHeader(raw []byte) error {
	var header struct {
		StateFormat  *string `json:"state_format"`
		StateVersion *int    `json:"state_version"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return fmt.Errorf("decode client state header: %w", err)
	}
	if header.StateFormat == nil || *header.StateFormat != CurrentConfigStateFormat {
		format := ""
		if header.StateFormat != nil {
			format = *header.StateFormat
		}
		return ClientStateFormatUnsupportedError{Format: format}
	}
	if header.StateVersion == nil {
		return ClientStateVersionUnsupportedError{Version: 0}
	}
	if *header.StateVersion != CurrentConfigStateVersion {
		return ClientStateVersionUnsupportedError{Version: *header.StateVersion}
	}
	return nil
}

func saveConfigFile(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	cfg = clonePersistentConfig(cfg)
	cfg = normalizePersistentConfig(cfg)
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	raw, err = protectConfigState(raw)
	if err != nil {
		return fmt.Errorf("protect client state %s: %w", path, err)
	}
	return writeFileAtomic(path, raw, 0o600)
}

func normalizePersistentConfig(cfg Config) Config {
	cfg.StateFormat = CurrentConfigStateFormat
	cfg.StateVersion = CurrentConfigStateVersion
	cfg.ControlPlaneURLs = clientapi.NormalizeControlPlaneURLs(cfg.ControlPlaneURLs...)
	return cfg
}

func configForCaller(path string, revision uint64, cfg Config) Config {
	out := clonePersistentConfig(cfg)
	baseline, _ := json.Marshal(out)
	out.storeMetadata = &configStoreMetadata{
		path:     path,
		revision: revision,
		baseline: baseline,
	}
	return out
}

func clonePersistentConfig(cfg Config) Config {
	cfg.storeMetadata = nil
	raw, err := json.Marshal(cfg)
	if err != nil {
		return cfg
	}
	var clone Config
	if err := json.Unmarshal(raw, &clone); err != nil {
		return cfg
	}
	return clone
}

func configMetadataSnapshot(metadata *configStoreMetadata) (string, uint64, []byte) {
	if metadata == nil {
		return "", 0, nil
	}
	metadata.mu.Lock()
	defer metadata.mu.Unlock()
	return metadata.path, metadata.revision, append([]byte(nil), metadata.baseline...)
}

func updateConfigMetadata(metadata *configStoreMetadata, path string, revision uint64, baseline Config) {
	if metadata == nil {
		return
	}
	raw, err := json.Marshal(clonePersistentConfig(baseline))
	if err != nil {
		return
	}
	metadata.mu.Lock()
	metadata.path = path
	metadata.revision = revision
	metadata.baseline = raw
	metadata.mu.Unlock()
}

func mergeConfigChanges(baseline []byte, proposed, current Config) (Config, error) {
	var baseFields map[string]json.RawMessage
	if err := json.Unmarshal(baseline, &baseFields); err != nil {
		return Config{}, fmt.Errorf("decode config update baseline: %w", err)
	}
	proposedRaw, err := json.Marshal(clonePersistentConfig(proposed))
	if err != nil {
		return Config{}, err
	}
	currentRaw, err := json.Marshal(clonePersistentConfig(current))
	if err != nil {
		return Config{}, err
	}
	var proposedFields, currentFields map[string]json.RawMessage
	if err := json.Unmarshal(proposedRaw, &proposedFields); err != nil {
		return Config{}, err
	}
	if err := json.Unmarshal(currentRaw, &currentFields); err != nil {
		return Config{}, err
	}
	keys := make(map[string]struct{}, len(baseFields)+len(proposedFields))
	for key := range baseFields {
		keys[key] = struct{}{}
	}
	for key := range proposedFields {
		keys[key] = struct{}{}
	}
	for key := range keys {
		baseValue, baseOK := baseFields[key]
		proposedValue, proposedOK := proposedFields[key]
		if baseOK == proposedOK && bytes.Equal(baseValue, proposedValue) {
			continue
		}
		if !proposedOK {
			delete(currentFields, key)
			continue
		}
		currentFields[key] = proposedValue
	}
	mergedRaw, err := json.Marshal(currentFields)
	if err != nil {
		return Config{}, err
	}
	var merged Config
	if err := json.Unmarshal(mergedRaw, &merged); err != nil {
		return Config{}, err
	}
	return merged, nil
}
