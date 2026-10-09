package cycles

import (
	"errors"
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

func TestReadCurrentDangling(t *testing.T) {
	s := setupVault(t)
	if err := fsutil.AtomicWrite(s.CurrentPath(), []byte("C-0009\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadCurrent(); codeOf(t, err) != exit.StateConflict {
		t.Errorf("dangling CURRENT code = %v, want state_conflict", codeOf(t, err))
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
