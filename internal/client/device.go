package client

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	wgkeys "github.com/unng-lab/endlessnet/clientapi/wireguard"
)

type installationState struct {
	InstallationID string    `json:"installation_id"`
	CreatedAt      time.Time `json:"created_at"`
}

func DeviceFingerprint(serverURL, publicKey string) (string, error) {
	installationID, err := loadOrCreateInstallationID()
	if err != nil {
		return "", err
	}
	parts := []string{
		"endlessnet-device-fingerprint-v1",
		strings.TrimSpace(installationID),
		strings.TrimRight(strings.TrimSpace(serverURL), "/"),
		strings.TrimSpace(publicKey),
	}
	hostBinding, err := hostInstallationBinding()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(hostBinding) != "" {
		parts = append(parts, strings.TrimSpace(hostBinding))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:]), nil
}

func DeviceFingerprintForConfig(cfg Config) (string, error) {
	urls := cfg.ControlURLs()
	if len(urls) == 0 {
		return "", errors.New("server URL is required")
	}
	if strings.TrimSpace(cfg.PrivateKey) == "" {
		return "", errors.New("private key is missing")
	}
	publicKey, err := wgkeys.PublicKey(cfg.PrivateKey)
	if err != nil {
		return "", err
	}
	return DeviceFingerprint(urls[0], publicKey)
}

func BindConfigDeviceFingerprint(cfg *Config, fingerprint string) (bool, error) {
	if cfg == nil {
		return false, errors.New("config is required")
	}
	fingerprint = strings.TrimSpace(fingerprint)
	if fingerprint == "" {
		return false, errors.New("device fingerprint is required")
	}
	stored := strings.TrimSpace(cfg.DeviceFingerprint)
	if stored != "" && stored != fingerprint {
		return false, fmt.Errorf("client state is bound to a different installation fingerprint; re-enroll this device instead of copying client.json")
	}
	if stored == "" {
		cfg.DeviceFingerprint = fingerprint
		return true, nil
	}
	return false, nil
}

func ValidateConfigCurrentDevice(cfg Config) error {
	if strings.TrimSpace(cfg.DeviceFingerprint) == "" {
		return nil
	}
	fingerprint, err := DeviceFingerprintForConfig(cfg)
	if err != nil {
		return err
	}
	_, err = BindConfigDeviceFingerprint(&cfg, fingerprint)
	return err
}

func loadOrCreateInstallationID() (string, error) {
	path, err := installationStatePath()
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(path)
	if err == nil {
		var state installationState
		if err := json.Unmarshal(raw, &state); err != nil {
			return "", err
		}
		if strings.TrimSpace(state.InstallationID) == "" {
			return "", errors.New("installation_id is empty")
		}
		return strings.TrimSpace(state.InstallationID), nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	state := installationState{
		InstallationID: randomInstallationID(),
		CreatedAt:      time.Now().UTC(),
	}
	raw, err = json.MarshalIndent(state, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := writeFileAtomic(path, raw, 0o600); err != nil {
		return "", err
	}
	return state.InstallationID, nil
}

func installationStatePath() (string, error) {
	if dir, ok, err := platformInstallationStateDir(); err != nil || ok {
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "installation.json"), nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "endlessnet", "installation.json"), nil
}

func randomInstallationID() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return hex.EncodeToString(buf)
}
