package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/orbit-sh/orbit-cli/internal/exit"
)

func newNeoCortexCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "neocortex",
		Short: "NeoCortex workflow: registry, issues, plans, addenda, tasks",
		Long: `Manage the NeoCortex development workflow.

Command groups:
  install   First-contact setup: fetch registry, deploy agents, bootstrap project
  update    OTA refresh of global assets (agents/commands) from the registry
  which     Print the active issue directory
  status    Render the active issue's full status overview
  issue     Create, lock, list, switch issues
  plan      Lock the plan (hash-verified)
  addenda   Course-correction records against a locked plan
  task      JIT task lifecycle (new/status/list/show/next)

Exit codes:
  0 ok  1 general  2 usage  3 config_error  4 registry_unreachable
  5 not_found  6 preflight_failed  7 state_conflict  8 tamper_detected
  9 no_active_run  10 io_error  11 not_initialized`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				// Unknown verb (e.g. 'close', which does not exist in
				// v0.1.0) — mirror cobra's unknown-command error.
				return exit.New(exit.Usage,
					fmt.Sprintf("unknown command %q for %q", args[0], cmd.CommandPath()),
					"run: orbit neocortex --help to see the verb tree")
			}
			return cmd.Help()
		},
	}
	cmd.AddCommand(
		newInstallCmd(),
		newUpdateCmd(),
		newWhichCmd(),
		newStatusCmd(),
		newIssueCmd(),
		newPlanCmd(),
		newAddendaCmd(),
		newTaskCmd(),
	)
	return cmd
}
