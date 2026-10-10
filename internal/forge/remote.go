// Package forge maps a project's git origin remote onto a forge provider
// (github | gitlab) and wraps that provider's CLI (gh | glab) with the typed
// operations Cycles needs to sync bets and cycles to git issues/milestones.
//
// Everything here is deterministic and offline except the wrapper calls
// themselves: ParseRemote is pure, Resolve shells out to git only to read the
// configured origin, and the Client operations are the only code that touches
// the network. Hosts other than github.com and GitLab are rejected with a coded
// error so the operator gets a clear hint instead of a confusing CLI failure.
package forge

import (
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"strings"

	"github.com/RTwoStudio/orbit/internal/exit"
)

// Provider identifiers for Remote.Provider.
const (
	ProviderGitHub = "github"
	ProviderGitLab = "gitlab"
)

// Remote is the provider-agnostic identity of a git remote: which forge it
// lives on plus the owner/namespace and repository name.
type Remote struct {
	Provider string // github | gitlab
	Host     string // e.g. github.com, gitlab.example.com (lowercased)
	Owner    string // owner or namespace path, e.g. RTwoStudio, group/subgroup
	Repo     string // repository name without the .git suffix
}

// Slug renders the owner/repo path used by both CLIs (--repo).
func (r Remote) Slug() string {
	if r.Owner == "" {
		return r.Repo
	}
	return r.Owner + "/" + r.Repo
}

// ParseRemote turns a git remote URL into a Remote. It accepts the four forms
// git actually emits — scp-like (git@host:owner/repo), ssh://, https:// and
// http:// — with or without a trailing .git. Provider inference follows the
// locked rule: github.com is github, any host mentioning "gitlab" is gitlab,
// and anything else is a coded error with a hint.
func ParseRemote(raw string) (Remote, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return Remote{}, exit.New(exit.Usage, "empty git remote URL",
			"set the project dir's origin remote to a github/gitlab URL")
	}

	var host, path string
	switch {
	case strings.Contains(s, "://"):
		u, err := url.Parse(s)
		if err != nil {
			return Remote{}, exit.Wrap(exit.Usage, err, fmt.Sprintf("cannot parse git remote %q", raw))
		}
		switch u.Scheme {
		case "ssh", "http", "https", "git":
		default:
			return Remote{}, exit.New(exit.Usage,
				fmt.Sprintf("unsupported git remote scheme %q in %q", u.Scheme, raw),
				"supported schemes: ssh, https, http, git")
		}
		host = u.Hostname()
		path = u.Path
	case isSCPLike(s):
		i := strings.Index(s, ":")
		host = s[:i]
		if at := strings.LastIndex(host, "@"); at >= 0 {
			host = host[at+1:]
		}
		path = s[i+1:]
	default:
		return Remote{}, exit.New(exit.Usage,
			fmt.Sprintf("unrecognized git remote %q", raw),
			"expected ssh://, https://, http://, or git@host:owner/repo")
	}

	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return Remote{}, exit.New(exit.Usage, fmt.Sprintf("git remote %q has no host", raw),
			"expected a git remote like https://github.com/owner/repo")
	}
	provider, err := providerForHost(host)
	if err != nil {
		return Remote{}, err
	}
	owner, repo, err := splitOwnerRepo(path)
	if err != nil {
		return Remote{}, exit.Wrap(exit.Usage, err, fmt.Sprintf("cannot derive owner/repo from %q", raw),
			"expected a git remote like https://github.com/owner/repo")
	}
	return Remote{Provider: provider, Host: host, Owner: owner, Repo: repo}, nil
}

// isSCPLike reports whether s looks like git's scp syntax (user@host:path or
// host:path) rather than a URL. The part before the first colon must contain no
// slash, which distinguishes it from a bare filesystem path.
func isSCPLike(s string) bool {
	i := strings.Index(s, ":")
	return i > 0 && !strings.Contains(s[:i], "/")
}

// providerForHost applies the locked host-based provider rule.
func providerForHost(host string) (string, error) {
	switch {
	case host == "github.com":
		return ProviderGitHub, nil
	case host == "gitlab.com", strings.Contains(host, "gitlab"):
		return ProviderGitLab, nil
	}
	return "", exit.New(exit.Usage,
		fmt.Sprintf("unsupported git host %q", host),
		"supported providers: github.com and GitLab hosts (gitlab.com or *gitlab*)")
}

// splitOwnerRepo normalizes a remote path into owner/namespace and repo. The
// owner may hold GitLab subgroups (group/subgroup), so only the final segment
// is the repo.
func splitOwnerRepo(p string) (string, string, error) {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, "/")
	p = strings.TrimSuffix(p, "/")
	p = strings.TrimSuffix(p, ".git")
	p = strings.TrimSuffix(p, "/")
	if p == "" {
		return "", "", fmt.Errorf("empty repository path")
	}
	var segs []string
	for _, s := range strings.Split(p, "/") {
		if s != "" {
			segs = append(segs, s)
		}
	}
	if len(segs) < 2 {
		return "", "", fmt.Errorf("expected owner/repo, got %q", p)
	}
	return strings.Join(segs[:len(segs)-1], "/"), segs[len(segs)-1], nil
}

// Resolve reads dir's git origin remote and parses it. It shells out to git
// only to read the configured URL (never to fetch), and returns a coded error
// when git is absent, dir has no origin, or the host is unsupported.
func Resolve(dir string) (Remote, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return Remote{}, exit.New(exit.General, "git is not installed",
			"install git, then retry")
	}
	ctx := context.Background()
	raw, err := gitOrigin(ctx, dir, "remote", "get-url", "origin")
	if err != nil {
		raw, err = gitOrigin(ctx, dir, "config", "--get", "remote.origin.url")
		if err != nil {
			return Remote{}, exit.New(exit.NotFound,
				fmt.Sprintf("no git origin remote in %s", dir),
				"add one: git -C "+dir+" remote add origin <url>")
		}
	}
	if strings.TrimSpace(raw) == "" {
		return Remote{}, exit.New(exit.NotFound,
			fmt.Sprintf("no git origin remote in %s", dir),
			"add one: git -C "+dir+" remote add origin <url>")
	}
	return ParseRemote(raw)
}

// gitOrigin runs a git subcommand scoped to dir and returns trimmed stdout.
func gitOrigin(ctx context.Context, dir string, args ...string) (string, error) {
	full := append([]string{"-C", dir}, args...)
	out, err := exec.CommandContext(ctx, "git", full...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
