package client

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

const (
	windowsAtomicReplaceRetryTimeout = 2 * time.Second
	windowsAtomicReplaceRetryDelay   = 5 * time.Millisecond
	windowsAtomicReplaceMaxDelay     = 50 * time.Millisecond
)

func replaceFileAtomic(source, target string) error {
	deadline := time.Now().Add(windowsAtomicReplaceRetryTimeout)
	delay := windowsAtomicReplaceRetryDelay
	for {
		err := os.Rename(source, target)
		if err == nil {
			return nil
		}
		if !isRetryableWindowsAtomicReplaceError(err) || atomicReplaceTargetIsDirectory(target) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(delay)
		delay = min(delay*2, windowsAtomicReplaceMaxDelay)
	}
}

// Windows can temporarily reject a new reader while the destination of an
// atomic replacement is open for deletion. Retry the read, not state decoding:
// callers must receive a complete snapshot or the original filesystem error.
func readAtomicFile(path string) ([]byte, error) {
	deadline := time.Now().Add(windowsAtomicReplaceRetryTimeout)
	delay := windowsAtomicReplaceRetryDelay
	for {
		raw, err := os.ReadFile(path)
		if err == nil || !isRetryableWindowsAtomicReplaceError(err) || time.Now().After(deadline) {
			return raw, err
		}
		time.Sleep(delay)
		delay = min(delay*2, windowsAtomicReplaceMaxDelay)
	}
}

func isRetryableWindowsAtomicReplaceError(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) ||
		errors.Is(err, windows.ERROR_SHARING_VIOLATION) ||
		errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}

func atomicReplaceTargetIsDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
