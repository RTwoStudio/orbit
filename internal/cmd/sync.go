package cmd

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/RTwoStudio/orbit/internal/cycles"
)

func newCyclesWorkSyncCmd() *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "sync <W-####> [--project <dir>]",
		Short: "Sync a Bet's issue to the forge (network)",
		Long: `Create or update the git issue for a Bet and assign it to the open cycle's
milestone. This is the one network-touching work verb: it shells out to the
gh (GitHub) or glab (GitLab) CLI and reuses the operator's existing login.

Preflight: the item must be Bet (exit 7 otherwise). The target repo is derived
from the project's git origin remote; the project comes from --project when
given, else the note's ` + "`project:`" + `. A Bet with neither is exit 6, and a
missing/unauthenticated forge CLI is exit 1. On first run the issue is created;
every later run updates and re-assigns in place (idempotent).

Exit codes:
  0 ok · 1 general (forge CLI missing/unauthenticated or failed) · 2 usage
  3 config_error · 5 not_found · 6 preflight_failed · 7 state_conflict
  10 io_error

Example:
  orbit cycles work sync W-0003
  orbit cycles work sync W-0003 --project ../orbit`,
		Args:         usageArgs(cobra.ExactArgs(1)),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openCyclesVault()
			if err != nil {
				return err
			}
			rep, err := s.SyncWork(cmd.Context(), args[0], project)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if flagJSON {
				return writeJSON(out, rep)
			}
			printWorkSync(out, rep)
			return nil
		},
	}
	cmd.Flags().StringVar(&project, "project", "",
		"project directory whose git origin remote targets the forge (default: the note's project:)")
	return cmd
}

func newCyclesCycleSyncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync the open cycle's milestones (network)",
		Long: `Create one forge milestone per distinct repo among the open cycle's bets
(title ` + "`C-#### — <goal>`" + `, due = the cycle's end) and record each number in
the cycle's milestone map. This is the one network-touching cycle verb: it
shells out to the gh (GitHub) or glab (GitLab) CLI and reuses the operator's
existing login.

Preflight: a cycle must be open (exit 7 otherwise). Repos come from each bet's
` + "`project:`" + `; a bet with no project is reported and skipped. A repo whose
milestone is already recorded is left untouched (idempotent); a missing or
unauthenticated forge CLI is exit 1.

Exit codes:
  0 ok · 1 general (forge CLI missing/unauthenticated or failed) · 2 usage
  3 config_error · 5 not_found · 6 preflight_failed · 7 state_conflict
  10 io_error

Example:
  orbit cycles cycle sync
  orbit cycles cycle sync --json`,
		Args:         usageArgs(cobra.NoArgs),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openCyclesVault()
			if err != nil {
				return err
			}
			rep, err := s.SyncCycle(cmd.Context())
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if flagJSON {
				return writeJSON(out, rep)
			}
			printCycleSync(out, rep)
			return nil
		},
	}
	return cmd
}

// printWorkSync renders the human one-line work-sync result.
func printWorkSync(out io.Writer, rep *cycles.WorkSyncReport) {
	fmt.Fprintf(out, "%s → issue #%d (%s), milestone #%d (%s, %s)\n",
		rep.Work.ID, rep.Issue, rep.Action, rep.Milestone, rep.Repo, rep.Provider)
}

// printCycleSync renders the human cycle-sync result: a header line, then one
// row per milestone and per skipped bet, or a placeholder when there is
// nothing to sync.
func printCycleSync(out io.Writer, rep *cycles.CycleSyncReport) {
	fmt.Fprintf(out, "Synced cycle %s — %s\n", rep.Cycle.ID, rep.Cycle.Goal)
	if len(rep.Milestones) == 0 && len(rep.Skipped) == 0 {
		fmt.Fprintln(out, "  (no bets to sync)")
		return
	}
	for _, m := range rep.Milestones {
		fmt.Fprintf(out, "  %-9s %s#%d (%s)\n", m.Action, m.Repo, m.Number, m.Provider)
	}
	for _, s := range rep.Skipped {
		fmt.Fprintf(out, "  skipped   %s — %s\n", s.ID, s.Reason)
	}
}
