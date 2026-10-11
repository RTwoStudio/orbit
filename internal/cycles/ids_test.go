package cycles

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/RTwoStudio/orbit/internal/exit"
	"github.com/RTwoStudio/orbit/internal/fsutil"
)

func TestParseWorkID(t *testing.T) {
	valid := []struct{ in, want string }{
		{"W-0001-arash-jafarpour", "W-0001-arash-jafarpour"},
		{" W-0001-arash-jafarpour ", "W-0001-arash-jafarpour"},
		{"W-99999-seyed-mahdi-rostami", "W-99999-seyed-mahdi-rostami"},
		// Legacy bare ids are accepted read-only.
		{"W-0001", "W-0001"},
		{" W-1234 ", "W-1234"},
	}
	for _, tc := range valid {
		got, err := ParseWorkID(tc.in)
		if err != nil {
			t.Errorf("ParseWorkID(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseWorkID(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	invalid := []string{
		"", "W-001", "W-1", "W-abcd", "C-0001", "0001", "W-0001x", "W 0001",
		// Composite grammar faults.
		"w-0001-arash-jafarpour", "W-0001-", "W-0001-Arash", "W-0001-arash_jafarpour",
		"W-0001--arash", "W-arash-jafarpour",
	}
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

func TestWorkIDFromFilename(t *testing.T) {
	cases := map[string]string{
		"W-0001-arash-jafarpour - voice-search.md": "W-0001-arash-jafarpour",
		"W-0001-arash-jafarpour.md":                "W-0001-arash-jafarpour",
		"W-0001 - voice-search.md":                 "W-0001",
		"W-0001.md":                                "W-0001",
		"W-0001-arash-jafarpour - a - b.md":        "W-0001-arash-jafarpour",
	}
	for name, want := range cases {
		if got := workIDFromFilename(name); got != want {
			t.Errorf("workIDFromFilename(%q) = %q, want %q", name, got, want)
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
	id, err := s.NextWorkID(testOwnerHandle)
	if err != nil {
		t.Fatalf("NextWorkID: %v", err)
	}
	if want := "W-0001-" + testOwnerHandle; id != want {
		t.Errorf("empty vault next id = %q, want %q", id, want)
	}
}

func TestNextWorkIDPerOwner(t *testing.T) {
	s := setupVault(t)
	// Two owners mint in their own namespaces; neither advances the other.
	writeRaw(t, filepath.Join(s.BacklogDir(), "W-0001-arash-jafarpour - a.md"))
	writeRaw(t, filepath.Join(s.BacklogDir(), "W-0002-arash-jafarpour - b.md"))
	writeRaw(t, filepath.Join(s.BacklogDir(), "W-0009-seyed-mahdi-rostami - c.md"))

	got, err := s.NextWorkID("arash-jafarpour")
	if err != nil {
		t.Fatal(err)
	}
	if want := "W-0003-arash-jafarpour"; got != want {
		t.Errorf("NextWorkID(arash) = %q, want %q", got, want)
	}

	other, err := s.NextWorkID("seyed-mahdi-rostami")
	if err != nil {
		t.Fatal(err)
	}
	if want := "W-0010-seyed-mahdi-rostami"; other != want {
		t.Errorf("NextWorkID(seyed) = %q, want %q", other, want)
	}
}

func TestNextWorkIDScansBacklogAndCycles(t *testing.T) {
	s := setupVault(t)
	writeRaw(t, filepath.Join(s.BacklogDir(), "W-0001-arash-jafarpour - a.md"))
	writeRaw(t, filepath.Join(s.BacklogDir(), "W-0003-arash-jafarpour - c.md"))
	writeRaw(t, filepath.Join(s.CycleDir("C-0007"), "W-0007-arash-jafarpour - g.md"))
	// Non-work files, other owners, and legacy bare ids are ignored.
	writeRaw(t, filepath.Join(s.BacklogDir(), "notes.md"))
	writeRaw(t, filepath.Join(s.BacklogDir(), "W-0042-seyed-mahdi-rostami - other.md"))
	writeRaw(t, filepath.Join(s.BacklogDir(), "W-0099 - legacy.md"))

	id, err := s.NextWorkID("arash-jafarpour")
	if err != nil {
		t.Fatal(err)
	}
	if want := "W-0008-arash-jafarpour"; id != want {
		t.Errorf("NextWorkID = %q, want %q", id, want)
	}
}

func TestNextWorkIDNeverReusesDelivered(t *testing.T) {
	s := setupVault(t)
	writeRaw(t, filepath.Join(s.BacklogDir(), "W-0002-arash-jafarpour - b.md"))
	writeRaw(t, filepath.Join(s.CycleDir("C-0001"), "W-0005-arash-jafarpour - delivered.md"))

	id, err := s.NextWorkID("arash-jafarpour")
	if err != nil {
		t.Fatal(err)
	}
	if want := "W-0006-arash-jafarpour"; id != want {
		t.Errorf("NextWorkID = %q, want %q (delivered ids are never reused)", id, want)
	}
}

func TestNextWorkIDEmptyOwner(t *testing.T) {
	s := setupVault(t)
	if _, err := s.NextWorkID("  "); codeOf(t, err) != exit.PreflightFailed {
		t.Errorf("empty owner code = %v, want preflight_failed", codeOf(t, err))
	}
}

func TestFindWorkComposite(t *testing.T) {
	s := setupVault(t)
	composite := filepath.Join(s.BacklogDir(), "W-0001-arash-jafarpour - alpha.md")
	writeRaw(t, composite)

	got, err := s.FindWork("W-0001-arash-jafarpour")
	if err != nil {
		t.Fatalf("FindWork composite: %v", err)
	}
	if got != composite {
		t.Errorf("FindWork = %q, want %q", got, composite)
	}

	if _, err := s.FindWork("W-0001-Arash"); codeOf(t, err) != exit.PreflightFailed {
		t.Errorf("malformed id code = %v, want preflight_failed", codeOf(t, err))
	}
	if _, err := s.FindWork("W-0099-arash-jafarpour"); codeOf(t, err) != exit.NotFound {
		t.Errorf("absent composite code = %v, want not_found", codeOf(t, err))
	}
}

func TestFindWorkLegacyReadCompat(t *testing.T) {
	s := setupVault(t)
	legacy := filepath.Join(s.BacklogDir(), "W-0001 - legacy.md")
	writeRaw(t, legacy)

	// A bare W-#### resolves the legacy note.
	got, err := s.FindWork("W-0001")
	if err != nil {
		t.Fatalf("FindWork legacy: %v", err)
	}
	if got != legacy {
		t.Errorf("FindWork(legacy) = %q, want %q", got, legacy)
	}

	// A bare query also resolves a composite note for the same sequence...
	composite := filepath.Join(s.BacklogDir(), "W-0002-arash-jafarpour - alpha.md")
	writeRaw(t, composite)
	got, err = s.FindWork("W-0002")
	if err != nil {
		t.Fatalf("FindWork bare→composite: %v", err)
	}
	if got != composite {
		t.Errorf("FindWork(W-0002) = %q, want the composite %q", got, composite)
	}

	// ...and the composite wins when both exist for the same sequence.
	both := filepath.Join(s.BacklogDir(), "W-0003 - legacy-both.md")
	writeRaw(t, both)
	bothComposite := filepath.Join(s.BacklogDir(), "W-0003-arash-jafarpour - composite-both.md")
	writeRaw(t, bothComposite)
	got, err = s.FindWork("W-0003")
	if err != nil {
		t.Fatalf("FindWork both: %v", err)
	}
	if got != bothComposite {
		t.Errorf("FindWork(both) = %q, want the composite %q", got, bothComposite)
	}

	if _, err := s.FindWork("nope"); codeOf(t, err) != exit.PreflightFailed {
		t.Errorf("bad id code = %v, want preflight_failed", codeOf(t, err))
	}
}

func TestFindWorkDuplicateIsConflict(t *testing.T) {
	s := setupVault(t)
	a := filepath.Join(s.BacklogDir(), "W-0001-arash-jafarpour - a.md")
	b := filepath.Join(s.CycleDir("C-0001"), "W-0001-arash-jafarpour - b.md")
	writeRaw(t, a)
	writeRaw(t, b)

	if _, err := s.FindWork("W-0001-arash-jafarpour"); codeOf(t, err) != exit.StateConflict {
		t.Errorf("duplicate composite code = %v, want state_conflict", codeOf(t, err))
	}
}

// TestCheckCycleIDsClean: a missing ledger, the canonical one-note layout, and
// a bare container directory are all clean (directory names are never counted).
func TestCheckCycleIDsClean(t *testing.T) {
	s := setupVault(t)
	if err := s.CheckCycleIDs(); err != nil {
		t.Fatalf("empty ledger CheckCycleIDs: %v", err)
	}
	writeCycleNote(t, s, "C-0007", "C-0007.md", "C-0007", "2026-01-01T00:00:00Z", testOwnerHandle)
	if err := fsutil.EnsureDir(s.CycleDir("C-0009")); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckCycleIDs(); err != nil {
		t.Fatalf("clean vault CheckCycleIDs: %v", err)
	}
}

// TestCheckCycleIDsDuplicate: two notes sharing a leading C-#### token are a
// state_conflict (7) naming both files; groups are ordered numerically.
func TestCheckCycleIDsDuplicate(t *testing.T) {
	s := setupVault(t)
	a := writeCycleNote(t, s, "C-0007", "C-0007.md", "C-0007", "2026-01-01T00:00:00Z", testOwnerHandle)
	b := writeCycleNote(t, s, "C-0007-copy", "C-0007.md", "C-0007", "2026-01-02T00:00:00Z", testOwnerHandle)

	err := s.CheckCycleIDs()
	if codeOf(t, err) != exit.StateConflict {
		t.Fatalf("duplicate code = %v, want state_conflict", codeOf(t, err))
	}
	msg := errMessage(t, err)
	if !strings.Contains(msg, a) || !strings.Contains(msg, b) {
		t.Errorf("message %q must name both %q and %q", msg, a, b)
	}

	dups, err := s.CycleIDDuplicates()
	if err != nil {
		t.Fatal(err)
	}
	if len(dups) != 1 || dups[0].ID != "C-0007" || len(dups[0].Paths) != 2 {
		t.Fatalf("CycleIDDuplicates = %+v, want one C-0007 group of 2", dups)
	}

	writeCycleNote(t, s, "C-0003-a", "C-0003.md", "C-0003", "2026-01-01T00:00:00Z", testOwnerHandle)
	writeCycleNote(t, s, "C-0003-b", "C-0003.md", "C-0003", "2026-01-01T00:00:00Z", testOwnerHandle)
	dups, err = s.CycleIDDuplicates()
	if err != nil {
		t.Fatal(err)
	}
	if len(dups) != 2 || dups[0].ID != "C-0003" || dups[1].ID != "C-0007" {
		t.Errorf("groups = %+v, want C-0003 then C-0007", dups)
	}
}
