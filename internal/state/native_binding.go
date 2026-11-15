package state

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
)

// NativeLoginBinding implements the published native_login.proto ordered,
// length-prefixed binding. The same routine validates enrollment and re-login.
func NativeLoginBinding(p Profile) (string, [32]byte, error) {
	if p.Attempt == nil || p.Attempt.Validate() != nil {
		return "", [32]byte{}, ErrCorrupt
	}
	fingerprint, err := p.Keys.Fingerprint()
	if err != nil || fingerprint != p.Attempt.DeviceKeyHash || p.ID != p.Attempt.ProfileID || p.ControlOrigin != p.Attempt.ControlOrigin {
		return "", [32]byte{}, ErrCorrupt
	}
	public := "enp_" + base64.RawURLEncoding.EncodeToString(p.Keys.IdentityPrivate[ed25519.SeedSize:])
	h := sha256.New()
	for _, field := range []string{"endlessnet-native-login-003-v1", p.ControlOrigin, p.ID, p.Attempt.AttemptID, p.Attempt.RequestID, public} {
		if uint64(len(field)) > uint64(^uint32(0)) {
			return "", [32]byte{}, ErrCorrupt
		}
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(len(field)))
		_, _ = h.Write(size[:])
		_, _ = h.Write([]byte(field))
	}
	var digest [32]byte
	copy(digest[:], h.Sum(nil))
	return public, digest, nil
}
