package control

import (
	"crypto/ed25519"
	"encoding/base64"

	signingcontract "github.com/endless-net/signing/api/contract"
)

// EmbeddedBootstrapRoots is the release-owned independent public trust catalog.
// Map keys are canonical control origins; KeyID identifies the root material.
// Public inventory was obtained on 2026-11-12 through the authenticated spb SSH
// channel from OpenBao Transit endlessnet-bootstrap-root version1. Private key
// export/backup/deletion are disabled. No HTTP response can replace this anchor.
func EmbeddedBootstrapRoots() map[string]signingcontract.BootstrapRoot {
	public, err := base64.StdEncoding.Strict().DecodeString("NUyS36ISrNjTAzf3g+gdJirn4clrb9S/M9qKe+zM7Hs=")
	if err != nil || len(public) != ed25519.PublicKeySize {
		return nil
	}
	const id = "ed25519:kVpTYm6e_7P3FClgPRZ4Ql83ssXdvlXqC0GVD9FJEC4"
	return map[string]signingcontract.BootstrapRoot{"https://api.endlessnet.ru": {KeyID: id, PublicKey: ed25519.PublicKey(public)}}
}
