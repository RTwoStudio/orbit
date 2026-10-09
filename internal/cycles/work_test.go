package cycles

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RTwoStudio/orbit/internal/domain"
	"github.com/RTwoStudio/orbit/internal/exit"
	"github.com/RTwoStudio/orbit/internal/fsutil"
	"github.com/RTwoStudio/orbit/internal/registry"
)

const testRegistryVersion = "0.1.0"

// workStubFixture mirrors orbit-registry/cycles/stubs/work.stub.md: exactly
// the five registered tokens, the CLI-owned frontmatter fields, and shape
// sections whose only content is an <!-- Agent --> comment (so the shape
// preflight must treat them as empty).
const workStubFixture = `---
id: {{WORK_ID}}
title: "{{WORK_TITLE}}"
scope: {{SCOPE}}
status: Backlog
appetite:
cycle:
project:
neocortex-issue:
history: []
created: {{DATE}}
registry-version: {{REGISTRY_VERSION}}
---

# {{WORK_ID}} — {{WORK_TITLE}}

> Status and frontmatter are CLI-owned. Never hand-edit the frontmatter.

## Problem

<!-- Agent: What problem are we solving, and for whom? -->

## Solution Sketch

<!-- Agent: The smallest shape that solves the problem. -->

## Rabbit Holes

<!-- Agent: Known traps, unknowns, and temptations to avoid. -->

## No-gos

<!-- Agent: Explicitly out of scope for this work item. -->

## Notes

<!-- Agent: Anything else. -->

## Related Links

<!-- Agent: Concrete links for this work item. -->
`

// writeFixtureRegistry writes a minimal, hash-valid cycles registry fixture.
func writeFixtureRegistry(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, domain.Cycles.Base)
	files := map[string]string{
		"stubs/work.stub.md":  workStubFixture,
		"stubs/cycle.stub.md": cycleStubFixture,
	}
	m := registry.Manifest{Version: testRegistryVersion, Layout: 1}
	for rel, content := range files {
		m.Files.Stubs = append(m.Files.Stubs, registry.FileEntry{
			Path:   rel,
			SHA256: registry.SHA256Hex([]byte(content)),
		})
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// setupVault isolates $HOME, primes the cycles cache from a fixture registry,
// and returns an initialized Store over a fresh temp vault.
func setupVault(t *testing.T) *Store {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	regRoot := t.TempDir()
	writeFixtureRegistry(t, regRoot)
	fc, err := registry.FetchAll(registry.DirFetcher{Root: regRoot, Domain: domain.Cycles})
	if err != nil {
		t.Fatalf("registry FetchAll: %v", err)
	}
	if err := registry.CacheSave(fc, domain.Cycles); err != nil {
		t.Fatalf("registry CacheSave: %v", err)
	}
	s := Open(t.TempDir())
	if err := fsutil.EnsureDir(s.BacklogDir()); err != nil {
		t.Fatal(err)
	}
	if err := fsutil.EnsureDir(s.LedgerDir()); err != nil {
		t.Fatal(err)
	}
	return s
}

// setCurrent opens cycle id in the vault: creates cycles/<id>/ and writes
// CURRENT. T4 never writes CURRENT itself; tests set the pointer directly.
func setCurrent(t *testing.T, s *Store, id string) {
	t.Helper()
	if err := fsutil.EnsureDir(s.CycleDir(id)); err != nil {
		t.Fatal(err)
	}
	if err := fsutil.AtomicWrite(s.CurrentPath(), []byte(id+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// codeOf asserts err is a coded exit.Error and returns its code.
func codeOf(t *testing.T, err error) exit.Code {
	t.Helper()
	var e *exit.Error
	if !errors.As(err, &e) {
		t.Fatalf("error %v is not *exit.Error", err)
	}
	return e.Code
}

// fillShapeSections inserts non-comment content under each shape heading so
// the shape preflight passes.
func fillShapeSections(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, name := range []string{"Problem", "Solution Sketch", "Rabbit Holes", "No-gos"} {
		marker := "## " + name + "\n"
		s = strings.Replace(s, marker, marker+"\nFilled "+name+".\n", 1)
	}
	if err := fsutil.AtomicWrite(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

// assertLastHistory checks the final history entry's approved format:
// RFC3339 UTC, two spaces, "From → To", optional "(detail)".
func assertLastHistory(t *testing.T, history []string, from, to, detail string) {
	t.Helper()
	if len(history) == 0 {
		t.Fatal("history is empty")
	}
	last := history[len(history)-1]
	parts := strings.SplitN(last, "  ", 2)
	if len(parts) != 2 {
		t.Fatalf("history %q missing the two-space separator", last)
	}
	if _, err := time.Parse(time.RFC3339, parts[0]); err != nil {
		t.Errorf("history timestamp %q is not RFC3339: %v", parts[0], err)
	}
	want := from + " → " + to
	if detail != "" {
		want += " (" + detail + ")"
	}
	if parts[1] != want {
		t.Errorf("history tail = %q, want %q", parts[1], want)
	}
}

func TestNewWorkLifecycle(t *testing.T) {
	s := setupVault(t)

	item, err := s.NewWork("Voice search", "delijan-driver-app")
	if err != nil {
		t.Fatalf("NewWork: %v", err)
	}
	if item.ID != "W-0001" {
		t.Errorf("id = %q, want W-0001", item.ID)
	}
	if item.Status != "Backlog" {
		t.Errorf("status = %q, want Backlog", item.Status)
	}
	if item.Scope != "delijan-driver-app" {
		t.Errorf("scope = %q", item.Scope)
	}
	if item.Appetite != "" || item.Cycle != "" {
		t.Errorf("appetite/cycle should be empty, got %q/%q", item.Appetite, item.Cycle)
	}
	if len(item.History) != 0 {
		t.Errorf("new history = %v, want empty", item.History)
	}
	if item.RegistryVersion != testRegistryVersion {
		t.Errorf("registry-version = %q, want %q", item.RegistryVersion, testRegistryVersion)
	}
	if _, err := time.Parse(time.RFC3339, item.Created); err != nil {
		t.Errorf("created %q is not RFC3339: %v", item.Created, err)
	}
	backlogPath := filepath.Join(s.BacklogDir(), "W-0001 - voice-search.md")
	if item.Path != backlogPath {
		t.Errorf("path = %q, want %q", item.Path, backlogPath)
	}
	if !fsutil.Exists(backlogPath) {
		t.Fatalf("backlog note missing at %s", backlogPath)
	}

	// Comments-only shape sections refuse with preflight_failed.
	if _, err := s.ShapeWork("W-0001", "small"); codeOf(t, err) != exit.PreflightFailed {
		t.Errorf("shape on empty sections code = %v, want preflight_failed", codeOf(t, err))
	}
	// Invalid appetite refuses with preflight_failed.
	if _, err := s.ShapeWork("W-0001", "huge"); codeOf(t, err) != exit.PreflightFailed {
		t.Errorf("shape with bad appetite code = %v, want preflight_failed", codeOf(t, err))
	}

	fillShapeSections(t, backlogPath)
	shaped, err := s.ShapeWork("W-0001", "small")
	if err != nil {
		t.Fatalf("ShapeWork: %v", err)
	}
	if shaped.Status != "Pitched" || shaped.Appetite != "small" {
		t.Errorf("shaped = %s/%s, want Pitched/small", shaped.Status, shaped.Appetite)
	}
	if shaped.Path != backlogPath {
		t.Errorf("shape moved the file: %q", shaped.Path)
	}
	if len(shaped.History) != 1 {
		t.Fatalf("history len = %d, want 1", len(shaped.History))
	}
	assertLastHistory(t, shaped.History, "Backlog", "Pitched", "appetite: small")

	setCurrent(t, s, "C-0001")
	bet, err := s.BetWork("W-0001")
	if err != nil {
		t.Fatalf("BetWork: %v", err)
	}
	cyclePath := filepath.Join(s.CycleDir("C-0001"), "W-0001 - voice-search.md")
	if bet.Status != "Bet" || bet.Cycle != "C-0001" {
		t.Errorf("bet = %s/%s, want Bet/C-0001", bet.Status, bet.Cycle)
	}
	if bet.Path != cyclePath {
		t.Errorf("bet path = %q, want %q", bet.Path, cyclePath)
	}
	if !fsutil.Exists(cyclePath) {
		t.Fatalf("cycle note missing at %s", cyclePath)
	}
	if fsutil.Exists(backlogPath) {
		t.Errorf("bet left a copy in backlog: %s", backlogPath)
	}
	assertLastHistory(t, bet.History, "Pitched", "Bet", "cycle: C-0001")

	shelved, err := s.ShelveWork("W-0001")
	if err != nil {
		t.Fatalf("ShelveWork: %v", err)
	}
	if shelved.Status != "Shelved" || shelved.Cycle != "" {
		t.Errorf("shelved = %s/%q, want Shelved/empty", shelved.Status, shelved.Cycle)
	}
	if shelved.Path != backlogPath {
		t.Errorf("shelved path = %q, want %q", shelved.Path, backlogPath)
	}
	if !fsutil.Exists(backlogPath) || fsutil.Exists(cyclePath) {
		t.Errorf("shelve did not move backlog←cycle")
	}
	assertLastHistory(t, shelved.History, "Bet", "Shelved", "cycle: C-0001")

	unshelved, err := s.UnshelveWork("W-0001")
	if err != nil {
		t.Fatalf("UnshelveWork: %v", err)
	}
	if unshelved.Status != "Pitched" || unshelved.Path != backlogPath {
		t.Errorf("unshelved = %s @ %q, want Pitched @ backlog", unshelved.Status, unshelved.Path)
	}
	assertLastHistory(t, unshelved.History, "Shelved", "Pitched", "")

	if _, err := s.BetWork("W-0001"); err != nil {
		t.Fatalf("re-BetWork: %v", err)
	}
	delivered, err := s.DeliverWork("W-0001")
	if err != nil {
		t.Fatalf("DeliverWork: %v", err)
	}
	if delivered.Status != "Delivered" || delivered.Path != cyclePath {
		t.Errorf("delivered = %s @ %q, want Delivered @ cycle", delivered.Status, delivered.Path)
	}
	if !fsutil.Exists(cyclePath) {
		t.Errorf("delivered note not left in the cycle dir")
	}
	assertLastHistory(t, delivered.History, "Bet", "Delivered", "cycle: C-0001")

	// Delivered is terminal.
	if _, err := s.DeliverWork("W-0001"); codeOf(t, err) != exit.StateConflict {
		t.Errorf("re-deliver code = %v, want state_conflict", codeOf(t, err))
	}
	if _, err := s.ShelveWork("W-0001"); codeOf(t, err) != exit.StateConflict {
		t.Errorf("shelve delivered code = %v, want state_conflict", codeOf(t, err))
	}
}

func TestBacklogToBetRefused(t *testing.T) {
	s := setupVault(t)
	if _, err := s.NewWork("Straight to bet", "scope-a"); err != nil {
		t.Fatal(err)
	}
	setCurrent(t, s, "C-0001")
	if _, err := s.BetWork("W-0001"); codeOf(t, err) != exit.StateConflict {
		t.Errorf("Backlog → Bet code = %v, want state_conflict", codeOf(t, err))
	}
}

func TestBetWithoutCurrent(t *testing.T) {
	s := setupVault(t)
	if _, err := s.NewWork("No cycle", "scope-a"); err != nil {
		t.Fatal(err)
	}
	fillShapeSections(t, filepath.Join(s.BacklogDir(), "W-0001 - no-cycle.md"))
	if _, err := s.ShapeWork("W-0001", "big"); err != nil {
		t.Fatal(err)
	}
	_, err := s.BetWork("W-0001")
	if codeOf(t, err) != exit.StateConflict {
		t.Fatalf("bet without CURRENT code = %v, want state_conflict", codeOf(t, err))
	}
	var e *exit.Error
	if !errors.As(err, &e) || !strings.Contains(strings.Join(e.Hints, " "), "cycle new") {
		t.Errorf("bet without CURRENT hint missing 'cycle new': %v", err)
	}
}

func TestNewWorkPreflight(t *testing.T) {
	s := setupVault(t)
	if _, err := s.NewWork("  ", "scope-a"); codeOf(t, err) != exit.PreflightFailed {
		t.Errorf("empty title code = %v, want preflight_failed", codeOf(t, err))
	}
	if _, err := s.NewWork("Title", "  "); codeOf(t, err) != exit.PreflightFailed {
		t.Errorf("empty scope code = %v, want preflight_failed", codeOf(t, err))
	}

	uninit := Open(t.TempDir())
	if _, err := uninit.NewWork("Title", "scope-a"); codeOf(t, err) != exit.NotInitialized {
		t.Errorf("uninitialized code = %v, want not_initialized", codeOf(t, err))
	}
}

func TestFindWork(t *testing.T) {
	s := setupVault(t)
	if _, err := s.NewWork("Alpha", "scope-a"); err != nil {
		t.Fatal(err)
	}
	backlogPath := filepath.Join(s.BacklogDir(), "W-0001 - alpha.md")
	got, err := s.FindWork("W-0001")
	if err != nil {
		t.Fatalf("FindWork: %v", err)
	}
	if got != backlogPath {
		t.Errorf("FindWork = %q, want %q", got, backlogPath)
	}
	if _, err := s.FindWork("W-0099"); codeOf(t, err) != exit.NotFound {
		t.Errorf("unknown id code = %v, want not_found", codeOf(t, err))
	}
	if _, err := s.FindWork("nope"); codeOf(t, err) != exit.PreflightFailed {
		t.Errorf("bad id code = %v, want preflight_failed", codeOf(t, err))
	}

	// Duplicate leading ID in a cycle dir → state_conflict.
	data, err := os.ReadFile(backlogPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := fsutil.AtomicWrite(filepath.Join(s.CycleDir("C-0001"), "W-0001 - dup.md"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FindWork("W-0001"); codeOf(t, err) != exit.StateConflict {
		t.Errorf("duplicate id code = %v, want state_conflict", codeOf(t, err))
	}
}

func TestListWorkAndShowWork(t *testing.T) {
	s := setupVault(t)

	for _, w := range []struct{ title, scope string }{
		{"Voice search", "scope-a"},
		{"Phone search", "scope-a"},
		{"Offline mode", "scope-b"},
	} {
		if _, err := s.NewWork(w.title, w.scope); err != nil {
			t.Fatalf("NewWork(%s): %v", w.title, err)
		}
	}
	fillShapeSections(t, filepath.Join(s.BacklogDir(), "W-0001 - voice-search.md"))
	if _, err := s.ShapeWork("W-0001", "small"); err != nil {
		t.Fatal(err)
	}

	all, err := s.ListWork("", "")
	if err != nil {
		t.Fatalf("ListWork: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("ListWork all len = %d, want 3", len(all))
	}
	for i, id := range []string{"W-0001", "W-0002", "W-0003"} {
		if all[i].ID != id {
			t.Errorf("ListWork[%d].ID = %q, want %q (sorted)", i, all[i].ID, id)
		}
	}

	pitched, err := s.ListWork("PITCHED", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(pitched) != 1 || pitched[0].ID != "W-0001" {
		t.Errorf("ListWork pitched = %+v, want [W-0001]", pitched)
	}

	scopeA, err := s.ListWork("", "scope-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(scopeA) != 2 {
		t.Errorf("ListWork scope-a len = %d, want 2", len(scopeA))
	}

	backlogA, err := s.ListWork("backlog", "scope-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(backlogA) != 1 || backlogA[0].ID != "W-0002" {
		t.Errorf("ListWork backlog+scope-a = %+v, want [W-0002]", backlogA)
	}

	if _, err := s.ListWork("bogus", ""); codeOf(t, err) != exit.PreflightFailed {
		t.Errorf("bad status code = %v, want preflight_failed", codeOf(t, err))
	}

	show, err := s.ShowWork("W-0003")
	if err != nil {
		t.Fatalf("ShowWork: %v", err)
	}
	if show.Title != "Offline mode" || show.Scope != "scope-b" {
		t.Errorf("ShowWork = %+v", show)
	}
	if _, err := s.ShowWork("W-0042"); codeOf(t, err) != exit.NotFound {
		t.Errorf("ShowWork unknown code = %v, want not_found", codeOf(t, err))
	}
}

func TestWorkItemJSONShape(t *testing.T) {
	s := setupVault(t)
	item, err := s.NewWork("JSON shape", "scope-a")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		`"id"`, `"title"`, `"scope"`, `"status"`, `"appetite"`, `"cycle"`,
		`"project"`, `"neocortex_issue"`, `"path"`, `"created"`,
		`"registry_version"`, `"history":[]`,
	} {
		if !strings.Contains(string(data), key) {
			t.Errorf("marshalled WorkItem missing %s: %s", key, data)
		}
	}
}
