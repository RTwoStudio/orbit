package fsutil

import (
	"os"
	"path/filepath"
	"syscall"
)

// Flock acquires an exclusive advisory lock on path (created if missing) and
// returns a release func. Blocks until the lock is available.
func Flock(path string) (func() error, error) {
	if err := EnsureDir(filepath.Dir(path)); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return f.Close, nil
}
