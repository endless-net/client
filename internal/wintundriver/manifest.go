// Package wintundriver verifies the exact producer binary bundled with Client.
// It does not install a driver or create a network adapter.
package wintundriver

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
)

var ErrDriver = errors.New("bundled Wintun driver unavailable or invalid")

//go:embed manifest.json
var manifestBytes []byte

type binaryPin struct {
	Entry  string `json:"entry"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type manifest struct {
	Version string               `json:"version"`
	Files   map[string]binaryPin `json:"files"`
}

func bundled() (manifest, error) {
	var m manifest
	if json.Unmarshal(manifestBytes, &m) != nil || m.Version == "" {
		return manifest{}, ErrDriver
	}
	p := m.Files["wintun.dll"]
	if p.Size <= 0 || len(p.SHA256) != 64 || p.Entry != "wintun/bin/amd64/wintun.dll" {
		return manifest{}, ErrDriver
	}
	return m, nil
}

func verify(r io.Reader, pin binaryPin) error {
	if r == nil || pin.Size <= 0 {
		return ErrDriver
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(r, pin.Size+1))
	if err != nil || n != pin.Size || hex.EncodeToString(h.Sum(nil)) != pin.SHA256 {
		return ErrDriver
	}
	return nil
}
