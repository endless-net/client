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
	"time"

	clientapi "github.com/unng-lab/endlessnet/clientapi/v1"
)

const LegacyConfigStateVersion = 2

type ConfigStateMigrationResult struct {
	StatePath     string `json:"state_path"`
	BackupPath    string `json:"backup_path,omitempty"`
	SourceFormat  string `json:"source_format"`
	SourceVersion int    `json:"source_version"`
	TargetFormat  string `json:"target_format"`
	TargetVersion int    `json:"target_version"`
	Migrated      bool   `json:"migrated"`
}

type legacyConfigStateV2 struct {
	StateVersion          *int                            `json:"state_version"`
	ServerURL             string                          `json:"server_url"`
	ServerURLs            []string                        `json:"server_urls,omitempty"`
	Token                 string                          `json:"token"`
	ActiveAccountID       string                          `json:"active_account_id,omitempty"`
	IdentityPrivateKey    string                          `json:"identity_private_key,omitempty"`
	PrivateKey            string                          `json:"private_key"`
	NodeID                string                          `json:"node_id,omitempty"`
	NetworkID             string                          `json:"network_id,omitempty"`
	NodeCredential        string                          `json:"node_credential,omitempty"`
	NodeApprovalState     string                          `json:"node_approval_state,omitempty"`
	EnrollmentRequestID   string                          `json:"enrollment_request_id,omitempty"`
	EnrollmentPollToken   string                          `json:"enrollment_poll_token,omitempty"`
	EnrollmentApprovalURL string                          `json:"enrollment_approval_url,omitempty"`
	EnrollmentRequest     *clientapi.RegisterNodeRequest  `json:"enrollment_request,omitempty"`
	DeviceFingerprint     string                          `json:"device_fingerprint,omitempty"`
	MapSigningTrust       *clientapi.SigningTrustBundle   `json:"map_signing_trust_bundle,omitempty"`
	MapRevision           uint64                          `json:"map_revision,omitempty"`
	SubnetRouterSNAT      bool                            `json:"subnet_router_snat,omitempty"`
	ExitLANPolicy         string                          `json:"exit_lan_policy,omitempty"`
	WireGuardMTU          int                             `json:"wireguard_mtu,omitempty"`
	WireGuardRouteTable   string                          `json:"wireguard_route_table,omitempty"`
	CachedMap             *clientapi.RegisterNodeResponse `json:"cached_map,omitempty"`
	CachedMapSavedAt      *time.Time                      `json:"cached_map_saved_at,omitempty"`
	ConnectionIntent      *ConnectionIntent               `json:"connection_intent,omitempty"`
}

func MigrateConfigState(path, backupPath string) (ConfigStateMigrationResult, error) {
	resolved, err := resolveConfigPath(path)
	if err != nil {
		return ConfigStateMigrationResult{}, err
	}
	result := ConfigStateMigrationResult{
		StatePath:     resolved,
		TargetFormat:  CurrentConfigStateFormat,
		TargetVersion: CurrentConfigStateVersion,
	}
	lockPath, err := AgentLockPath(resolved)
	if err != nil {
		return result, err
	}
	lock, err := AcquireAgentLock(lockPath)
	if err != nil {
		return result, fmt.Errorf("lock client state migration: %w", err)
	}
	defer func() { _ = lock.Close() }()

	info, err := os.Lstat(resolved)
	if err != nil {
		return result, fmt.Errorf("stat client state: %w", err)
	}
	if !info.Mode().IsRegular() {
		return result, fmt.Errorf("client state %s is not a regular file", resolved)
	}
	sourceRaw, err := os.ReadFile(resolved)
	if err != nil {
		return result, fmt.Errorf("read client state: %w", err)
	}
	if currentRaw, currentErr := unprotectConfigState(sourceRaw); currentErr == nil {
		current, present, headerErr := currentConfigStateHeader(currentRaw)
		if headerErr != nil {
			return result, headerErr
		}
		if present {
			result.SourceFormat = current.StateFormat
			result.SourceVersion = current.StateVersion
			if err := validateClientStateHeader(currentRaw); err != nil {
				return result, err
			}
			if _, err := loadConfigFile(resolved); err != nil {
				return result, fmt.Errorf("validate current client state: %w", err)
			}
			return result, nil
		}
	}

	legacyRaw, err := unprotectLegacyConfigState(sourceRaw)
	if err != nil {
		return result, fmt.Errorf("unprotect legacy client state: %w", err)
	}
	legacy, err := decodeLegacyConfigStateV2(legacyRaw)
	if err != nil {
		return result, err
	}
	result.SourceFormat = "legacy"
	if legacy.StateVersion != nil {
		result.SourceVersion = *legacy.StateVersion
	}
	if legacy.StateVersion == nil || *legacy.StateVersion != LegacyConfigStateVersion {
		return result, fmt.Errorf(
			"legacy client state version %d is unsupported; version %d is required for migration",
			result.SourceVersion,
			LegacyConfigStateVersion,
		)
	}
	migrated := legacy.currentConfig()
	if err := validateConfigFilePermissions(resolved, migrated); err != nil {
		return result, err
	}
	resolvedBackup, err := resolveMigrationBackupPath(resolved, backupPath)
	if err != nil {
		return result, err
	}
	result.BackupPath = resolvedBackup
	if err := ensureMigrationBackup(resolvedBackup, sourceRaw); err != nil {
		return result, err
	}
	if err := saveConfigFile(resolved, migrated); err != nil {
		return result, fmt.Errorf("write migrated client state: %w", err)
	}
	if _, err := loadConfigFile(resolved); err != nil {
		restoreErr := writeFileAtomic(resolved, sourceRaw, info.Mode().Perm())
		if restoreErr != nil {
			return result, errors.Join(
				fmt.Errorf("validate migrated client state: %w", err),
				fmt.Errorf("restore legacy client state: %w", restoreErr),
			)
		}
		return result, fmt.Errorf("validate migrated client state; restored legacy state: %w", err)
	}
	result.Migrated = true
	return result, nil
}

type configStateHeader struct {
	StateFormat  string `json:"state_format"`
	StateVersion int    `json:"state_version"`
}

func currentConfigStateHeader(raw []byte) (configStateHeader, bool, error) {
	var fields struct {
		StateFormat  *string `json:"state_format"`
		StateVersion *int    `json:"state_version"`
	}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return configStateHeader{}, false, fmt.Errorf("decode client state header: %w", err)
	}
	if fields.StateFormat == nil {
		return configStateHeader{}, false, nil
	}
	header := configStateHeader{StateFormat: *fields.StateFormat}
	if fields.StateVersion != nil {
		header.StateVersion = *fields.StateVersion
	}
	return header, true, nil
}

func decodeLegacyConfigStateV2(raw []byte) (legacyConfigStateV2, error) {
	var legacy legacyConfigStateV2
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&legacy); err != nil {
		return legacy, fmt.Errorf("decode legacy client state v2: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return legacy, fmt.Errorf("legacy client state has trailing JSON: %w", err)
	}
	return legacy, nil
}

func (legacy legacyConfigStateV2) currentConfig() Config {
	urls := append([]string{legacy.ServerURL}, legacy.ServerURLs...)
	return normalizePersistentConfig(Config{
		ControlPlaneURLs:    clientapi.NormalizeControlPlaneURLs(urls...),
		Token:               legacy.Token,
		ActiveAccountID:     legacy.ActiveAccountID,
		IdentityPrivateKey:  legacy.IdentityPrivateKey,
		PrivateKey:          legacy.PrivateKey,
		NodeID:              legacy.NodeID,
		NetworkID:           legacy.NetworkID,
		NodeCredential:      legacy.NodeCredential,
		NodeApprovalState:   legacy.NodeApprovalState,
		EnrollmentRequestID: legacy.EnrollmentRequestID,
		EnrollmentPollToken: legacy.EnrollmentPollToken,
		ApprovalURL:         legacy.EnrollmentApprovalURL,
		EnrollmentRequest:   legacy.EnrollmentRequest,
		DeviceFingerprint:   legacy.DeviceFingerprint,
		MapSigningTrust:     legacy.MapSigningTrust,
		MapRevision:         legacy.MapRevision,
		SubnetRouterSNAT:    legacy.SubnetRouterSNAT,
		ExitLANPolicy:       legacy.ExitLANPolicy,
		WireGuardMTU:        legacy.WireGuardMTU,
		WireGuardRouteTable: legacy.WireGuardRouteTable,
		CachedMap:           legacy.CachedMap,
		CachedMapSavedAt:    legacy.CachedMapSavedAt,
		ConnectionIntent:    legacy.ConnectionIntent,
	})
}

func resolveMigrationBackupPath(statePath, backupPath string) (string, error) {
	backupPath = strings.TrimSpace(backupPath)
	if backupPath == "" {
		backupPath = statePath + ".pre-migration-v2.bak"
	}
	abs, err := filepath.Abs(backupPath)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	samePath := abs == statePath
	if runtime.GOOS == "windows" {
		samePath = strings.EqualFold(abs, statePath)
	}
	if samePath {
		return "", errors.New("migration backup path must differ from client state path")
	}
	return abs, nil
}

func ensureMigrationBackup(path string, raw []byte) error {
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("migration backup %s is not a regular file", path)
		}
		existing, readErr := os.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("read migration backup: %w", readErr)
		}
		if !bytes.Equal(existing, raw) {
			return fmt.Errorf("migration backup %s already exists with different content", path)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
			return fmt.Errorf("migration backup %s must not be accessible by group or other users", path)
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat migration backup: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create migration backup directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create migration backup: %w", err)
	}
	remove := true
	defer func() {
		if remove {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(raw); err != nil {
		_ = file.Close()
		return fmt.Errorf("write migration backup: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync migration backup: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close migration backup: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("secure migration backup: %w", err)
	}
	remove = false
	syncDirectory(filepath.Dir(path))
	return nil
}
