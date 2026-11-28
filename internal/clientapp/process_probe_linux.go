package clientapp

import (
	"errors"
	"io"
	"os"
	"strconv"
)

func scanProcesses(visit func(name, path string)) bool {
	return scanProcDirectory("/proc", visit)
}

func scanProcDirectory(root string, visit func(name, path string)) bool {
	dir, err := os.Open(root)
	if err != nil {
		return false
	}
	defer func() { _ = dir.Close() }()
	for {
		names, err := dir.Readdirnames(256)
		for _, pid := range names {
			if _, err := strconv.ParseUint(pid, 10, 32); err != nil {
				continue
			}
			path, err := os.Readlink(root + "/" + pid + "/exe")
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				visit("", "")
				continue
			}
			visit("", path)
		}
		if err != nil {
			return errors.Is(err, io.EOF)
		}
	}
}
