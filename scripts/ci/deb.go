package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
)

func (r runner) buildDeb(ctx context.Context, files []archiveEntry, output, tag, sha string) error {
	root, err := os.MkdirTemp("", "endlessnet-deb-input-")
	if err != nil {
		return err
	}
	defer cleanup(root)
	for _, file := range files {
		relative := filepath.FromSlash(file.name)
		if !filepath.IsLocal(relative) {
			return errors.New("invalid Debian package input")
		}
		name := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(name, file.content, file.mode); err != nil {
			return err
		}
	}
	epoch, err := r.command(ctx, "git", "show", "-s", "--format=%ct", sha)
	if err != nil {
		return err
	}
	_, err = r.run(ctx, root, []string{"SOURCE_DATE_EPOCH=" + epoch}, "sh",
		filepath.Join(root, "packaging", "build-deb.sh"), tag, output)
	return err
}
