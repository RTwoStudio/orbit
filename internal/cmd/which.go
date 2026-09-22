package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/RTwoStudio/orbit-cli/internal/exit"
	"github.com/RTwoStudio/orbit-cli/internal/neocortex"
)

func newWhichCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "which",
		Short: "Print the absolute path of the active issue directory",
		Long: `Prints the absolute path of the active issue directory
(resolved from .neocortex/ACTIVE).

Preflights: ACTIVE present, non-empty, and pointing at an existing issue
(exit 9 no_active_run otherwise).

Exit codes: 0 ok · 5 not_found · 9 no_active_run · 10 io_error

Example:
  orbit neocortex which --json`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := neocortex.ReadActive()
			if err != nil {
				return exit.New(exit.NoActiveRun,
					"no active issue — run: orbit neocortex issue new",
					"or switch to an existing issue: orbit neocortex issue switch <n>")
			}
			path := neocortex.IssueDir(n)
			if flagJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
					"issue": n,
					"path":  path,
				})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s\n", path)
			return nil
		},
	}
	return cmd
}

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Render the active issue's full status overview",
		Long: `Renders a human overview of the active issue: concept/plan status,
addenda counts, and the effective Task DAG with per-task statuses
(edges shown in both directions: Blocked By and Blocks).

--json exposes {issue, concept, plan, addenda, tasks}.

Exit codes: 0 ok · 9 no_active_run · 5 not_found · 6 preflight_failed

Example:
  orbit neocortex status --json`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := neocortex.ReadActive()
			if err != nil {
				return exit.New(exit.NoActiveRun,
					"no active issue — run: orbit neocortex issue new",
					"or switch to an existing issue: orbit neocortex issue switch <n>")
			}
			tasks, err := neocortex.ListTasks(n)
			if err != nil {
				return err
			}
			issues, err := neocortex.ListIssuesInfo()
			if err != nil {
				return err
			}
			var info *neocortex.IssueInfo
			for i := range issues {
				if issues[i].Number == n {
					info = &issues[i]
					break
				}
			}
			if info == nil {
				return exit.New(exit.NotFound, "active issue directory not found")
			}
			if flagJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
					"issue":   n,
					"path":    neocortex.IssueDir(n),
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
			out := cmd.OutOrStdout()
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
		},
	}
	return cmd
}

func joinStrings(ss []string, sep string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += sep
		}
		out += s
	}
	return out
}
