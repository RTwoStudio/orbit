// Package fsutil provides atomic file operations, locking, and small
// filesystem helpers used across the CLI.
package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AtomicWrite writes data to path via a temp file in the same directory,
// then renames. Never leaves a partial file behind on crash. Parent
// directories are created as needed.
func AtomicWrite(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// Exists reports whether path exists (file or directory).
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// IsDir reports whether path exists and is a directory.
func IsDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// EnsureDir creates dir (and parents) if missing.
func EnsureDir(dir string) error {
	return os.MkdirAll(dir, 0o755)
}

// CopyFile copies src to dst atomically with the source's permission bits
// (or 0644 if unknown).
func CopyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	perm := os.FileMode(0o644)
	if fi, err := os.Stat(src); err == nil {
		perm = fi.Mode().Perm()
	}
	return AtomicWrite(dst, data, perm)
}

// AppendToFile appends data to an existing file (creates if missing).
func AppendToFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
}

// GitignoreAppend appends line to a .gitignore file at dir/.gitignore if not
// already present (idempotent).
func GitignoreAppend(dir, line string) error {
	path := filepath.Join(dir, ".gitignore")
	var content string
	if data, err := os.ReadFile(path); err == nil {
		content = string(data)
		for _, l := range strings.Split(content, "\n") {
			if strings.TrimSpace(l) == line {
				return nil
			}
		}
		if content != "" && !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
	}
	content += line + "\n"
	return AtomicWrite(path, []byte(content), 0o644)
}

// MaxIssueNumber scans dir for "issue-<n>" subdirectories and returns the
// largest n (0 if none).
func MaxIssueNumber(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	max := 0
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || !strings.HasPrefix(name, "issue-") {
			continue
		}
		var n int
		if _, err := fmt.Sscanf(name, "issue-%d", &n); err == nil && n > max {
			max = n
		}
	}
	return max
}
