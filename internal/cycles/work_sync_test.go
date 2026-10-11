package cycles

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/RTwoStudio/orbit/internal/doc"
	"github.com/RTwoStudio/orbit/internal/exit"
	"github.com/RTwoStudio/orbit/internal/forge"
)

// wfClient is a self-contained forge.Client recording every call for the
// work-sync tests. It is deliberately separate from sync_test.go's fakeClient
// so those milestone tests stay untouched.
type wfClient struct {
	provider string

	availErr    error
	createErr   error
	updateErr   error
	assignErr   error
	issueAsgErr error

	availCalls    int
	createCalls   int
	updateCalls   int
	assignCalls   int
	issueAsgCalls int

	nextIssue int

	createSpec    forge.IssueSpec
	updateNum     int
	updateSpec    forge.IssueSpec
	assignIssue   int
	assignMS      int
	issueAsgIssue int
	issueAsgUsers []string
}

var _ forge.Client = (*wfClient)(nil)

func (c *wfClient) Available(context.Context) error {
	c.availCalls++
	return c.availErr
}

func (c *wfClient) CreateIssue(_ context.Context, spec forge.IssueSpec) (forge.Issue, error) {
	c.createCalls++
	c.createSpec = spec
	if c.createErr != nil {
		return forge.Issue{}, c.createErr
	}
	c.nextIssue++
	return forge.Issue{
		Number: c.nextIssue,
		URL:    fmt.Sprintf("https://example.test/issues/%d", c.nextIssue),
	}, nil
}

func (c *wfClient) UpdateIssue(_ context.Context, number int, spec forge.IssueSpec) (forge.Issue, error) {
	c.updateCalls++
	c.updateNum = number
	c.updateSpec = spec
	if c.updateErr != nil {
		return forge.Issue{}, c.updateErr
	}
	return forge.Issue{Number: number, URL: fmt.Sprintf("https://example.test/issues/%d", number)}, nil
}

func (c *wfClient) CreateMilestone(context.Context, string, string) (forge.Milestone, error) {
	return forge.Milestone{}, nil
}

func (c *wfClient) AssignIssue(_ context.Context, issue, milestone int) error {
	c.assignCalls++
	c.assignIssue = issue
	c.assignMS = milestone
	return c.assignErr
}

func (c *wfClient) SetIssueAssignees(_ context.Context, issue int, usernames []string) error {
	c.issueAsgCalls++
	c.issueAsgIssue = issue
	c.issueAsgUsers = usernames
	return c.issueAsgErr
}

// wfHarness installs fresh resolveRemote/newForgeClient seams for one test.
type wfHarness struct {
	resolve    map[string]forge.Remote
	resolveErr map[string]error
	clients    map[string]*wfClient
	built      []forge.Remote
}

func installWorkForge(t *testing.T, h *wfHarness) {
	t.Helper()
	if h.clients == nil {
		h.clients = map[string]*wfClient{}
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
			c = &wfClient{provider: r.Provider}
			h.clients[r.Provider] = c
		}
		return c
	}
}

func (h *wfHarness) client(p string) *wfClient {
	if h.clients == nil {
		h.clients = map[string]*wfClient{}
	}
	c, ok := h.clients[p]
	if !ok {
		c = &wfClient{provider: p}
		h.clients[p] = c
	}
	return c
}

// seedMilestone records a milestone number for repo in the open cycle C-0001.
func seedMilestone(t *testing.T, s *Store, repo string, n int) {
	t.Helper()
	if _, err := s.SetCycleForge("C-0001", CycleForge{Milestones: map[string]int{repo: n}}); err != nil {
		t.Fatalf("SetCycleForge: %v", err)
	}
}

// issueBody is the body renderIssueBody must produce for a shaped fixture note.
const issueBody = "## Problem\nFilled Problem.\n\n## Solution Sketch\nFilled Solution Sketch."

// TestSyncWorkCreatesIssue covers the first sync of a Bet: CreateIssue with the
// em-dash title and seeded body, assigned to the recorded milestone, and every
// forge link recorded while the body stays byte-identical.
func TestSyncWorkCreatesIssue(t *testing.T) {
	s := setupVault(t)
	if _, err := s.NewCycle("Ship v1", "0.1.0", "", ""); err != nil {
		t.Fatal(err)
	}
	it := betWithProject(t, s, "Cancellation", "scope-a", "/p/orbit")
	seedMilestone(t, s, "RTwoStudio/orbit", 7)

	h := &wfHarness{resolve: map[string]forge.Remote{"/p/orbit": ghRemote("RTwoStudio", "orbit")}}
	installWorkForge(t, h)

	cur, err := s.ShowWork(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	before, err := doc.ParseDoc(cur.Path)
	if err != nil {
		t.Fatal(err)
	}
	beforeBody := string(before.Body)

	rep, err := s.SyncWork(context.Background(), it.ID, "")
	if err != nil {
		t.Fatalf("SyncWork: %v", err)
	}
	if rep.Action != SyncCreated {
		t.Errorf("action = %q, want %q", rep.Action, SyncCreated)
	}
	if rep.Provider != "github" || rep.Repo != "RTwoStudio/orbit" || rep.Issue != 1 || rep.Milestone != 7 {
		t.Errorf("report = %+v, want github/RTwoStudio/orbit/1/7", rep)
	}
	if rep.Work == nil || rep.Work.Issue != 1 || rep.Work.Milestone != 7 ||
		rep.Work.Provider != "github" || rep.Work.Repo != "RTwoStudio/orbit" {
		t.Errorf("report.Work = %+v, want the recorded links", rep.Work)
	}

	cl := h.client("github")
	if cl.availCalls != 1 {
		t.Errorf("availCalls = %d, want 1", cl.availCalls)
	}
	if cl.createCalls != 1 || cl.updateCalls != 0 {
		t.Errorf("create/update calls = %d/%d, want 1/0", cl.createCalls, cl.updateCalls)
	}
	if want := it.ID + " \u2014 Cancellation"; cl.createSpec.Title != want {
		t.Errorf("title = %q, want %q (U+2014)", cl.createSpec.Title, want)
	}
	if cl.createSpec.Body != issueBody {
		t.Errorf("body = %q, want %q", cl.createSpec.Body, issueBody)
	}
	if cl.assignCalls != 1 || cl.assignIssue != 1 || cl.assignMS != 7 {
		t.Errorf("assign = %d issue=%d milestone=%d, want 1/1/7", cl.assignCalls, cl.assignIssue, cl.assignMS)
	}

	// Recorded on disk; body untouched.
	show, err := s.ShowWork(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if show.Issue != 1 || show.Milestone != 7 {
		t.Errorf("recorded issue/milestone = %d/%d, want 1/7", show.Issue, show.Milestone)
	}
	after, err := doc.ParseDoc(show.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after.Body) != beforeBody {
		t.Errorf("sync changed the work body:\n before=%q\n after =%q", beforeBody, string(after.Body))
	}
}

// TestSyncWorkSecondRunUpdates pins idempotency: a second sync calls
// UpdateIssue (never CreateIssue) and re-assigns, so no duplicate is ever made.
func TestSyncWorkSecondRunUpdates(t *testing.T) {
	s := setupVault(t)
	if _, err := s.NewCycle("Ship v1", "0.1.0", "", ""); err != nil {
		t.Fatal(err)
	}
	it := betWithProject(t, s, "Cancellation", "scope-a", "/p/orbit")
	seedMilestone(t, s, "RTwoStudio/orbit", 7)

	h := &wfHarness{resolve: map[string]forge.Remote{"/p/orbit": ghRemote("RTwoStudio", "orbit")}}
	installWorkForge(t, h)

	if _, err := s.SyncWork(context.Background(), it.ID, ""); err != nil {
		t.Fatalf("first SyncWork: %v", err)
	}
	rep, err := s.SyncWork(context.Background(), it.ID, "")
	if err != nil {
		t.Fatalf("second SyncWork: %v", err)
	}
	if rep.Action != SyncUpdated {
		t.Errorf("action = %q, want %q", rep.Action, SyncUpdated)
	}
	cl := h.client("github")
	if cl.createCalls != 1 {
		t.Errorf("createCalls = %d, want still 1 (never a duplicate)", cl.createCalls)
	}
	if cl.updateCalls != 1 || cl.updateNum != 1 {
		t.Errorf("updateCalls = %d number=%d, want 1/1", cl.updateCalls, cl.updateNum)
	}
	if cl.assignCalls != 2 {
		t.Errorf("assignCalls = %d, want 2 (re-assign every run)", cl.assignCalls)
	}
	if rep.Issue != 1 {
		t.Errorf("issue = %d, want 1", rep.Issue)
	}
}

// TestSyncWorkMissingMilestone pins the hard failure: no recorded milestone is
// a state_conflict with the cycle-sync hint and performs no forge call.
func TestSyncWorkMissingMilestone(t *testing.T) {
	s := setupVault(t)
	if _, err := s.NewCycle("Ship v1", "0.1.0", "", ""); err != nil {
		t.Fatal(err)
	}
	it := betWithProject(t, s, "Cancellation", "scope-a", "/p/orbit")

	h := &wfHarness{resolve: map[string]forge.Remote{"/p/orbit": ghRemote("RTwoStudio", "orbit")}}
	installWorkForge(t, h)

	_, err := s.SyncWork(context.Background(), it.ID, "")
	if codeOf(t, err) != exit.StateConflict {
		t.Fatalf("code = %v, want state_conflict", codeOf(t, err))
	}
	var e *exit.Error
	if !errors.As(err, &e) || !strings.Contains(strings.Join(e.Hints, " "), "orbit cycles cycle sync") {
		t.Errorf("missing cycle sync hint: %v", err)
	}
	if len(h.built) != 0 {
		t.Errorf("newForgeClient called %d times, want 0", len(h.built))
	}
}

// TestSyncWorkNonBet pins Bet-only: every other status is a state_conflict.
func TestSyncWorkNonBet(t *testing.T) {
	ctx := context.Background()

	t.Run("Backlog", func(t *testing.T) {
		s := setupVault(t)
		it, err := s.NewWork("Backlog item", "scope-a")
		if err != nil {
			t.Fatal(err)
		}
		assertSyncWorkStateConflict(t, s, ctx, it.ID)
	})
	t.Run("Pitched", func(t *testing.T) {
		s := setupVault(t)
		it, err := s.NewWork("Pitched item", "scope-a")
		if err != nil {
			t.Fatal(err)
		}
		fillShapeSections(t, it.Path)
		if _, err := s.ShapeWork(it.ID, "small"); err != nil {
			t.Fatal(err)
		}
		assertSyncWorkStateConflict(t, s, ctx, it.ID)
	})
	t.Run("Shelved", func(t *testing.T) {
		s := setupVault(t)
		it, err := s.NewWork("Shelved item", "scope-a")
		if err != nil {
			t.Fatal(err)
		}
		fillShapeSections(t, it.Path)
		if _, err := s.ShapeWork(it.ID, "small"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ShelveWork(it.ID); err != nil {
			t.Fatal(err)
		}
		assertSyncWorkStateConflict(t, s, ctx, it.ID)
	})
	t.Run("Delivered", func(t *testing.T) {
		s := setupVault(t)
		if _, err := s.NewCycle("Ship v1", "0.1.0", "", ""); err != nil {
			t.Fatal(err)
		}
		it, err := s.NewWork("Delivered item", "scope-a")
		if err != nil {
			t.Fatal(err)
		}
		fillShapeSections(t, it.Path)
		if _, err := s.ShapeWork(it.ID, "small"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.BetWork(it.ID, []string{testOwnerHandle}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.DeliverWork(it.ID); err != nil {
			t.Fatal(err)
		}
		assertSyncWorkStateConflict(t, s, ctx, it.ID)
	})
}

func assertSyncWorkStateConflict(t *testing.T, s *Store, ctx context.Context, id string) {
	t.Helper()
	_, err := s.SyncWork(ctx, id, "")
	if codeOf(t, err) != exit.StateConflict {
		t.Fatalf("code = %v, want state_conflict", codeOf(t, err))
	}
}

// TestSyncWorkNoProject pins preflight_failed (6) plus a hint when neither the
// note nor an override supplies a project.
func TestSyncWorkNoProject(t *testing.T) {
	s := setupVault(t)
	if _, err := s.NewCycle("Ship v1", "0.1.0", "", ""); err != nil {
		t.Fatal(err)
	}
	it := betWithProject(t, s, "No project", "scope-a", "")

	h := &wfHarness{}
	installWorkForge(t, h)

	_, err := s.SyncWork(context.Background(), it.ID, "")
	if codeOf(t, err) != exit.PreflightFailed {
		t.Fatalf("code = %v, want preflight_failed", codeOf(t, err))
	}
	var e *exit.Error
	if !errors.As(err, &e) || !strings.Contains(strings.Join(e.Hints, " "), "--project") {
		t.Errorf("missing --project hint: %v", err)
	}
	if len(h.built) != 0 {
		t.Errorf("newForgeClient called %d times, want 0", len(h.built))
	}
}

// TestSyncWorkResolveErrorPassesThrough pins that a coded resolution error is
// returned unchanged (not remapped to a generic failure).
func TestSyncWorkResolveErrorPassesThrough(t *testing.T) {
	s := setupVault(t)
	if _, err := s.NewCycle("Ship v1", "0.1.0", "", ""); err != nil {
		t.Fatal(err)
	}
	it := betWithProject(t, s, "Bad project", "scope-a", "/p/bad")

	h := &wfHarness{resolveErr: map[string]error{
		"/p/bad": exit.New(exit.Usage, "unsupported git host \"example.com\""),
	}}
	installWorkForge(t, h)

	_, err := s.SyncWork(context.Background(), it.ID, "")
	if codeOf(t, err) != exit.Usage {
		t.Fatalf("code = %v, want usage (pass-through)", codeOf(t, err))
	}
	if len(h.built) != 0 {
		t.Errorf("newForgeClient called %d times, want 0", len(h.built))
	}
}

// TestSyncWorkProjectOverride pins that a non-empty override is used for
// resolution and persisted into project:, while an empty override uses the
// note's field.
func TestSyncWorkProjectOverride(t *testing.T) {
	s := setupVault(t)
	if _, err := s.NewCycle("Ship v1", "0.1.0", "", ""); err != nil {
		t.Fatal(err)
	}
	// Bet with no project: the override is the only source.
	it := betWithProject(t, s, "Override me", "scope-a", "")
	seedMilestone(t, s, "RTwoStudio/orbit", 3)

	h := &wfHarness{resolve: map[string]forge.Remote{"/p/orbit": ghRemote("RTwoStudio", "orbit")}}
	installWorkForge(t, h)

	if _, err := s.SyncWork(context.Background(), it.ID, "/p/orbit"); err != nil {
		t.Fatalf("SyncWork: %v", err)
	}
	show, err := s.ShowWork(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if show.Project != "/p/orbit" {
		t.Errorf("project = %q, want the persisted override /p/orbit", show.Project)
	}
	if show.Repo != "RTwoStudio/orbit" {
		t.Errorf("repo = %q, want RTwoStudio/orbit", show.Repo)
	}
}

// TestSyncWorkAssignFailureRecordsIssue pins retry-safety: an AssignIssue
// failure after create still records the issue + milestone, and a retry
// updates in place instead of creating a duplicate.
func TestSyncWorkAssignFailureRecordsIssue(t *testing.T) {
	s := setupVault(t)
	if _, err := s.NewCycle("Ship v1", "0.1.0", "", ""); err != nil {
		t.Fatal(err)
	}
	it := betWithProject(t, s, "Cancellation", "scope-a", "/p/orbit")
	seedMilestone(t, s, "RTwoStudio/orbit", 7)

	h := &wfHarness{resolve: map[string]forge.Remote{"/p/orbit": ghRemote("RTwoStudio", "orbit")}}
	installWorkForge(t, h)
	cl := h.client("github")
	cl.assignErr = exit.New(exit.General, "gh issue edit --milestone failed")

	if _, err := s.SyncWork(context.Background(), it.ID, ""); codeOf(t, err) != exit.General {
		t.Fatalf("code = %v, want general", codeOf(t, err))
	}
	show, err := s.ShowWork(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if show.Issue != 1 || show.Milestone != 7 {
		t.Fatalf("recorded issue/milestone = %d/%d, want 1/7 despite the assign failure", show.Issue, show.Milestone)
	}

	// Retry: updates in place, never a second create.
	cl.assignErr = nil
	if _, err := s.SyncWork(context.Background(), it.ID, ""); err != nil {
		t.Fatalf("retry SyncWork: %v", err)
	}
	if cl.createCalls != 1 {
		t.Errorf("createCalls = %d, want 1 (no duplicate)", cl.createCalls)
	}
	if cl.updateCalls != 1 {
		t.Errorf("updateCalls = %d, want 1 on the retry", cl.updateCalls)
	}
}

// TestSyncWorkPreservesNeocortexIssue pins that work sync never touches the
// neocortex-issue frontmatter key and keeps key order + the body intact.
func TestSyncWorkPreservesNeocortexIssue(t *testing.T) {
	s := setupVault(t)
	if _, err := s.NewCycle("Ship v1", "0.1.0", "", ""); err != nil {
		t.Fatal(err)
	}
	it := betWithProject(t, s, "Cancellation", "scope-a", "/p/orbit")
	seedMilestone(t, s, "RTwoStudio/orbit", 7)

	cur, err := s.ShowWork(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	d, err := doc.ParseDoc(cur.Path)
	if err != nil {
		t.Fatal(err)
	}
	d.Set("neocortex-issue", "NC-42")
	if err := d.Save(); err != nil {
		t.Fatal(err)
	}
	beforeBody := string(d.Body)

	h := &wfHarness{resolve: map[string]forge.Remote{"/p/orbit": ghRemote("RTwoStudio", "orbit")}}
	installWorkForge(t, h)

	if _, err := s.SyncWork(context.Background(), it.ID, ""); err != nil {
		t.Fatalf("SyncWork: %v", err)
	}
	show, err := s.ShowWork(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if show.NeocortexIssue != "NC-42" {
		t.Errorf("neocortex-issue = %q, want NC-42 (untouched)", show.NeocortexIssue)
	}
	after, err := doc.ParseDoc(show.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after.Body) != beforeBody {
		t.Errorf("sync changed the work body:\n before=%q\n after =%q", beforeBody, string(after.Body))
	}
	raw, err := os.ReadFile(show.Path)
	if err != nil {
		t.Fatal(err)
	}
	note := string(raw)
	nc, hist := strings.Index(note, "neocortex-issue:"), strings.Index(note, "history:")
	if nc < 0 || hist < 0 || nc > hist {
		t.Errorf("frontmatter key order not preserved:\n%s", note)
	}
}

// TestSyncWorkMissingCLI pins that a missing/unauthenticated CLI is a coded
// error with a hint, and that no issue call is attempted.
func TestSyncWorkMissingCLI(t *testing.T) {
	s := setupVault(t)
	if _, err := s.NewCycle("Ship v1", "0.1.0", "", ""); err != nil {
		t.Fatal(err)
	}
	it := betWithProject(t, s, "Cancellation", "scope-a", "/p/orbit")
	seedMilestone(t, s, "RTwoStudio/orbit", 7)

	h := &wfHarness{resolve: map[string]forge.Remote{"/p/orbit": ghRemote("RTwoStudio", "orbit")}}
	h.client("github").availErr = exit.New(exit.General, "GitHub CLI (gh) not found on PATH", "install it")
	installWorkForge(t, h)

	_, err := s.SyncWork(context.Background(), it.ID, "")
	if codeOf(t, err) != exit.General {
		t.Fatalf("code = %v, want general", codeOf(t, err))
	}
	var e *exit.Error
	if !errors.As(err, &e) || !strings.Contains(strings.Join(e.Hints, " "), "install it") {
		t.Errorf("missing install hint: %v", err)
	}
	if h.client("github").createCalls != 0 || h.client("github").updateCalls != 0 {
		t.Errorf("create/update calls = %d/%d, want 0/0 (never reached)",
			h.client("github").createCalls, h.client("github").updateCalls)
	}
}

// TestSyncWorkMissingNote pins not_found (5) for an unknown work id.
func TestSyncWorkMissingNote(t *testing.T) {
	s := setupVault(t)
	installWorkForge(t, &wfHarness{})
	_, err := s.SyncWork(context.Background(), "W-9999", "/p/orbit")
	if codeOf(t, err) != exit.NotFound {
		t.Fatalf("code = %v, want not_found", codeOf(t, err))
	}
}

// TestWorkSyncReportJSONShape pins the T6 --json contract: the documented tags.
func TestWorkSyncReportJSONShape(t *testing.T) {
	rep := &WorkSyncReport{
		Work:      &WorkItem{ID: "W-0001-arash-jafarpour"},
		Provider:  "github",
		Repo:      "RTwoStudio/orbit",
		Issue:     1,
		Milestone: 7,
		Assignees: []string{"arashjf"},
		Action:    SyncCreated,
	}
	data, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		`"work"`, `"provider":"github"`, `"repo":"RTwoStudio/orbit"`,
		`"issue":1`, `"milestone":7`, `"assignees":["arashjf"]`, `"action":"created"`,
	} {
		if !strings.Contains(string(data), key) {
			t.Errorf("marshalled report missing %s: %s", key, data)
		}
	}
}

// forgeRoster maps two handles to distinct github/gitlab usernames so the
// provider-selection mapping is observable; arash's name/email match the vault
// clone's git identity so NewWork resolves.
const forgeRoster = "arash-jafarpour:\n" +
	"  name: Arash Jafarpour\n" +
	"  email: arash@example.com\n" +
	"  github: arashjf\n" +
	"  gitlab: arash-jf-gl\n" +
	"seyed-mahdi-rostami:\n" +
	"  name: Seyed Mahdi Rostami\n" +
	"  email: seyed@example.com\n" +
	"  github: seyedmr\n" +
	"  gitlab: seyed-mr-gl\n"

// TestSyncWorkMapsAssigneesPerProvider: work sync resolves each stored handle to
// the repo provider's roster username (github → GitHub, gitlab → GitLab), sets
// them on the issue after the milestone assignment, and reports them.
func TestSyncWorkMapsAssigneesPerProvider(t *testing.T) {
	cases := []struct {
		name     string
		remote   forge.Remote
		provider string
		want     []string
	}{
		{"github", ghRemote("RTwoStudio", "orbit"), "github", []string{"seyedmr", "arashjf"}},
		{"gitlab", glRemote("RTwoStudio", "orbit"), "gitlab", []string{"seyed-mr-gl", "arash-jf-gl"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := setupVault(t)
			writeRoster(t, s, forgeRoster)
			if _, err := s.NewCycle("Ship v1", "0.1.0", "", ""); err != nil {
				t.Fatal(err)
			}
			it := betWithAssignees(t, s, "Assigned", "scope-a", "/p/orbit",
				[]string{"seyed-mahdi-rostami", "arash-jafarpour"})
			seedMilestone(t, s, "RTwoStudio/orbit", 7)

			h := &wfHarness{resolve: map[string]forge.Remote{"/p/orbit": tc.remote}}
			installWorkForge(t, h)

			rep, err := s.SyncWork(context.Background(), it.ID, "")
			if err != nil {
				t.Fatalf("SyncWork: %v", err)
			}
			if !reflect.DeepEqual(rep.Assignees, tc.want) {
				t.Errorf("report assignees = %v, want %v", rep.Assignees, tc.want)
			}
			cl := h.client(tc.provider)
			if cl.assignCalls != 1 {
				t.Errorf("AssignIssue calls = %d, want 1 (milestone first)", cl.assignCalls)
			}
			if cl.issueAsgCalls != 1 || cl.issueAsgIssue != 1 {
				t.Errorf("SetIssueAssignees calls = %d issue=%d, want 1/1", cl.issueAsgCalls, cl.issueAsgIssue)
			}
			if !reflect.DeepEqual(cl.issueAsgUsers, tc.want) {
				t.Errorf("SetIssueAssignees usernames = %v, want %v", cl.issueAsgUsers, tc.want)
			}
		})
	}
}

// TestSyncWorkMissingProviderUsernameFails: a handle with no username for the
// repo's provider is preflight_failed (6) naming the handle + provider, and no
// assignee is set.
func TestSyncWorkMissingProviderUsernameFails(t *testing.T) {
	// gitlab-only roster: neither handle carries a github username.
	const gitlabOnly = "arash-jafarpour:\n" +
		"  name: Arash Jafarpour\n" +
		"  email: arash@example.com\n" +
		"  gitlab: arash-jf-gl\n" +
		"seyed-mahdi-rostami:\n" +
		"  name: Seyed Mahdi Rostami\n" +
		"  email: seyed@example.com\n" +
		"  gitlab: seyed-mr-gl\n"
	s := setupVault(t)
	writeRoster(t, s, gitlabOnly)
	if _, err := s.NewCycle("Ship v1", "0.1.0", "", ""); err != nil {
		t.Fatal(err)
	}
	it := betWithAssignees(t, s, "No gh user", "scope-a", "/p/orbit", []string{"seyed-mahdi-rostami"})
	seedMilestone(t, s, "RTwoStudio/orbit", 7)

	h := &wfHarness{resolve: map[string]forge.Remote{"/p/orbit": ghRemote("RTwoStudio", "orbit")}}
	installWorkForge(t, h)

	_, err := s.SyncWork(context.Background(), it.ID, "")
	if codeOf(t, err) != exit.PreflightFailed {
		t.Fatalf("code = %v, want preflight_failed", codeOf(t, err))
	}
	var e *exit.Error
	if !errors.As(err, &e) || !strings.Contains(e.Message, "seyed-mahdi-rostami") || !strings.Contains(e.Message, "github") {
		t.Errorf("error should name the handle + provider: %v", err)
	}
	if h.client("github").issueAsgCalls != 0 {
		t.Errorf("SetIssueAssignees calls = %d, want 0", h.client("github").issueAsgCalls)
	}
}

// TestSyncWorkUnknownAssigneeFails: a stored handle absent from the roster
// (a stale/foreign vault) is preflight_failed (6) and no assignee is set.
func TestSyncWorkUnknownAssigneeFails(t *testing.T) {
	s := setupVault(t)
	if _, err := s.NewCycle("Ship v1", "0.1.0", "", ""); err != nil {
		t.Fatal(err)
	}
	it := betWithAssignees(t, s, "Ghost", "scope-a", "/p/orbit", []string{testOwnerHandle})
	seedMilestone(t, s, "RTwoStudio/orbit", 7)

	stale, err := s.ShowWork(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	d, err := doc.ParseDoc(stale.Path)
	if err != nil {
		t.Fatal(err)
	}
	d.SetStringList("assignees", []string{"ghost-handle"})
	if err := d.Save(); err != nil {
		t.Fatal(err)
	}

	h := &wfHarness{resolve: map[string]forge.Remote{"/p/orbit": ghRemote("RTwoStudio", "orbit")}}
	installWorkForge(t, h)

	_, err = s.SyncWork(context.Background(), it.ID, "")
	if codeOf(t, err) != exit.PreflightFailed {
		t.Fatalf("code = %v, want preflight_failed", codeOf(t, err))
	}
	if h.client("github").issueAsgCalls != 0 {
		t.Errorf("SetIssueAssignees calls = %d, want 0", h.client("github").issueAsgCalls)
	}
}

// TestSyncWorkEmptyAssigneesFails: a Bet whose stored list was cleared is
// preflight_failed (6) and no assignee is set.
func TestSyncWorkEmptyAssigneesFails(t *testing.T) {
	s := setupVault(t)
	if _, err := s.NewCycle("Ship v1", "0.1.0", "", ""); err != nil {
		t.Fatal(err)
	}
	it := betWithAssignees(t, s, "Cleared", "scope-a", "/p/orbit", []string{testOwnerHandle})
	seedMilestone(t, s, "RTwoStudio/orbit", 7)

	cur, err := s.ShowWork(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	d, err := doc.ParseDoc(cur.Path)
	if err != nil {
		t.Fatal(err)
	}
	d.SetStringList("assignees", nil)
	if err := d.Save(); err != nil {
		t.Fatal(err)
	}

	h := &wfHarness{resolve: map[string]forge.Remote{"/p/orbit": ghRemote("RTwoStudio", "orbit")}}
	installWorkForge(t, h)

	_, err = s.SyncWork(context.Background(), it.ID, "")
	if codeOf(t, err) != exit.PreflightFailed {
		t.Fatalf("code = %v, want preflight_failed", codeOf(t, err))
	}
	if h.client("github").issueAsgCalls != 0 {
		t.Errorf("SetIssueAssignees calls = %d, want 0", h.client("github").issueAsgCalls)
	}
}

// TestSyncWorkAssigneesFailureSurfaces: a SetIssueAssignees failure propagates
// and the recorded forge links survive (a retry stays idempotent).
func TestSyncWorkAssigneesFailureSurfaces(t *testing.T) {
	s := setupVault(t)
	if _, err := s.NewCycle("Ship v1", "0.1.0", "", ""); err != nil {
		t.Fatal(err)
	}
	it := betWithAssignees(t, s, "Assign fails", "scope-a", "/p/orbit", []string{testOwnerHandle})
	seedMilestone(t, s, "RTwoStudio/orbit", 7)

	h := &wfHarness{resolve: map[string]forge.Remote{"/p/orbit": ghRemote("RTwoStudio", "orbit")}}
	installWorkForge(t, h)
	cl := h.client("github")
	cl.issueAsgErr = exit.New(exit.General, "gh issue edit --add-assignee failed")

	if _, err := s.SyncWork(context.Background(), it.ID, ""); codeOf(t, err) != exit.General {
		t.Fatalf("code = %v, want general", codeOf(t, err))
	}
	show, err := s.ShowWork(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if show.Issue != 1 || show.Milestone != 7 {
		t.Fatalf("recorded issue/milestone = %d/%d, want 1/7 despite the assignee failure", show.Issue, show.Milestone)
	}

	cl.issueAsgErr = nil
	if _, err := s.SyncWork(context.Background(), it.ID, ""); err != nil {
		t.Fatalf("retry SyncWork: %v", err)
	}
	if cl.createCalls != 1 {
		t.Errorf("createCalls = %d, want 1 (no duplicate)", cl.createCalls)
	}
}
