package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/RTwoStudio/orbit/internal/exit"
)

const glabCLI = "glab"

// Available probes glab: present on PATH and authenticated.
func (c *gitlabClient) Available(ctx context.Context) error {
	if _, err := c.lookPath(glabCLI); err != nil {
		return exit.New(exit.General, "GitLab CLI (glab) not found on PATH",
			"install it: https://gitlab.com/gitlab-org/cli",
			"then authenticate: glab auth login")
	}
	if _, err := c.run(ctx, glabCLI, "auth", "status"); err != nil {
		return exit.Wrap(exit.General, err, "glab is not authenticated",
			"run: glab auth login")
	}
	return nil
}

// CreateIssue creates a glab issue and parses its number from the printed URL.
func (c *gitlabClient) CreateIssue(ctx context.Context, spec IssueSpec) (Issue, error) {
	out, err := c.run(ctx, glabCLI, "issue", "create",
		"--repo", c.remote.Slug(),
		"--title", spec.Title,
		"--description", spec.Body,
		"--yes",
	)
	if err != nil {
		return Issue{}, exit.Wrap(exit.General, err, "glab issue create failed")
	}
	rawURL := firstHTTPURL(string(out))
	if rawURL == "" {
		return Issue{}, exit.New(exit.General, "glab issue create printed no issue URL",
			"unexpected glab output")
	}
	num, err := issueNumberFromURL(rawURL)
	if err != nil {
		return Issue{}, err
	}
	return Issue{Number: num, URL: rawURL}, nil
}

// UpdateIssue rewrites an existing glab issue. glab prints no canonical URL, so
// it is reconstructed from the remote.
func (c *gitlabClient) UpdateIssue(ctx context.Context, number int, spec IssueSpec) (Issue, error) {
	if _, err := c.run(ctx, glabCLI, "issue", "update", strconv.Itoa(number),
		"--repo", c.remote.Slug(),
		"--title", spec.Title,
		"--description", spec.Body,
	); err != nil {
		return Issue{}, exit.Wrap(exit.General, err, "glab issue update failed")
	}
	return Issue{Number: number, URL: c.issueURL(number)}, nil
}

// CreateMilestone creates a milestone via the REST API. The project is
// addressed by URL-encoded full path rather than the :id placeholder, because
// glab's api command resolves :id from the current working directory and the
// sync verbs may run from the vault, not the project checkout. due is encoded
// to the YYYY-MM-DD due_date glab expects.
func (c *gitlabClient) CreateMilestone(ctx context.Context, title, due string) (Milestone, error) {
	endpoint := fmt.Sprintf("projects/%s/milestones", url.PathEscape(c.remote.Slug()))
	args := []string{"api", endpoint, "-f", "title=" + title}
	if strings.TrimSpace(due) != "" {
		d, err := glabDue(due)
		if err != nil {
			return Milestone{}, err
		}
		args = append(args, "-f", "due_date="+d)
	}
	out, err := c.run(ctx, glabCLI, args...)
	if err != nil {
		return Milestone{}, exit.Wrap(exit.General, err, "glab api milestones failed")
	}
	var resp struct {
		IID    int    `json:"iid"`
		Title  string `json:"title"`
		WebURL string `json:"web_url"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return Milestone{}, exit.Wrap(exit.General, err, "cannot parse glab milestone response")
	}
	return Milestone{Number: resp.IID, Title: resp.Title, URL: resp.WebURL}, nil
}

// AssignIssue attaches an issue to a milestone.
func (c *gitlabClient) AssignIssue(ctx context.Context, issue, milestone int) error {
	if _, err := c.run(ctx, glabCLI, "issue", "update", strconv.Itoa(issue),
		"--repo", c.remote.Slug(),
		"--milestone", strconv.Itoa(milestone),
	); err != nil {
		return exit.Wrap(exit.General, err, "glab issue update --milestone failed")
	}
	return nil
}

// issueURL reconstructs the canonical web URL of a GitLab issue.
func (c *gitlabClient) issueURL(number int) string {
	return fmt.Sprintf("https://%s/%s/-/issues/%d", c.remote.Host, c.remote.Slug(), number)
}

// glabDue normalizes a due string to the YYYY-MM-DD due_date glab wants.
func glabDue(due string) (string, error) {
	due = strings.TrimSpace(due)
	if t, err := time.Parse("2006-01-02", due); err == nil {
		return t.Format("2006-01-02"), nil
	}
	if t, err := time.Parse(time.RFC3339, due); err == nil {
		return t.UTC().Format("2006-01-02"), nil
	}
	return "", exit.New(exit.Usage,
		fmt.Sprintf("invalid milestone due date %q", due),
		"use YYYY-MM-DD or RFC3339")
}

// compile-time guard that the glab impl satisfies Client.
var _ Client = (*gitlabClient)(nil)
