package cycles

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/RTwoStudio/orbit/internal/doc"
	"github.com/RTwoStudio/orbit/internal/exit"
	"github.com/RTwoStudio/orbit/internal/forge"
)

// fakeClient is a forge.Client whose operations are recorded instead of
// performed. One instance stands in for one provider.
type fakeClient struct {
	provider    string
	availErr    error
	createErr   error
	failOnCall  int // 1-based CreateMilestone call to fail (0 = never)
	availCalls  int
	createCalls int
	created     []fakeMilestone
	nextNumber  int
}

type fakeMilestone struct{ title, due string }

var _ forge.Client = (*fakeClient)(nil)

func (f *fakeClient) Available(context.Context) error {
	f.availCalls++
	return f.availErr
}

func (f *fakeClient) CreateIssue(context.Context, forge.IssueSpec) (forge.Issue, error) {
	return forge.Issue{}, nil
}

func (f *fakeClient) UpdateIssue(context.Context, int, forge.IssueSpec) (forge.Issue, error) {
	return forge.Issue{}, nil
}

func (f *fakeClient) CreateMilestone(_ context.Context, title, due string) (forge.Milestone, error) {
	f.createCalls++
	f.created = append(f.created, fakeMilestone{title: title, due: due})
	if f.failOnCall > 0 && f.createCalls == f.failOnCall {
		return forge.Milestone{}, f.createErr
	}
	f.nextNumber++
	return forge.Milestone{
		Number: f.nextNumber,
		Title:  title,
		URL:    fmt.Sprintf("https://example.test/%d", f.nextNumber),
	}, nil
}

func (f *fakeClient) AssignIssue(context.Context, int, int) error { return nil }

// forgeHarness installs fake resolveRemote/newForgeClient seams for a test.
type forgeHarness struct {
	resolve    map[string]forge.Remote // project dir → remote
	resolveErr map[string]error        // project dir → resolution error
	clients    map[string]*fakeClient  // provider → client
	built      []forge.Remote          // remotes newForgeClient was called with
}

func installForge(t *testing.T, h *forgeHarness) {
	t.Helper()
	if h.clients == nil {
		h.clients = map[string]*fakeClient{}
	}
	prevResolve, prevNew := resolveRemote, newForgeClient
	t.Cleanup(func() { resolveRemote, newForgeClient = prevResolve, prevNew })

	resolveRemote = func(dir string) (forge.Remote, error) {
		if err, ok := h.resolveErr[dir]; ok {
			return forge.Remote{}, err
		}
		r, ok := h.resolve[dir]
		if !ok {
			return forge.Remote{}, exit.New(exit.NotFound, "no git origin remote in "+dir)
		}
		return r, nil
	}
	newForgeClient = func(r forge.Remote, _ ...forge.Option) forge.Client {
		h.built = append(h.built, r)
		c, ok := h.clients[r.Provider]
		if !ok {
			c = &fakeClient{provider: r.Provider}
			h.clients[r.Provider] = c
		}
		return c
	}
}

// client returns the fake for a provider, creating it on first use.
func (h *forgeHarness) client(p string) *fakeClient {
	if h.clients == nil {
		h.clients = map[string]*fakeClient{}
	}
	c, ok := h.clients[p]
	if !ok {
		c = &fakeClient{provider: p}
		h.clients[p] = c
	}
	return c
}

func ghRemote(owner, repo string) forge.Remote {
	return forge.Remote{Provider: forge.ProviderGitHub, Host: "github.com", Owner: owner, Repo: repo}
}

func glRemote(owner, repo string) forge.Remote {
	return forge.Remote{Provider: forge.ProviderGitLab, Host: "gitlab.com", Owner: owner, Repo: repo}
}

// betWithProject creates, shapes, sets an optional project:, and bets a work
// item into the current cycle.
func betWithProject(t *testing.T, s *Store, title, scope, project string) *WorkItem {
	t.Helper()
	it, err := s.NewWork(title, scope)
	if err != nil {
		t.Fatalf("NewWork(%s): %v", title, err)
	}
	fillShapeSections(t, it.Path)
	if _, err := s.ShapeWork(it.ID, "small"); err != nil {
		t.Fatalf("ShapeWork(%s): %v", it.ID, err)
	}
	if project != "" {
		d, err := doc.ParseDoc(it.Path)
		if err != nil {
			t.Fatal(err)
		}
		d.Set("project", project)
		if err := d.Save(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.BetWork(it.ID); err != nil {
		t.Fatalf("BetWork(%s): %v", it.ID, err)
	}
	return it
}

// TestSyncCycleCreatesMilestonePerDistinctRepo covers grouping (bets sharing a
// repo collapse), title/due, per-repo creation order, recorded numbers, and
// that the body/key order survive the write.
func TestSyncCycleCreatesMilestonePerDistinctRepo(t *testing.T) {
	s := setupVault(t)
	cy, err := s.NewCycle("Ship v1", "0.1.0", "2026-02-01", "2026-03-01")
	if err != nil {
		t.Fatal(err)
	}
	betWithProject(t, s, "Orbit A", "scope-a", "/p/orbit")
	betWithProject(t, s, "Orbit B", "scope-a", "/p/orbit") // same repo → one milestone
	betWithProject(t, s, "Site", "scope-a", "/p/site")

	h := &forgeHarness{
		resolve: map[string]forge.Remote{
			"/p/orbit": ghRemote("RTwoStudio", "orbit"),
			"/p/site":  ghRemote("RTwoStudio", "site"),
		},
	}
	installForge(t, h)

	before, err := doc.ParseDoc(cy.Path)
	if err != nil {
		t.Fatal(err)
	}
	beforeBody := string(before.Body)

	rep, err := s.SyncCycle(context.Background())
	if err != nil {
		t.Fatalf("SyncCycle: %v", err)
	}
	if len(rep.Skipped) != 0 {
		t.Errorf("skipped = %+v, want none", rep.Skipped)
	}
	if len(rep.Milestones) != 2 {
		t.Fatalf("milestones = %+v, want 2", rep.Milestones)
	}
	// Deterministic: owner/repo slug alpha order within one provider.
	wantRepos := []string{"RTwoStudio/orbit", "RTwoStudio/site"}
	for i, want := range wantRepos {
		got := rep.Milestones[i]
		if got.Repo != want || got.Provider != "github" || got.Action != SyncCreated {
			t.Errorf("milestone[%d] = %+v, want provider=github repo=%s action=created", i, got, want)
		}
		if got.Number != i+1 {
			t.Errorf("milestone[%d].Number = %d, want %d", i, got.Number, i+1)
		}
	}
	cl := h.client("github")
	if cl.createCalls != 2 {
		t.Errorf("createCalls = %d, want 2", cl.createCalls)
	}
	if cl.availCalls != 1 {
		t.Errorf("availCalls = %d, want 1 (probed once per provider)", cl.availCalls)
	}
	for i, c := range cl.created {
		if c.title != "C-0001 — Ship v1" {
			t.Errorf("created[%d].title = %q, want %q", i, c.title, "C-0001 — Ship v1")
		}
		if c.due != "2026-03-01" {
			t.Errorf("created[%d].due = %q, want the cycle end 2026-03-01", i, c.due)
		}
	}

	// Recorded in the cycle's milestone map.
	show, err := s.ShowCycle("C-0001")
	if err != nil {
		t.Fatal(err)
	}
	if show.Milestones["RTwoStudio/orbit"] != 1 || show.Milestones["RTwoStudio/site"] != 2 {
		t.Errorf("recorded milestones = %v, want orbit:1 site:2", show.Milestones)
	}

	// Multi-repo ⇒ scalars stay empty.
	if show.Project != "" || show.Repo != "" || show.Provider != "" {
		t.Errorf("multi-repo scalars = %q/%q/%q, want empty", show.Project, show.Repo, show.Provider)
	}

	after, err := doc.ParseDoc(cy.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after.Body) != beforeBody {
		t.Errorf("sync changed the cycle body:\n before=%q\n after =%q", beforeBody, string(after.Body))
	}
	note := cycleNote(t, s, "C-0001")
	if that := strings.Index(note, "release:"); that < 0 || that > strings.Index(note, "milestone:") {
		t.Errorf("frontmatter key order not preserved:\n%s", note)
	}
}

// TestSyncCycleDeterministicOrder pins the provider-then-slug ordering across
// providers regardless of bet insertion order.
func TestSyncCycleDeterministicOrder(t *testing.T) {
	s := setupVault(t)
	if _, err := s.NewCycle("Order", "0.1.0", "", ""); err != nil {
		t.Fatal(err)
	}
	betWithProject(t, s, "Z", "scope-a", "/p/z") // gitlab zzz/aaa
	betWithProject(t, s, "B", "scope-a", "/p/b") // github bbb/ccc
	betWithProject(t, s, "A", "scope-a", "/p/a") // github aaa/bbb

	h := &forgeHarness{resolve: map[string]forge.Remote{
		"/p/z": glRemote("zzz", "aaa"),
		"/p/b": ghRemote("bbb", "ccc"),
		"/p/a": ghRemote("aaa", "bbb"),
	}}
	installForge(t, h)

	rep, err := s.SyncCycle(context.Background())
	if err != nil {
		t.Fatalf("SyncCycle: %v", err)
	}
	want := []string{"aaa/bbb", "bbb/ccc", "zzz/aaa"}
	if len(rep.Milestones) != len(want) {
		t.Fatalf("milestones = %+v, want %v", rep.Milestones, want)
	}
	for i := range want {
		if rep.Milestones[i].Repo != want[i] {
			t.Errorf("order[%d] = %s, want %s", i, rep.Milestones[i].Repo, want[i])
		}
	}
	if rep.Milestones[0].Provider != "github" || rep.Milestones[2].Provider != "gitlab" {
		t.Errorf("providers = %s..%s, want github..gitlab", rep.Milestones[0].Provider, rep.Milestones[2].Provider)
	}
}

// TestSyncCycleUnchangedSkipsCreate pins record-and-skip: a repo whose number
// is already recorded is reported unchanged with no forge call at all.
func TestSyncCycleUnchangedSkipsCreate(t *testing.T) {
	s := setupVault(t)
	if _, err := s.NewCycle("Idem", "0.1.0", "", ""); err != nil {
		t.Fatal(err)
	}
	betWithProject(t, s, "Orbit", "scope-a", "/p/orbit")
	if _, err := s.SetCycleForge("C-0001", CycleForge{
		Milestones: map[string]int{"RTwoStudio/orbit": 9},
	}); err != nil {
		t.Fatal(err)
	}

	h := &forgeHarness{resolve: map[string]forge.Remote{"/p/orbit": ghRemote("RTwoStudio", "orbit")}}
	installForge(t, h)

	rep, err := s.SyncCycle(context.Background())
	if err != nil {
		t.Fatalf("SyncCycle: %v", err)
	}
	if len(rep.Milestones) != 1 || rep.Milestones[0].Action != SyncUnchanged || rep.Milestones[0].Number != 9 {
		t.Fatalf("milestones = %+v, want one unchanged #9", rep.Milestones)
	}
	if h.client("github").createCalls != 0 {
		t.Errorf("createCalls = %d, want 0", h.client("github").createCalls)
	}
	if h.client("github").availCalls != 0 {
		t.Errorf("availCalls = %d, want 0 (all-unchanged run stays offline)", h.client("github").availCalls)
	}
	if len(h.built) != 0 {
		t.Errorf("newForgeClient called %d times, want 0 for an unchanged run", len(h.built))
	}
}

// TestSyncCycleIdempotent pins that a second sync of an unchanged cycle issues
// zero CreateMilestone calls.
func TestSyncCycleIdempotent(t *testing.T) {
	s := setupVault(t)
	if _, err := s.NewCycle("Twice", "0.1.0", "", ""); err != nil {
		t.Fatal(err)
	}
	betWithProject(t, s, "Orbit", "scope-a", "/p/orbit")

	h := &forgeHarness{resolve: map[string]forge.Remote{"/p/orbit": ghRemote("RTwoStudio", "orbit")}}
	installForge(t, h)

	if _, err := s.SyncCycle(context.Background()); err != nil {
		t.Fatalf("first SyncCycle: %v", err)
	}
	if h.client("github").createCalls != 1 {
		t.Fatalf("first createCalls = %d, want 1", h.client("github").createCalls)
	}
	rep, err := s.SyncCycle(context.Background())
	if err != nil {
		t.Fatalf("second SyncCycle: %v", err)
	}
	if h.client("github").createCalls != 1 {
		t.Errorf("second run createCalls = %d, want still 1 (idempotent)", h.client("github").createCalls)
	}
	if len(rep.Milestones) != 1 || rep.Milestones[0].Action != SyncUnchanged {
		t.Errorf("second run milestones = %+v, want unchanged", rep.Milestones)
	}
}

// TestSyncCycleSkipsUnresolvableBets pins best-effort skipping: a bet with no
// project: and one whose project cannot be resolved are reported, not fatal,
// while a good repo still syncs.
func TestSyncCycleSkipsUnresolvableBets(t *testing.T) {
	s := setupVault(t)
	if _, err := s.NewCycle("Skips", "0.1.0", "", ""); err != nil {
		t.Fatal(err)
	}
	betWithProject(t, s, "No project", "scope-a", "")
	betWithProject(t, s, "Bad project", "scope-a", "/p/bad")
	betWithProject(t, s, "Good", "scope-a", "/p/orbit")

	h := &forgeHarness{
		resolve:    map[string]forge.Remote{"/p/orbit": ghRemote("RTwoStudio", "orbit")},
		resolveErr: map[string]error{"/p/bad": exit.New(exit.Usage, "unsupported git host \"example.com\"")},
	}
	installForge(t, h)

	rep, err := s.SyncCycle(context.Background())
	if err != nil {
		t.Fatalf("SyncCycle: %v", err)
	}
	if len(rep.Skipped) != 2 {
		t.Fatalf("skipped = %+v, want 2", rep.Skipped)
	}
	if rep.Skipped[0].ID != "W-0001" || rep.Skipped[0].Reason != "no project: set" {
		t.Errorf("skip[0] = %+v, want W-0001 no project: set", rep.Skipped[0])
	}
	if rep.Skipped[1].ID != "W-0002" || !strings.Contains(rep.Skipped[1].Reason, "unsupported git host") {
		t.Errorf("skip[1] = %+v, want W-0002 resolve error", rep.Skipped[1])
	}
	if len(rep.Milestones) != 1 || rep.Milestones[0].Repo != "RTwoStudio/orbit" {
		t.Errorf("milestones = %+v, want the one resolvable repo", rep.Milestones)
	}
}

// TestSyncCycleMissingCLI pins that a missing/unauthenticated CLI is a coded
// error with a hint, surfaced only when a create is actually needed.
func TestSyncCycleMissingCLI(t *testing.T) {
	s := setupVault(t)
	if _, err := s.NewCycle("NoCLI", "0.1.0", "", ""); err != nil {
		t.Fatal(err)
	}
	betWithProject(t, s, "Orbit", "scope-a", "/p/orbit")

	h := &forgeHarness{resolve: map[string]forge.Remote{"/p/orbit": ghRemote("RTwoStudio", "orbit")}}
	h.client("github").availErr = exit.New(exit.General, "GitHub CLI (gh) not found on PATH", "install it")
	installForge(t, h)

	_, err := s.SyncCycle(context.Background())
	if codeOf(t, err) != exit.General {
		t.Fatalf("code = %v, want general", codeOf(t, err))
	}
	var e *exit.Error
	if !errors.As(err, &e) || !strings.Contains(strings.Join(e.Hints, " "), "install it") {
		t.Errorf("missing install hint: %v", err)
	}
	if h.client("github").createCalls != 0 {
		t.Errorf("createCalls = %d, want 0 (never reached)", h.client("github").createCalls)
	}
}

// TestSyncCycleCollision pins the {provider, owner/repo} collision guard: the
// first repo is created, the second is skipped, and only one map entry exists.
func TestSyncCycleCollision(t *testing.T) {
	s := setupVault(t)
	if _, err := s.NewCycle("Collide", "0.1.0", "", ""); err != nil {
		t.Fatal(err)
	}
	betWithProject(t, s, "GH", "scope-a", "/p/gh")
	betWithProject(t, s, "GL", "scope-a", "/p/gl")

	h := &forgeHarness{resolve: map[string]forge.Remote{
		"/p/gh": ghRemote("RTwoStudio", "orbit"),
		"/p/gl": glRemote("RTwoStudio", "orbit"), // same slug, different provider
	}}
	installForge(t, h)

	rep, err := s.SyncCycle(context.Background())
	if err != nil {
		t.Fatalf("SyncCycle: %v", err)
	}
	if len(rep.Milestones) != 1 || rep.Milestones[0].Action != SyncCreated || rep.Milestones[0].Provider != "github" {
		t.Fatalf("milestones = %+v, want the github repo created first", rep.Milestones)
	}
	if len(rep.Skipped) != 1 || rep.Skipped[0].ID != "W-0002" || rep.Skipped[0].Reason != "milestone map key collision" {
		t.Fatalf("skipped = %+v, want W-0002 collision", rep.Skipped)
	}
	show, err := s.ShowCycle("C-0001")
	if err != nil {
		t.Fatal(err)
	}
	if len(show.Milestones) != 1 || show.Milestones["RTwoStudio/orbit"] != 1 {
		t.Errorf("recorded = %v, want exactly one orbit:1", show.Milestones)
	}
	if h.client("github").createCalls != 1 || h.client("gitlab").createCalls != 0 {
		t.Errorf("create calls gh=%d gl=%d, want 1/0", h.client("github").createCalls, h.client("gitlab").createCalls)
	}
}

// TestSyncCycleZeroReposNoWrite pins that a cycle with no resolvable repo
// performs no frontmatter write and returns an empty milestone report.
func TestSyncCycleZeroReposNoWrite(t *testing.T) {
	s := setupVault(t)
	if _, err := s.NewCycle("Nothing", "0.1.0", "", ""); err != nil {
		t.Fatal(err)
	}
	betWithProject(t, s, "No project", "scope-a", "")

	h := &forgeHarness{}
	installForge(t, h)

	before := cycleNote(t, s, "C-0001")
	rep, err := s.SyncCycle(context.Background())
	if err != nil {
		t.Fatalf("SyncCycle: %v", err)
	}
	if len(rep.Milestones) != 0 {
		t.Errorf("milestones = %+v, want none", rep.Milestones)
	}
	if len(rep.Skipped) != 1 {
		t.Errorf("skipped = %+v, want the single no-project bet", rep.Skipped)
	}
	if after := cycleNote(t, s, "C-0001"); after != before {
		t.Errorf("zero-repo sync wrote the note:\n before=%q\n after =%q", before, after)
	}
	if len(h.built) != 0 {
		t.Errorf("newForgeClient called %d times, want 0", len(h.built))
	}
}

// TestSyncCycleScalarsSingleRepo pins that a single-repo cycle records the
// project/repo/provider scalars.
func TestSyncCycleScalarsSingleRepo(t *testing.T) {
	s := setupVault(t)
	if _, err := s.NewCycle("Single", "0.1.0", "", ""); err != nil {
		t.Fatal(err)
	}
	betWithProject(t, s, "Orbit", "scope-a", "/p/orbit")

	h := &forgeHarness{resolve: map[string]forge.Remote{"/p/orbit": ghRemote("RTwoStudio", "orbit")}}
	installForge(t, h)

	rep, err := s.SyncCycle(context.Background())
	if err != nil {
		t.Fatalf("SyncCycle: %v", err)
	}
	if rep.Cycle.Project != "/p/orbit" || rep.Cycle.Repo != "RTwoStudio/orbit" || rep.Cycle.Provider != "github" {
		t.Errorf("scalars = %q/%q/%q, want /p/orbit/RTwoStudio/orbit/github",
			rep.Cycle.Project, rep.Cycle.Repo, rep.Cycle.Provider)
	}
	note := cycleNote(t, s, "C-0001")
	for _, want := range []string{"project: /p/orbit", "repo: RTwoStudio/orbit", "provider: github"} {
		if !strings.Contains(note, want) {
			t.Errorf("cycle note missing %q:\n%s", want, note)
		}
	}
}

// TestSyncCyclePartialFailurePersists pins that a mid-run CreateMilestone
// failure persists the milestones created so far and returns the coded error.
func TestSyncCyclePartialFailurePersists(t *testing.T) {
	s := setupVault(t)
	if _, err := s.NewCycle("Partial", "0.1.0", "", ""); err != nil {
		t.Fatal(err)
	}
	betWithProject(t, s, "A", "scope-a", "/p/a")
	betWithProject(t, s, "B", "scope-a", "/p/b")

	h := &forgeHarness{resolve: map[string]forge.Remote{
		"/p/a": ghRemote("RTwoStudio", "a"),
		"/p/b": ghRemote("RTwoStudio", "b"),
	}}
	cl := h.client("github")
	cl.failOnCall = 2
	cl.createErr = exit.New(exit.General, "gh api milestones failed")
	installForge(t, h)

	_, err := s.SyncCycle(context.Background())
	if codeOf(t, err) != exit.General {
		t.Fatalf("code = %v, want general", codeOf(t, err))
	}
	show, err := s.ShowCycle("C-0001")
	if err != nil {
		t.Fatal(err)
	}
	// Sorted order is a/b then b/b: the first (RTwoStudio/a) was persisted.
	if show.Milestones["RTwoStudio/a"] != 1 {
		t.Errorf("recorded = %v, want RTwoStudio/a:1 persisted after the failure", show.Milestones)
	}
	if _, ok := show.Milestones["RTwoStudio/b"]; ok {
		t.Errorf("recorded = %v, must not contain the failed repo", show.Milestones)
	}
	if cl.createCalls != 2 {
		t.Errorf("createCalls = %d, want 2", cl.createCalls)
	}
}

// TestSyncCycleNoOpenCycle pins the state_conflict for a missing CURRENT.
func TestSyncCycleNoOpenCycle(t *testing.T) {
	s := setupVault(t)
	h := &forgeHarness{}
	installForge(t, h)
	if _, err := s.SyncCycle(context.Background()); codeOf(t, err) != exit.StateConflict {
		t.Errorf("code = %v, want state_conflict", codeOf(t, err))
	}
}

// TestSyncCycleDropsStaleEntries pins step 9's recomputation: a recorded repo
// no longer among the bets is dropped by the final reconciliation.
func TestSyncCycleDropsStaleEntries(t *testing.T) {
	s := setupVault(t)
	if _, err := s.NewCycle("Stale", "0.1.0", "", ""); err != nil {
		t.Fatal(err)
	}
	betWithProject(t, s, "Orbit", "scope-a", "/p/orbit")
	if _, err := s.SetCycleForge("C-0001", CycleForge{
		Milestones: map[string]int{"old/repo": 2, "RTwoStudio/orbit": 5},
	}); err != nil {
		t.Fatal(err)
	}

	h := &forgeHarness{resolve: map[string]forge.Remote{"/p/orbit": ghRemote("RTwoStudio", "orbit")}}
	installForge(t, h)

	if _, err := s.SyncCycle(context.Background()); err != nil {
		t.Fatalf("SyncCycle: %v", err)
	}
	show, err := s.ShowCycle("C-0001")
	if err != nil {
		t.Fatal(err)
	}
	if len(show.Milestones) != 1 || show.Milestones["RTwoStudio/orbit"] != 5 {
		t.Errorf("recorded = %v, want just orbit:5 (stale dropped)", show.Milestones)
	}
	if strings.Contains(cycleNote(t, s, "C-0001"), "old/repo") {
		t.Errorf("stale entry survived the sync")
	}
}

// TestCycleSyncReportJSONShape pins the T6 --json contract: non-nil slices and
// the documented tags.
func TestCycleSyncReportJSONShape(t *testing.T) {
	s := setupVault(t)
	if _, err := s.NewCycle("JSON", "0.1.0", "", ""); err != nil {
		t.Fatal(err)
	}
	betWithProject(t, s, "Orbit", "scope-a", "/p/orbit")
	h := &forgeHarness{resolve: map[string]forge.Remote{"/p/orbit": ghRemote("RTwoStudio", "orbit")}}
	installForge(t, h)

	rep, err := s.SyncCycle(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		`"cycle"`, `"milestones"`, `"skipped"`,
		`"provider":"github"`, `"repo":"RTwoStudio/orbit"`, `"number":1`,
		`"url":"https://example.test/1"`, `"action":"created"`,
	} {
		if !strings.Contains(string(data), key) {
			t.Errorf("marshalled report missing %s: %s", key, data)
		}
	}
}
