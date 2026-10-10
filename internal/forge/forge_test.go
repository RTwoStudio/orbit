package forge

import (
	"context"
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/RTwoStudio/orbit/internal/exit"
)

// call is one recorded exec invocation.
type call struct {
	name string
	args []string
}

func (c call) String() string { return c.name + " " + strings.Join(c.args, " ") }

// fakeRunner records every call and returns scripted stdout/errors in order.
type fakeRunner struct {
	calls   []call
	outputs [][]byte
	errs    []error
	idx     int
}

func (f *fakeRunner) run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, call{name: name, args: append([]string(nil), args...)})
	i := f.idx
	f.idx++
	var out []byte
	var err error
	if i < len(f.outputs) {
		out = f.outputs[i]
	}
	if i < len(f.errs) {
		err = f.errs[i]
	}
	return out, err
}

func ghRemote() Remote {
	return Remote{Provider: ProviderGitHub, Host: "github.com", Owner: "RTwoStudio", Repo: "orbit"}
}

func glRemote() Remote {
	return Remote{Provider: ProviderGitLab, Host: "gitlab.com", Owner: "RTwoStudio", Repo: "orbit"}
}

func assertCalls(t *testing.T, got []call, want ...call) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	for i := range want {
		if got[i].name != want[i].name || !slices.Equal(got[i].args, want[i].args) {
			t.Errorf("call[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}

func wantError(t *testing.T, err error, code exit.Code) *exit.Error {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	var ee *exit.Error
	if !errors.As(err, &ee) {
		t.Fatalf("error type = %T, want *exit.Error", err)
	}
	if ee.Code != code {
		t.Errorf("code = %v, want %v", ee.Code, code)
	}
	return ee
}

func TestNewSelectsProvider(t *testing.T) {
	if _, ok := New(ghRemote()).(*githubClient); !ok {
		t.Errorf("New(github) = %T, want *githubClient", New(ghRemote()))
	}
	if _, ok := New(glRemote()).(*gitlabClient); !ok {
		t.Errorf("New(gitlab) = %T, want *gitlabClient", New(glRemote()))
	}
}

func TestGitHubCreateIssue(t *testing.T) {
	const url = "https://github.com/RTwoStudio/orbit/issues/42"
	fr := &fakeRunner{outputs: [][]byte{[]byte("Creating issue in RTwoStudio/orbit\n\n" + url + "\n")}}
	c := New(ghRemote(), withRunner(fr.run))

	got, err := c.CreateIssue(context.Background(), IssueSpec{Title: "Add sync", Body: "Body text"})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if got.Number != 42 || got.URL != url {
		t.Errorf("CreateIssue = %+v, want {42 %s}", got, url)
	}
	assertCalls(t, fr.calls, call{"gh", []string{
		"issue", "create", "--repo", "RTwoStudio/orbit", "--title", "Add sync", "--body", "Body text",
	}})
}

func TestGitHubCreateIssueNoURL(t *testing.T) {
	fr := &fakeRunner{outputs: [][]byte{[]byte("something exploded\n")}}
	c := New(ghRemote(), withRunner(fr.run))
	_, err := c.CreateIssue(context.Background(), IssueSpec{Title: "T", Body: "B"})
	wantError(t, err, exit.General)
}

func TestGitHubUpdateIssue(t *testing.T) {
	fr := &fakeRunner{}
	c := New(ghRemote(), withRunner(fr.run))

	got, err := c.UpdateIssue(context.Background(), 42, IssueSpec{Title: "Add sync", Body: "Body text"})
	if err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}
	if got.Number != 42 || got.URL != "https://github.com/RTwoStudio/orbit/issues/42" {
		t.Errorf("UpdateIssue = %+v", got)
	}
	assertCalls(t, fr.calls, call{"gh", []string{
		"issue", "edit", "42", "--repo", "RTwoStudio/orbit", "--title", "Add sync", "--body", "Body text",
	}})
}

func TestGitHubCreateMilestone(t *testing.T) {
	const url = "https://github.com/RTwoStudio/orbit/milestone/3"
	fr := &fakeRunner{outputs: [][]byte{[]byte(`{"number":3,"title":"C-0001 — Goal","html_url":"` + url + `"}`)}}
	c := New(ghRemote(), withRunner(fr.run))

	got, err := c.CreateMilestone(context.Background(), "C-0001 — Goal", "2026-10-23")
	if err != nil {
		t.Fatalf("CreateMilestone: %v", err)
	}
	if got.Number != 3 || got.Title != "C-0001 — Goal" || got.URL != url {
		t.Errorf("CreateMilestone = %+v", got)
	}
	assertCalls(t, fr.calls, call{"gh", []string{
		"api", "repos/RTwoStudio/orbit/milestones",
		"-f", "title=C-0001 — Goal",
		"-f", "due_on=2026-10-23T00:00:00Z",
	}})
}

func TestGitHubCreateMilestoneRFC3339Due(t *testing.T) {
	fr := &fakeRunner{outputs: [][]byte{[]byte(`{"number":3,"title":"M","html_url":"u"}`)}}
	c := New(ghRemote(), withRunner(fr.run))

	if _, err := c.CreateMilestone(context.Background(), "M", "2026-10-23T15:00:00+02:00"); err != nil {
		t.Fatalf("CreateMilestone: %v", err)
	}
	assertCalls(t, fr.calls, call{"gh", []string{
		"api", "repos/RTwoStudio/orbit/milestones",
		"-f", "title=M",
		"-f", "due_on=2026-10-23T13:00:00Z",
	}})
}

func TestGitHubCreateMilestoneBadDue(t *testing.T) {
	fr := &fakeRunner{}
	c := New(ghRemote(), withRunner(fr.run))
	_, err := c.CreateMilestone(context.Background(), "M", "not-a-date")
	wantError(t, err, exit.Usage)
	if len(fr.calls) != 0 {
		t.Errorf("bad due should not invoke gh, got %v", fr.calls)
	}
}

func TestGitHubAssignIssue(t *testing.T) {
	fr := &fakeRunner{}
	c := New(ghRemote(), withRunner(fr.run))

	if err := c.AssignIssue(context.Background(), 42, 3); err != nil {
		t.Fatalf("AssignIssue: %v", err)
	}
	assertCalls(t, fr.calls, call{"gh", []string{
		"issue", "edit", "42", "--repo", "RTwoStudio/orbit", "--milestone", "3",
	}})
}

func TestGitLabCreateIssue(t *testing.T) {
	const url = "https://gitlab.com/RTwoStudio/orbit/-/issues/9"
	fr := &fakeRunner{outputs: [][]byte{[]byte("Creating issue in RTwoStudio/orbit\n\n" + url + "\n")}}
	c := New(glRemote(), withRunner(fr.run))

	got, err := c.CreateIssue(context.Background(), IssueSpec{Title: "Add sync", Body: "Body text"})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if got.Number != 9 || got.URL != url {
		t.Errorf("CreateIssue = %+v, want {9 %s}", got, url)
	}
	assertCalls(t, fr.calls, call{"glab", []string{
		"issue", "create", "--repo", "RTwoStudio/orbit",
		"--title", "Add sync", "--description", "Body text", "--yes",
	}})
}

func TestGitLabUpdateIssue(t *testing.T) {
	fr := &fakeRunner{}
	c := New(glRemote(), withRunner(fr.run))

	got, err := c.UpdateIssue(context.Background(), 9, IssueSpec{Title: "Add sync", Body: "Body text"})
	if err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}
	if got.Number != 9 || got.URL != "https://gitlab.com/RTwoStudio/orbit/-/issues/9" {
		t.Errorf("UpdateIssue = %+v", got)
	}
	assertCalls(t, fr.calls, call{"glab", []string{
		"issue", "update", "9", "--repo", "RTwoStudio/orbit",
		"--title", "Add sync", "--description", "Body text",
	}})
}

func TestGitLabCreateMilestone(t *testing.T) {
	const url = "https://gitlab.com/RTwoStudio/orbit/-/milestones/4"
	fr := &fakeRunner{outputs: [][]byte{[]byte(`{"id":44,"iid":4,"title":"C-0001 — Goal","web_url":"` + url + `"}`)}}
	c := New(glRemote(), withRunner(fr.run))

	got, err := c.CreateMilestone(context.Background(), "C-0001 — Goal", "2026-10-23")
	if err != nil {
		t.Fatalf("CreateMilestone: %v", err)
	}
	if got.Number != 4 || got.Title != "C-0001 — Goal" || got.URL != url {
		t.Errorf("CreateMilestone = %+v", got)
	}
	assertCalls(t, fr.calls, call{"glab", []string{
		"api", "projects/RTwoStudio%2Forbit/milestones",
		"-f", "title=C-0001 — Goal",
		"-f", "due_date=2026-10-23",
	}})
}

func TestGitLabCreateMilestoneRFC3339Due(t *testing.T) {
	fr := &fakeRunner{outputs: [][]byte{[]byte(`{"iid":4,"title":"M","web_url":"u"}`)}}
	c := New(glRemote(), withRunner(fr.run))

	if _, err := c.CreateMilestone(context.Background(), "M", "2026-10-23T15:00:00+02:00"); err != nil {
		t.Fatalf("CreateMilestone: %v", err)
	}
	assertCalls(t, fr.calls, call{"glab", []string{
		"api", "projects/RTwoStudio%2Forbit/milestones",
		"-f", "title=M",
		"-f", "due_date=2026-10-23",
	}})
}

func TestGitLabAssignIssue(t *testing.T) {
	fr := &fakeRunner{}
	c := New(glRemote(), withRunner(fr.run))

	if err := c.AssignIssue(context.Background(), 9, 4); err != nil {
		t.Fatalf("AssignIssue: %v", err)
	}
	assertCalls(t, fr.calls, call{"glab", []string{
		"issue", "update", "9", "--repo", "RTwoStudio/orbit", "--milestone", "4",
	}})
}

func TestAvailableOK(t *testing.T) {
	fr := &fakeRunner{}
	c := New(ghRemote(), withRunner(fr.run), withLookPath(func(string) (string, error) {
		return "/usr/bin/gh", nil
	}))
	if err := c.Available(context.Background()); err != nil {
		t.Fatalf("Available: %v", err)
	}
	assertCalls(t, fr.calls, call{"gh", []string{"auth", "status"}})
}

func TestAvailableMissingCLI(t *testing.T) {
	for _, tc := range []struct {
		name string
		rm   Remote
		cli  string
	}{
		{"github", ghRemote(), "gh"},
		{"gitlab", glRemote(), "glab"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fr := &fakeRunner{}
			c := New(tc.rm, withRunner(fr.run), withLookPath(func(string) (string, error) {
				return "", exec.ErrNotFound
			}))
			ee := wantError(t, c.Available(context.Background()), exit.General)
			if len(ee.Hints) == 0 {
				t.Error("missing-CLI error has no install hint")
			}
			if len(fr.calls) != 0 {
				t.Errorf("absent CLI should not be executed, got %v", fr.calls)
			}
		})
	}
}

func TestAvailableUnauthenticated(t *testing.T) {
	for _, tc := range []struct {
		name string
		rm   Remote
		cli  string
	}{
		{"github", ghRemote(), "gh"},
		{"gitlab", glRemote(), "glab"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fr := &fakeRunner{errs: []error{errors.New("not logged in")}}
			c := New(tc.rm, withRunner(fr.run), withLookPath(func(string) (string, error) {
				return "/usr/bin/" + tc.cli, nil
			}))
			ee := wantError(t, c.Available(context.Background()), exit.General)
			if len(ee.Hints) == 0 {
				t.Error("unauthenticated error has no login hint")
			}
			assertCalls(t, fr.calls, call{tc.cli, []string{"auth", "status"}})
		})
	}
}

func TestFirstHTTPURL(t *testing.T) {
	cases := map[string]string{
		"https://example.com/x\n":     "https://example.com/x",
		"see http://a/b.cgit done":    "http://a/b.cgit",
		"no url here":                 "",
		"prefix https://x/y). suffix": "https://x/y",
	}
	for in, want := range cases {
		if got := firstHTTPURL(in); got != want {
			t.Errorf("firstHTTPURL(%q) = %q, want %q", in, got, want)
		}
	}
}
