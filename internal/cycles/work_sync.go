package cycles

import (
	"context"
	"fmt"
	"strings"

	"github.com/RTwoStudio/orbit/internal/doc"
	"github.com/RTwoStudio/orbit/internal/exit"
	"github.com/RTwoStudio/orbit/internal/forge"
	"github.com/RTwoStudio/orbit/internal/logx"
)

// SyncUpdated marks a work item whose git issue already existed and was
// rewritten (and re-assigned) in place by this run.
const SyncUpdated = "updated"

// WorkSyncReport is the JSON-tagged result of SyncWork, reused by T6's
// `work sync --json` rendering. Work is the refreshed read shape (with the
// recorded forge links); Provider/Repo/Issue/Milestone mirror what was
// recorded, and Action is SyncCreated or SyncUpdated.
type WorkSyncReport struct {
	Work      *WorkItem `json:"work"`
	Provider  string    `json:"provider"`
	Repo      string    `json:"repo"`
	Issue     int       `json:"issue"`
	Milestone int       `json:"milestone"`
	Assignees []string  `json:"assignees"`
	Action    string    `json:"action"`
}

// SyncWork syncs one Bet's git issue and its cycle-milestone assignment. On the
// first run it creates the issue; on every later run it updates and re-assigns
// in place, so the operation is idempotent and never duplicates.
//
// Bet-only: any other status is a state_conflict (7). The project is taken from
// projectOverride when non-empty, else the note's `project:`; an empty project
// is a preflight_failed (6). The repo/provider come from that project's git
// origin remote (resolveRemote), and the milestone is read from the note's
// cycle's recorded `owner/repo` → number map — a missing milestone is a
// state_conflict (7) pointing at `orbit cycles cycle sync` (SyncWork never
// mints milestones).
//
// The issue title is `<composite-id> — <title>` (U+2014) and the body is the
// note's Problem + Solution Sketch with HTML comments stripped. The forge links
// are
// persisted before AssignIssue, so an assignment failure can never orphan a
// created issue; a retry updates in place. A non-empty override is persisted
// into `project:`.
//
// After the milestone assignment it maps the stored `assignees:` handles to the
// provider's forge usernames via the roster and sets them on the issue; an empty
// stored list, an unknown handle, or a handle with no username for the provider
// is a preflight_failed (6).
func (s *Store) SyncWork(ctx context.Context, id, projectOverride string) (*WorkSyncReport, error) {
	d, err := s.findAndParse(id)
	if err != nil {
		return nil, err
	}
	it := workItemFromDoc(d)

	if workStatusFromFile(d.Get("status")) != WorkBet {
		return nil, exit.New(exit.StateConflict,
			fmt.Sprintf("work %s is %s — work sync requires a Bet item", it.ID, canonicalStatus(d.Get("status"))))
	}

	noteProject := strings.TrimSpace(d.Get("project"))
	override := strings.TrimSpace(projectOverride) != ""
	project := noteProject
	if override {
		project = strings.TrimSpace(projectOverride)
	}
	if project == "" {
		return nil, exit.New(exit.PreflightFailed,
			fmt.Sprintf("work %s has no project: — cannot resolve a forge repo", it.ID),
			"set a project at creation: orbit cycles work new --project <dir>",
			"or pass: orbit cycles work sync "+it.ID+" --project <dir>")
	}

	remote, err := resolveRemote(project)
	if err != nil {
		return nil, err // a coded resolution error passes through unchanged
	}

	cycleID := strings.TrimSpace(d.Get("cycle"))
	if cycleID == "" {
		return nil, exit.New(exit.StateConflict,
			fmt.Sprintf("work %s has no cycle: — a Bet must belong to a cycle", it.ID))
	}
	cy, err := s.ShowCycle(cycleID)
	if err != nil {
		return nil, err
	}
	milestone := cy.Milestones[remote.Slug()]
	if milestone <= 0 {
		return nil, exit.New(exit.StateConflict,
			fmt.Sprintf("no milestone recorded for %s in cycle %s", remote.Slug(), cycleID),
			"run: orbit cycles cycle sync")
	}

	client := newForgeClient(remote)
	if err := client.Available(ctx); err != nil {
		return nil, err
	}

	spec := forge.IssueSpec{
		Title: fmt.Sprintf("%s — %s", it.ID, it.Title),
		Body:  renderIssueBody(d.Body),
	}

	action := SyncUpdated
	var issue forge.Issue
	if it.Issue > 0 {
		issue, err = client.UpdateIssue(ctx, it.Issue, spec)
	} else {
		action = SyncCreated
		issue, err = client.CreateIssue(ctx, spec)
	}
	if err != nil {
		return nil, err
	}
	number := issue.Number
	if number <= 0 {
		number = it.Issue // a wrapper that prints no number: keep the known one
	}

	// Persist the applied override and the forge links BEFORE assigning, so a
	// failed assignment can never orphan the issue; a later retry is idempotent.
	if override && project != noteProject {
		d.Set("project", project)
		if err := d.Save(); err != nil {
			return nil, exit.Wrap(exit.IOError, err, "cannot rewrite "+d.Path)
		}
	}
	work, err := s.SetWorkForge(it.ID, WorkForge{
		Provider:  remote.Provider,
		Repo:      remote.Slug(),
		Issue:     number,
		Milestone: milestone,
	})
	if err != nil {
		return nil, err
	}

	if err := client.AssignIssue(ctx, number, milestone); err != nil {
		return nil, err
	}

	// Map the stored handles to forge usernames via the roster and set the
	// issue's assignees — the visible, real-time claim that prevents
	// double-pickup. This runs after the milestone assignment so the recorded
	// forge links (persisted above) are never lost to a later failure.
	usernames, err := s.resolveAssigneeUsernames(work.Assignees, remote)
	if err != nil {
		return nil, err
	}
	if err := client.SetIssueAssignees(ctx, number, usernames); err != nil {
		return nil, err
	}

	logx.Info("work sync id=%s issue=%d milestone=%d assignees=%d action=%s", it.ID, number, milestone, len(usernames), action)
	return &WorkSyncReport{
		Work:      work,
		Provider:  remote.Provider,
		Repo:      remote.Slug(),
		Issue:     number,
		Milestone: milestone,
		Assignees: usernames,
		Action:    action,
	}, nil
}

// resolveAssigneeUsernames maps a work item's stored `assignees:` handles to the
// forge usernames for remote's provider (github → Developer.GitHub, gitlab →
// Developer.GitLab). An empty stored list, an unknown handle, or a handle with
// no username for the target provider is a preflight_failed (6) naming the
// handle and provider, so the mapping fails loudly rather than silently
// skipping a claim.
func (s *Store) resolveAssigneeUsernames(handles []string, remote forge.Remote) ([]string, error) {
	if len(handles) == 0 {
		return nil, exit.New(exit.PreflightFailed,
			"work has no assignees — a Bet must carry at least one assignee to sync",
			"re-bet with: orbit cycles work bet <id> --assignee <handle>")
	}
	r, err := s.LoadRoster()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(handles))
	for _, h := range handles {
		dev, ok := r.Lookup(h)
		if !ok {
			return nil, exit.New(exit.PreflightFailed,
				fmt.Sprintf("unknown assignee %q — not in the developer roster", h),
				"add the handle to "+s.RosterPath()+" first")
		}
		username := dev.GitHub
		if remote.Provider == forge.ProviderGitLab {
			username = dev.GitLab
		}
		if username == "" {
			return nil, exit.New(exit.PreflightFailed,
				fmt.Sprintf("assignee %q has no %s username in the developer roster", h, remote.Provider),
				"add "+remote.Provider+": <username> to "+s.RosterPath())
		}
		out = append(out, username)
	}
	return out, nil
}

// renderIssueBody seeds a git issue body from a work note's shape: each
// non-empty `## Problem` / `## Solution Sketch` section (HTML comments
// stripped) under its own heading, joined by a blank line. The shape preflight
// guarantees both sections exist for a Bet, but an empty one is simply omitted.
func renderIssueBody(body []byte) string {
	var sections []string
	for _, name := range []string{"Problem", "Solution Sketch"} {
		text := strings.TrimSpace(doc.StripHTMLComments(doc.Section(body, name)))
		if text == "" {
			continue
		}
		sections = append(sections, "## "+name+"\n"+text)
	}
	return strings.Join(sections, "\n\n")
}
