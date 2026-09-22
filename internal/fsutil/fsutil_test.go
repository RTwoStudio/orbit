package fsutil

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestAtomicWriteRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "file.md")
	if err := AtomicWrite(path, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "hello\n" {
		t.Errorf("got %q", data)
	}
	// No temp litter.
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("temp files left behind: %v", entries)
	}
}

func TestAtomicWriteByteExactness(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	// Trailing whitespace must be preserved (hash byte-exactness).
	if err := AtomicWrite(path, []byte("body\n\n\n  "), 0o644); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "body\n\n\n  " {
		t.Errorf("body normalized: %q", data)
	}
}

func TestGitignoreAppendIdempotent(t *testing.T) {
	dir := t.TempDir()
	if err := GitignoreAppend(dir, ".neocortex/"); err != nil {
		t.Fatal(err)
	}
	if err := GitignoreAppend(dir, ".neocortex/"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if strings.Count(string(data), ".neocortex/") != 1 {
		t.Errorf("duplicate entries: %q", data)
	}
}

func TestFlockExclusive(t *testing.T) {
	dir := t.TempDir()
	release, err := Flock(filepath.Join(dir, ".lock"))
	if err != nil {
		t.Fatal(err)
	}
	// A second non-blocking acquisition must fail (we test via syscall
	// directly with LOCK_EX|LOCK_NB semantics through the same file).
	f2, _ := os.OpenFile(filepath.Join(dir, ".lock"), os.O_CREATE|os.O_RDWR, 0o644)
	defer f2.Close()
	if err := syscall.Flock(int(f2.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
		t.Error("second exclusive lock should not be held simultaneously")
	} else {
		syscall.Flock(int(f2.Fd()), syscall.LOCK_UN)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
}

func TestMaxIssueNumber(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "issue-3"), 0o755)
	os.Mkdir(filepath.Join(dir, "issue-7"), 0o755)
	os.Mkdir(filepath.Join(dir, "issue-12"), 0o755)
	os.WriteFile(filepath.Join(dir, "issue-99.md"), nil, 0o644) // file, ignored
	if got := MaxIssueNumber(dir); got != 12 {
		t.Errorf("max = %d, want 12", got)
	}
	if got := MaxIssueNumber(t.TempDir()); got != 0 {
		t.Errorf("empty = %d, want 0", got)
	}
}
