package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/RTwoStudio/orbit/internal/neocortex"
)

func newTaskCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task",
		Short: "JIT task lifecycle (new/start/revise/close/rework/list/show/next)",
		Long: `Tasks are created Just-In-Time from the plan's effective Task DAG
(original rows as amended by applied addenda).

Subcommands:
  new      Render task.stub.md → tasks/<ID>.md (JIT; refuses duplicates)
  start    Open → In Progress (also Rework → In Progress)
  revise   In Progress → Revise (requires Completion Notes + ticked checks)
  close    Revise → Close
  rework   Revise → Rework
  list     ID | Name | Status | dependsOn | origin | deps' statuses
  show     Print the task file
  next     First Open task whose every dependency is Close (exit 0 if none)

Transitions target the ACTIVE issue. Close is Orchestrator-only.

Exit codes: 0 ok · 5 not_found · 6 preflight_failed · 7 state_conflict
            8 tamper_detected · 9 no_active_run · 10 io_error`,
		SilenceUsage: true,
	}
	cmd.AddCommand(newTaskNewCmd(), newTaskListCmd(),
		newTaskShowCmd(), newTaskNextCmd())
	for _, v := range taskVerbs {
		cmd.AddCommand(newTaskVerbCmd(v.verb, v.short, v.target))
	}
	return cmd
}

func newTaskNewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   `new <ID> "<Name>"`,
		Short: "Create a task file JIT from the plan DAG (never overwrites)",
		Long: `Preflights (in order):
  1. plan Locked AND concept hash verified (tamper → exit 8)
  2. ID syntax ^T[0-9]+$
  3. ID exists in the effective Task DAG → else exit 6 with hint
     "enter via addenda"
  4. tasks/<ID>.md must NOT exist → else exit 7 state_conflict reporting
     the current status + remaining agent placeholder count (the agent
     resumes instead of recreating)

Derived fields: dependsOn (DAG row), Blocks (computed inverse), origin
(plan | addenda-<NN>, carried through MODifies), amendments pre-list.

Dep warnings (NEVER block):
  - a dependency's task file exists but is not Close
  - a dependency has NO task file yet

Exit codes: 0 ok · 5 not_found · 6 preflight_failed · 7 state_conflict
            8 tamper_detected · 9 no_active_run · 10 io_error

Example:
  orbit neocortex task new T2 "Implement parser"`,
		Args:         usageArgs(cobra.ExactArgs(2)),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := activeIssue()
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
			fmt.Fprintf(cmd.OutOrStdout(), "Created %s\nNext: fill the task, then: orbit neocortex task start %s\n", path, args[0])
			return nil
		},
	}
	return cmd
}

// taskVerbs maps the short task verbs to their canonical target status.
var taskVerbs = []struct {
	verb   string
	target neocortex.TaskStatus
	short  string
}{
	{"start", neocortex.StatusInProgress, "Open → In Progress (also Rework → In Progress)"},
	{"revise", neocortex.StatusRevise, "In Progress → Revise (requires Completion Notes + ticked checks)"},
	{"close", neocortex.StatusClose, "Revise → Close"},
	{"rework", neocortex.StatusRework, "Revise → Rework"},
}

func newTaskVerbCmd(verb, short string, to neocortex.TaskStatus) *cobra.Command {
	return &cobra.Command{
		Use:   verb + " <ID>",
		Short: short,
		Long: `Transitions are validated against the fixed map:
  Open → In Progress → Revise → {Rework, Close}; Rework → In Progress
Close is terminal.

Preflights:
  - task file exists (exit 5) and parses
  - transition legal (exit 7 listing valid transitions FROM current)
  - → Revise: Completion Notes non-empty AND every '- [ ]' under the
    Verification bullet is ticked (exit 6 quoting the offender)
  - → Close / → Rework: current must be Revise (exit 7 otherwise)

On success the frontmatter is rewritten and a line
'<RFC3339>  <from> → <to>' is appended to the trailing CLI log comment.

Example:
  orbit neocortex task ` + verb + ` T1`,
		Args:         usageArgs(cobra.ExactArgs(1)),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := activeIssue()
			if err != nil {
				return err
			}
			from, toStatus, err := neocortex.SetTaskStatus(n, args[0], to.FileValue())
			if err != nil {
				return err
			}
			if flagJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
					"task": args[0], "from": from.FileValue(), "to": toStatus.FileValue(),
				})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s: %s → %s\n", args[0], from.FileValue(), toStatus.FileValue())
			return nil
		},
	}
}

func newTaskListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List tasks from the effective DAG with live statuses",
		Long: `ID | Name | Status | dependsOn | origin | deps' statuses (ACTIVE issue).

Exit codes: 0 ok · 5 not_found · 6 preflight_failed · 9 no_active_run

Example:
  orbit neocortex task list --json`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := activeIssue()
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
	return cmd
}

func newTaskShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <ID>",
		Short: "Print the full task file",
		Long: `Prints tasks/<ID>.md verbatim (ACTIVE issue).

Exit codes: 0 ok · 5 not_found · 9 no_active_run

Example:
  orbit neocortex task show T1`,
		Args:         usageArgs(cobra.ExactArgs(1)),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := activeIssue()
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
	return cmd
}

func newTaskNextCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "next",
		Short: "First Open task whose every dependency is Close",
		Long: `Returns the next runnable task in DAG order (ACTIVE issue). Exit 0 with
a friendly message when nothing is runnable (NOT an error).

Exit codes: 0 ok · 5 not_found · 6 preflight_failed · 9 no_active_run

Example:
  orbit neocortex task next --json`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := activeIssue()
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
	return cmd
}
