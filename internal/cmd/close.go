package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/RTwoStudio/orbit/internal/neocortex"
)

func newCloseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "close [n]",
		Short: "Verify a default-lane issue is complete and print its report",
		Long: `Verifies a default-lane issue is ready to close: the plan is Locked and
every task in the effective DAG is Close. Defaults to the ACTIVE issue; pass
N to target another. Read-only — it never mutates files.

Quick runs are refused here; close them with: orbit neocortex quick close <n>

Exit codes: 0 ok · 5 not_found · 6 preflight_failed · 7 state_conflict
            9 no_active_run

Example:
  orbit neocortex close`,
		Args:         usageArgs(cobra.MaximumNArgs(1)),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := resolveIssueArgs(args)
			if err != nil {
				return err
			}
			report, err := neocortex.BuildCloseReport(n)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if flagJSON {
				return json.NewEncoder(out).Encode(report)
			}
			fmt.Fprintf(out, "issue %d %q is complete — concept: %s · plan: %s\n\n",
				report.Issue, report.Title, report.Concept, report.Plan)
			fmt.Fprintln(out, "Tasks:")
			for _, t := range report.Tasks {
				fmt.Fprintf(out, "  [%s] %-30s %s\n", t.ID, t.Name, t.Status)
			}
			fmt.Fprintln(out, "\nNext: commit the work (/neocortex:commit or the committer agent).")
			return nil
		},
	}
	return cmd
}
