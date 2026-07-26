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

func isRetryableWindowsAtomicReplaceError(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) ||
		errors.Is(err, windows.ERROR_SHARING_VIOLATION) ||
		errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}

func atomicReplaceTargetIsDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
