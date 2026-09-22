package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RTwoStudio/orbit/internal/fsutil"
)

// journeyInstallFresh: install in a bare dir creates cache, deploys
// opencode assets, bootstraps the project, copies NEOCORTEX.md.
func TestJourneyInstallFresh(t *testing.T) {
	h := newHarness(t)
	h.runOK(t, "neocortex", "install")

	// Cache populated.
	if !fsutil.Exists(filepath.Join(h.cacheDir(), "manifest.json")) {
		t.Error("cache manifest missing")
	}
	if !fsutil.Exists(filepath.Join(h.cacheDir(), "stubs", "task.stub.md")) {
		t.Error("cache stubs missing")
	}
	// Agents/commands deployed straight to opencode dir (not cached).
	if !fsutil.Exists(filepath.Join(h.opencodeDir(), "agents", "neocortex-planner.md")) {
		t.Error("agent not deployed")
	}
	if !fsutil.Exists(filepath.Join(h.opencodeDir(), "commands", "close.md")) {
		t.Error("command not deployed")
	}
	// Project bootstrapped.
	if !fsutil.IsDir(filepath.Join(h.project, ".neocortex", "issues")) {
		t.Error(".neocortex tree missing")
	}
	if !fsutil.Exists(filepath.Join(h.project, ".neocortex", "CONVENTIONS.md")) {
		t.Error("CONVENTIONS.md missing")
	}
	if _, err := os.Stat(filepath.Join(h.project, ".neocortex", "ACTIVE")); err != nil {
		t.Error("ACTIVE missing")
	}
	// .gitignore appended.
	gi, _ := os.ReadFile(filepath.Join(h.project, ".gitignore"))
	if !strings.Contains(string(gi), ".neocortex/") {
		t.Error(".gitignore entry missing")
	}
	// NEOCORTEX.md copied.
	if !fsutil.Exists(filepath.Join(h.project, "NEOCORTEX.md")) {
		t.Error("NEOCORTEX.md not copied")
	}
}

func TestGateInstallInInitializedProject(t *testing.T) {
	h := newHarness(t)
	h.runOK(t, "neocortex", "install")
	before := h.repo.Fetches
	err := h.runCode(t, 11, "neocortex", "install")
	if !strings.Contains(err.Message, "already initialized") ||
		!strings.Contains(err.Message, "orbit neocortex update") {
		t.Errorf("refusal message wrong: %q", err.Message)
	}
	if h.repo.Fetches != before {
		t.Errorf("refused install made network calls (%d → %d)", before, h.repo.Fetches)
	}
}

func TestGateUpdateInBareDir(t *testing.T) {
	h := newHarness(t)
	before := h.repo.Fetches
	err := h.runCode(t, 11, "neocortex", "update")
	if !strings.Contains(err.Message, "no .neocortex/") ||
		!strings.Contains(err.Message, "orbit neocortex install first") {
		t.Errorf("refusal message wrong: %q", err.Message)
	}
	if h.repo.Fetches != before {
		t.Error("refused update made network calls")
	}
}

func TestInstallGlobalOnlySkipsGateAndProject(t *testing.T) {
	h := newHarness(t) // bare dir
	h.runOK(t, "neocortex", "install", "--global-only")
	if fsutil.Exists(filepath.Join(h.project, ".neocortex")) {
		t.Error("--global-only must not touch the project")
	}
	if !fsutil.Exists(filepath.Join(h.opencodeDir(), "agents", "neocortex.md")) {
		t.Error("--global-only must still deploy opencode assets")
	}
}

func TestInstallRefusesDifferingOpencodeAsset(t *testing.T) {
	h := newHarness(t)
	os.MkdirAll(filepath.Join(h.opencodeDir(), "agents"), 0o755)
	os.WriteFile(filepath.Join(h.opencodeDir(), "agents", "neocortex.md"),
		[]byte("user-customized\n"), 0o644)
	h.runCode(t, 7, "neocortex", "install")
	// The user's file must survive untouched.
	data, _ := os.ReadFile(filepath.Join(h.opencodeDir(), "agents", "neocortex.md"))
	if string(data) != "user-customized\n" {
		t.Error("pre-existing asset was overwritten")
	}
}

func TestUpdateVersionGateDeclineAndAccept(t *testing.T) {
	h := newHarness(t)
	h.runOK(t, "neocortex", "install")

	// Bump registry version with changed content.
	h.repo.setVersion("0.2.0")
	os.WriteFile(filepath.Join(h.repo.Root, "neocortex", "opencode", "agents", "neocortex-planner.md"),
		[]byte("# planner v0.2.0\n"), 0o644)
	runManifest(h.repo.Root, "0.2.0")

	// Non-TTY without --yes → all declined (treated as NO), exit 0.
	h.runOK(t, "neocortex", "update")
	data, _ := os.ReadFile(filepath.Join(h.opencodeDir(), "agents", "neocortex-planner.md"))
	if strings.Contains(string(data), "v0.2.0") {
		t.Error("declined update was applied")
	}
	// Summary still prints the final line.
	if !strings.Contains(h.stdout.String(), "Deployed version:") {
		t.Errorf("final line missing: %q", h.stdout.String())
	}

	// --yes accepts all.
	h.runOK(t, "neocortex", "update", "--yes")
	data, _ = os.ReadFile(filepath.Join(h.opencodeDir(), "agents", "neocortex-planner.md"))
	if !strings.Contains(string(data), "v0.2.0") {
		t.Error("--yes update not applied")
	}
	// deployed.json records 0.2.0.
	dj, _ := os.ReadFile(filepath.Join(h.home, ".config", "orbit", "neocortex", "deployed.json"))
	if !strings.Contains(string(dj), "0.2.0") {
		t.Errorf("deployed.json not updated: %s", dj)
	}
}

func TestUpdateNeverTouchesProject(t *testing.T) {
	h := newHarness(t)
	h.runOK(t, "neocortex", "install")
	activeBefore, _ := os.ReadFile(filepath.Join(h.project, ".neocortex", "ACTIVE"))
	h.repo.setVersion("0.3.0")
	runManifest(h.repo.Root, "0.3.0")
	h.runOK(t, "neocortex", "update", "--yes")
	activeAfter, _ := os.ReadFile(filepath.Join(h.project, ".neocortex", "ACTIVE"))
	if string(activeBefore) != string(activeAfter) {
		t.Error("update modified project state")
	}
}

// fullJourney walks the entire DoD script (§15).
func TestFullJourneyEndToEnd(t *testing.T) {
	h := newHarness(t)
	h.runOK(t, "neocortex", "install")

	// issue new --from-file
	spec := filepath.Join(h.project, "spec.md")
	os.WriteFile(spec, []byte("# Spec\n\nStreaming API details.\n"), 0o644)
	h.runOK(t, "neocortex", "issue", "new", "--title", "Add streaming API", "--from-file", spec)
	conceptPath := filepath.Join(h.project, ".neocortex", "issues", "issue-1", "00-concept.md")
	if !fsutil.Exists(conceptPath) {
		t.Fatal("concept not scaffolded")
	}
	if !fsutil.Exists(filepath.Join(h.project, ".neocortex", "issues", "issue-1", "01-plan.md")) {
		t.Fatal("plan not scaffolded")
	}
	// Detail injected verbatim + ACTIVE set.
	if data, _ := os.ReadFile(conceptPath); !strings.Contains(string(data), "Streaming API details.") {
		t.Error("Detail not injected from file")
	}
	active, _ := os.ReadFile(filepath.Join(h.project, ".neocortex", "ACTIVE"))
	if strings.TrimSpace(string(active)) != "1" {
		t.Errorf("ACTIVE = %q, want 1", active)
	}
	// which
	h.runOK(t, "neocortex", "which", "--json")
	var w struct {
		Issue int    `json:"issue"`
		Path  string `json:"path"`
	}
	json.Unmarshal(h.stdout.Bytes(), &w)
	if w.Issue != 1 || !strings.HasSuffix(w.Path, "issue-1") {
		t.Errorf("which json = %+v", w)
	}

	// issue lock: preflights refuse placeholders first.
	h.runCode(t, 6, "neocortex", "issue", "lock")
	h.fillConcept(t)
	h.runOK(t, "neocortex", "issue", "lock")
	// one-way
	h.runCode(t, 7, "neocortex", "issue", "lock")

	// plan lock: verify-first tamper check (edit concept → exit 8).
	h.fillPlan(t,
		"- [T1] Seed database schema (Blocked by: None)",
		"- [T2] Implement stream endpoint (Blocked by: T1) — uses T1 schema",
	)
	// Tamper with the locked concept body.
	appendTo(conceptPath, "tamper line\n")
	h.runCode(t, 8, "neocortex", "plan", "lock")
	// Restore (re-lock from scratch is impossible one-way, so re-scaffold
	// the journey from a clean harness for the remaining steps).
	h2 := newHarness(t)
	h2.runOK(t, "neocortex", "install")
	spec2 := filepath.Join(h2.project, "spec.md")
	os.WriteFile(spec2, []byte("Streaming API details.\n"), 0o644)
	h2.runOK(t, "neocortex", "issue", "new", "--title", "Add streaming API", "--from-file", spec2)
	h2.fillConcept(t)
	h2.runOK(t, "neocortex", "issue", "lock")

	// Bad DAG refused with line quote.
	h2.fillPlan(t, "- [T2] Endpoint (Blocked by: T1)")
	err := h2.runCode(t, 6, "neocortex", "plan", "lock")
	if !strings.Contains(err.Message, "T1") {
		t.Errorf("DAG error should quote offending line: %q", err.Message)
	}
	h2.fillPlan(t,
		"- [T1] Seed database schema (Blocked by: None)",
		"- [T2] Implement stream endpoint (Blocked by: T1) — uses T1 schema",
		"- [T3] Wire client (Blocked by: T2)",
	)
	h2.runOK(t, "neocortex", "plan", "lock")

	// Open question still present → refuse (fresh issue to test cleanly).
	// (Covered implicitly by fillPlan removing the checkbox.)

	// addenda new / approve / apply
	h2.runOK(t, "neocortex", "addenda", "new", "Add cache layer")
	h2.fillAddenda(t, 1, "- ADD [T4] Add cache layer (Blocked by: T1) — new subsystem")
	// approve refuses while agent placeholders remain? fillAddenda replaced them.
	h2.runOK(t, "neocortex", "addenda", "approve", "1")
	// one-way
	h2.runCode(t, 7, "neocortex", "addenda", "approve", "1")
	h2.runOK(t, "neocortex", "addenda", "apply", "1")

	// Plan DAG now contains T4; Amendments line appended; hash re-chained.
	planPath := filepath.Join(h2.project, ".neocortex", "issues", "issue-1", "01-plan.md")
	planData, _ := os.ReadFile(planPath)
	planStr := string(planData)
	if !strings.Contains(planStr, "- [T4] Add cache layer (Blocked by: T1)") {
		t.Errorf("plan DAG not amended:\n%s", planStr)
	}
	if !strings.Contains(planStr, `addenda-01: "Add cache layer"`) ||
		!strings.Contains(planStr, "ADD T4") {
		t.Errorf("Amendments line missing:\n%s", planStr)
	}
	if !strings.Contains(planStr, "Status: Locked") {
		t.Error("plan must stay Locked after apply")
	}

	// task new ×3 (T1, T2, T4), with dep warnings.
	h2.runOK(t, "neocortex", "task", "new", "T2", "Implement stream endpoint")
	if !strings.Contains(h2.stderr.String(), "NO task file yet") {
		t.Errorf("dep warning (missing file) expected, stderr=%q", h2.stderr.String())
	}
	h2.runOK(t, "neocortex", "task", "new", "T1", "Seed database schema")
	// duplicate → state_conflict with placeholder count
	err2 := h2.runCode(t, 7, "neocortex", "task", "new", "T1", "Seed database schema")
	if !strings.Contains(err2.Message, "placeholders") {
		t.Errorf("duplicate refusal should report placeholders: %q", err2.Message)
	}
	h2.runOK(t, "neocortex", "task", "new", "T4", "Add cache layer")
	if !strings.Contains(h2.stderr.String(), "not Close") || !strings.Contains(h2.stderr.String(), "WARNING") {
		t.Errorf("dep warning (not Close) expected, stderr=%q", h2.stderr.String())
	}
	// T5 not in DAG → preflight_failed with addenda hint
	err3 := h2.runCode(t, 6, "neocortex", "task", "new", "T5", "Ghost task")
	if !strings.Contains(err3.Message, "addenda") {
		t.Errorf("hint should mention addenda: %q", err3.Message)
	} // origin carried from addenda
	t4, _ := os.ReadFile(filepath.Join(h2.project, ".neocortex", "issues", "issue-1", "tasks", "T4.md"))
	if !strings.Contains(string(t4), "origin: addenda-01") {
		t.Errorf("T4 origin wrong:\n%s", t4)
	}

	// task status full lifecycle: T1 Open → In Progress → Revise → Close.
	h2.runCode(t, 7, "neocortex", "task", "status", "T1", "--set=Close") // illegal jump
	h2.runOK(t, "neocortex", "task", "status", "T1", "--set=In Progress")
	// Revise refused while Completion Notes empty.
	h2.runCode(t, 6, "neocortex", "task", "status", "T1", "--set=Revise")
	h2.fillTaskForRevise(t, "T1")
	h2.runOK(t, "neocortex", "task", "status", "T1", "--set=revise")
	h2.runOK(t, "neocortex", "task", "status", "T1", "--set=Rework")
	h2.runOK(t, "neocortex", "task", "status", "T1", "--set=In Progress")
	h2.runOK(t, "neocortex", "task", "status", "T1", "--set=Revise")
	h2.runOK(t, "neocortex", "task", "status", "T1", "--set=Close")
	h2.runCode(t, 7, "neocortex", "task", "status", "T1", "--set=In Progress") // terminal
	// CLI log lines present.
	t1Data, _ := os.ReadFile(filepath.Join(h2.project, ".neocortex", "issues", "issue-1", "tasks", "T1.md"))
	if !strings.Contains(string(t1Data), "CLI log") || !strings.Contains(string(t1Data), "→") {
		t.Error("CLI log lines missing")
	}

	// task next ordering: T2 runnable after T1 closes; T3/T4 blocked.
	h2.runOK(t, "neocortex", "task", "next", "--json")
	var next struct {
		Task *struct {
			ID string `json:"id"`
		} `json:"task"`
	}
	json.Unmarshal(h2.stdout.Bytes(), &next)
	if next.Task == nil || next.Task.ID != "T2" {
		t.Errorf("expected T2 next, got %+v (stdout=%s)", next.Task, h2.stdout.String())
	}
	h2.runOK(t, "neocortex", "task", "status", "T2", "--set=In Progress")
	// T3 is blocked (T2 not Close); T4 runnable (T1 Close) — DAG order first match.
	h2.runOK(t, "neocortex", "task", "next", "--json")
	var next2 struct {
		Task *struct {
			ID string `json:"id"`
		} `json:"task"`
	}
	json.Unmarshal(h2.stdout.Bytes(), &next2)
	if next2.Task == nil || next2.Task.ID != "T4" {
		t.Errorf("expected T4 next, got %+v (stdout=%s)", next2.Task, h2.stdout.String())
	}
	// After T4 closes, nothing runnable (T3 still blocked by T2).
	h2.fillTaskForRevise(t, "T4")
	h2.runOK(t, "neocortex", "task", "status", "T4", "--set=In Progress")
	h2.runOK(t, "neocortex", "task", "status", "T4", "--set=Revise")
	h2.runOK(t, "neocortex", "task", "status", "T4", "--set=Close")
	h2.runOK(t, "neocortex", "task", "next")
	if !strings.Contains(h2.stdout.String(), "No runnable task") {
		t.Errorf("friendly no-runnable expected: %q", h2.stdout.String())
	}

	// status overview
	h2.runOK(t, "neocortex", "status", "--json")
	if !strings.Contains(h2.stdout.String(), `"issue":1`) {
		t.Errorf("status json broken: %q", h2.stdout.String())
	}

	// issue list
	h2.runOK(t, "neocortex", "issue", "list", "--json")
	if !strings.Contains(h2.stdout.String(), "Add streaming API") {
		t.Errorf("issue list json missing title: %q", h2.stdout.String())
	}
}

func TestWhichNoActiveRun(t *testing.T) {
	h := newHarness(t)
	h.runOK(t, "neocortex", "install")
	err := h.runCode(t, 9, "neocortex", "which")
	if !strings.Contains(err.Message, "no active issue") {
		t.Errorf("message wrong: %q", err.Message)
	}
}

func TestLogStartEndPairsAndNoDebug(t *testing.T) {
	h := newHarness(t)
	h.runOK(t, "neocortex", "install")
	h.runOK(t, "neocortex", "issue", "list")
	logData, err := os.ReadFile(filepath.Join(h.home, "log", "orbit.log"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(logData)
	if strings.Count(s, "| INFO  | start cmd=") < 2 {
		t.Errorf("start lines missing:\n%s", s)
	}
	if strings.Count(s, "exit=0") < 2 {
		t.Errorf("end lines missing:\n%s", s)
	}
	if strings.Contains(s, "| DEBUG") {
		t.Error("DEBUG leaked without ORBIT_DEBUG=1")
	}
	if strings.Contains(s, "duration=") == false {
		t.Error("duration missing on end lines")
	}
}

// appendTo appends data to a file (tamper helper).
func appendTo(path, s string) {
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	defer f.Close()
	f.WriteString(s)
}

// fillTaskForRevise completes Completion Notes + ticks Verification.
func (h *harness) fillTaskForRevise(t *testing.T, id string) {
	t.Helper()
	path := filepath.Join(h.project, ".neocortex", "issues", "issue-1", "tasks", id+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	s = replaceBetween(s, "- **Verification:**", "- **Completion Notes:**",
		"\n  - [x] Schema migration runs\n\n")
	s = replaceBetween(s, "- **Completion Notes:**", "## Orchestrator Feedback (Rework)",
		"\n    Implemented the schema seed. Files: db/migrations/001.sql\n\n")
	os.WriteFile(path, []byte(s), 0o644)
}

func TestConfigErrorHint(t *testing.T) {
	h := newHarness(t)
	// Explicitly blank registry.url overrides the embedded default
	// (https://github.com/RTwoStudio/orbit-registry.git).
	os.WriteFile(filepath.Join(h.home, ".config", "orbit", "config.yml"),
		[]byte("registry:\n  url: \"\"\nui:\n  color: never\n"), 0o644)
	err := h.runCode(t, 3, "neocortex", "install")
	if !strings.Contains(err.Message, "registry.url") {
		t.Errorf("config error message wrong: %q", err.Message)
	}
}

func fmt_sprintf(a, b string) string { return fmt.Sprintf("%s%s", a, b) }

func TestUnknownVerbCloseIsUsageError(t *testing.T) {
	h := newHarness(t)
	h.runOK(t, "neocortex", "install")
	// v0.1.0: no 'close' verb exists — must fall through to cobra's
	// unknown-command error with the usage exit code.
	err := h.runCode(t, 2, "neocortex", "close")
	if !strings.Contains(err.Message, "unknown command") {
		t.Errorf("expected unknown-command error, got: %q", err.Message)
	}
	// Bad args are usage-class too.
	h.runCode(t, 2, "neocortex", "task", "status", "T1", "--set=Close", "extra")
	h.runCode(t, 2, "neocortex", "task", "status", "T1", "--bogus=1")
}
