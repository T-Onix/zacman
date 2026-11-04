package transaction

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAcquireLockExcludesConcurrentOperation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "zacman.lock")

	unlock, err := AcquireLock(path)
	if err != nil {
		t.Fatalf("AcquireLock() error = %v", err)
	}

	if secondUnlock, err := AcquireLock(path); err == nil {
		secondUnlock()
		t.Fatal("second AcquireLock() succeeded while the first lock was held")
	}

	unlock()

	thirdUnlock, err := AcquireLock(path)
	if err != nil {
		t.Fatalf("AcquireLock() after unlock error = %v", err)
	}
	thirdUnlock()
}

func TestAcquireLockRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	lock := filepath.Join(dir, "zacman.lock")
	if err := os.WriteFile(target, []byte("do not touch"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, lock); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}

	unlock, err := AcquireLock(lock)
	if err == nil {
		unlock()
		t.Fatal("AcquireLock() accepted a symlink lock path")
	}
	if !strings.Contains(err.Error(), "unsafe lock path") {
		t.Fatalf("AcquireLock() error = %v, want unsafe lock path", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "do not touch" {
		t.Fatalf("symlink target was modified: %q", got)
	}
}

func TestAcquireLockRejectsDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	unlock, err := AcquireLock(path)
	if err == nil {
		unlock()
		t.Fatal("AcquireLock() accepted a directory as a lock file")
	}
	if !strings.Contains(err.Error(), "unsafe lock path") {
		t.Fatalf("AcquireLock() error = %v, want unsafe lock path", err)
	}
}
