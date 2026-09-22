package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/orbit-sh/orbit-cli/internal/exit"
	"github.com/orbit-sh/orbit-cli/internal/neocortex"
)

func newTaskCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task",
		Short: "JIT task lifecycle (new/status/list/show/next)",
		Long: `Tasks are created Just-In-Time from the plan's effective Task DAG
(original rows as amended by applied addenda).

Subcommands:
  new      Render task.stub.md → tasks/<ID>.md (JIT; refuses duplicates)
  status   Transition a task's status (validated; CLI log appended)
  list     ID | Name | Status | dependsOn | origin | deps' statuses
  show     Print the task file
  next     First Open task whose every dependency is Close (exit 0 if none)

Exit codes: 0 ok · 5 not_found · 6 preflight_failed · 7 state_conflict
            8 tamper_detected · 10 io_error`,
		SilenceUsage: true,
	}
	cmd.AddCommand(newTaskNewCmd(), newTaskStatusCmd(), newTaskListCmd(),
		newTaskShowCmd(), newTaskNextCmd())
	return cmd
}

func newTaskNewCmd() *cobra.Command {
	var issue int
	cmd := &cobra.Command{
		Use:   "new <ID> \"<Name>\" [--issue=<n>]",
		Short: "Create a task file JIT from the plan DAG (never overwrites)",
		Long: `Preflights (in order):
  1. plan Locked AND concept hash verified (tamper → exit 8)
  2. ID syntax ^T[0-9]+$
  3. ID exists in the effective Task DAG → else exit 6 with hint
     "enter via /addenda"
  4. tasks/<ID>.md must NOT exist → else exit 7 state_conflict reporting
     the current status + remaining agent placeholder count (the agent
     resumes instead of recreating)

Derived fields: dependsOn (DAG row), Blocks (computed inverse), origin
(plan | addenda-<NN>, carried through MODifies), amendments pre-list.

Dep warnings (NEVER block):
  - a dependency's task file exists but is not Close
  - a dependency has NO task file yet

Exit codes: 0 ok · 5 not_found · 6 preflight_failed · 7 state_conflict
            8 tamper_detected · 10 io_error

Example:
  orbit neocortex task new T2 "Implement parser"`,
		Args:         usageArgs(cobra.ExactArgs(2)),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := resolveIssue(issue)
			if err != nil {
				return err
			}
			path, warnings, err := neocortex.NewTask(n, args[0], args[1])
			if err != nil {
				return err
			}
			for _, w := range warnings {
				fmt.Fprintf(cmd.ErrOrStderr(), "⚠ %s\n", w)
			}
			if flagJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
					"issue": n, "task": args[0], "path": path, "warnings": warnings,
				})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created %s\nNext: fill via /new-task, then: orbit neocortex task status %s --set=In Progress\n", path, args[0])
			return nil
		},
	}
	cmd.Flags().IntVar(&issue, "issue", 0, "issue number (default: ACTIVE)")
	return cmd
}

func newTaskStatusCmd() *cobra.Command {
	var (
		issue int
		set   string
	)
	cmd := &cobra.Command{
		Use:   "status <ID> --set=<s> [--issue=<n>]",
		Short: "Transition a task's status (validated state machine)",
		Long: `Transitions are validated against the fixed map (§7):
  Open → In Progress → Revise → {Rework, Close}; Rework → In Progress
Close is terminal. Input is case-insensitive ("in-progress", "In Progress").

Preflights:
  - task file exists (exit 5) and parses
  - transition legal (exit 7 listing valid transitions FROM current)
  - → Revise: Completion Notes non-empty AND every '- [ ]' under the
    Verification bullet is ticked (exit 6 quoting the offender)
  - → Close / → Rework: current must be Revise (exit 7 otherwise)

On success the frontmatter is rewritten and a line
'<RFC3339>  <from> → <to>' is appended to the trailing CLI log comment.

Exit codes: 0 ok · 5 not_found · 6 preflight_failed · 7 state_conflict

Example:
  orbit neocortex task status T1 --set=revise`,
		Args:         usageArgs(cobra.ExactArgs(1)),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := resolveIssue(issue)
			if err != nil {
				return err
			}
			if set == "" {
				return exit.New(exit.Usage, "--set is required (Open, In Progress, Revise, Rework, Close)")
			}
			from, to, err := neocortex.SetTaskStatus(n, args[0], set)
			if err != nil {
				return err
			}
			if flagJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
					"task": args[0], "from": from.FileValue(), "to": to.FileValue(),
				})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s: %s → %s\n", args[0], from.FileValue(), to.FileValue())
			return nil
		},
	}
	cmd.Flags().IntVar(&issue, "issue", 0, "issue number (default: ACTIVE)")
	cmd.Flags().StringVar(&set, "set", "", "target status (Open, In Progress, Revise, Rework, Close)")
	return cmd
}

func newTaskListCmd() *cobra.Command {
	var issue int
	cmd := &cobra.Command{
		Use:   "list [--issue=<n>]",
		Short: "List tasks from the effective DAG with live statuses",
		Long: `ID | Name | Status | dependsOn | origin | deps' statuses

Exit codes: 0 ok · 5 not_found · 6 preflight_failed

Example:
  orbit neocortex task list --json`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := resolveIssue(issue)
			if err != nil {
				return err
			}
			rows, err := neocortex.ListTasks(n)
			if err != nil {
				return err
			}
			if flagJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(rows)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%-5s %-30s %-12s %-12s %-11s %s\n",
				"ID", "NAME", "STATUS", "DEPENDSON", "ORIGIN", "DEP STATUS")
			for _, r := range rows {
				fmt.Fprintf(cmd.OutOrStdout(), "%-5s %-30s %-12s %-12s %-11s %s\n",
					r.ID, truncate(r.Name, 28), r.Status, joinStrings(r.DependsOn, ","), r.Origin,
					joinStrings(r.DepStatus, " "))
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&issue, "issue", 0, "issue number (default: ACTIVE)")
	return cmd
}

func newTaskShowCmd() *cobra.Command {
	var issue int
	cmd := &cobra.Command{
		Use:   "show <ID> [--issue=<n>]",
		Short: "Print the full task file",
		Long: `Prints tasks/<ID>.md verbatim.

Exit codes: 0 ok · 5 not_found

Example:
  orbit neocortex task show T1`,
		Args:         usageArgs(cobra.ExactArgs(1)),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := resolveIssue(issue)
			if err != nil {
				return err
			}
			data, err := neocortex.ShowTask(n, args[0])
			if err != nil {
				return err
			}
			_, werr := cmd.OutOrStdout().Write(data)
			return werr
		},
	}
	cmd.Flags().IntVar(&issue, "issue", 0, "issue number (default: ACTIVE)")
	return cmd
}

func newTaskNextCmd() *cobra.Command {
	var issue int
	cmd := &cobra.Command{
		Use:   "next [--issue=<n>]",
		Short: "First Open task whose every dependency is Close",
		Long: `Returns the next runnable task in DAG order. Exit 0 with a friendly
message when nothing is runnable (NOT an error).

Exit codes: 0 ok · 5 not_found · 6 preflight_failed

Example:
  orbit neocortex task next --json`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := resolveIssue(issue)
			if err != nil {
				return err
			}
			task, err := neocortex.NextTask(n)
			if err != nil {
				return err
			}
			if flagJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
					"issue": n, "task": task,
				})
			}
			if task == nil {
				fmt.Fprintln(cmd.OutOrStdout(), "No runnable task right now — everything is blocked or closed.")
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Next: %s — %s\n", task.ID, task.Name)
			return nil
		},
	}
	cmd.Flags().IntVar(&issue, "issue", 0, "issue number (default: ACTIVE)")
	return cmd
}
