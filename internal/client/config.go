package client

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
)

const atomicTempFileMaxAge = 24 * time.Hour

const (
	CurrentConfigStateFormat  = "endlessnet-client-state"
	CurrentConfigStateVersion = 1
)

const (
	ExitLANPolicyAllow = "allow"
	ExitLANPolicyBlock = "block"
	MinWireGuardMTU    = 1280
	MaxWireGuardMTU    = 65535
)

type Config struct {
	StateFormat         string                          `json:"state_format"`
	StateVersion        int                             `json:"state_version"`
	LocalOwnerID        string                          `json:"local_owner_id,omitempty"`
	ControlPlaneURLs    []string                        `json:"control_plane_urls,omitempty"`
	ManagementURL       string                          `json:"management_url,omitempty"`
	Token               string                          `json:"token"`
	ActiveAccountID     string                          `json:"active_account_id,omitempty"`
	IdentityPrivateKey  string                          `json:"identity_private_key,omitempty"`
	PrivateKey          string                          `json:"private_key"`
	NodeID              string                          `json:"node_id,omitempty"`
	NetworkID           string                          `json:"network_id,omitempty"`
	NodeCredential      string                          `json:"node_credential,omitempty"`
	NodeApprovalState   string                          `json:"node_approval_state,omitempty"`
	EnrollmentRequestID string                          `json:"enrollment_request_id,omitempty"`
	EnrollmentPollToken string                          `json:"enrollment_poll_token,omitempty"`
	ApprovalURL         string                          `json:"approval_url,omitempty"`
	EnrollmentRequest   *clientapi.RegisterNodeRequest  `json:"enrollment_request,omitempty"`
	DeviceFingerprint   string                          `json:"device_fingerprint,omitempty"`
	MapSigningTrust     *clientapi.SigningTrustBundle   `json:"map_signing_trust_bundle,omitempty"`
	MapRevision         uint64                          `json:"map_revision,omitempty"`
	MapGlobalRevision   uint64                          `json:"map_global_revision,omitempty"`
	MapHash             string                          `json:"map_hash,omitempty"`
	SubnetRouterSNAT    bool                            `json:"subnet_router_snat,omitempty"`
	ExitLANPolicy       string                          `json:"exit_lan_policy,omitempty"`
	WireGuardMTU        int                             `json:"wireguard_mtu,omitempty"`
	WireGuardRouteTable string                          `json:"wireguard_route_table,omitempty"`
	CachedMap           *clientapi.RegisterNodeResponse `json:"cached_map,omitempty"`
	CachedMapSavedAt    *time.Time                      `json:"cached_map_saved_at,omitempty"`
	ConnectionIntent    *ConnectionIntent               `json:"connection_intent,omitempty"`

	storeMetadata *configStoreMetadata
}

func NormalizeWireGuardRouteTable(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "":
		return "", nil
	case "auto", "off":
		return value, nil
	}
	if strings.HasPrefix(value, "+") || strings.HasPrefix(value, "-") {
		return "", fmt.Errorf("wireguard_route_table must be empty, %q, %q, or a positive numeric table ID", "auto", "off")
	}
	n, err := strconv.ParseUint(value, 10, 32)
	if err != nil || n == 0 {
		return "", fmt.Errorf("wireguard_route_table must be empty, %q, %q, or a positive numeric table ID", "auto", "off")
	}
	return strconv.FormatUint(n, 10), nil
}

func NormalizeWireGuardMTU(value int) (int, error) {
	if value == 0 {
		return 0, nil
	}
	if value < MinWireGuardMTU || value > MaxWireGuardMTU {
		return 0, fmt.Errorf("wireguard_mtu must be 0 or between %d and %d", MinWireGuardMTU, MaxWireGuardMTU)
	}
	return value, nil
}

func NormalizeExitLANPolicy(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "":
		return "", nil
	case ExitLANPolicyAllow, ExitLANPolicyBlock:
		return value, nil
	default:
		return "", fmt.Errorf("exit_lan_policy must be %q or %q", ExitLANPolicyAllow, ExitLANPolicyBlock)
	}
}

func ExitLANPolicyBlocksLocalLAN(value string) (bool, error) {
	policy, err := NormalizeExitLANPolicy(value)
	if err != nil {
		return false, err
	}
	return policy == ExitLANPolicyBlock, nil
}

func (c Config) ControlURLs() []string {
	return clientapi.NormalizeControlPlaneURLs(c.ControlPlaneURLs...)
}

func DefaultConfigPath() (string, error) {
	if runtime.GOOS == "linux" {
		return DefaultLinuxServiceConfigPath, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "endlessnet", "client.json"), nil
}

func LoadConfig(path string) (Config, error) {
	store, err := OpenConfigStore(path)
	if err != nil {
		return Config{}, err
	}
	return store.Read(), nil
}

func SaveConfig(path string, cfg Config) error {
	store, err := OpenConfigStore(path)
	if err != nil {
		return err
	}
	return store.Save(cfg)
}

func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	if path == "" {
		return errors.New("path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeFileAtomic(path, data, perm)
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	name := filepath.Base(path)
	cleanupStaleAtomicTempFiles(dir, name, atomicTempFileMaxAge)
	tmpPath, err := temporaryPath(dir, name)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		return err
	}
	if err := replaceFileAtomic(tmpPath, path); err != nil {
		return err
	}
	cleanup = false
	syncDirectory(dir)
	return nil
}

func temporaryPath(dir, name string) (string, error) {
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", err
	}
	return filepath.Join(dir, fmt.Sprintf(".%s.tmp-%s", name, hex.EncodeToString(suffix[:]))), nil
}

func cleanupStaleAtomicTempFiles(dir, name string, maxAge time.Duration) {
	if maxAge <= 0 {
		return
	}
	matches, err := filepath.Glob(filepath.Join(dir, "."+name+".tmp-*"))
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-maxAge)
	for _, match := range matches {
		info, err := os.Stat(match)
		if err != nil || !info.Mode().IsRegular() || !info.ModTime().Before(cutoff) {
			continue
		}
		_ = os.Remove(match)
	}
}

func syncDirectory(dir string) {
	handle, err := os.Open(dir)
	if err != nil {
		return
	}
	defer func() { _ = handle.Close() }()
	_ = handle.Sync()
}

func validateConfigFilePermissions(path string, cfg Config) error {
	if runtime.GOOS == "windows" || !configContainsSecrets(cfg) {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("client config %s is not a regular file", path)
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		return fmt.Errorf("client config %s contains credentials and must not be readable or writable by group or other users; mode is %04o, want 0600 or stricter", path, mode)
	}
	return nil
}

func configContainsSecrets(cfg Config) bool {
	return strings.TrimSpace(cfg.Token) != "" ||
		strings.TrimSpace(cfg.IdentityPrivateKey) != "" ||
		strings.TrimSpace(cfg.PrivateKey) != "" ||
		strings.TrimSpace(cfg.NodeCredential) != "" ||
		strings.TrimSpace(cfg.EnrollmentPollToken) != ""
}
