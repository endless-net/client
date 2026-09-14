//go:build !windows

package client

import "os"

func readAtomicFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func replaceFileAtomic(source, target string) error {
	return os.Rename(source, target)
}
