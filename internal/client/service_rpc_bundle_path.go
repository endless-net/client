package client

import (
	"errors"
	"os"
	"path/filepath"
)

func validateRPCBundlePath(path string) error {
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			if current != path || !errors.Is(err, os.ErrNotExist) {
				return errors.New("cannot inspect bundle state path")
			}
		} else {
			trusted := isTrustedDiagnosticsPathAlias(current, info)
			if (isRPCBundleReparsePoint(info) && !trusted) || (current != path && !info.IsDir() && !trusted) {
				return errors.New("unsafe bundle state path")
			}
		}
		if filepath.Dir(current) == current {
			return nil
		}
	}
}
