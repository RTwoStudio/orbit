package cycles

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RTwoStudio/orbit/internal/exit"
	"github.com/RTwoStudio/orbit/internal/fsutil"
)

func TestReadCurrentMissing(t *testing.T) {
	s := setupVault(t)
	_, err := s.ReadCurrent()
	if codeOf(t, err) != exit.StateConflict {
		t.Fatalf("missing CURRENT code = %v, want state_conflict", codeOf(t, err))
	}
	var e *exit.Error
	if !errors.As(err, &e) || !strings.Contains(strings.Join(e.Hints, " "), "cycle new") {
		t.Errorf("missing CURRENT hint missing 'cycle new': %v", err)
	}
}

func TestReadCurrentEmpty(t *testing.T) {
	s := setupVault(t)
	if err := fsutil.AtomicWrite(s.CurrentPath(), []byte("  \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadCurrent(); codeOf(t, err) != exit.StateConflict {
		t.Errorf("empty CURRENT code = %v, want state_conflict", codeOf(t, err))
	}
}

func TestReadCurrentMalformed(t *testing.T) {
	s := setupVault(t)
	if err := fsutil.AtomicWrite(s.CurrentPath(), []byte("banana\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadCurrent(); codeOf(t, err) != exit.StateConflict {
		t.Errorf("malformed CURRENT code = %v, want state_conflict", codeOf(t, err))
	}
}

// TestReadCurrentMissingDirAutoClears: a well-formed pointer to a cycle whose
// directory is gone auto-clears CURRENT (design §6) and reports no open cycle.
func TestReadCurrentMissingDirAutoClears(t *testing.T) {
	s := setupVault(t)
	setCurrent(t, s, "C-0009")
	if err := os.RemoveAll(s.CycleDir("C-0009")); err != nil {
		t.Fatal(err)
	}

	_, err := s.ReadCurrent()
	if codeOf(t, err) != exit.StateConflict {
		t.Fatalf("missing-dir code = %v, want state_conflict", codeOf(t, err))
	}
	if got := currentContents(t, s); got != "" {
		t.Errorf("CURRENT = %q after auto-clear, want empty", got)
	}
	// The clear is stable: a second read sees the empty pointer.
	if _, err := s.ReadCurrent(); codeOf(t, err) != exit.StateConflict {
		t.Errorf("second read after clear code = %v, want state_conflict", codeOf(t, err))
	}
}

// TestReadCurrentClosedAutoClears: a pointer to a cycle that is Closed upstream
// auto-clears CURRENT and reports no open cycle.
func TestReadCurrentClosedAutoClears(t *testing.T) {
	s := setupVault(t)
	cy, err := s.NewCycle("Closed upstream", "0.1.0", "", "")
	if err != nil {
		t.Fatalf("NewCycle: %v", err)
	}
	if _, err := s.CloseCycle(); err != nil {
		t.Fatalf("CloseCycle: %v", err)
	}
	// CloseCycle cleared CURRENT; re-point it at the Closed cycle as a stale
	// teammate's clone would.
	if err := s.WriteCurrent(cy.ID); err != nil {
		t.Fatalf("WriteCurrent: %v", err)
	}

	_, err = s.ReadCurrent()
	if codeOf(t, err) != exit.StateConflict {
		t.Fatalf("closed-cycle code = %v, want state_conflict", codeOf(t, err))
	}
	if got := currentContents(t, s); got != "" {
		t.Errorf("CURRENT = %q after auto-clear, want empty", got)
	}
}

// TestReadCurrentUnparseableStaysLoud: a note that cannot be parsed must never
// be silently cleared.
func TestReadCurrentUnparseableStaysLoud(t *testing.T) {
	s := setupVault(t)
	setCurrent(t, s, "C-0001")
	note := filepath.Join(s.CycleDir("C-0001"), "C-0001.md")
	if err := fsutil.AtomicWrite(note, []byte("not frontmatter\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := s.ReadCurrent(); codeOf(t, err) != exit.StateConflict {
		t.Fatalf("unparseable-note code = %v, want state_conflict", codeOf(t, err))
	}
	if got := currentContents(t, s); got != "C-0001" {
		t.Errorf("CURRENT = %q, want C-0001 (must not auto-clear)", got)
	}
}

// TestReadCurrentMissingNoteIsPermissive: the cycle directory exists but its
// note has not been written — v1 behavior is preserved (the id is returned, no
// clear). Only a note that exists and cannot be parsed stays loud.
func TestReadCurrentMissingNoteIsPermissive(t *testing.T) {
	s := setupVault(t)
	setCurrent(t, s, "C-0001")

	got, err := s.ReadCurrent()
	if err != nil {
		t.Fatalf("missing-note ReadCurrent: %v", err)
	}
	if got != "C-0001" {
		t.Errorf("ReadCurrent = %q, want C-0001", got)
	}
	if c := currentContents(t, s); c != "C-0001" {
		t.Errorf("CURRENT = %q, want C-0001 (must not auto-clear)", c)
	}
}

func TestReadCurrentOK(t *testing.T) {
	s := setupVault(t)
	setCurrent(t, s, "C-0001")
	got, err := s.ReadCurrent()
	if err != nil {
		t.Fatalf("ReadCurrent: %v", err)
	}
	if got != "C-0001" {
		t.Errorf("ReadCurrent = %q, want C-0001", got)
	}
}

// currentContents returns the trimmed bytes of the CURRENT file.
func currentContents(t *testing.T, s *Store) string {
	t.Helper()
	data, err := os.ReadFile(s.CurrentPath())
	if err != nil {
		t.Fatalf("read CURRENT: %v", err)
	}
	return strings.TrimSpace(string(data))
}
