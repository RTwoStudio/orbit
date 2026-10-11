package cycles

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/RTwoStudio/orbit/internal/doc"
	"github.com/RTwoStudio/orbit/internal/exit"
	"github.com/RTwoStudio/orbit/internal/fsutil"
)

// cycleStubFixture mirrors orbit-registry/cycles/stubs/cycle.stub.md: exactly
// the registered tokens, the design §5 frontmatter, and the CLI-generated
// ## Bets read view.
const cycleStubFixture = `---
release: {{RELEASE}}
id: {{CYCLE_ID}}
goal: "{{CYCLE_GOAL}}"
created_by: {{CREATED_BY}}
subscribers: []
status: Open
start: {{START_DATE}}
end: {{END_DATE}}
project:
repo:
provider:
milestone: {}
history: []
created: {{DATE}}
registry-version: {{REGISTRY_VERSION}}
---

# {{CYCLE_ID}} — {{CYCLE_GOAL}}

> Status and frontmatter are CLI-owned. Never hand-edit anything above the
> closing delimiter.

## Goal

<!-- Agent: What this cycle commits to ship, in one or two lines. -->

## Bets

<!-- CLI-generated read view over the cycle's committed work — do not hand-edit -->

## Notes

<!-- Agent: Anything else — scope notes, links, open questions. -->
`

// mustWork creates a work item, fills its shape sections, shapes it to Pitched
// and (optionally) bets it into the current cycle.
func mustWork(t *testing.T, s *Store, title, scope string, bet bool) *WorkItem {
	t.Helper()
	it, err := s.NewWork(title, scope)
	if err != nil {
		t.Fatalf("NewWork(%s): %v", title, err)
	}
	fillShapeSections(t, it.Path)
	if _, err := s.ShapeWork(it.ID, "small"); err != nil {
		t.Fatalf("ShapeWork(%s): %v", it.ID, err)
	}
	if bet {
		if _, err := s.BetWork(it.ID, []string{testOwnerHandle}); err != nil {
			t.Fatalf("BetWork(%s): %v", it.ID, err)
		}
	}
	return it
}

// cycleNote reads a cycle note's raw bytes.
func cycleNote(t *testing.T, s *Store, id string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(s.CycleDir(id), id+".md"))
	if err != nil {
		t.Fatalf("read cycle note %s: %v", id, err)
	}
	return string(data)
}

// cycleBetsSection returns the inner text of a cycle note's ## Bets section.
func cycleBetsSection(t *testing.T, note string) string {
	t.Helper()
	d, err := doc.ParseDocBytes("cycle.md", []byte(note))
	if err != nil {
		t.Fatalf("parse cycle note: %v", err)
	}
	return string(doc.Section(d.Body, "Bets"))
}

func TestParseCycleStatus(t *testing.T) {
	cases := map[string]CycleStatus{
		"Open":       CycleOpen,
		"open":       CycleOpen,
		"OPEN":       CycleOpen,
		"Closed":     CycleClosed,
		"closed":     CycleClosed,
		"  Closed  ": CycleClosed,
	}
	for in, want := range cases {
		got, err := ParseCycleStatus(in)
		if err != nil {
			t.Errorf("ParseCycleStatus(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseCycleStatus(%q) = %q, want %q", in, got, want)
		}
	}
	if _, err := ParseCycleStatus("bogus"); codeOf(t, err) != exit.PreflightFailed {
		t.Errorf("unknown status code = %v, want preflight_failed", codeOf(t, err))
	}
}

func TestCheckCycleTransition(t *testing.T) {
	if err := CheckCycleTransition(CycleOpen, CycleClosed); err != nil {
		t.Errorf("Open → Closed = %v, want nil", err)
	}
	for _, c := range [][2]CycleStatus{
		{CycleOpen, CycleOpen},
		{CycleClosed, CycleOpen},
		{CycleClosed, CycleClosed},
	} {
		err := CheckCycleTransition(c[0], c[1])
		if codeOf(t, err) != exit.StateConflict {
			t.Errorf("CheckCycleTransition(%s, %s) code = %v, want state_conflict", c[0], c[1], codeOf(t, err))
		}
	}
}

func TestParseCycleID(t *testing.T) {
	valid := []string{"C-0001", "C-1234", "C-99999", " C-0001 "}
	for _, id := range valid {
		if _, err := ParseCycleID(id); err != nil {
			t.Errorf("ParseCycleID(%q): %v", id, err)
		}
	}
	invalid := []string{"", "C-001", "C-1", "C-abcd", "W-0001-arash-jafarpour", "c-0001", "0001", "C-0001x", "C 0001"}
	for _, id := range invalid {
		if _, err := ParseCycleID(id); codeOf(t, err) != exit.PreflightFailed {
			t.Errorf("ParseCycleID(%q) code = %v, want preflight_failed", id, codeOf(t, err))
		}
	}
}

func TestNextCycleID(t *testing.T) {
	s := setupVault(t)
	id, err := s.NextCycleID()
	if err != nil {
		t.Fatalf("NextCycleID: %v", err)
	}
	if id != "C-0001" {
		t.Errorf("empty vault next id = %q, want C-0001", id)
	}

	if err := fsutil.EnsureDir(s.CycleDir("C-0003")); err != nil {
		t.Fatal(err)
	}
	id, err = s.NextCycleID()
	if err != nil {
		t.Fatal(err)
	}
	if id != "C-0004" {
		t.Errorf("NextCycleID = %q, want C-0004 (monotonic)", id)
	}

	// A cycle-note filename is observed even under a non-cycle directory name.
	if err := fsutil.EnsureDir(filepath.Join(s.LedgerDir(), "misc")); err != nil {
		t.Fatal(err)
	}
	if err := fsutil.AtomicWrite(filepath.Join(s.LedgerDir(), "misc", "C-0009.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	id, err = s.NextCycleID()
	if err != nil {
		t.Fatal(err)
	}
	if id != "C-0010" {
		t.Errorf("NextCycleID = %q, want C-0010 (note filenames count)", id)
	}
}

func TestWriteClearCurrent(t *testing.T) {
	s := setupVault(t)
	// WriteCurrent requires the cycle directory to exist.
	if err := s.WriteCurrent("C-0001"); codeOf(t, err) != exit.StateConflict {
		t.Errorf("write to missing cycle code = %v, want state_conflict", codeOf(t, err))
	}
	if err := s.WriteCurrent("nope"); codeOf(t, err) != exit.PreflightFailed {
		t.Errorf("bad id code = %v, want preflight_failed", codeOf(t, err))
	}
	if err := fsutil.EnsureDir(s.CycleDir("C-0001")); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteCurrent("C-0001"); err != nil {
		t.Fatalf("WriteCurrent: %v", err)
	}
	got, err := s.ReadCurrent()
	if err != nil {
		t.Fatalf("ReadCurrent: %v", err)
	}
	if got != "C-0001" {
		t.Errorf("ReadCurrent = %q, want C-0001", got)
	}
	if err := s.ClearCurrent(); err != nil {
		t.Fatalf("ClearCurrent: %v", err)
	}
	if !fsutil.Exists(s.CurrentPath()) {
		t.Errorf("ClearCurrent removed the CURRENT file; it should stay empty")
	}
	if _, err := s.ReadCurrent(); codeOf(t, err) != exit.StateConflict {
		t.Errorf("after ClearCurrent code = %v, want state_conflict", codeOf(t, err))
	}
}

func TestNewCycle(t *testing.T) {
	s := setupVault(t)
	cy, err := s.NewCycle("Ship v1", "0.3.0", "", "")
	if err != nil {
		t.Fatalf("NewCycle: %v", err)
	}
	if cy.ID != "C-0001" {
		t.Errorf("id = %q, want C-0001", cy.ID)
	}
	if cy.Status != "Open" {
		t.Errorf("status = %q, want Open", cy.Status)
	}
	if cy.Release != "0.3.0" {
		t.Errorf("release = %q, want 0.3.0", cy.Release)
	}
	if cy.Goal != "Ship v1" {
		t.Errorf("goal = %q", cy.Goal)
	}
	if cy.RegistryVersion != testRegistryVersion {
		t.Errorf("registry-version = %q, want %q", cy.RegistryVersion, testRegistryVersion)
	}
	wantPath := filepath.Join(s.CycleDir("C-0001"), "C-0001.md")
	if cy.Path != wantPath {
		t.Errorf("path = %q, want %q", cy.Path, wantPath)
	}
	if !fsutil.Exists(wantPath) {
		t.Fatalf("cycle note missing at %s", wantPath)
	}
	if got, err := s.ReadCurrent(); err != nil || got != "C-0001" {
		t.Errorf("ReadCurrent = %q, %v; want C-0001", got, err)
	}
	if _, err := time.Parse(time.RFC3339, cy.Created); err != nil {
		t.Errorf("created %q is not RFC3339: %v", cy.Created, err)
	}
	start, err := time.Parse("2006-01-02", cy.Start)
	if err != nil {
		t.Errorf("start %q is not a bare date: %v", cy.Start, err)
	}
	end, err := time.Parse("2006-01-02", cy.End)
	if err != nil {
		t.Errorf("end %q is not a bare date: %v", cy.End, err)
	}
	if DefaultCycleWindow != 42*24*time.Hour {
		t.Errorf("DefaultCycleWindow = %v, want 42 days", DefaultCycleWindow)
	}
	if end.Sub(start) != DefaultCycleWindow {
		t.Errorf("default window = %v, want %v", end.Sub(start), DefaultCycleWindow)
	}

	note := cycleNote(t, s, "C-0001")
	for _, want := range []string{"release: 0.3.0", "id: C-0001", "goal:", "status: Open", "created:"} {
		if !strings.Contains(note, want) {
			t.Errorf("cycle note missing %q:\n%s", want, note)
		}
	}
	if bets := cycleBetsSection(t, note); strings.TrimSpace(bets) != "" {
		t.Errorf("new cycle Bets should be empty, got %q", bets)
	}
}

func TestNewCycleExplicitDates(t *testing.T) {
	s := setupVault(t)
	cy, err := s.NewCycle("Window", "1.2.3", "2026-01-01", "2026-01-15")
	if err != nil {
		t.Fatalf("NewCycle: %v", err)
	}
	if cy.Start != "2026-01-01" || cy.End != "2026-01-15" {
		t.Errorf("window = %s..%s, want 2026-01-01..2026-01-15", cy.Start, cy.End)
	}
}

func TestNewCycleRefusals(t *testing.T) {
	s := setupVault(t)
	if _, err := s.NewCycle("  ", "0.1.0", "", ""); codeOf(t, err) != exit.PreflightFailed {
		t.Errorf("empty goal code = %v, want preflight_failed", codeOf(t, err))
	}
	if _, err := s.NewCycle("Goal", "1.2", "", ""); codeOf(t, err) != exit.PreflightFailed {
		t.Errorf("bad release code = %v, want preflight_failed", codeOf(t, err))
	}
	if _, err := s.NewCycle("Goal", "1.2.3.4", "", ""); codeOf(t, err) != exit.PreflightFailed {
		t.Errorf("four-part release code = %v, want preflight_failed", codeOf(t, err))
	}
	if _, err := s.NewCycle("Goal", "0.1.0", "2026-02-01", "2026-01-01"); codeOf(t, err) != exit.PreflightFailed {
		t.Errorf("end before start code = %v, want preflight_failed", codeOf(t, err))
	}
	if _, err := s.NewCycle("Goal", "0.1.0", "not-a-date", ""); codeOf(t, err) != exit.PreflightFailed {
		t.Errorf("bad start code = %v, want preflight_failed", codeOf(t, err))
	}
	if _, err := s.NewCycle("Goal", "0.1.0", "", "not-a-date"); codeOf(t, err) != exit.PreflightFailed {
		t.Errorf("bad end code = %v, want preflight_failed", codeOf(t, err))
	}

	if _, err := s.NewCycle("First", "0.1.0", "", ""); err != nil {
		t.Fatalf("first NewCycle: %v", err)
	}
	_, err := s.NewCycle("Second", "0.2.0", "", "")
	if codeOf(t, err) != exit.StateConflict {
		t.Errorf("open-cycle code = %v, want state_conflict", codeOf(t, err))
	}
	var e *exit.Error
	if !errors.As(err, &e) || !strings.Contains(strings.Join(e.Hints, " "), "cycle close") {
		t.Errorf("open-cycle hint missing 'cycle close': %v", err)
	}
}

func TestNewCycleUninitialized(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	uninit := Open(t.TempDir())
	if _, err := uninit.NewCycle("Goal", "0.1.0", "", ""); codeOf(t, err) != exit.NotInitialized {
		t.Errorf("uninitialized code = %v, want not_initialized", codeOf(t, err))
	}
}

func TestCloseCycleAutoShelve(t *testing.T) {
	s := setupVault(t)
	cy, err := s.NewCycle("Close me", "0.5.0", "", "")
	if err != nil {
		t.Fatal(err)
	}
	mustWork(t, s, "Cancellation flow", "scope-a", true)
	mustWork(t, s, "Phone search", "scope-a", true)
	if _, err := s.DeliverWork("W-0001-arash-jafarpour"); err != nil {
		t.Fatalf("DeliverWork: %v", err)
	}

	closed, err := s.CloseCycle()
	if err != nil {
		t.Fatalf("CloseCycle: %v", err)
	}
	if closed.Status != "Closed" {
		t.Errorf("closed status = %q, want Closed", closed.Status)
	}
	if _, err := s.ReadCurrent(); codeOf(t, err) != exit.StateConflict {
		t.Errorf("CURRENT after close code = %v, want state_conflict", codeOf(t, err))
	}
	if !fsutil.Exists(filepath.Join(s.CycleDir("C-0001"), "W-0001-arash-jafarpour - cancellation-flow.md")) {
		t.Errorf("delivered note was not left in the cycle folder")
	}
	shelvedPath := filepath.Join(s.BacklogDir(), "W-0002-arash-jafarpour - phone-search.md")
	if !fsutil.Exists(shelvedPath) {
		t.Fatalf("shelved note missing at %s", shelvedPath)
	}
	got, err := s.ShowWork("W-0002-arash-jafarpour")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "Shelved" || got.Cycle != "" {
		t.Errorf("shelved = %s/%q, want Shelved/empty", got.Status, got.Cycle)
	}
	assertLastHistory(t, got.History, "Bet", "Shelved", "cycle: C-0001")

	note := cycleNote(t, s, "C-0001")
	if !strings.Contains(note, "status: Closed") {
		t.Errorf("cycle note status not Closed:\n%s", note)
	}
	bets := cycleBetsSection(t, note)
	if !strings.Contains(bets, "W-0001-arash-jafarpour") || !strings.Contains(bets, "(Delivered)") {
		t.Errorf("closed Bets missing delivered item: %q", bets)
	}
	if strings.Contains(bets, "W-0002-arash-jafarpour") {
		t.Errorf("closed Bets still lists the shelved item: %q", bets)
	}

	if _, err := s.CloseCycle(); codeOf(t, err) != exit.StateConflict {
		t.Errorf("close with no open cycle code = %v, want state_conflict", codeOf(t, err))
	}
	if err := s.WriteCurrent(cy.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CloseCycle(); codeOf(t, err) != exit.StateConflict {
		t.Errorf("close already-Closed code = %v, want state_conflict", codeOf(t, err))
	}
}

func TestBetsRefreshOnWorkVerbs(t *testing.T) {
	s := setupVault(t)
	if _, err := s.NewCycle("Bets", "0.1.0", "", ""); err != nil {
		t.Fatal(err)
	}
	it, err := s.NewWork("Cancellation flow", "scope-a")
	if err != nil {
		t.Fatal(err)
	}
	fillShapeSections(t, it.Path)
	if _, err := s.ShapeWork(it.ID, "small"); err != nil {
		t.Fatal(err)
	}

	if bets := cycleBetsSection(t, cycleNote(t, s, "C-0001")); strings.TrimSpace(bets) != "" {
		t.Errorf("bets before bet = %q, want empty", bets)
	}
	if _, err := s.BetWork(it.ID, []string{testOwnerHandle}); err != nil {
		t.Fatal(err)
	}
	if bets := cycleBetsSection(t, cycleNote(t, s, "C-0001")); !strings.Contains(bets, "- W-0001-arash-jafarpour — Cancellation flow (Bet)") {
		t.Errorf("bets after bet = %q", bets)
	}
	if _, err := s.ShelveWork(it.ID); err != nil {
		t.Fatal(err)
	}
	if bets := cycleBetsSection(t, cycleNote(t, s, "C-0001")); strings.TrimSpace(bets) != "" {
		t.Errorf("bets after shelve = %q, want empty", bets)
	}
	if _, err := s.UnshelveWork(it.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BetWork(it.ID, []string{testOwnerHandle}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeliverWork(it.ID); err != nil {
		t.Fatal(err)
	}
	if bets := cycleBetsSection(t, cycleNote(t, s, "C-0001")); !strings.Contains(bets, "- W-0001-arash-jafarpour — Cancellation flow (Delivered)") {
		t.Errorf("bets after deliver = %q", bets)
	}
}

// TestWorkVerbWithoutCycleNote proves the best-effort Bets refresh never fails a
// work verb when the cycle note is missing.
func TestWorkVerbWithoutCycleNote(t *testing.T) {
	s := setupVault(t)
	setCurrent(t, s, "C-0001") // cycle dir, no cycle note
	it, err := s.NewWork("No note", "scope-a")
	if err != nil {
		t.Fatal(err)
	}
	fillShapeSections(t, it.Path)
	if _, err := s.ShapeWork(it.ID, "small"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BetWork(it.ID, []string{testOwnerHandle}); err != nil {
		t.Fatalf("BetWork without a cycle note: %v", err)
	}
}

func TestListShowCycle(t *testing.T) {
	s := setupVault(t)
	if _, err := s.NewCycle("First", "0.1.0", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CloseCycle(); err != nil {
		t.Fatal(err)
	}
	cy2, err := s.NewCycle("Second", "0.2.0", "", "")
	if err != nil {
		t.Fatal(err)
	}

	list, err := s.ListCycles()
	if err != nil {
		t.Fatalf("ListCycles: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("ListCycles len = %d, want 2", len(list))
	}
	if list[0].ID != "C-0001" || list[1].ID != "C-0002" {
		t.Errorf("ListCycles order = %s, %s; want C-0001, C-0002", list[0].ID, list[1].ID)
	}
	if list[0].Status != "Closed" || list[1].Status != "Open" {
		t.Errorf("ListCycles statuses = %s, %s; want Closed, Open", list[0].Status, list[1].Status)
	}
	if list[0].Bets != nil || list[1].Bets != nil {
		t.Errorf("ListCycles must not attach bets")
	}

	mustWork(t, s, "Delta", "scope-a", true)
	cy, err := s.ShowCycle(cy2.ID)
	if err != nil {
		t.Fatalf("ShowCycle: %v", err)
	}
	if len(cy.Bets) != 1 || cy.Bets[0].ID != "W-0001-arash-jafarpour" {
		t.Errorf("ShowCycle bets = %+v, want [W-0001-arash-jafarpour]", cy.Bets)
	}
	if _, err := s.ShowCycle("C-0099"); codeOf(t, err) != exit.NotFound {
		t.Errorf("unknown cycle code = %v, want not_found", codeOf(t, err))
	}
	if _, err := s.ShowCycle("nope"); codeOf(t, err) != exit.PreflightFailed {
		t.Errorf("bad cycle id code = %v, want preflight_failed", codeOf(t, err))
	}
}

func TestCycleJSONShape(t *testing.T) {
	s := setupVault(t)
	cy, err := s.NewCycle("JSON", "0.1.0", "", "")
	if err != nil {
		t.Fatal(err)
	}
	cy, err = s.SetCycleForge(cy.ID, CycleForge{
		Project:    "/home/x/proj",
		Milestones: map[string]int{"RTwoStudio/orbit": 4},
	})
	if err != nil {
		t.Fatalf("SetCycleForge: %v", err)
	}
	data, err := json.Marshal(cy)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		`"id"`, `"goal"`, `"created_by":"` + testOwnerHandle + `"`, `"subscribers":[]`,
		`"release"`, `"status"`, `"start"`, `"end"`,
		`"project":"/home/x/proj"`, `"repo"`, `"provider"`,
		`"milestone":{"RTwoStudio/orbit":4}`,
		`"created"`, `"registry_version"`, `"path"`, `"history":[]`, `"bets":[]`,
	} {
		if !strings.Contains(string(data), key) {
			t.Errorf("marshalled Cycle missing %s: %s", key, data)
		}
	}
}

// testRosterTwo is a two-entry roster so the subscribers helpers can exercise
// order, dedupe, and unknown-handle validation.
const testRosterTwo = "# two developers\n" +
	"arash-jafarpour:\n" +
	"  name: Arash Jafarpour\n" +
	"  email: arash@example.com\n" +
	"  github: arashjf\n" +
	"seyed-mahdi-rostami:\n" +
	"  name: Seyed Mahdi Rostami\n" +
	"  email: seyed@example.com\n" +
	"  github: seyedmr\n"

// TestNewCycleStampsCreatedBy: `cycle new` resolves the creator from git
// identity + roster and stamps `created_by:`; subscribers seed empty.
func TestNewCycleStampsCreatedBy(t *testing.T) {
	s := setupVault(t)
	cy, err := s.NewCycle("Ship v1", "0.3.0", "", "")
	if err != nil {
		t.Fatalf("NewCycle: %v", err)
	}
	if cy.CreatedBy != testOwnerHandle {
		t.Errorf("created_by = %q, want %q", cy.CreatedBy, testOwnerHandle)
	}
	if cy.Subscribers == nil || len(cy.Subscribers) != 0 {
		t.Errorf("subscribers = %#v, want empty non-nil", cy.Subscribers)
	}
	note := cycleNote(t, s, cy.ID)
	for _, want := range []string{"created_by: " + testOwnerHandle, "subscribers: []"} {
		if !strings.Contains(note, want) {
			t.Errorf("cycle note missing %q:\n%s", want, note)
		}
	}
}

// TestNewCycleRequiresRosterIdentity: a vault with no roster fails
// preflight_failed (6), exactly like work new.
func TestNewCycleRequiresRosterIdentity(t *testing.T) {
	s := testStore(t) // initialized, no roster, no git identity
	if _, err := s.NewCycle("Goal", "0.1.0", "", ""); codeOf(t, err) != exit.PreflightFailed {
		t.Fatalf("no-roster code = %v, want preflight_failed", codeOf(t, err))
	}
}

// TestSetSubscribersValidatesAndDedupes: handles are trimmed, validated against
// the roster, deduped preserving first-seen order, and round-trip through a read.
func TestSetSubscribersValidatesAndDedupes(t *testing.T) {
	s := setupVault(t)
	writeRoster(t, s, testRosterTwo)
	cy, err := s.NewCycle("Team", "0.1.0", "", "")
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.SetSubscribers(cy.ID, []string{
		"seyed-mahdi-rostami",
		" arash-jafarpour ",
		"seyed-mahdi-rostami", // duplicate dropped
	})
	if err != nil {
		t.Fatalf("SetSubscribers: %v", err)
	}
	want := []string{"seyed-mahdi-rostami", "arash-jafarpour"}
	if !reflect.DeepEqual(got.Subscribers, want) {
		t.Errorf("subscribers = %v, want %v", got.Subscribers, want)
	}

	back, err := s.ShowCycle(cy.ID)
	if err != nil {
		t.Fatalf("ShowCycle: %v", err)
	}
	if !reflect.DeepEqual(back.Subscribers, want) {
		t.Errorf("round-trip subscribers = %v, want %v", back.Subscribers, want)
	}

	// An empty list round-trips as a non-nil empty slice, not null.
	empty, err := s.SetSubscribers(cy.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if empty.Subscribers == nil || len(empty.Subscribers) != 0 {
		t.Errorf("cleared subscribers = %#v, want empty non-nil", empty.Subscribers)
	}
}

// TestSetSubscribersFaults: unknown/empty handles and bad ids are coded errors.
func TestSetSubscribersFaults(t *testing.T) {
	s := setupVault(t)
	cy, err := s.NewCycle("Team", "0.1.0", "", "")
	if err != nil {
		t.Fatal(err)
	}

	_, err = s.SetSubscribers(cy.ID, []string{"nobody"})
	if codeOf(t, err) != exit.PreflightFailed {
		t.Fatalf("unknown handle code = %v, want preflight_failed", codeOf(t, err))
	}
	var e *exit.Error
	if !errors.As(err, &e) || !strings.Contains(strings.Join(e.Hints, " "), s.RosterPath()) {
		t.Errorf("unknown-handle hint should name the roster: %v", err)
	}
	if _, err := s.SetSubscribers(cy.ID, []string{"  "}); codeOf(t, err) != exit.PreflightFailed {
		t.Errorf("empty handle code = %v, want preflight_failed", codeOf(t, err))
	}
	if _, err := s.SetSubscribers("C-0099", []string{testOwnerHandle}); codeOf(t, err) != exit.NotFound {
		t.Errorf("missing cycle code = %v, want not_found", codeOf(t, err))
	}
	if _, err := s.SetSubscribers("nope", nil); codeOf(t, err) != exit.PreflightFailed {
		t.Errorf("bad id code = %v, want preflight_failed", codeOf(t, err))
	}
}

// TestAddSubscriberIdempotent: adding an existing handle rewrites the same list.
func TestAddSubscriberIdempotent(t *testing.T) {
	s := setupVault(t)
	cy, err := s.NewCycle("Team", "0.1.0", "", "")
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.AddSubscriber(cy.ID, testOwnerHandle)
	if err != nil {
		t.Fatalf("AddSubscriber: %v", err)
	}
	if !reflect.DeepEqual(got.Subscribers, []string{testOwnerHandle}) {
		t.Errorf("after add = %v, want [%s]", got.Subscribers, testOwnerHandle)
	}
	got, err = s.AddSubscriber(cy.ID, testOwnerHandle)
	if err != nil {
		t.Fatalf("re-add: %v", err)
	}
	if !reflect.DeepEqual(got.Subscribers, []string{testOwnerHandle}) {
		t.Errorf("after re-add = %v, want [%s] (idempotent)", got.Subscribers, testOwnerHandle)
	}
	if _, err := s.AddSubscriber(cy.ID, "nobody"); codeOf(t, err) != exit.PreflightFailed {
		t.Errorf("add unknown code = %v, want preflight_failed", codeOf(t, err))
	}
}

// TestCycleNotePathIsDeterministic pins the locked invariant: the cycle note
// path is cycles/C-####/C-####.md — the id in the directory and filename, with
// no goal slug.
func TestCycleNotePathIsDeterministic(t *testing.T) {
	s := setupVault(t)
	cy, err := s.NewCycle("A goal with spaces and punctuation!", "2.4.6", "", "")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(s.LedgerDir(), "C-0001", "C-0001.md")
	if cy.Path != want {
		t.Errorf("path = %q, want %q", cy.Path, want)
	}
	if !fsutil.Exists(want) {
		t.Errorf("cycle note not at the deterministic path %s", want)
	}
	if got := filepath.Base(want); got != "C-0001.md" {
		t.Errorf("filename = %q, want C-0001.md (no slug)", got)
	}
}

// writeCycleNote writes a minimal cycle note at cycles/<dir>/<name> carrying
// the given id and provenance, so the reconcile loser rule is observable.
func writeCycleNote(t *testing.T, s *Store, dir, name, id, created, createdBy string) string {
	t.Helper()
	path := filepath.Join(s.LedgerDir(), dir, name)
	content := "---\n" +
		"id: " + id + "\n" +
		"created_by: " + createdBy + "\n" +
		"status: Open\n" +
		"history: []\n" +
		"created: " + created + "\n" +
		"---\n\n# " + id + "\n"
	if err := fsutil.AtomicWrite(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// errMessage returns a coded error's message.
func errMessage(t *testing.T, err error) string {
	t.Helper()
	var e *exit.Error
	if !errors.As(err, &e) {
		t.Fatalf("error %v is not *exit.Error", err)
	}
	return e.Message
}

// TestNewCycleHistoryField: a fresh cycle carries an empty, non-nil history and
// the note stub renders `history: []`.
func TestNewCycleHistoryField(t *testing.T) {
	s := setupVault(t)
	cy, err := s.NewCycle("History", "0.1.0", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if cy.History == nil || len(cy.History) != 0 {
		t.Errorf("history = %#v, want empty non-nil", cy.History)
	}
	if note := cycleNote(t, s, cy.ID); !strings.Contains(note, "history: []") {
		t.Errorf("cycle note missing history: []:\n%s", note)
	}
}

// TestNewCycleRefusesDuplicate: the post-pull duplicate-C-#### preflight runs
// before allocation and refuses with state_conflict (7) + a git pull --rebase
// hint, naming both files.
func TestNewCycleRefusesDuplicate(t *testing.T) {
	s := setupVault(t)
	a := writeCycleNote(t, s, "C-0001", "C-0001.md", "C-0001", "2026-01-01T00:00:00Z", testOwnerHandle)
	b := writeCycleNote(t, s, "C-0001-copy", "C-0001.md", "C-0001", "2026-01-02T00:00:00Z", testOwnerHandle)

	_, err := s.NewCycle("Post-pull", "0.1.0", "", "")
	if codeOf(t, err) != exit.StateConflict {
		t.Fatalf("duplicate NewCycle code = %v, want state_conflict", codeOf(t, err))
	}
	var e *exit.Error
	if !errors.As(err, &e) || !strings.Contains(strings.Join(e.Hints, " "), "git pull --rebase") {
		t.Errorf("duplicate NewCycle hint should mention git pull --rebase: %v", err)
	}
	msg := errMessage(t, err)
	if !strings.Contains(msg, a) || !strings.Contains(msg, b) {
		t.Errorf("message %q must name both %q and %q", msg, a, b)
	}
}

// TestNewCycleRefusesExistingID: the refuse-existing guard fires when the
// computed id already exists on disk but NextCycleID's directory scan did not
// see it (here simulated with a symlinked cycle folder, as a concurrent
// writer's checkout could leave). A clean vault still opens the cycle.
func TestNewCycleRefusesExistingID(t *testing.T) {
	s := setupVault(t)
	target := t.TempDir()
	if err := os.Symlink(target, s.CycleDir("C-0001")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	_, err := s.NewCycle("Race", "0.1.0", "", "")
	if codeOf(t, err) != exit.StateConflict {
		t.Fatalf("existing-id NewCycle code = %v, want state_conflict", codeOf(t, err))
	}
	var e *exit.Error
	if !errors.As(err, &e) || !strings.Contains(strings.Join(e.Hints, " "), "git pull --rebase") {
		t.Errorf("existing-id hint should mention git pull --rebase: %v", err)
	}
}

// TestReconcileCycleIDsNoDuplicates: a clean vault renumbers nothing.
func TestReconcileCycleIDsNoDuplicates(t *testing.T) {
	s := setupVault(t)
	if _, err := s.NewCycle("Solo", "0.1.0", "", ""); err != nil {
		t.Fatal(err)
	}
	out, err := s.ReconcileCycleIDs()
	if err != nil {
		t.Fatalf("ReconcileCycleIDs: %v", err)
	}
	if out != nil {
		t.Errorf("renumbers = %+v, want nil", out)
	}
}

// TestReconcileCycleIDsByCreated: the later `created:` note loses and is
// renumbered — folder + note move to the deterministic path, id: rewritten,
// one history line appended, and a re-scan is clean.
func TestReconcileCycleIDsByCreated(t *testing.T) {
	s := setupVault(t)
	early := writeCycleNote(t, s, "C-0007", "C-0007.md", "C-0007", "2026-01-01T00:00:00Z", testOwnerHandle)
	late := writeCycleNote(t, s, "C-0007-copy", "C-0007.md", "C-0007", "2026-01-02T00:00:00Z", testOwnerHandle)

	out, err := s.ReconcileCycleIDs()
	if err != nil {
		t.Fatalf("ReconcileCycleIDs: %v", err)
	}
	if len(out) != 1 || out[0].From != "C-0007" || out[0].To != "C-0008" {
		t.Fatalf("renumbers = %+v, want one C-0007 → C-0008", out)
	}
	wantPath := filepath.Join(s.LedgerDir(), "C-0008", "C-0008.md")
	if out[0].Path != wantPath {
		t.Errorf("renumber path = %q, want %q", out[0].Path, wantPath)
	}
	if !fsutil.Exists(wantPath) {
		t.Fatalf("renumbered note missing at %s", wantPath)
	}
	if !fsutil.Exists(early) || fsutil.Exists(late) {
		t.Errorf("winner/loser note placement wrong: early=%v late=%v", fsutil.Exists(early), fsutil.Exists(late))
	}
	if fsutil.Exists(filepath.Dir(late)) {
		t.Errorf("loser dir %s still present", filepath.Dir(late))
	}
	got := cycleNote(t, s, "C-0008")
	if !strings.Contains(got, "id: C-0008") {
		t.Errorf("renumbered note id not rewritten:\n%s", got)
	}
	if !strings.Contains(got, "C-0007 → C-0008 (duplicate reconciled)") {
		t.Errorf("renumbered note missing history line:\n%s", got)
	}
	if err := s.CheckCycleIDs(); err != nil {
		t.Errorf("post-reconcile CheckCycleIDs: %v", err)
	}
}

// TestReconcileCycleIDsCreatedByTiebreak: equal `created:` tiebreaks on the
// greater `created_by:` handle, which loses.
func TestReconcileCycleIDsCreatedByTiebreak(t *testing.T) {
	s := setupVault(t)
	early := writeCycleNote(t, s, "C-0005", "C-0005.md", "C-0005", "2026-01-01T00:00:00Z", "arash-jafarpour")
	late := writeCycleNote(t, s, "C-0005-copy", "C-0005.md", "C-0005", "2026-01-01T00:00:00Z", "seyed-mahdi-rostami")

	out, err := s.ReconcileCycleIDs()
	if err != nil {
		t.Fatalf("ReconcileCycleIDs: %v", err)
	}
	if len(out) != 1 || out[0].To != "C-0006" {
		t.Fatalf("renumbers = %+v, want one → C-0006", out)
	}
	if !fsutil.Exists(early) || fsutil.Exists(late) {
		t.Errorf("tiebreak winner/loser wrong: early=%v late=%v", fsutil.Exists(early), fsutil.Exists(late))
	}
}

// TestReconcileCycleIDsSameDirKeepsWinner: when the winner and loser share a
// folder, only the loser moves — the winner's folder is left untouched.
func TestReconcileCycleIDsSameDirKeepsWinner(t *testing.T) {
	s := setupVault(t)
	winner := writeCycleNote(t, s, "C-0007", "C-0007.md", "C-0007", "2026-01-01T00:00:00Z", testOwnerHandle)
	loser := writeCycleNote(t, s, "C-0007", "C-0007 - copy.md", "C-0007", "2026-01-02T00:00:00Z", testOwnerHandle)

	out, err := s.ReconcileCycleIDs()
	if err != nil {
		t.Fatalf("ReconcileCycleIDs: %v", err)
	}
	if len(out) != 1 || out[0].To != "C-0008" {
		t.Fatalf("renumbers = %+v, want one → C-0008", out)
	}
	if !fsutil.Exists(winner) {
		t.Errorf("winner %s was moved with the loser's folder", winner)
	}
	if fsutil.Exists(loser) {
		t.Errorf("loser %s still present", loser)
	}
	if !fsutil.Exists(filepath.Join(s.CycleDir("C-0008"), "C-0008.md")) {
		t.Errorf("loser not renumbered to the deterministic path")
	}
	if err := s.CheckCycleIDs(); err != nil {
		t.Errorf("post-reconcile CheckCycleIDs: %v", err)
	}
}

// TestReconcileCycleIDsMovesWorkNotes: committed work notes travel with the
// renamed cycle folder.
func TestReconcileCycleIDsMovesWorkNotes(t *testing.T) {
	s := setupVault(t)
	writeCycleNote(t, s, "C-0007", "C-0007.md", "C-0007", "2026-01-01T00:00:00Z", testOwnerHandle)
	writeCycleNote(t, s, "C-0007-copy", "C-0007.md", "C-0007", "2026-01-02T00:00:00Z", testOwnerHandle)
	betName := "W-0001-" + testOwnerHandle + " - bet.md"
	writeRaw(t, filepath.Join(s.CycleDir("C-0007-copy"), betName))

	if _, err := s.ReconcileCycleIDs(); err != nil {
		t.Fatalf("ReconcileCycleIDs: %v", err)
	}
	if !fsutil.Exists(filepath.Join(s.CycleDir("C-0008"), betName)) {
		t.Errorf("committed work note did not move with the cycle folder")
	}
}

// TestReconcileCycleIDsCurrentUntouched: reconcile acts on the ledger only and
// never rewrites CURRENT.
func TestReconcileCycleIDsCurrentUntouched(t *testing.T) {
	s := setupVault(t)
	if _, err := s.NewCycle("Open", "0.1.0", "", ""); err != nil {
		t.Fatal(err)
	}
	writeCycleNote(t, s, "C-0001-copy", "C-0001.md", "C-0001", "2099-01-01T00:00:00Z", testOwnerHandle)

	if _, err := s.ReconcileCycleIDs(); err != nil {
		t.Fatalf("ReconcileCycleIDs: %v", err)
	}
	if got := currentContents(t, s); got != "C-0001" {
		t.Errorf("CURRENT = %q, want C-0001 (reconcile must not touch it)", got)
	}
	if !fsutil.Exists(filepath.Join(s.CycleDir("C-0002"), "C-0002.md")) {
		t.Errorf("duplicate was not renumbered to C-0002")
	}
}

// TestReconcileCycleIDsMultipleLosers: a three-way duplicate converges with one
// renumber per losing note.
func TestReconcileCycleIDsMultipleLosers(t *testing.T) {
	s := setupVault(t)
	writeCycleNote(t, s, "C-0007", "C-0007.md", "C-0007", "2026-01-01T00:00:00Z", testOwnerHandle)
	writeCycleNote(t, s, "C-0007-b", "C-0007.md", "C-0007", "2026-01-02T00:00:00Z", testOwnerHandle)
	writeCycleNote(t, s, "C-0007-c", "C-0007.md", "C-0007", "2026-01-03T00:00:00Z", testOwnerHandle)

	out, err := s.ReconcileCycleIDs()
	if err != nil {
		t.Fatalf("ReconcileCycleIDs: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("renumbers = %+v, want 2", out)
	}
	if err := s.CheckCycleIDs(); err != nil {
		t.Errorf("post-reconcile CheckCycleIDs: %v", err)
	}
}
