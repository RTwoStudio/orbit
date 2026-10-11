package cycles

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/RTwoStudio/orbit/internal/domain"
	"github.com/RTwoStudio/orbit/internal/exit"
	"github.com/RTwoStudio/orbit/internal/fsutil"
	"github.com/RTwoStudio/orbit/internal/registry"
)

const testRegistryVersion = "0.1.0"

// The test roster's single developer. Every composite work id minted by the
// test vault is W-<seq>-<testOwnerHandle>.
const (
	testOwnerHandle = "arash-jafarpour"
	testOwnerName   = "Arash Jafarpour"
	testOwnerEmail  = "arash@example.com"
)

// testRoster is a one-entry roster whose email/name match the vault clone's git
// identity configured by setupVault.
const testRoster = "# test roster\n" +
	testOwnerHandle + ":\n" +
	"  name: " + testOwnerName + "\n" +
	"  email: " + testOwnerEmail + "\n" +
	"  github: arashjf\n" +
	"  gitlab: arashjf\n"

// workStubFixture mirrors orbit-registry/cycles/stubs/work.stub.md: exactly
// the five registered tokens, the CLI-owned frontmatter fields, and shape
// sections whose only content is an <!-- Agent --> comment (so the shape
// preflight must treat them as empty).
const workStubFixture = `---
id: {{WORK_ID}}
title: "{{WORK_TITLE}}"
owner: {{WORK_OWNER}}
owner_name: "{{WORK_OWNER_NAME}}"
scope: {{SCOPE}}
status: Backlog
appetite:
cycle:
assignees: []
project:
provider:
repo:
issue:
milestone:
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
// and returns an initialized Store over a fresh temp vault carrying the test
// roster and a matching git identity.
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
	seedTestRoster(t, s)
	return s
}

// seedTestRoster writes the test roster and gives the vault clone a matching
// git identity, so ResolveHandle succeeds. It skips when git is unavailable.
func seedTestRoster(t *testing.T, s *Store) {
	t.Helper()
	if err := fsutil.AtomicWrite(s.RosterPath(), []byte(testRoster), 0o644); err != nil {
		t.Fatal(err)
	}
	setGitIdentity(t, s.vaultRoot())
}

// setGitIdentity initializes the vault clone with the test developer's local
// git identity. Tests skip when git is not installed.
func setGitIdentity(t *testing.T, dir string) {
	t.Helper()
	setGitIdentityAs(t, dir, testOwnerName, testOwnerEmail)
}

// gitAvailable reports whether the git binary is on PATH.
func gitAvailable() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// runGit runs a git subcommand in dir and fails the test on error.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-C", dir}, args...)
	if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
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

// fillSection inserts non-comment content under one shape heading.
func fillSection(t *testing.T, path, name string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	marker := "## " + name + "\n"
	s := strings.Replace(string(data), marker, marker+"\nFilled "+name+".\n", 1)
	if err := fsutil.AtomicWrite(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fillShapeSections inserts non-comment content under each shape heading so
// the shape preflight passes.
func fillShapeSections(t *testing.T, path string) {
	t.Helper()
	for _, name := range []string{"Problem", "Solution Sketch"} {
		fillSection(t, path, name)
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
	if item.ID != "W-0001-arash-jafarpour" {
		t.Errorf("id = %q, want W-0001-arash-jafarpour", item.ID)
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
	backlogPath := filepath.Join(s.BacklogDir(), "W-0001-arash-jafarpour - voice-search.md")
	if item.Path != backlogPath {
		t.Errorf("path = %q, want %q", item.Path, backlogPath)
	}
	if !fsutil.Exists(backlogPath) {
		t.Fatalf("backlog note missing at %s", backlogPath)
	}

	// Comments-only shape sections refuse with preflight_failed.
	if _, err := s.ShapeWork("W-0001-arash-jafarpour", "small"); codeOf(t, err) != exit.PreflightFailed {
		t.Errorf("shape on empty sections code = %v, want preflight_failed", codeOf(t, err))
	}
	// Invalid appetite refuses with preflight_failed.
	if _, err := s.ShapeWork("W-0001-arash-jafarpour", "huge"); codeOf(t, err) != exit.PreflightFailed {
		t.Errorf("shape with bad appetite code = %v, want preflight_failed", codeOf(t, err))
	}

	fillShapeSections(t, backlogPath)
	shaped, err := s.ShapeWork("W-0001-arash-jafarpour", "small")
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
	bet, err := s.BetWork("W-0001-arash-jafarpour", []string{testOwnerHandle})
	if err != nil {
		t.Fatalf("BetWork: %v", err)
	}
	if !reflect.DeepEqual(bet.Assignees, []string{testOwnerHandle}) {
		t.Errorf("bet assignees = %v, want [%s]", bet.Assignees, testOwnerHandle)
	}
	cyclePath := filepath.Join(s.CycleDir("C-0001"), "W-0001-arash-jafarpour - voice-search.md")
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

	shelved, err := s.ShelveWork("W-0001-arash-jafarpour")
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

	unshelved, err := s.UnshelveWork("W-0001-arash-jafarpour")
	if err != nil {
		t.Fatalf("UnshelveWork: %v", err)
	}
	if unshelved.Status != "Pitched" || unshelved.Path != backlogPath {
		t.Errorf("unshelved = %s @ %q, want Pitched @ backlog", unshelved.Status, unshelved.Path)
	}
	assertLastHistory(t, unshelved.History, "Shelved", "Pitched", "")

	if _, err := s.BetWork("W-0001-arash-jafarpour", []string{testOwnerHandle}); err != nil {
		t.Fatalf("re-BetWork: %v", err)
	}
	delivered, err := s.DeliverWork("W-0001-arash-jafarpour")
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
	if _, err := s.DeliverWork("W-0001-arash-jafarpour"); codeOf(t, err) != exit.StateConflict {
		t.Errorf("re-deliver code = %v, want state_conflict", codeOf(t, err))
	}
	if _, err := s.ShelveWork("W-0001-arash-jafarpour"); codeOf(t, err) != exit.StateConflict {
		t.Errorf("shelve delivered code = %v, want state_conflict", codeOf(t, err))
	}
}

// TestShapeWorkTwoSectionShape is the T1 regression: `shape` requires exactly
// ## Problem and ## Solution Sketch, so either left comment-only is refused.
func TestShapeWorkTwoSectionShape(t *testing.T) {
	s := setupVault(t)
	if _, err := s.NewWork("Half shaped", "scope-a"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.BacklogDir(), "W-0001-arash-jafarpour - half-shaped.md")

	// Problem filled, Solution Sketch still comment-only → preflight_failed.
	fillSection(t, path, "Problem")
	if _, err := s.ShapeWork("W-0001-arash-jafarpour", "small"); codeOf(t, err) != exit.PreflightFailed {
		t.Errorf("shape with Solution Sketch empty code = %v, want preflight_failed", codeOf(t, err))
	}

	// Filling the missing half lets the shape through (Backlog → Pitched).
	fillSection(t, path, "Solution Sketch")
	shaped, err := s.ShapeWork("W-0001-arash-jafarpour", "small")
	if err != nil {
		t.Fatalf("ShapeWork with both sections: %v", err)
	}
	if shaped.Status != "Pitched" {
		t.Errorf("shaped status = %q, want Pitched", shaped.Status)
	}

	// Fresh item, Solution Sketch filled but Problem comment-only → refused.
	if _, err := s.NewWork("Other half", "scope-a"); err != nil {
		t.Fatal(err)
	}
	otherPath := filepath.Join(s.BacklogDir(), "W-0002-arash-jafarpour - other-half.md")
	fillSection(t, otherPath, "Solution Sketch")
	if _, err := s.ShapeWork("W-0002-arash-jafarpour", "big"); codeOf(t, err) != exit.PreflightFailed {
		t.Errorf("shape with Problem empty code = %v, want preflight_failed", codeOf(t, err))
	}
}

func TestBacklogToBetRefused(t *testing.T) {
	s := setupVault(t)
	if _, err := s.NewWork("Straight to bet", "scope-a"); err != nil {
		t.Fatal(err)
	}
	setCurrent(t, s, "C-0001")
	if _, err := s.BetWork("W-0001-arash-jafarpour", []string{testOwnerHandle}); codeOf(t, err) != exit.StateConflict {
		t.Errorf("Backlog → Bet code = %v, want state_conflict", codeOf(t, err))
	}
}

func TestBetWithoutCurrent(t *testing.T) {
	s := setupVault(t)
	if _, err := s.NewWork("No cycle", "scope-a"); err != nil {
		t.Fatal(err)
	}
	fillShapeSections(t, filepath.Join(s.BacklogDir(), "W-0001-arash-jafarpour - no-cycle.md"))
	if _, err := s.ShapeWork("W-0001-arash-jafarpour", "big"); err != nil {
		t.Fatal(err)
	}
	_, err := s.BetWork("W-0001-arash-jafarpour", []string{testOwnerHandle})
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
	backlogPath := filepath.Join(s.BacklogDir(), "W-0001-arash-jafarpour - alpha.md")
	got, err := s.FindWork("W-0001-arash-jafarpour")
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
	if err := fsutil.AtomicWrite(filepath.Join(s.CycleDir("C-0001"), "W-0001-arash-jafarpour - dup.md"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FindWork("W-0001-arash-jafarpour"); codeOf(t, err) != exit.StateConflict {
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
	fillShapeSections(t, filepath.Join(s.BacklogDir(), "W-0001-arash-jafarpour - voice-search.md"))
	if _, err := s.ShapeWork("W-0001-arash-jafarpour", "small"); err != nil {
		t.Fatal(err)
	}

	all, err := s.ListWork("", "")
	if err != nil {
		t.Fatalf("ListWork: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("ListWork all len = %d, want 3", len(all))
	}
	for i, id := range []string{"W-0001-arash-jafarpour", "W-0002-arash-jafarpour", "W-0003-arash-jafarpour"} {
		if all[i].ID != id {
			t.Errorf("ListWork[%d].ID = %q, want %q (sorted)", i, all[i].ID, id)
		}
	}

	pitched, err := s.ListWork("PITCHED", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(pitched) != 1 || pitched[0].ID != "W-0001-arash-jafarpour" {
		t.Errorf("ListWork pitched = %+v, want [W-0001-arash-jafarpour]", pitched)
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
	if len(backlogA) != 1 || backlogA[0].ID != "W-0002-arash-jafarpour" {
		t.Errorf("ListWork backlog+scope-a = %+v, want [W-0002-arash-jafarpour]", backlogA)
	}

	if _, err := s.ListWork("bogus", ""); codeOf(t, err) != exit.PreflightFailed {
		t.Errorf("bad status code = %v, want preflight_failed", codeOf(t, err))
	}

	show, err := s.ShowWork("W-0003-arash-jafarpour")
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
	// Populate the forge links so the omitempty int fields are present.
	item, err = s.SetWorkForge(item.ID, WorkForge{
		Provider: "github", Repo: "RTwoStudio/orbit", Issue: 7, Milestone: 3,
	})
	if err != nil {
		t.Fatalf("SetWorkForge: %v", err)
	}
	data, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		`"id"`, `"title"`, `"owner"`, `"owner_name"`, `"scope"`, `"status"`, `"appetite"`, `"cycle"`,
		`"assignees":[]`,
		`"project"`, `"provider":"github"`, `"repo":"RTwoStudio/orbit"`,
		`"issue":7`, `"milestone":3`, `"neocortex_issue"`, `"path"`,
		`"created"`, `"registry_version"`, `"history":[]`,
	} {
		if !strings.Contains(string(data), key) {
			t.Errorf("marshalled WorkItem missing %s: %s", key, data)
		}
	}
}

// TestWorkItemAssigneesReadShape: a new note and a legacy note without the key
// both render a non-nil empty assignees list, so --json never emits null.
func TestWorkItemAssigneesReadShape(t *testing.T) {
	s := setupVault(t)
	it, err := s.NewWork("No assignees yet", "scope-a")
	if err != nil {
		t.Fatal(err)
	}
	if it.Assignees == nil || len(it.Assignees) != 0 {
		t.Errorf("new item assignees = %#v, want empty non-nil", it.Assignees)
	}
	data, err := json.Marshal(it)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"assignees":[]`) {
		t.Errorf("new item JSON missing \"assignees\":[]: %s", data)
	}

	// A legacy note with no assignees: key normalizes to [] (not null).
	legacy := filepath.Join(s.BacklogDir(), "W-0042 - legacy.md")
	note := "---\nid: W-0042\ntitle: \"Legacy\"\nscope: old\nstatus: Backlog\nhistory: []\n---\n\n# W-0042 — Legacy\n"
	if err := fsutil.AtomicWrite(legacy, []byte(note), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := s.ShowWork("W-0042")
	if err != nil {
		t.Fatal(err)
	}
	if got.Assignees == nil || len(got.Assignees) != 0 {
		t.Errorf("legacy item assignees = %#v, want empty non-nil", got.Assignees)
	}
}

// TestBetWorkAssigneesStampsDedupes: BetWork trims, roster-validates and dedupes
// the assignee list, preserving first-seen order, and it round-trips through a
// read.
func TestBetWorkAssigneesStampsDedupes(t *testing.T) {
	s := setupVault(t)
	writeRoster(t, s, testRosterTwo)
	if _, err := s.NewCycle("Team", "0.1.0", "", ""); err != nil {
		t.Fatal(err)
	}
	it, err := s.NewWork("Assigned", "scope-a")
	if err != nil {
		t.Fatal(err)
	}
	fillShapeSections(t, it.Path)
	if _, err := s.ShapeWork(it.ID, "small"); err != nil {
		t.Fatal(err)
	}

	bet, err := s.BetWork(it.ID, []string{
		"seyed-mahdi-rostami",
		" arash-jafarpour ",
		"seyed-mahdi-rostami", // duplicate dropped
	})
	if err != nil {
		t.Fatalf("BetWork: %v", err)
	}
	want := []string{"seyed-mahdi-rostami", "arash-jafarpour"}
	if !reflect.DeepEqual(bet.Assignees, want) {
		t.Errorf("assignees = %v, want %v", bet.Assignees, want)
	}

	back, err := s.ShowWork(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back.Assignees, want) {
		t.Errorf("round-trip assignees = %v, want %v", back.Assignees, want)
	}
}

// TestBetWorkAssigneesFaults: an empty set and an unknown handle are
// preflight_failed (6) and leave the item Pitched in backlog/.
func TestBetWorkAssigneesFaults(t *testing.T) {
	s := setupVault(t)
	if _, err := s.NewCycle("Team", "0.1.0", "", ""); err != nil {
		t.Fatal(err)
	}
	it, err := s.NewWork("Assigned", "scope-a")
	if err != nil {
		t.Fatal(err)
	}
	fillShapeSections(t, it.Path)
	if _, err := s.ShapeWork(it.ID, "small"); err != nil {
		t.Fatal(err)
	}

	if _, err := s.BetWork(it.ID, nil); codeOf(t, err) != exit.PreflightFailed {
		t.Errorf("empty assignees code = %v, want preflight_failed", codeOf(t, err))
	}
	if _, err := s.BetWork(it.ID, []string{"  "}); codeOf(t, err) != exit.PreflightFailed {
		t.Errorf("blank assignee code = %v, want preflight_failed", codeOf(t, err))
	}
	if _, err := s.BetWork(it.ID, []string{"nobody"}); codeOf(t, err) != exit.PreflightFailed {
		t.Errorf("unknown assignee code = %v, want preflight_failed", codeOf(t, err))
	}

	show, err := s.ShowWork(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if show.Status != "Pitched" || show.Cycle != "" {
		t.Errorf("status after failed bets = %s/%q, want Pitched/empty", show.Status, show.Cycle)
	}
}

// TestSetAssigneesValidatesAndPersists: handles are trimmed, roster-validated,
// deduped, non-empty enforced, persisted, and round-trip through a read.
func TestSetAssigneesValidatesAndPersists(t *testing.T) {
	s := setupVault(t)
	writeRoster(t, s, testRosterTwo)
	it, err := s.NewWork("Assign me", "scope-a")
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.SetAssignees(it.ID, []string{
		" arash-jafarpour ", "seyed-mahdi-rostami", "arash-jafarpour",
	})
	if err != nil {
		t.Fatalf("SetAssignees: %v", err)
	}
	want := []string{"arash-jafarpour", "seyed-mahdi-rostami"}
	if !reflect.DeepEqual(got.Assignees, want) {
		t.Errorf("assignees = %v, want %v", got.Assignees, want)
	}
	back, err := s.ShowWork(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back.Assignees, want) {
		t.Errorf("round-trip assignees = %v, want %v", back.Assignees, want)
	}

	if _, err := s.SetAssignees(it.ID, nil); codeOf(t, err) != exit.PreflightFailed {
		t.Errorf("empty assignees code = %v, want preflight_failed", codeOf(t, err))
	}
	if _, err := s.SetAssignees(it.ID, []string{"nobody"}); codeOf(t, err) != exit.PreflightFailed {
		t.Errorf("unknown assignee code = %v, want preflight_failed", codeOf(t, err))
	}
	if _, err := s.SetAssignees("W-0099", []string{testOwnerHandle}); codeOf(t, err) != exit.NotFound {
		t.Errorf("missing work code = %v, want not_found", codeOf(t, err))
	}
}
