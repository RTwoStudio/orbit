package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/RTwoStudio/orbit/internal/config"
	"github.com/RTwoStudio/orbit/internal/exit"
	"github.com/RTwoStudio/orbit/internal/neocortex"
)

func newIssueCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "issue",
		Short: "Create, lock, list, and switch issues",
		Long: `Issue lifecycle management.

Subcommands:
  new      Scaffold the issue tree (concept + plan + dirs) and set ACTIVE
  lock     Hash-lock the concept (one-way; Draft → Locked)
  list     Table of all issues with per-artifact statuses
  switch   Point ACTIVE at an existing issue

Exit codes: 0 ok · 5 not_found · 6 preflight_failed · 7 state_conflict
            9 no_active_run · 10 io_error`,
		SilenceUsage: true,
	}
	cmd.AddCommand(newIssueNewCmd(), newIssueLockCmd(), newIssueListCmd(), newIssueSwitchCmd())
	return cmd
}

func newIssueNewCmd() *cobra.Command {
	var (
		title       string
		interactive bool
		fromRemote  string
		fromFile    string
	)
	cmd := &cobra.Command{
		Use:   "new --title \"...\" (--interactive | --from-remote <url> | --from-file <path>)",
		Short: "Scaffold a new issue tree and set it ACTIVE",
		Long: `Scaffolds issues/issue-<n>/ (00-concept.md, 01-plan.md, tasks/,
addenda/, notes/) from the cached registry stubs and writes ACTIVE.
n = max existing + 1. All-or-nothing: nothing is written unless every
step succeeds.

Args & preflights:
  --title                 REQUIRED, non-empty, ≤120 chars (used verbatim in H1)
  exactly ONE source flag is required:
    --interactive         Detail becomes an agent-instruction placeholder
    --from-file <path>    Detail = verbatim file content
    --from-remote <url>   Detail fetched via API (15s timeout; token via
                          token_env; failure = exit 4, nothing written)

Exit codes: 0 ok · 3 config_error · 4 registry_unreachable · 5 not_found
            6 preflight_failed · 7 state_conflict · 10 io_error

Example:
  orbit neocortex issue new --title "Add streaming API" --from-file spec.md`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			sources := 0
			for _, b := range []bool{interactive, fromRemote != "", fromFile != ""} {
				if b {
					sources++
				}
			}
			if sources != 1 {
				return exit.New(exit.Usage,
					"exactly one source flag is required (--interactive | --from-remote <url> | --from-file <path>)")
			}
			if title == "" {
				return exit.New(exit.Usage, "--title is required (non-empty, ≤120 chars)")
			}
			cfg, err := config.Load(flagConfig)
			if err != nil {
				return err
			}
			src := neocortex.IssueSource{TokenEnv: cfg.Registry.TokenEnv}
			switch {
			case interactive:
				src.Mode = "interactive"
			case fromFile != "":
				src.Mode = "file"
				src.FilePath = fromFile
			case fromRemote != "":
				src.Mode = "remote"
				src.RemoteURL = fromRemote
			}
			res, err := neocortex.NewIssue(title, src)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if flagJSON {
				return json.NewEncoder(out).Encode(res)
			}
			fmt.Fprintf(out, "Created issue-%d (%s):\n", res.Number, res.Dir)
			for _, t := range res.Tree {
				fmt.Fprintf(out, "  %s\n", t)
			}
			fmt.Fprintf(out, "\nNext: fill the concept via /issue in opencode, then: orbit neocortex issue lock\n")
			return nil
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "issue title (verbatim H1, ≤120 chars)")
	cmd.Flags().BoolVar(&interactive, "interactive", false, "source: agent fills Detail interactively")
	cmd.Flags().StringVar(&fromRemote, "from-remote", "", "source: fetch Detail from a remote URL (GitHub issue)")
	cmd.Flags().StringVar(&fromFile, "from-file", "", "source: read Detail from a local file")
	return cmd
}

func newIssueLockCmd() *cobra.Command {
	var issue int
	cmd := &cobra.Command{
		Use:   "lock [--issue=<n>]",
		Short: "Hash-lock the concept (one-way; Draft → Locked)",
		Long: `Locks the concept file: Status Locked, Locked-At now, and Lock-Hash
= sha256 of the body (byte-exact, after the closing ---).

Preflights (first failure exits 6 preflight_failed):
  1. concept exists and Status == Draft (already Locked → exit 7, one-way)
  2. body contains no '<!-- Agent:' placeholders
  3. body contains no '{{' tokens

Exit codes: 0 ok · 5 not_found · 6 preflight_failed · 7 state_conflict
            8 tamper_detected (on later preflights) · 10 io_error

Example:
  orbit neocortex issue lock --issue=7`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := resolveIssue(issue)
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
	cmd.Flags().IntVar(&issue, "issue", 0, "issue number (default: ACTIVE)")
	return cmd
}

func newIssueListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Table of all issues with per-artifact statuses",
		Long: `Lists every issue: ID | Title (H1) | Concept status | Plan status |
Addenda (draft/approved/applied) | Tasks (per-status counts) | Active?

Exit codes: 0 ok · 10 io_error

Example:
  orbit neocortex issue list --json`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			rows, err := neocortex.ListIssuesInfo()
			if err != nil {
				return exit.Wrap(exit.IOError, err, "cannot scan issues")
			}
			if flagJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(rows)
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%-5s %-34s %-7s %-7s %-22s %-34s %s\n",
				"ID", "TITLE", "CONCEPT", "PLAN", "ADDENDA (d/a/ap)", "TASKS", "ACTIVE")
			for _, r := range rows {
				taskStr := ""
				for _, s := range []string{"Open", "In Progress", "Revise", "Rework", "Close"} {
					if c := r.TaskCounts[s]; c > 0 {
						taskStr += fmt.Sprintf("%s:%d ", shortStatus(s), c)
					}
				}
				active := ""
				if r.Active {
					active = "*"
				}
				title := r.Title
				if len(title) > 32 {
					title = title[:29] + "..."
				}
				fmt.Fprintf(out, "%-5d %-34s %-7s %-7s %-22s %-34s %s\n",
					r.Number, title, r.Concept, r.Plan,
					fmt.Sprintf("%d/%d/%d", r.ConceptAddenda, r.ApprovedAddenda, r.AppliedAddenda),
					taskStr, active)
			}
			return nil
		},
	}
	return cmd
}

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
			n, err := strconv.Atoi(args[0])
			if err != nil || n <= 0 {
				return exit.New(exit.Usage, fmt.Sprintf("issue number %q is not a positive integer", args[0]))
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

// resolveIssue returns the flag value or ACTIVE.
func resolveIssue(flag int) (int, error) {
	if flag > 0 {
		return flag, nil
	}
	n, err := neocortex.ReadActive()
	if err != nil {
		return 0, exit.New(exit.NoActiveRun,
			"no active issue — run: orbit neocortex issue new",
			"or pass --issue=<n> / switch: orbit neocortex issue switch <n>")
	}
	return n, nil
}
