package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/RTwoStudio/orbit/internal/config"
	"github.com/RTwoStudio/orbit/internal/exit"
	"github.com/RTwoStudio/orbit/internal/neocortex"
)

func newIssueCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "issue",
		Short: "Create, lock, list, show, and switch issues (default lane)",
		Long: `Issue lifecycle management. Every work item is an issue; the default
lane is the full protocol (concept + plan + task DAG).

Subcommands:
  new      Scaffold a default-lane issue (interactive intake) and set ACTIVE
  import   Scaffold from a remote reference (GitHub issue/PR number or URL)
  ingest   Scaffold from a local file (verbatim)
  lock     Hash-lock the concept (one-way; Draft → Locked)
  list     Table of all issues (both lanes; --lane / --status filters)
  show     Render one issue (quick file, or concept + plan + task view)
  switch   Point ACTIVE at an existing issue

Exit codes: 0 ok · 5 not_found · 6 preflight_failed · 7 state_conflict
            9 no_active_run · 10 io_error`,
		SilenceUsage: true,
	}
	cmd.AddCommand(newIssueNewCmd(), newIssueImportCmd(), newIssueIngestCmd(),
		newIssueLockCmd(), newIssueListCmd(), newIssueShowCmd(), newIssueSwitchCmd())
	return cmd
}

func newIssueNewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   `new "<title>"`,
		Short: "Scaffold a new issue (interactive intake) and set it ACTIVE",
		Long: `Scaffolds issues/issue-<n>/ (00-concept.md, 01-plan.md, tasks/,
addenda/, notes/) from the cached registry stubs and writes ACTIVE.
n = max existing + 1. All-or-nothing.

Detail is injected as an agent-instruction placeholder; the Orchestrator
fills Objective/Requirements/Scope through approved writes before locking.

Exit codes: 0 ok · 4 registry_unreachable · 6 preflight_failed
            7 state_conflict · 10 io_error

Example:
  orbit neocortex issue new "Add streaming API"`,
		Args:         usageArgs(cobra.ExactArgs(1)),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runIssueNew(cmd, args[0], neocortex.IssueSource{Mode: "interactive"})
		},
	}
	return cmd
}

func newIssueImportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   `import <n|url> "<title>"`,
		Short: "Scaffold an issue from a remote reference (Detail verbatim)",
		Long: `Fetches the remote content and injects it VERBATIM into Detail. Supports
GitHub and GitLab issues/PRs:
  - github.com/<o>/<r>/issues/<n>  (or /pull/<n>)
  - gitlab.com/<group>/<proj>/-/issues/<n>  (or /merge_requests/<n>)
Any other URL is fetched as-is (raw GET). 15s timeout; the token is read from
the env var named in config under tokens.github / tokens.gitlab.
(GitHub: Authorization: Bearer; GitLab: PRIVATE-TOKEN). Nothing is written on
failure (exit 4).

Exit codes: 0 ok · 3 config_error · 4 registry_unreachable
            6 preflight_failed · 7 state_conflict · 10 io_error

Examples:
  orbit neocortex issue import 42 "Retry limit on the client"
  orbit neocortex issue import https://github.com/acme/app/issues/42 "Retry limit"
  orbit neocortex issue import https://gitlab.com/acme/app/-/issues/42 "Retry limit"`,
		Args:         usageArgs(cobra.ExactArgs(2)),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(flagConfig)
			if err != nil {
				return err
			}
			src := neocortex.IssueSource{Mode: "remote", RemoteURL: args[0], TokenEnv: cfg.TokenEnvFor(neocortex.ProviderOf(args[0]))}
			return runIssueNew(cmd, args[1], src)
		},
	}
	return cmd
}

func newIssueIngestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   `ingest <path> "<title>"`,
		Short: "Scaffold an issue from a local file (Detail verbatim)",
		Long: `Reads the file and injects its content VERBATIM into Detail.

Exit codes: 0 ok · 5 not_found · 6 preflight_failed · 7 state_conflict
            10 io_error

Example:
  orbit neocortex issue ingest spec.md "Add streaming API"`,
		Args:         usageArgs(cobra.ExactArgs(2)),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runIssueNew(cmd, args[1], neocortex.IssueSource{Mode: "file", FilePath: args[0]})
		},
	}
	return cmd
}

func runIssueNew(cmd *cobra.Command, title string, src neocortex.IssueSource) error {
	res, err := neocortex.NewIssue(title, src)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if flagJSON {
		return json.NewEncoder(out).Encode(map[string]any{"issue": res.Number, "path": res.Dir})
	}
	fmt.Fprintf(out, "Created issue-%d (%s):\n", res.Number, res.Dir)
	for _, t := range res.Tree {
		fmt.Fprintf(out, "  %s\n", t)
	}
	fmt.Fprintf(out, "\nNext: fill the concept (Objective/Detail/Requirements/Scope), then: orbit neocortex issue lock\n")
	return nil
}

func newIssueLockCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lock [n]",
		Short: "Hash-lock the concept (one-way; Draft → Locked)",
		Long: `Locks the concept file: Status Locked, Locked-At now, Lock-Hash = sha256
of the body. Defaults to the ACTIVE issue; pass N to target another.

Preflights (first failure exits 6 preflight_failed):
  1. concept exists and Status == Draft (already Locked → exit 7, one-way)
  2. body contains no '<!-- Agent:' placeholders
  3. body contains no '{{' tokens

Exit codes: 0 ok · 5 not_found · 6 preflight_failed · 7 state_conflict
            8 tamper_detected · 9 no_active_run · 10 io_error

Example:
  orbit neocortex issue lock 7`,
		Args:         usageArgs(cobra.MaximumNArgs(1)),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := resolveIssueArgs(args)
			if err != nil {
				return err
			}
			hash, err := neocortex.LockIssue(n)
			if err != nil {
				return err
			}
			if flagJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
					"issue": n, "lock_hash": hash,
				})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Locked issue-%d concept (hash %s…)\n", n, hash[:19])
			return nil
		},
	}
	return cmd
}

func newIssueListCmd() *cobra.Command {
	var lane, status string
	cmd := &cobra.Command{
		Use:   "list [--lane=quick|full] [--status <s>]",
		Short: "Table of all issues across both lanes",
		Long: `Lists every issue: ID | LANE | STATUS | TASKS | TITLE | ACTIVE.

--lane     filter by lane: quick | full (default: both)
--status   case-insensitive match against any of the issue's statuses
           (concept, plan, or quick status)

Exit codes: 0 ok · 2 usage · 10 io_error

Example:
  orbit neocortex issue list --lane=quick --json`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			rows, err := neocortex.ListIssuesInfo()
			if err != nil {
				return exit.Wrap(exit.IOError, err, "cannot scan issues")
			}
			rows = filterIssues(rows, lane, status)
			if flagJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(rows)
			}
			printIssueTable(cmd.OutOrStdout(), rows)
			return nil
		},
	}
	cmd.Flags().StringVar(&lane, "lane", "", "filter by lane: quick | full")
	cmd.Flags().StringVar(&status, "status", "", "filter by status (case-insensitive; concept/plan/quick)")
	return cmd
}

func newIssueShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <n>",
		Short: "Render one issue (quick file, or concept + plan + task view)",
		Long: `For a quick run: prints 00-quick.md verbatim.
For a default-lane issue: prints statuses, addenda counts, and the effective
Task DAG with per-task statuses.

Exit codes: 0 ok · 5 not_found · 6 preflight_failed

Example:
  orbit neocortex issue show 43`,
		Args:         usageArgs(cobra.ExactArgs(1)),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := parseIssueArg(args[0])
			if err != nil {
				return err
			}
			return showIssue(cmd, n)
		},
	}
	return cmd
}

func newIssueSwitchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "switch <n>",
		Short: "Point ACTIVE at an existing issue",
		Long: `Writes ACTIVE = n after verifying the issue directory exists.

Exit codes: 0 ok · 5 not_found · 10 io_error

Example:
  orbit neocortex issue switch 7`,
		SilenceUsage: true,
		Args:         usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := parseIssueArg(args[0])
			if err != nil {
				return err
			}
			if err := neocortex.SwitchIssue(n); err != nil {
				return err
			}
			if flagJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"issue": n, "path": neocortex.IssueDir(n)})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Active issue: %d (%s)\n", n, neocortex.IssueDir(n))
			return nil
		},
	}
	return cmd
}

// showIssue renders a single issue: quick file verbatim, or a default-lane
// status view.
func showIssue(cmd *cobra.Command, n int) error {
	out := cmd.OutOrStdout()
	if data, err := os.ReadFile(neocortex.QuickPath(n)); err == nil {
		_, werr := out.Write(data)
		return werr
	}
	rows, err := neocortex.ListIssuesInfo()
	if err != nil {
		return exit.Wrap(exit.IOError, err, "cannot scan issues")
	}
	var info *neocortex.IssueInfo
	for i := range rows {
		if rows[i].Number == n {
			info = &rows[i]
			break
		}
	}
	if info == nil {
		return exit.New(exit.NotFound, fmt.Sprintf("issue-%d does not exist", n))
	}
	tasks, err := neocortex.ListTasks(n)
	if err != nil {
		return err
	}
	if flagJSON {
		return json.NewEncoder(out).Encode(map[string]any{
			"issue":   n,
			"path":    neocortex.IssueDir(n),
			"lane":    info.Lane,
			"concept": info.Concept,
			"plan":    info.Plan,
			"addenda": map[string]int{
				"draft":    info.ConceptAddenda,
				"approved": info.ApprovedAddenda,
				"applied":  info.AppliedAddenda,
			},
			"tasks": tasks,
		})
	}
	fmt.Fprintf(out, "issue-%d %q — concept: %s · plan: %s · addenda: %d draft / %d approved / %d applied\n\n",
		n, info.Title, info.Concept, info.Plan, info.ConceptAddenda, info.ApprovedAddenda, info.AppliedAddenda)
	fmt.Fprintln(out, "Task DAG (order = topological):")
	for _, t := range tasks {
		depStr := "None"
		if len(t.DependsOn) > 0 {
			depStr = joinStrings(t.DependsOn, ", ")
		}
		fmt.Fprintf(out, "  [%s] %-30s %-12s blocked by: %s · origin: %s\n",
			t.ID, t.Name, t.Status, depStr, t.Origin)
		for _, ds := range t.DepStatus {
			fmt.Fprintf(out, "        ↳ dep %s\n", ds)
		}
	}
	return nil
}

// filterIssues applies --lane and --status filters.
func filterIssues(rows []neocortex.IssueInfo, lane, status string) []neocortex.IssueInfo {
	lane = strings.ToLower(strings.TrimSpace(lane))
	status = strings.TrimSpace(status)
	var out []neocortex.IssueInfo
	for _, r := range rows {
		if lane != "" && strings.ToLower(r.Lane) != lane {
			continue
		}
		if status != "" && !issueMatchesStatus(r, status) {
			continue
		}
		out = append(out, r)
	}
	return out
}

func issueMatchesStatus(r neocortex.IssueInfo, status string) bool {
	tokens := []string{r.Concept, r.Plan}
	if r.Lane == "quick" {
		tokens = []string{r.Quick}
	}
	for _, t := range tokens {
		if t != "" && strings.EqualFold(t, status) {
			return true
		}
	}
	return false
}

// issueStatusDisplay renders the STATUS column.
func issueStatusDisplay(r neocortex.IssueInfo) string {
	if r.Lane == "quick" {
		return r.Quick
	}
	return fmt.Sprintf("concept:%s plan:%s", r.Concept, r.Plan)
}

// taskSummary renders compact per-status task counts.
func taskSummary(counts map[string]int) string {
	var parts []string
	for _, s := range []string{"Open", "In Progress", "Revise", "Rework", "Close"} {
		if c := counts[s]; c > 0 {
			parts = append(parts, fmt.Sprintf("%s:%d", shortStatus(s), c))
		}
	}
	return strings.Join(parts, " ")
}

func printIssueTable(out io.Writer, rows []neocortex.IssueInfo) {
	fmt.Fprintf(out, "%-5s %-6s %-26s %-16s %-34s %s\n",
		"ID", "LANE", "STATUS", "TASKS", "TITLE", "ACTIVE")
	for _, r := range rows {
		active := ""
		if r.Active {
			active = "*"
		}
		title := r.Title
		if len(title) > 32 {
			title = title[:29] + "..."
		}
		fmt.Fprintf(out, "%-5d %-6s %-26s %-16s %-34s %s\n",
			r.Number, r.Lane, issueStatusDisplay(r), taskSummary(r.TaskCounts), title, active)
	}
}

// shortStatus abbreviates a task status for table output.
func shortStatus(s string) string {
	switch s {
	case "Open":
		return "O"
	case "In Progress":
		return "IP"
	case "Revise":
		return "R"
	case "Rework":
		return "W"
	case "Close":
		return "C"
	}
	return "?"
}

// writeJSON encodes v as 2-space-indented JSON to w.
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// parseIssueArg parses a positive issue number.
func parseIssueArg(s string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 {
		return 0, exit.New(exit.Usage, fmt.Sprintf("issue number %q is not a positive integer", s))
	}
	return n, nil
}

// resolveIssueArgs returns the positional issue number, or ACTIVE when absent.
func resolveIssueArgs(args []string) (int, error) {
	if len(args) == 1 {
		return parseIssueArg(args[0])
	}
	return activeIssue()
}

// activeIssue returns the ACTIVE issue number or a no_active_run error.
func activeIssue() (int, error) {
	n, err := neocortex.ReadActive()
	if err != nil {
		return 0, exit.New(exit.NoActiveRun,
			"no active issue",
			"create one: orbit neocortex issue new \"<title>\"",
			"or switch: orbit neocortex issue switch <n>")
	}
	return n, nil
}
