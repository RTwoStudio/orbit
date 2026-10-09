package cycles

import (
	"path/filepath"
	"testing"

	"github.com/RTwoStudio/orbit/internal/exit"
	"github.com/RTwoStudio/orbit/internal/fsutil"
)

func TestParseWorkID(t *testing.T) {
	valid := []string{"W-0001", "W-1234", "W-99999", " W-0001 "}
	for _, id := range valid {
		got, err := ParseWorkID(id)
		if err != nil {
			t.Errorf("ParseWorkID(%q): %v", id, err)
			continue
		}
		if got != "W-0001" && got != "W-1234" && got != "W-99999" {
			t.Errorf("ParseWorkID(%q) = %q", id, got)
		}
	}

	invalid := []string{"", "W-001", "W-1", "W-abcd", "C-0001", "w-0001", "0001", "W-0001x", "W 0001"}
	for _, id := range invalid {
		_, err := ParseWorkID(id)
		if err == nil {
			t.Errorf("ParseWorkID(%q) = nil, want preflight_failed", id)
			continue
		}
		if codeOf(t, err) != exit.PreflightFailed {
			t.Errorf("ParseWorkID(%q) code = %v, want preflight_failed", id, codeOf(t, err))
		}
	}
}

// writeRaw places a placeholder note at path so filename scanning sees it.
func writeRaw(t *testing.T, path string) {
	t.Helper()
	if err := fsutil.AtomicWrite(path, []byte("---\nid: placeholder\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestNextWorkIDSingle(t *testing.T) {
	s := setupVault(t)
	id, err := s.NextWorkID()
	if err != nil {
		t.Fatalf("NextWorkID: %v", err)
	}
	if id != "W-0001" {
		t.Errorf("empty vault next id = %q, want W-0001", id)
	}
}

func TestNextWorkIDScansBacklogAndCycles(t *testing.T) {
	s := setupVault(t)
	writeRaw(t, filepath.Join(s.BacklogDir(), "W-0001 - a.md"))
	writeRaw(t, filepath.Join(s.BacklogDir(), "W-0003 - c.md"))
	writeRaw(t, filepath.Join(s.CycleDir("C-0007"), "W-0007 - g.md"))
	// Non-work files are ignored.
	writeRaw(t, filepath.Join(s.BacklogDir(), "notes.md"))

	id, err := s.NextWorkID()
	if err != nil {
		t.Fatal(err)
	}
	if id != "W-0008" {
		t.Errorf("NextWorkID = %q, want W-0008", id)
	}
}

func TestNextWorkIDNeverReusesDelivered(t *testing.T) {
	s := setupVault(t)
	writeRaw(t, filepath.Join(s.BacklogDir(), "W-0002 - b.md"))
	writeRaw(t, filepath.Join(s.CycleDir("C-0001"), "W-0005 - delivered.md"))

	id, err := s.NextWorkID()
	if err != nil {
		t.Fatal(err)
	}
	if id != "W-0006" {
		t.Errorf("NextWorkID = %q, want W-0006 (delivered ids are never reused)", id)
	}
}
