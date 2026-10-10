package cycles

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/RTwoStudio/orbit/internal/doc"
	"github.com/RTwoStudio/orbit/internal/exit"
)

func TestSetWorkForgeRoundTrip(t *testing.T) {
	s := setupVault(t)
	it, err := s.NewWork("Forge me", "scope-a")
	if err != nil {
		t.Fatal(err)
	}
	before, err := doc.ParseDoc(it.Path)
	if err != nil {
		t.Fatal(err)
	}
	beforeBody := string(before.Body)

	got, err := s.SetWorkForge(it.ID, WorkForge{
		Provider: "github", Repo: "RTwoStudio/orbit", Issue: 12, Milestone: 3,
	})
	if err != nil {
		t.Fatalf("SetWorkForge: %v", err)
	}
	if got.Provider != "github" || got.Repo != "RTwoStudio/orbit" || got.Issue != 12 || got.Milestone != 3 {
		t.Errorf("returned shape = %+v", got)
	}

	// An independent read reflects the persisted values.
	show, err := s.ShowWork(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if show.Provider != "github" || show.Repo != "RTwoStudio/orbit" || show.Issue != 12 || show.Milestone != 3 {
		t.Errorf("read shape = %+v", show)
	}

	data, err := os.ReadFile(it.Path)
	if err != nil {
		t.Fatal(err)
	}
	note := string(data)
	for _, want := range []string{
		"provider: github", "repo: RTwoStudio/orbit", "issue: 12", "milestone: 3",
	} {
		if !strings.Contains(note, want) {
			t.Errorf("work note missing %q:\n%s", want, note)
		}
	}
	if strings.Contains(note, `"12"`) || strings.Contains(note, `"3"`) {
		t.Errorf("forge numbers should be bare ints, not quoted strings:\n%s", note)
	}

	after, err := doc.ParseDoc(it.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after.Body) != beforeBody {
		t.Errorf("body changed:\n before=%q\n after =%q", beforeBody, string(after.Body))
	}
	if after.Get("title") != "Forge me" || after.Get("status") != "Backlog" {
		t.Errorf("sibling frontmatter changed: title=%q status=%q", after.Get("title"), after.Get("status"))
	}
}

// TestSetWorkForgeKeepsNeocortexIssue pins that the git `issue` and the
// NeoCortex `neocortex-issue` fields are independent in both directions.
func TestSetWorkForgeKeepsNeocortexIssue(t *testing.T) {
	s := setupVault(t)
	it, err := s.NewWork("Independence", "scope-a")
	if err != nil {
		t.Fatal(err)
	}
	d, err := doc.ParseDoc(it.Path)
	if err != nil {
		t.Fatal(err)
	}
	d.Set("neocortex-issue", "77")
	if err := d.Save(); err != nil {
		t.Fatal(err)
	}

	got, err := s.SetWorkForge(it.ID, WorkForge{Provider: "gitlab", Repo: "g/l", Issue: 5, Milestone: 1})
	if err != nil {
		t.Fatalf("SetWorkForge: %v", err)
	}
	if got.NeocortexIssue != "77" {
		t.Errorf("neocortex-issue = %q, want 77 (untouched by SetWorkForge)", got.NeocortexIssue)
	}
	if got.Issue != 5 {
		t.Errorf("issue = %d, want 5", got.Issue)
	}

	// The reverse: writing neocortex-issue leaves the git issue intact.
	d2, err := doc.ParseDoc(it.Path)
	if err != nil {
		t.Fatal(err)
	}
	d2.Set("neocortex-issue", "78")
	if err := d2.Save(); err != nil {
		t.Fatal(err)
	}
	show, err := s.ShowWork(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if show.Issue != 5 || show.NeocortexIssue != "78" {
		t.Errorf("issue=%d neocortex-issue=%q; want 5/78", show.Issue, show.NeocortexIssue)
	}
}

func TestSetWorkForgeRejectsBadInput(t *testing.T) {
	s := setupVault(t)
	if _, err := s.NewWork("Neg", "scope-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetWorkForge("W-0001", WorkForge{Issue: -1}); codeOf(t, err) != exit.PreflightFailed {
		t.Errorf("negative issue code = %v, want preflight_failed", codeOf(t, err))
	}
	if _, err := s.SetWorkForge("W-0001", WorkForge{Milestone: -2}); codeOf(t, err) != exit.PreflightFailed {
		t.Errorf("negative milestone code = %v, want preflight_failed", codeOf(t, err))
	}
	if _, err := s.SetWorkForge("W-0099", WorkForge{}); codeOf(t, err) != exit.NotFound {
		t.Errorf("unknown id code = %v, want not_found", codeOf(t, err))
	}
}

// TestWorkForgeTolerantRead pins that a malformed frontmatter integer reads as
// 0 rather than erroring the read view.
func TestWorkForgeTolerantRead(t *testing.T) {
	s := setupVault(t)
	it, err := s.NewWork("Tolerant", "scope-a")
	if err != nil {
		t.Fatal(err)
	}
	d, err := doc.ParseDoc(it.Path)
	if err != nil {
		t.Fatal(err)
	}
	d.Set("issue", "not-a-number")
	if err := d.Save(); err != nil {
		t.Fatal(err)
	}
	show, err := s.ShowWork(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if show.Issue != 0 {
		t.Errorf("malformed issue = %d, want 0", show.Issue)
	}
}

func TestSetCycleForgeRoundTrip(t *testing.T) {
	s := setupVault(t)
	cy, err := s.NewCycle("Ship", "0.1.0", "", "")
	if err != nil {
		t.Fatal(err)
	}
	before, err := doc.ParseDoc(cy.Path)
	if err != nil {
		t.Fatal(err)
	}
	beforeBody := string(before.Body)

	got, err := s.SetCycleForge(cy.ID, CycleForge{
		Project: "/home/x/proj",
		Milestones: map[string]int{
			"RTwoStudio/orbit": 4,
			"RTwoStudio/other": 7,
		},
	})
	if err != nil {
		t.Fatalf("SetCycleForge: %v", err)
	}
	if got.Project != "/home/x/proj" {
		t.Errorf("project = %q, want /home/x/proj", got.Project)
	}
	if got.Milestones["RTwoStudio/orbit"] != 4 || got.Milestones["RTwoStudio/other"] != 7 {
		t.Errorf("milestones = %v, want orbit:4 other:7", got.Milestones)
	}

	note := cycleNote(t, s, "C-0001")
	iOrbit, iOther := strings.Index(note, "RTwoStudio/orbit"), strings.Index(note, "RTwoStudio/other")
	if iOrbit < 0 || iOther < 0 || iOrbit > iOther {
		t.Errorf("milestone keys not sorted:\n%s", note)
	}
	if !strings.Contains(note, "project: /home/x/proj") || !strings.Contains(note, "milestone:") {
		t.Errorf("cycle note missing forge keys:\n%s", note)
	}

	after, err := doc.ParseDoc(cy.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after.Body) != beforeBody {
		t.Errorf("body changed:\n before=%q\n after =%q", beforeBody, string(after.Body))
	}
}

func TestSetCycleForgeMilestonesJSONObject(t *testing.T) {
	s := setupVault(t)
	if _, err := s.NewCycle("JSON map", "0.1.0", "", ""); err != nil {
		t.Fatal(err)
	}
	got, err := s.SetCycleForge("C-0001", CycleForge{
		Milestones: map[string]int{"RTwoStudio/orbit": 4},
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"milestone":{"RTwoStudio/orbit":4}`) {
		t.Errorf("milestone did not render as a JSON object: %s", data)
	}
}

func TestSetCycleForgeRejectsBadInput(t *testing.T) {
	s := setupVault(t)
	if _, err := s.NewCycle("Neg", "0.1.0", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetCycleForge("C-0001", CycleForge{Milestones: map[string]int{"a/b": -1}}); codeOf(t, err) != exit.PreflightFailed {
		t.Errorf("negative milestone code = %v, want preflight_failed", codeOf(t, err))
	}
	if _, err := s.SetCycleForge("C-0099", CycleForge{}); codeOf(t, err) != exit.NotFound {
		t.Errorf("unknown cycle code = %v, want not_found", codeOf(t, err))
	}
	if _, err := s.SetCycleForge("nope", CycleForge{}); codeOf(t, err) != exit.PreflightFailed {
		t.Errorf("bad cycle id code = %v, want preflight_failed", codeOf(t, err))
	}
}
