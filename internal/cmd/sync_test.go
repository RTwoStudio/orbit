package cmd

import (
	"strings"
	"testing"

	"github.com/RTwoStudio/orbit/internal/cycles"
	"github.com/RTwoStudio/orbit/internal/exit"
)

// TestCyclesSyncHelp checks that both group trees list the new sync verb and
// that work sync documents its --project flag and the exit-code footer.
func TestCyclesSyncHelp(t *testing.T) {
	env := setupCyclesCmd(t)

	out, _, err := runCycles(t, env.cfgPath, "cycles", "work", "--help")
	if err != nil {
		t.Fatalf("work --help: %v", err)
	}
	if !strings.Contains(out, "sync") {
		t.Errorf("work --help missing sync; got:\n%s", out)
	}

	out, _, err = runCycles(t, env.cfgPath, "cycles", "work", "sync", "--help")
	if err != nil {
		t.Fatalf("work sync --help: %v", err)
	}
	for _, want := range []string{"--project", "Exit codes", "general", "preflight_failed", "state_conflict"} {
		if !strings.Contains(out, want) {
			t.Errorf("work sync --help missing %q; got:\n%s", want, out)
		}
	}

	out, _, err = runCycles(t, env.cfgPath, "cycles", "cycle", "--help")
	if err != nil {
		t.Fatalf("cycle --help: %v", err)
	}
	if !strings.Contains(out, "sync") {
		t.Errorf("cycle --help missing sync; got:\n%s", out)
	}

	out, _, err = runCycles(t, env.cfgPath, "cycles", "cycle", "sync", "--help")
	if err != nil {
		t.Fatalf("cycle sync --help: %v", err)
	}
	for _, want := range []string{"Exit codes", "general", "state_conflict"} {
		if !strings.Contains(out, want) {
			t.Errorf("cycle sync --help missing %q; got:\n%s", want, out)
		}
	}
}

// TestCyclesWorkSyncUsageAndLookup covers the offline argument/id error paths.
func TestCyclesWorkSyncUsageAndLookup(t *testing.T) {
	env := setupCyclesCmd(t)

	// Missing operand → usage (2).
	_, _, err := runCycles(t, env.cfgPath, "cycles", "work", "sync")
	wantCode(t, err, exit.Usage)

	// Unknown well-formed id → not_found (5).
	_, _, err = runCycles(t, env.cfgPath, "cycles", "work", "sync", "W-9999")
	wantCode(t, err, exit.NotFound)
}

// TestCyclesWorkSyncNonBet: sync is Bet-only, so any other status is a
// state_conflict that trips before any project/forge work.
func TestCyclesWorkSyncNonBet(t *testing.T) {
	env := setupCyclesCmd(t)

	if _, _, err := runCycles(t, env.cfgPath, "cycles", "work", "new", "Backlog item", "--scope", "app", "--json"); err != nil {
		t.Fatalf("work new: %v", err)
	}
	_, _, err := runCycles(t, env.cfgPath, "cycles", "work", "sync", "W-0001")
	wantCode(t, err, exit.StateConflict)
}

// TestCyclesWorkSyncNoProject: a Bet with no project: and no --project cannot
// resolve a repo, so it is a preflight_failed (6) — still fully offline.
func TestCyclesWorkSyncNoProject(t *testing.T) {
	env := setupCyclesCmd(t)
	mustCyclesWork(t, env.cfgPath, "No project", "app") // W-0001 Pitched

	if _, _, err := runCycles(t, env.cfgPath, "cycles", "cycle", "new", "Ship the beta", "--release", "0.3.0", "--json"); err != nil {
		t.Fatalf("cycle new: %v", err)
	}
	if _, _, err := runCycles(t, env.cfgPath, "cycles", "work", "bet", "W-0001", "--json"); err != nil {
		t.Fatalf("work bet: %v", err)
	}

	_, _, err := runCycles(t, env.cfgPath, "cycles", "work", "sync", "W-0001")
	wantCode(t, err, exit.PreflightFailed)

	// An explicit empty --project behaves the same (still no repo).
	_, _, err = runCycles(t, env.cfgPath, "cycles", "work", "sync", "W-0001", "--project", "")
	wantCode(t, err, exit.PreflightFailed)
}

// TestCyclesCycleSyncNoOpenCycle: without an open cycle, cycle sync is a
// state_conflict (7) surfaced unchanged from ReadCurrent.
func TestCyclesCycleSyncNoOpenCycle(t *testing.T) {
	env := setupCyclesCmd(t)

	_, _, err := runCycles(t, env.cfgPath, "cycles", "cycle", "sync")
	wantCode(t, err, exit.StateConflict)
}

// TestCyclesCycleSyncEmpty: an open cycle with no bets syncs nothing, exits 0,
// and renders the agreed report/human shapes (non-nil slices → JSON []).
func TestCyclesCycleSyncEmpty(t *testing.T) {
	env := setupCyclesCmd(t)

	if _, _, err := runCycles(t, env.cfgPath, "cycles", "cycle", "new", "Ship the beta", "--release", "0.3.0", "--json"); err != nil {
		t.Fatalf("cycle new: %v", err)
	}

	out, _, err := runCycles(t, env.cfgPath, "cycles", "cycle", "sync", "--json")
	if err != nil {
		t.Fatalf("cycle sync --json: %v", err)
	}
	if !strings.Contains(out, `"milestones": []`) || !strings.Contains(out, `"skipped": []`) {
		t.Errorf("cycle sync --json must render non-nil empty slices; got:\n%s", out)
	}
	rep := decodeJSON[cycles.CycleSyncReport](t, out)
	if rep.Cycle == nil || rep.Cycle.ID != "C-0001" {
		t.Fatalf("report cycle = %+v, want C-0001", rep.Cycle)
	}
	if len(rep.Milestones) != 0 || len(rep.Skipped) != 0 {
		t.Fatalf("report = %+v, want empty milestones/skipped", rep)
	}

	out, _, err = runCycles(t, env.cfgPath, "cycles", "cycle", "sync")
	if err != nil {
		t.Fatalf("cycle sync: %v", err)
	}
	if !strings.Contains(out, "Synced cycle C-0001") || !strings.Contains(out, "(no bets to sync)") {
		t.Errorf("cycle sync human output = %q", out)
	}
}
