package cycles

import (
	"path/filepath"
	"testing"

	"github.com/RTwoStudio/orbit/internal/fsutil"
)

func TestOpenBuildsVaultPaths(t *testing.T) {
	vault := "/tmp/some-vault"
	s := Open(vault)

	if got, want := s.Root, filepath.Join(vault, "Cycles"); got != want {
		t.Errorf("Root = %q, want %q", got, want)
	}
	if got, want := s.CyclesDir(), s.Root; got != want {
		t.Errorf("CyclesDir = %q, want %q", got, want)
	}
	if got, want := s.BacklogDir(), filepath.Join(vault, "Cycles", "backlog"); got != want {
		t.Errorf("BacklogDir = %q, want %q", got, want)
	}
	if got, want := s.LedgerDir(), filepath.Join(vault, "Cycles", "cycles"); got != want {
		t.Errorf("LedgerDir = %q, want %q", got, want)
	}
	if got, want := s.CycleDir("C-0001"), filepath.Join(vault, "Cycles", "cycles", "C-0001"); got != want {
		t.Errorf("CycleDir = %q, want %q", got, want)
	}
	if got, want := s.CurrentPath(), filepath.Join(vault, "Cycles", "CURRENT"); got != want {
		t.Errorf("CurrentPath = %q, want %q", got, want)
	}
}

func TestWorkPath(t *testing.T) {
	dir := filepath.Join("/v", "Cycles", "backlog")
	if got, want := WorkPath(dir, "W-0001", "voice-search"), filepath.Join(dir, "W-0001 - voice-search.md"); got != want {
		t.Errorf("WorkPath = %q, want %q", got, want)
	}
	if got, want := WorkPath(dir, "W-0001", ""), filepath.Join(dir, "W-0001.md"); got != want {
		t.Errorf("WorkPath(empty slug) = %q, want %q", got, want)
	}
}

func TestIsInitialized(t *testing.T) {
	s := Open(t.TempDir())
	if s.IsInitialized() {
		t.Error("fresh vault reported initialized")
	}
	if err := fsutil.EnsureDir(s.BacklogDir()); err != nil {
		t.Fatal(err)
	}
	if !s.IsInitialized() {
		t.Error("vault with backlog/ reported uninitialized")
	}
}
