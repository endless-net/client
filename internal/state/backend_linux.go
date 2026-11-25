package state

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	guuid "github.com/google/uuid"
	"golang.org/x/sys/unix"
)

const linuxStateFile = "state.sealed"
const linuxIdentityFile = "installation"

type LinuxBackend struct{ directory string }

type linuxSlot struct{ directory *os.File }

type linuxProtector struct {
	aead cipher.AEAD
	id   string
}

func (p linuxProtector) Seal(plain []byte) ([]byte, error) {
	return p.aead.Seal(nil, nil, plain, []byte(p.id)), nil
}

func (p linuxProtector) Open(sealed []byte) ([]byte, error) {
	return p.aead.Open(nil, nil, sealed, []byte(p.id))
}

// OpenLinux uses a service-owned directory prepared by systemd or the installer.
// Only explicit installation can create a missing installation identity.
func OpenLinux(ctx context.Context, directory string, initialize bool, maxPlain, maxCipher int64) (*Store, error) {
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory || directory == "/" || os.Geteuid() != 0 {
		return nil, ErrCorrupt
	}
	backend := LinuxBackend{directory: directory}
	var store *Store
	err := backend.Exclusive(ctx, func(slot Slot) error {
		root := slot.(linuxSlot)
		identity, err := root.read(linuxIdentityFile, 48)
		if initialize && errors.Is(err, ErrMissing) {
			// A missing identity alongside state is corruption, not first install.
			if _, err := root.Read(maxCipher); !errors.Is(err, ErrMissing) {
				return ErrCorrupt
			}
			identity = make([]byte, 48)
			defer clear(identity)
			id, err := guuid.NewRandom()
			if err != nil {
				return err
			}
			copy(identity, id[:])
			if _, err := rand.Read(identity[16:]); err != nil {
				return err
			}
			if err := root.replace(linuxIdentityFile, identity); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		defer clear(identity)
		if len(identity) != 48 {
			return ErrCorrupt
		}
		id, err := guuid.FromBytes(identity[:16])
		if err != nil || id == guuid.Nil {
			return ErrCorrupt
		}
		block, err := aes.NewCipher(identity[16:])
		if err != nil {
			return err
		}
		aead, err := cipher.NewGCMWithRandomNonce(block)
		if err != nil {
			return err
		}
		store, err = New(backend, linuxProtector{aead: aead, id: id.String()}, id.String(), maxPlain, maxCipher)
		return err
	})
	return store, err
}

func (b LinuxBackend) Exclusive(ctx context.Context, operation func(Slot) error) error {
	dir, err := openLinuxDirectory(b.directory)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := unix.Flock(int(dir.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return operation(linuxSlot{directory: dir})
		}
		if err != unix.EWOULDBLOCK && err != unix.EINTR {
			return ErrCorrupt
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func openLinuxDirectory(directory string) (*os.File, error) {
	fd, err := unix.Openat2(unix.AT_FDCWD, directory, &unix.OpenHow{
		Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_NO_SYMLINKS,
	})
	if err != nil {
		return nil, ErrCorrupt
	}
	var info unix.Stat_t
	if unix.Fstat(fd, &info) != nil || info.Uid != 0 || info.Mode&07777 != 0700 {
		_ = unix.Close(fd)
		return nil, ErrCorrupt
	}
	return os.NewFile(uintptr(fd), directory), nil
}

// LockLinuxRuntime serializes the service's native effects for its entire
// lifetime. State transactions continue using a separate directory lock.
func LockLinuxRuntime(directory string) (*os.File, error) {
	dir, err := openLinuxDirectory(directory)
	if err != nil {
		return nil, err
	}
	defer func() { _ = dir.Close() }()
	fd, err := unix.Openat(int(dir.Fd()), "runtime.lock",
		unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, ErrCorrupt
	}
	var info unix.Stat_t
	if unix.Fstat(fd, &info) != nil || info.Uid != 0 || info.Nlink != 1 ||
		info.Mode&unix.S_IFMT != unix.S_IFREG || info.Mode&07777 != 0600 {
		_ = unix.Close(fd)
		return nil, ErrCorrupt
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = unix.Close(fd)
		return nil, ErrStale
	}
	return os.NewFile(uintptr(fd), "runtime.lock"), nil
}

func (s linuxSlot) Read(limit int64) ([]byte, error) { return s.read(linuxStateFile, limit) }

func (s linuxSlot) read(name string, limit int64) ([]byte, error) {
	fd, err := unix.Openat(int(s.directory.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err == unix.ENOENT {
		return nil, ErrMissing
	}
	if err != nil {
		return nil, ErrCorrupt
	}
	f := os.NewFile(uintptr(fd), name)
	defer func() { _ = f.Close() }()
	var info unix.Stat_t
	if unix.Fstat(fd, &info) != nil || info.Uid != 0 || info.Mode&unix.S_IFMT != unix.S_IFREG || info.Mode&07777 != 0600 || info.Nlink != 1 || info.Size <= 0 || info.Size > limit {
		return nil, ErrCorrupt
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(data)) != info.Size {
		return nil, ErrCorrupt
	}
	return data, nil
}

func (s linuxSlot) Replace(data []byte) error { return s.replace(linuxStateFile, data) }

func (s linuxSlot) replace(name string, data []byte) error {
	id, err := guuid.NewRandom()
	if err != nil {
		return err
	}
	tmp := ".state-" + id.String()
	dir := int(s.directory.Fd())
	fd, err := unix.Openat(dir, tmp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = unix.Unlinkat(dir, tmp, 0) }()
	f := os.NewFile(uintptr(fd), tmp)
	_, writeErr := f.Write(data)
	var syncErr error
	if writeErr == nil {
		syncErr = f.Sync()
	}
	if err := errors.Join(writeErr, syncErr, f.Close()); err != nil {
		return err
	}
	if err := unix.Renameat(dir, tmp, dir, name); err != nil {
		return err
	}
	return s.directory.Sync()
}
