//go:build !windows

package client

import (
	"fmt"
	"os"
	"strings"
)

func debugLogUserHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve debug log user home: %w", err)
	}
	if strings.TrimSpace(home) == "" {
		return "", fmt.Errorf("resolve debug log user home: empty home directory")
	}
	return home, nil
}
