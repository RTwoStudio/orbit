package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/RTwoStudio/orbit/internal/cycles"
	"github.com/RTwoStudio/orbit/internal/domain"
	"github.com/RTwoStudio/orbit/internal/exit"
	"github.com/RTwoStudio/orbit/internal/fsutil"
	"github.com/RTwoStudio/orbit/internal/registry"
)

// cyclesCmdWorkStub mirrors orbit-registry/cycles/stubs/work.stub.md: exactly
// the five registered tokens and the CLI-owned frontmatter fields.
const cyclesCmdWorkStub = `---
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
`

// cyclesCmdCycleStub mirrors orbit-registry/cycles/stubs/cycle.stub.md.
const cyclesCmdCycleStub = `---
release: {{RELEASE}}
id: {{CYCLE_ID}}
goal: "{{CYCLE_GOAL}}"
status: Open
start: {{START_DATE}}
end: {{END_DATE}}
created: {{DATE}}
registry-version: {{REGISTRY_VERSION}}
---

# {{CYCLE_ID}} — {{CYCLE_GOAL}}

## Goal

<!-- Agent: What this cycle commits to ship. -->

## Bets

<!-- CLI-generated read view — do not hand-edit -->

## Notes

<!-- Agent: Anything else. -->
`

// writeCyclesCmdFixture writes a hash-valid cycles registry fixture.
func writeCyclesCmdFixture(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, domain.Cycles.Base)
	files := map[string]string{
		"stubs/work.stub.md":  cyclesCmdWorkStub,
		"stubs/cycle.stub.md": cyclesCmdCycleStub,
	}
	m := registry.Manifest{Version: "0.1.0", Layout: 1}
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

// cyclesCmdEnv is one isolated cycles command environment: an initialized
// vault plus a temp config pointing at it.
type cyclesCmdEnv struct {
	vault   string
	cfgPath string
}

// setupCyclesCmd isolates $HOME, primes the cycles cache from a fixture
// registry, and writes a temp config whose vault.dir is an initialized vault.
// The vault layout is created directly — `orbit cycles install` is T7.
func setupCyclesCmd(t *testing.T) *cyclesCmdEnv {
	t.Helper()
	t.Setenv("HOME", t.TempDir())

	regRoot := t.TempDir()
	writeCyclesCmdFixture(t, regRoot)
	fc, err := registry.FetchAll(registry.DirFetcher{Root: regRoot, Domain: domain.Cycles})
	if err != nil {
		t.Fatalf("registry FetchAll: %v", err)
	}
	if err := registry.CacheSave(fc, domain.Cycles); err != nil {
		t.Fatalf("registry CacheSave: %v", err)
	}

	vault := t.TempDir()
	initCyclesVault(t, vault)

	cfgPath := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(cfgPath, []byte("vault:\n  dir: "+vault+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return &cyclesCmdEnv{vault: vault, cfgPath: cfgPath}
}

// initCyclesVault creates the minimal Cycles layout (install is T7).
func initCyclesVault(t *testing.T, vault string) {
	t.Helper()
	s := cycles.Open(vault)
	for _, d := range []string{s.BacklogDir(), s.LedgerDir()} {
		if err := fsutil.EnsureDir(d); err != nil {
			t.Fatal(err)
		}
	}
	if err := fsutil.AtomicWrite(s.CurrentPath(), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
}

// runCycles executes the full root tree against cfgPath and captures output.
// It mirrors execute()'s flag-error wrapping so usage exits are observable.
func runCycles(t *testing.T, cfgPath string, args ...string) (string, string, error) {
	t.Helper()
	root := NewRootCmd()
	root.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		return exit.New(exit.Usage, err.Error(), "run with --help to see valid flags")
	})
	var out, errb bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errb)
	root.SetArgs(append([]string{"--config", cfgPath}, args...))
	err := root.Execute()
	return out.String(), errb.String(), err
}

// wantCode asserts err is a coded exit.Error with the given code.
func wantCode(t *testing.T, err error, want exit.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected exit %d, got nil", want)
	}
	var e *exit.Error
	if !errors.As(err, &e) {
		t.Fatalf("error %v is not *exit.Error", err)
	}
	if e.Code != want {
		t.Fatalf("exit code = %s(%d), want %s(%d): %v", e.Code.Name(), e.Code, want.Name(), want, err)
	}
}

// decodeJSON unmarshals command stdout into T.
func decodeJSON[T any](t *testing.T, s string) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("invalid JSON %q: %v", s, err)
	}
	return v
}

// fillCyclesShapeSections inserts non-comment content under every shape
// heading so the shape preflight passes.
func fillCyclesShapeSections(t *testing.T, path string) {
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

// mustCyclesWork creates one work item and shapes it to Pitched.
func mustCyclesWork(t *testing.T, cfgPath, title, scope string) cycles.WorkItem {
	t.Helper()
	out, _, err := runCycles(t, cfgPath, "cycles", "work", "new", title, "--scope", scope, "--json")
	if err != nil {
		t.Fatalf("work new %q: %v", title, err)
	}
	item := decodeJSON[cycles.WorkItem](t, out)
	fillCyclesShapeSections(t, item.Path)
	out, _, err = runCycles(t, cfgPath, "cycles", "work", "shape", item.ID, "--appetite", "small", "--json")
	if err != nil {
		t.Fatalf("work shape %s: %v", item.ID, err)
	}
	return decodeJSON[cycles.WorkItem](t, out)
}

func TestCyclesHelpTree(t *testing.T) {
	env := setupCyclesCmd(t)

	out, _, err := runCycles(t, env.cfgPath, "cycles", "--help")
	if err != nil {
		t.Fatalf("cycles --help: %v", err)
	}
	for _, want := range []string{"work", "cycle", "status", "install", "Exit codes", "not_initialized"} {
		if !strings.Contains(out, want) {
			t.Errorf("cycles --help missing %q; got:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "alias: init") {
		t.Errorf("cycles --help must note the install/init alias; got:\n%s", out)
	}

	// Unknown verb at the group and at each subgroup.
	_, _, err = runCycles(t, env.cfgPath, "cycles", "bogus")
	wantCode(t, err, exit.Usage)
	_, _, err = runCycles(t, env.cfgPath, "cycles", "work", "bogus")
	wantCode(t, err, exit.Usage)
	_, _, err = runCycles(t, env.cfgPath, "cycles", "cycle", "bogus")
	wantCode(t, err, exit.Usage)
}

func TestCyclesWorkLifecycle(t *testing.T) {
	env := setupCyclesCmd(t)

	out, _, err := runCycles(t, env.cfgPath, "cycles", "work", "new", "Cancellation flow", "--scope", "app", "--json")
	if err != nil {
		t.Fatalf("work new --json: %v", err)
	}
	item := decodeJSON[cycles.WorkItem](t, out)
	if item.ID != "W-0001" || item.Status != "Backlog" || item.Scope != "app" {
		t.Fatalf("unexpected WorkItem: %+v", item)
	}
	if item.Path == "" {
		t.Fatal("WorkItem path is empty")
	}

	// Human default for a mutation.
	out, _, err = runCycles(t, env.cfgPath, "cycles", "work", "new", "Second", "--scope", "app")
	if err != nil {
		t.Fatalf("work new: %v", err)
	}
	if !strings.Contains(out, "Created W-0002 (Backlog)") {
		t.Errorf("human work new output = %q", out)
	}

	fillCyclesShapeSections(t, item.Path)
	out, _, err = runCycles(t, env.cfgPath, "cycles", "work", "shape", "W-0001", "--appetite", "small", "--json")
	if err != nil {
		t.Fatalf("work shape: %v", err)
	}
	item = decodeJSON[cycles.WorkItem](t, out)
	if item.Status != "Pitched" || item.Appetite != "small" {
		t.Errorf("shaped item = %+v", item)
	}

	// list --json emits the typed slice.
	out, _, err = runCycles(t, env.cfgPath, "cycles", "work", "list", "--json")
	if err != nil {
		t.Fatalf("work list --json: %v", err)
	}
	items := decodeJSON[[]cycles.WorkItem](t, out)
	if len(items) != 2 {
		t.Fatalf("work list --json len = %d, want 2", len(items))
	}

	// list human has the aligned header and a row.
	out, _, err = runCycles(t, env.cfgPath, "cycles", "work", "list")
	if err != nil {
		t.Fatalf("work list: %v", err)
	}
	for _, want := range []string{"ID", "TITLE", "STATUS", "SCOPE", "CYCLE", "W-0001"} {
		if !strings.Contains(out, want) {
			t.Errorf("work list missing %q; got:\n%s", want, out)
		}
	}

	// show human prints the note verbatim.
	out, _, err = runCycles(t, env.cfgPath, "cycles", "work", "show", "W-0001")
	if err != nil {
		t.Fatalf("work show: %v", err)
	}
	if !strings.Contains(out, "# W-0001") {
		t.Errorf("work show human missing note body; got:\n%s", out)
	}

	// show --json emits the typed WorkItem.
	out, _, err = runCycles(t, env.cfgPath, "cycles", "work", "show", "W-0001", "--json")
	if err != nil {
		t.Fatalf("work show --json: %v", err)
	}
	if got := decodeJSON[cycles.WorkItem](t, out); got.ID != "W-0001" || got.Status != "Pitched" {
		t.Errorf("work show --json = %+v", got)
	}
}

func TestCyclesCycleAndStatus(t *testing.T) {
	env := setupCyclesCmd(t)
	mustCyclesWork(t, env.cfgPath, "Cancellation flow", "app")

	out, _, err := runCycles(t, env.cfgPath, "cycles", "cycle", "new", "Ship the beta", "--release", "0.3.0", "--json")
	if err != nil {
		t.Fatalf("cycle new --json: %v", err)
	}
	cy := decodeJSON[cycles.Cycle](t, out)
	if cy.ID != "C-0001" || cy.Status != "Open" || cy.Release != "0.3.0" {
		t.Fatalf("cycle new = %+v", cy)
	}

	out, _, err = runCycles(t, env.cfgPath, "cycles", "work", "bet", "W-0001", "--json")
	if err != nil {
		t.Fatalf("work bet: %v", err)
	}
	bet := decodeJSON[cycles.WorkItem](t, out)
	if bet.Status != "Bet" || bet.Cycle != "C-0001" {
		t.Fatalf("bet item = %+v", bet)
	}

	// status --json exposes the typed board.
	out, _, err = runCycles(t, env.cfgPath, "cycles", "status", "--json")
	if err != nil {
		t.Fatalf("status --json: %v", err)
	}
	board := decodeJSON[cycles.StatusBoard](t, out)
	if board.Current == nil || board.Current.ID != "C-0001" || len(board.Current.Bets) != 1 {
		t.Fatalf("board = %+v", board)
	}

	// status human shows the header and every labelled block.
	out, _, err = runCycles(t, env.cfgPath, "cycles", "status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	for _, want := range []string{"Current cycle: C-0001", "Backlog:", "Pitched:", "Shelved:", "W-0001"} {
		if !strings.Contains(out, want) {
			t.Errorf("status missing %q; got:\n%s", want, out)
		}
	}

	// cycle list human + json.
	out, _, err = runCycles(t, env.cfgPath, "cycles", "cycle", "list")
	if err != nil {
		t.Fatalf("cycle list: %v", err)
	}
	for _, want := range []string{"ID", "GOAL", "RELEASE", "STATUS", "START", "END", "C-0001"} {
		if !strings.Contains(out, want) {
			t.Errorf("cycle list missing %q; got:\n%s", want, out)
		}
	}
	out, _, err = runCycles(t, env.cfgPath, "cycles", "cycle", "list", "--json")
	if err != nil {
		t.Fatalf("cycle list --json: %v", err)
	}
	if rows := decodeJSON[[]cycles.Cycle](t, out); len(rows) != 1 || rows[0].ID != "C-0001" {
		t.Errorf("cycle list --json = %+v", rows)
	}

	// cycle show human + json.
	out, _, err = runCycles(t, env.cfgPath, "cycles", "cycle", "show", "C-0001")
	if err != nil {
		t.Fatalf("cycle show: %v", err)
	}
	if !strings.Contains(out, "# C-0001") {
		t.Errorf("cycle show human missing note body; got:\n%s", out)
	}
	out, _, err = runCycles(t, env.cfgPath, "cycles", "cycle", "show", "C-0001", "--json")
	if err != nil {
		t.Fatalf("cycle show --json: %v", err)
	}
	if got := decodeJSON[cycles.Cycle](t, out); got.ID != "C-0001" || len(got.Bets) != 1 {
		t.Errorf("cycle show --json = %+v", got)
	}

	// close: human default, then board has no current and W-0001 is Shelved.
	out, _, err = runCycles(t, env.cfgPath, "cycles", "cycle", "close")
	if err != nil {
		t.Fatalf("cycle close: %v", err)
	}
	if !strings.Contains(out, "Closed cycle C-0001") {
		t.Errorf("cycle close output = %q", out)
	}
	out, _, err = runCycles(t, env.cfgPath, "cycles", "status", "--json")
	if err != nil {
		t.Fatalf("status --json after close: %v", err)
	}
	board = decodeJSON[cycles.StatusBoard](t, out)
	if board.Current != nil {
		t.Errorf("board.Current after close = %+v, want nil", board.Current)
	}
	if len(board.Backlog.Shelved) != 1 || board.Backlog.Shelved[0].ID != "W-0001" {
		t.Errorf("shelved after close = %+v", board.Backlog.Shelved)
	}

	// Human status with no open cycle.
	out, _, err = runCycles(t, env.cfgPath, "cycles", "status")
	if err != nil {
		t.Fatalf("status after close: %v", err)
	}
	if !strings.Contains(out, "No open cycle.") {
		t.Errorf("status after close missing 'No open cycle.'; got:\n%s", out)
	}
}

func TestCyclesExitCodes(t *testing.T) {
	env := setupCyclesCmd(t)

	// Malformed id → preflight_failed (6).
	_, _, err := runCycles(t, env.cfgPath, "cycles", "work", "show", "W-X")
	wantCode(t, err, exit.PreflightFailed)

	// Unknown well-formed id → not_found (5).
	_, _, err = runCycles(t, env.cfgPath, "cycles", "work", "show", "W-9999")
	wantCode(t, err, exit.NotFound)

	// Missing operands → usage (2).
	_, _, err = runCycles(t, env.cfgPath, "cycles", "work", "new")
	wantCode(t, err, exit.Usage)
	_, _, err = runCycles(t, env.cfgPath, "cycles", "cycle", "new")
	wantCode(t, err, exit.Usage)

	// Unknown flag → usage (2).
	_, _, err = runCycles(t, env.cfgPath, "cycles", "work", "list", "--bogus")
	wantCode(t, err, exit.Usage)

	// Bad status filter → preflight_failed (6).
	_, _, err = runCycles(t, env.cfgPath, "cycles", "work", "list", "--status", "Nope")
	wantCode(t, err, exit.PreflightFailed)

	// A shaped item, but no open cycle → bet is a state_conflict (7).
	mustCyclesWork(t, env.cfgPath, "No cycle yet", "app")
	_, _, err = runCycles(t, env.cfgPath, "cycles", "work", "bet", "W-0001")
	wantCode(t, err, exit.StateConflict)

	// Uninitialized vault → not_initialized (11).
	uninit := t.TempDir()
	_, _, err = runCycles(t, env.cfgPath, "cycles", "--vault", uninit, "work", "new", "X", "--scope", "s")
	wantCode(t, err, exit.NotInitialized)

	// Invalid release at cycle new → preflight_failed (6).
	_, _, err = runCycles(t, env.cfgPath, "cycles", "cycle", "new", "Goal", "--release", "v0.3")
	wantCode(t, err, exit.PreflightFailed)

	// Invalid date at cycle new → preflight_failed (6).
	_, _, err = runCycles(t, env.cfgPath, "cycles", "cycle", "new", "Goal", "--release", "0.3.0", "--start", "10-2026")
	wantCode(t, err, exit.PreflightFailed)
}

func TestCyclesVaultResolution(t *testing.T) {
	env := setupCyclesCmd(t)

	// --vault overrides config vault.dir: the note lands under the override.
	other := t.TempDir()
	initCyclesVault(t, other)
	out, _, err := runCycles(t, env.cfgPath, "cycles", "--vault", other, "work", "new", "Overridden", "--scope", "s", "--json")
	if err != nil {
		t.Fatalf("work new --vault: %v", err)
	}
	item := decodeJSON[cycles.WorkItem](t, out)
	if !strings.HasPrefix(item.Path, other) {
		t.Errorf("work note path %q does not live under --vault %q", item.Path, other)
	}
	if _, err := os.Stat(filepath.Join(env.vault, "Cycles", "backlog", filepath.Base(item.Path))); err == nil {
		t.Errorf("work note wrongly created in the config vault")
	}

	// An empty resolved vault → config_error (3).
	emptyCfg := filepath.Join(t.TempDir(), "empty.yml")
	if err := os.WriteFile(emptyCfg, []byte("vault:\n  dir: \"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err = runCycles(t, emptyCfg, "cycles", "status")
	wantCode(t, err, exit.ConfigError)
}
