package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/RTwoStudio/orbit/internal/exit"
	"github.com/RTwoStudio/orbit/internal/neocortex"
)

func newAddendaCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "addenda",
		Short: "Course-correction records against a locked plan",
		Long: `Addenda are the ONLY sanctioned way to change a locked plan.

Subcommands:
  new      Scaffold NN-<slug>.md (requires plan Locked + concept hash ok)
  list     N | Title | Status | Created | Applied-At
  show     Render the addenda file
  approve  Draft → Approved (one-way; requires ≥1 parseable delta line)
  apply    Amend the locked plan (re-chains the hash), mark Applied

Delta grammar (Plan Changes section):
  - ADD [T5] Task name (Blocked by: T2)
  - REMOVE [T3] Task name — reason
  - MODIFY [T2] New name (Blocked by: T1) — what changes

Exit codes: 0 ok · 5 not_found · 6 preflight_failed · 7 state_conflict
            8 tamper_detected · 10 io_error`,
		SilenceUsage: true,
	}
	cmd.AddCommand(newAddendaNewCmd(), newAddendaListCmd(), newAddendaShowCmd(),
		newAddendaApproveCmd(), newAddendaApplyCmd())
	return cmd
}

func newAddendaNewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   `new "<title>"`,
		Short: "Scaffold the next addenda record",
		Long: `Preflights: plan Locked AND concept hash verified (tamper check).
Targets the ACTIVE issue.

NN = max existing + 1, zero-padded 2 in the filename (NN-<slug>.md —
slug: lowercase, non-alnum→'-', trimmed, ≤40 chars; the H1 keeps the
verbatim title).

Exit codes: 0 ok · 5 not_found · 6 preflight_failed · 8 tamper_detected
            9 no_active_run · 10 io_error

Example:
  orbit neocortex addenda new "Add retry queue"`,
		Args:         usageArgs(cobra.ExactArgs(1)),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := activeIssue()
			if err != nil {
				return err
			}
			path, err := neocortex.NewAddenda(n, args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created %s\nNext: fill Reasoning/Impact/Plan Changes, then: orbit neocortex addenda approve <NN>\n", path)
			return nil
		},
	}
	return cmd
}

func newAddendaListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List addenda records for the active issue",
		Long: `N | Title | Status | Created | Applied-At (ACTIVE issue).

Exit codes: 0 ok · 5 not_found · 9 no_active_run · 10 io_error

Example:
  orbit neocortex addenda list --json`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := activeIssue()
			if err != nil {
				return err
			}
			rows, err := neocortex.ListAddenda(n)
			if err != nil {
				return err
			}
			if flagJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(rows)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%-4s %-40s %-9s %-21s %s\n", "N", "TITLE", "STATUS", "CREATED", "APPLIED-AT")
			for _, r := range rows {
				fmt.Fprintf(cmd.OutOrStdout(), "%-4d %-40s %-9s %-21s %s\n", r.NN, truncate(r.Title, 38), r.Status, r.Created, r.AppliedAt)
			}
			return nil
		},
	}
	return cmd
}

func newAddendaShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <NN>",
		Short: "Print an addenda record (frontmatter rendered as a header block)",
		Long: `Prints the full addenda file (ACTIVE issue).

Exit codes: 0 ok · 5 not_found · 9 no_active_run · 10 io_error

Example:
  orbit neocortex addenda show 1`,
		Args:         usageArgs(cobra.ExactArgs(1)),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := activeIssue()
			if err != nil {
				return err
			}
			var nn int
			if _, err := fmt.Sscanf(args[0], "%d", &nn); err != nil || nn <= 0 {
				return exit.New(exit.Usage, fmt.Sprintf("addenda number %q is not a positive integer", args[0]))
			}
			path, err := neocortex.FindAddendaFile(n, nn)
			if err != nil {
				return exit.New(exit.NotFound, fmt.Sprintf("addenda %02d not found in issue %d", nn, n))
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return exit.Wrap(exit.IOError, err, "cannot read "+path)
			}
			_, err = cmd.OutOrStdout().Write(data)
			return err
		},
	}
	return cmd
}

func newAddendaApproveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "approve <NN>",
		Short: "Approve a Draft addenda (one-way)",
		Long: `Preflights (ACTIVE issue):
  - Status == Draft (already Approved/Applied → exit 7, one-way)
  - no '<!-- Agent:' placeholders remain
  - Plan Changes has ≥1 ADD/REMOVE/MODIFY line that parses

Exit codes: 0 ok · 5 not_found · 6 preflight_failed · 7 state_conflict
            9 no_active_run

Example:
  orbit neocortex addenda approve 1`,
		Args:         usageArgs(cobra.ExactArgs(1)),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			n, nn, err := resolveAddendaTarget(args[0])
			if err != nil {
				return err
			}
			if err := neocortex.ApproveAddenda(n, nn); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Approved addenda %02d for issue %d\nNext: orbit neocortex addenda apply %d\n", nn, n, nn)
			return nil
		},
	}
	return cmd
}

func newAddendaApplyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply <NN>",
		Short: "Apply an Approved addenda to the locked plan (re-chains the hash)",
		Long: `All-or-nothing apply (ACTIVE issue). Preflights:
  - Status == Approved
  - concept AND plan hashes verified (tamper → exit 8)
  - ADD ids not in effective DAG; REMOVE/MODIFY ids exist
  - simulated resulting DAG: unique ids, deps exist, topologically valid
    → else exit 6 quoting the offending line

Actions (single pass):
  1. rewrite the plan's Task DAG lines per the delta
  2. append a '## Amendments' line with the delta summary
  3. recompute the plan Lock-Hash (Status stays Locked)
  4. stamp touched task files' amendments list (ADD creates nothing — JIT)
  5. addenda: Status Applied, Applied-At now

Prints the before/after DAG diff.

Exit codes: 0 ok · 5 not_found · 6 preflight_failed · 7 state_conflict
            8 tamper_detected · 9 no_active_run · 10 io_error

Example:
  orbit neocortex addenda apply 1`,
		Args:         usageArgs(cobra.ExactArgs(1)),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			n, nn, err := resolveAddendaTarget(args[0])
			if err != nil {
				return err
			}
			res, err := neocortex.ApplyAddenda(n, nn)
			if err != nil {
				return err
			}
			if flagJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(res)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Applied addenda-%02d (%s)\n\nDAG before:\n", nn, res.Summary)
			for _, l := range res.Before {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", l)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "DAG after:")
			for _, l := range res.After {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", l)
			}
			return nil
		},
	}
	return cmd
}

func resolveAddendaTarget(nnArg string) (int, int, error) {
	n, err := activeIssue()
	if err != nil {
		return 0, 0, err
	}
	var nn int
	if _, err := fmt.Sscanf(strings.TrimSpace(nnArg), "%d", &nn); err != nil || nn <= 0 {
		return 0, 0, exit.New(exit.Usage, fmt.Sprintf("addenda number %q is not a positive integer", nnArg))
	}
	return n, nn, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
