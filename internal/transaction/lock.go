package transaction

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

const DefaultLockPath = "/run/lock/zacman.lock"

// AcquireLock prevents concurrent Zacman state-changing operations.
func AcquireLock(path string) (func(), error) {
	if path == "" {
		path = DefaultLockPath
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("create lock directory: %w", err)
	}
	if info, err := os.Lstat(path); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular()) {
		return nil, fmt.Errorf("refusing unsafe lock path: %s", path)
	} else if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("inspect lock path: %w", err)
	}

	// O_NOFOLLOW closes the lstat/open race: a path swapped to a symlink
	// between the check above and this open must not be followed as root.
	fd, err := syscall.Open(path, syscall.O_CREAT|syscall.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	f := os.NewFile(uintptr(fd), path)
	if f == nil {
		_ = syscall.Close(fd)
		return nil, fmt.Errorf("open lock file: unable to create file handle")
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("inspect opened lock file: %w", err)
	}
	if !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, fmt.Errorf("refusing unsafe lock path: %s", path)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("another Zacman operation may be running (lock %s): %w", path, err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
