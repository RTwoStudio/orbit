package forge

import (
	"errors"
	"os/exec"
	"testing"

	"github.com/RTwoStudio/orbit/internal/exit"
)

func TestParseRemote(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		provider string
		host     string
		owner    string
		repo     string
	}{
		{"scp github .git", "git@github.com:RTwoStudio/orbit.git", ProviderGitHub, "github.com", "RTwoStudio", "orbit"},
		{"scp github no .git", "git@github.com:RTwoStudio/orbit", ProviderGitHub, "github.com", "RTwoStudio", "orbit"},
		{"https github .git", "https://github.com/RTwoStudio/orbit.git", ProviderGitHub, "github.com", "RTwoStudio", "orbit"},
		{"https github no .git", "https://github.com/RTwoStudio/orbit", ProviderGitHub, "github.com", "RTwoStudio", "orbit"},
		{"https github trailing slash", "https://github.com/RTwoStudio/orbit/", ProviderGitHub, "github.com", "RTwoStudio", "orbit"},
		{"http github", "http://github.com/RTwoStudio/orbit.git", ProviderGitHub, "github.com", "RTwoStudio", "orbit"},
		{"ssh github", "ssh://git@github.com/RTwoStudio/orbit.git", ProviderGitHub, "github.com", "RTwoStudio", "orbit"},
		{"scp gitlab", "git@gitlab.com:RTwoStudio/orbit.git", ProviderGitLab, "gitlab.com", "RTwoStudio", "orbit"},
		{"https gitlab", "https://gitlab.com/RTwoStudio/orbit.git", ProviderGitLab, "gitlab.com", "RTwoStudio", "orbit"},
		{"ssh self-hosted gitlab", "ssh://git@gitlab.example.com/RTwoStudio/orbit.git", ProviderGitLab, "gitlab.example.com", "RTwoStudio", "orbit"},
		{"scp self-hosted gitlab", "git@gitlab.example.com:RTwoStudio/orbit.git", ProviderGitLab, "gitlab.example.com", "RTwoStudio", "orbit"},
		{"gitlab subgroup", "https://gitlab.example.com/group/subgroup/repo.git", ProviderGitLab, "gitlab.example.com", "group/subgroup", "repo"},
		{"uppercase host", "git@GitHub.com:RTwoStudio/orbit.git", ProviderGitHub, "github.com", "RTwoStudio", "orbit"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseRemote(tc.raw)
			if err != nil {
				t.Fatalf("ParseRemote(%q) error: %v", tc.raw, err)
			}
			if got.Provider != tc.provider || got.Host != tc.host || got.Owner != tc.owner || got.Repo != tc.repo {
				t.Errorf("ParseRemote(%q) = %+v, want {Provider:%s Host:%s Owner:%s Repo:%s}",
					tc.raw, got, tc.provider, tc.host, tc.owner, tc.repo)
			}
			if want := tc.owner + "/" + tc.repo; got.Slug() != want {
				t.Errorf("Slug() = %q, want %q", got.Slug(), want)
			}
		})
	}
}

func TestParseRemoteErrors(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		code exit.Code
	}{
		{"empty", "", exit.Usage},
		{"whitespace", "   ", exit.Usage},
		{"unsupported host", "https://bitbucket.org/team/repo.git", exit.Usage},
		{"unsupported scp host", "git@codeberg.org:team/repo.git", exit.Usage},
		{"not a remote", "just-a-string", exit.Usage},
		{"no repo", "https://github.com/onlyowner.git", exit.Usage},
		{"no host", "ssh:///owner/repo.git", exit.Usage},
		{"unsupported scheme", "ftp://github.com/owner/repo.git", exit.Usage},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseRemote(tc.raw)
			if err == nil {
				t.Fatalf("ParseRemote(%q) = nil error, want coded error", tc.raw)
			}
			var ee *exit.Error
			if !errors.As(err, &ee) {
				t.Fatalf("ParseRemote(%q) error type = %T, want *exit.Error", tc.raw, err)
			}
			if ee.Code != tc.code {
				t.Errorf("ParseRemote(%q) code = %v, want %v", tc.raw, ee.Code, tc.code)
			}
			if len(ee.Hints) == 0 {
				t.Errorf("ParseRemote(%q) error has no hint", tc.raw)
			}
		})
	}
}

func TestResolveOrigin(t *testing.T) {
	dir := initRepo(t)
	runGit(t, dir, "remote", "add", "origin", "git@github.com:RTwoStudio/orbit.git")

	r, err := Resolve(dir)
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if r.Provider != ProviderGitHub || r.Slug() != "RTwoStudio/orbit" {
		t.Errorf("Resolve() = %+v, want github RTwoStudio/orbit", r)
	}
}

func TestResolveNoOrigin(t *testing.T) {
	dir := initRepo(t)

	_, err := Resolve(dir)
	if err == nil {
		t.Fatal("Resolve() = nil error, want coded error")
	}
	var ee *exit.Error
	if !errors.As(err, &ee) {
		t.Fatalf("Resolve() error type = %T, want *exit.Error", err)
	}
	if ee.Code != exit.NotFound {
		t.Errorf("Resolve() code = %v, want NotFound", ee.Code)
	}
	if len(ee.Hints) == 0 {
		t.Error("Resolve() error has no hint")
	}
}

func TestResolveUnsupportedHost(t *testing.T) {
	dir := initRepo(t)
	runGit(t, dir, "remote", "add", "origin", "https://bitbucket.org/team/repo.git")

	_, err := Resolve(dir)
	if err == nil {
		t.Fatal("Resolve() = nil error, want coded error")
	}
	var ee *exit.Error
	if !errors.As(err, &ee) {
		t.Fatalf("Resolve() error type = %T, want *exit.Error", err)
	}
	if ee.Code != exit.Usage {
		t.Errorf("Resolve() code = %v, want Usage", ee.Code)
	}
}

// initRepo creates a fresh git repo or skips when git is unavailable.
func initRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")
	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("git %v failed: %v: %s", args, err, out)
	}
}
