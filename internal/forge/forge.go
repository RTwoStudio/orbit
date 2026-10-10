package forge

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/RTwoStudio/orbit/internal/exit"
)

// Issue is a forge issue reference returned by the wrappers.
type Issue struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
}

// Milestone is a forge milestone reference returned by the wrappers.
type Milestone struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	URL    string `json:"url"`
}

// IssueSpec is the mutable content of an issue (create or update).
type IssueSpec struct {
	Title string
	Body  string
}

// Client is the provider-agnostic forge surface. One implementation wraps gh,
// the other wraps glab; New picks by Remote.Provider.
type Client interface {
	// Available reports whether the CLI exists on PATH and is authenticated,
	// returning a coded error with an install/login hint otherwise.
	Available(ctx context.Context) error
	// CreateIssue creates an issue and returns its number/URL.
	CreateIssue(ctx context.Context, spec IssueSpec) (Issue, error)
	// UpdateIssue rewrites an existing issue's title/body.
	UpdateIssue(ctx context.Context, number int, spec IssueSpec) (Issue, error)
	// CreateMilestone creates a milestone with an optional due date (a plain
	// YYYY-MM-DD or RFC3339 string; provider encoding is the impl's job).
	CreateMilestone(ctx context.Context, title, due string) (Milestone, error)
	// AssignIssue attaches an issue to a milestone.
	AssignIssue(ctx context.Context, issue, milestone int) error
}

// runFunc is the injectable seam over exec so wrapper argv and output parsing
// are unit-testable without a shell. It returns stdout; failures are expected
// to carry stderr in the error.
type runFunc func(ctx context.Context, name string, args ...string) ([]byte, error)

// execRun is the production runFunc: run the command, capture stdout, and fold
// stderr into the error so coded errors stay actionable.
func execRun(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return stdout.Bytes(), fmt.Errorf("%s %s: %s", name, strings.Join(args, " "), msg)
	}
	return stdout.Bytes(), nil
}

// client holds the shared state of both provider impls.
type client struct {
	remote   Remote
	run      runFunc
	lookPath func(string) (string, error)
}

// Option customizes a Client at construction. The exported concrete helpers
// are intentionally absent: options exist for the package's own tests.
type Option func(*client)

func withRunner(r runFunc) Option {
	return func(c *client) { c.run = r }
}

func withLookPath(lp func(string) (string, error)) Option {
	return func(c *client) { c.lookPath = lp }
}

// New builds the provider-appropriate Client for remote. A non-gitlab provider
// selects the gh impl; gitlab selects glab.
func New(remote Remote, opts ...Option) Client {
	c := client{
		remote:   remote,
		run:      execRun,
		lookPath: exec.LookPath,
	}
	for _, opt := range opts {
		opt(&c)
	}
	if remote.Provider == ProviderGitLab {
		return &gitlabClient{c}
	}
	return &githubClient{c}
}

// githubClient wraps the gh CLI.
type githubClient struct{ client }

// gitlabClient wraps the glab CLI.
type gitlabClient struct{ client }

var issueURLRe = regexp.MustCompile(`/issues/(\d+)`)

// issueNumberFromURL extracts the trailing <number> from an issue URL. It
// matches both GitHub (.../issues/5) and GitLab (.../-/issues/5) shapes.
func issueNumberFromURL(rawURL string) (int, error) {
	m := issueURLRe.FindStringSubmatch(rawURL)
	if m == nil {
		return 0, exit.New(exit.General,
			fmt.Sprintf("cannot parse issue number from URL %q", rawURL),
			"expected a URL containing /issues/<number>")
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n <= 0 {
		return 0, exit.New(exit.General, fmt.Sprintf("invalid issue number in %q", rawURL))
	}
	return n, nil
}

// firstHTTPURL returns the first http(s) token in s, stripped of trailing
// sentence punctuation — CLIs print a human line or two around the URL.
func firstHTTPURL(s string) string {
	for _, tok := range strings.Fields(s) {
		if strings.HasPrefix(tok, "http://") || strings.HasPrefix(tok, "https://") {
			return strings.TrimRight(tok, ".,)>\u201d\"'")
		}
	}
	return ""
}
