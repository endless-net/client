//go:build windows

package client

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

func hostInstallationBinding() (string, error) {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Cryptography`, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return "", err
	}
	defer func() { _ = key.Close() }()
	guid, _, err := key.GetStringValue("MachineGuid")
	if err != nil {
		return "", err
	}
	guid = strings.TrimSpace(guid)
	if guid == "" {
		return "", errors.New("windows machine guid is empty")
	}
	sum := sha256.Sum256([]byte("endlessnet-windows-machine-guid-v1\x00" + strings.ToLower(guid)))
	return "windows-machine-guid-sha256:" + hex.EncodeToString(sum[:]), nil
}

func platformInstallationStateDir() (string, bool, error) {
	programData := strings.TrimSpace(os.Getenv("PROGRAMDATA"))
	if programData == "" {
		programData = `C:\ProgramData`
	}
	return filepath.Join(programData, "EndlessNet"), true, nil
}
