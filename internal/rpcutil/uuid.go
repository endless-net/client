// Package rpcutil provides shared validation for native IPC consumers and handlers.
package rpcutil

import (
	"encoding/hex"
	"strings"
)

// ValidUUID accepts a nonzero UUID in hyphenated hexadecimal form.
// It does not constrain the UUID version or variant.
func ValidUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	raw := strings.ReplaceAll(value, "-", "")
	_, err := hex.DecodeString(raw)
	return err == nil && len(raw) == 32 && strings.Trim(raw, "0") != ""
}
