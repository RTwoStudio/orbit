package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/RTwoStudio/orbit/internal/exit"
)

const ghCLI = "gh"

// Available probes gh: present on PATH and authenticated.
func (c *githubClient) Available(ctx context.Context) error {
	if _, err := c.lookPath(ghCLI); err != nil {
		return exit.New(exit.General, "GitHub CLI (gh) not found on PATH",
			"install it: https://cli.github.com",
			"then authenticate: gh auth login")
	}
	if _, err := c.run(ctx, ghCLI, "auth", "status"); err != nil {
		return exit.Wrap(exit.General, err, "gh is not authenticated",
			"run: gh auth login")
	}
	return nil
}

// CreateIssue creates a gh issue and parses its number from the printed URL.
func (c *githubClient) CreateIssue(ctx context.Context, spec IssueSpec) (Issue, error) {
	out, err := c.run(ctx, ghCLI, "issue", "create",
		"--repo", c.remote.Slug(),
		"--title", spec.Title,
		"--body", spec.Body,
	)
	if err != nil {
		return Issue{}, exit.Wrap(exit.General, err, "gh issue create failed")
	}
	rawURL := firstHTTPURL(string(out))
	if rawURL == "" {
		return Issue{}, exit.New(exit.General, "gh issue create printed no issue URL",
			"unexpected gh output")
	}
	num, err := issueNumberFromURL(rawURL)
	if err != nil {
		return Issue{}, err
	}
	return Issue{Number: num, URL: rawURL}, nil
}

// UpdateIssue rewrites an existing gh issue. gh prints no canonical URL, so it
// is reconstructed from the remote.
func (c *githubClient) UpdateIssue(ctx context.Context, number int, spec IssueSpec) (Issue, error) {
	if _, err := c.run(ctx, ghCLI, "issue", "edit", strconv.Itoa(number),
		"--repo", c.remote.Slug(),
		"--title", spec.Title,
		"--body", spec.Body,
	); err != nil {
		return Issue{}, exit.Wrap(exit.General, err, "gh issue edit failed")
	}
	return Issue{Number: number, URL: c.issueURL(number)}, nil
}

// CreateMilestone creates a milestone via the REST API (the gh CLI has no
// milestone subcommand). due is encoded to the RFC3339 due_on gh expects.
func (c *githubClient) CreateMilestone(ctx context.Context, title, due string) (Milestone, error) {
	args := []string{"api", fmt.Sprintf("repos/%s/milestones", c.remote.Slug()),
		"-f", "title=" + title,
	}
	if strings.TrimSpace(due) != "" {
		d, err := ghDue(due)
		if err != nil {
			return Milestone{}, err
		}
		args = append(args, "-f", "due_on="+d)
	}
	out, err := c.run(ctx, ghCLI, args...)
	if err != nil {
		return Milestone{}, exit.Wrap(exit.General, err, "gh api milestones failed")
	}
	var resp struct {
		Number  int    `json:"number"`
		Title   string `json:"title"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return Milestone{}, exit.Wrap(exit.General, err, "cannot parse gh milestone response")
	}
	return Milestone{Number: resp.Number, Title: resp.Title, URL: resp.HTMLURL}, nil
}

// AssignIssue attaches an issue to a milestone by number.
func (c *githubClient) AssignIssue(ctx context.Context, issue, milestone int) error {
	if _, err := c.run(ctx, ghCLI, "issue", "edit", strconv.Itoa(issue),
		"--repo", c.remote.Slug(),
		"--milestone", strconv.Itoa(milestone),
	); err != nil {
		return exit.Wrap(exit.General, err, "gh issue edit --milestone failed")
	}
	return nil
}

// issueURL reconstructs the canonical web URL of a GitHub issue.
func (c *githubClient) issueURL(number int) string {
	return fmt.Sprintf("https://%s/%s/issues/%d", c.remote.Host, c.remote.Slug(), number)
}

// ghDue normalizes a due string to the RFC3339 timestamp gh's due_on wants.
func ghDue(due string) (string, error) {
	due = strings.TrimSpace(due)
	if t, err := time.Parse("2006-01-02", due); err == nil {
		return t.UTC().Format(time.RFC3339), nil
	}
	if t, err := time.Parse(time.RFC3339, due); err == nil {
		return t.UTC().Format(time.RFC3339), nil
	}
	return "", exit.New(exit.Usage,
		fmt.Sprintf("invalid milestone due date %q", due),
		"use YYYY-MM-DD or RFC3339")
}

// compile-time guard that the gh impl satisfies Client.
var _ Client = (*githubClient)(nil)
