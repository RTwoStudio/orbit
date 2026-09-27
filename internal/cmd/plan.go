package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/RTwoStudio/orbit/internal/exit"
	"github.com/RTwoStudio/orbit/internal/neocortex"
)

func newPlanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "plan",
		Short:        "Plan operations (lock)",
		Long:         `Plan lifecycle. Currently: lock (hash-verified, one-way).`,
		SilenceUsage: true,
	}
	cmd.AddCommand(newPlanLockCmd())
	return cmd
}

func newPlanLockCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lock [n]",
		Short: "Hash-lock the plan (one-way; Draft → Locked)",
		Long: `Locks 01-plan.md: Status Locked, Locked-At now, Lock-Hash over the
plan body. Prints a DAG summary on success. Defaults to the ACTIVE issue;
pass N to target another.

Preflights, in order:
  1. TAMPER CHECK FIRST: the concept's Lock-Hash is re-verified before any
     plan preflight — mismatch exits 8 tamper_detected
  2. plan exists and Status == Draft (already Locked → exit 7, one-way)
  3. no '<!-- Agent:' placeholders, no '{{' tokens
  4. '## Open Questions' contains no unticked '- [ ]' items
  5. '## Task DAG' parses (§ grammar): ≥1 task, unique IDs, deps exist,
     listed order topologically valid — the offending raw line is quoted

Exit codes: 0 ok · 5 not_found · 6 preflight_failed · 7 state_conflict
            8 tamper_detected · 9 no_active_run · 10 io_error

Example:
  orbit neocortex plan lock`,
		Args:         usageArgs(cobra.MaximumNArgs(1)),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := resolveIssueArgs(args)
			if err != nil {
				return err
			}
			dag, err := neocortex.LockPlan(n)
			if err != nil {
				return err
			}
			if flagJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
					"issue": n, "tasks": dag.Tasks,
				})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Locked plan for issue-%d — Task DAG (%d tasks):\n", n, len(dag.Tasks))
			for _, t := range dag.Tasks {
				deps := "None"
				if len(t.DependsOn) > 0 {
					deps = joinStrings(t.DependsOn, ", ")
				}
				fmt.Fprintf(cmd.OutOrStdout(), "  [%s] %s (Blocked by: %s)\n", t.ID, t.Name, deps)
			}
			return nil
		},
	}
	return cmd
}

var _ = exit.OK
