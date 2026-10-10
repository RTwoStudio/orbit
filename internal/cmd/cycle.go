package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/RTwoStudio/orbit/internal/cycles"
	"github.com/RTwoStudio/orbit/internal/exit"
)

func newCyclesCycleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cycle",
		Short: "Open and close cycles (new/close/sync/list/show)",
		Long: `Manage time-boxed cycles. At most one cycle may be open at a time; the
CURRENT pointer names it, and ` + "`cycle close`" + ` shelves every non-delivered bet.

Subcommands:
  new "<goal>" --release <semver> [--start <date>] [--end <date>]
                              Open the next cycle (sets CURRENT)
  close                       Close the open cycle and clear CURRENT
  sync                        Sync the open cycle's milestones (network)
  list                        Table of cycles (--json)
  show <C-####>               Print the cycle note (--json)

Exit codes:
  0 ok  2 usage  3 config_error  4 registry_unreachable  5 not_found
  6 preflight_failed  7 state_conflict  10 io_error  11 not_initialized`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return exit.New(exit.Usage,
					fmt.Sprintf("unknown command %q for %q", args[0], cmd.CommandPath()),
					"run: orbit cycles cycle --help to see the verb tree")
			}
			return cmd.Help()
		},
	}
	cmd.AddCommand(
		newCyclesCycleNewCmd(),
		newCyclesCycleCloseCmd(),
		newCyclesCycleSyncCmd(),
		newCyclesCycleListCmd(),
		newCyclesCycleShowCmd(),
	)
	return cmd
}

func newCyclesCycleNewCmd() *cobra.Command {
	var release, start, end string
	cmd := &cobra.Command{
		Use:   `new "<goal>" --release <semver> [--start <date>] [--end <date>]`,
		Short: "Open the next cycle and point CURRENT at it",
		Long: `Creates cycles/C-####/ with the next free id and makes it the open cycle.

Preflight: the goal must be non-empty (exit 6); release must be strict
MAJOR.MINOR.PATCH (exit 6); the vault must be initialized (exit 11); no cycle
may already be open (exit 7). --start defaults to UTC today, --end to start +
six weeks; both are bare YYYY-MM-DD and end must not precede start (exit 6).

Exit codes: 0 ok · 2 usage · 4 registry_unreachable · 6 preflight_failed
            7 state_conflict · 10 io_error · 11 not_initialized

Example:
  orbit cycles cycle new "Ship the beta" --release 0.3.0`,
		Args:         usageArgs(cobra.ExactArgs(1)),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openCyclesVault()
			if err != nil {
				return err
			}
			cy, err := s.NewCycle(args[0], release, start, end)
			if err != nil {
				return err
			}
			if flagJSON {
				return writeJSON(cmd.OutOrStdout(), cy)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Opened cycle %s — %s (%s, %s → %s)\n",
				cy.ID, cy.Goal, cy.Release, cy.Start, cy.End)
			return nil
		},
	}
	cmd.Flags().StringVar(&release, "release", "", "release semver, MAJOR.MINOR.PATCH (required)")
	cmd.Flags().StringVar(&start, "start", "", "start date YYYY-MM-DD (default: today UTC)")
	cmd.Flags().StringVar(&end, "end", "", "end date YYYY-MM-DD (default: start + 6 weeks)")
	return cmd
}

func newCyclesCycleCloseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "close",
		Short: "Close the open cycle (shelves every non-delivered bet)",
		Long: `Shelves every work note in the open cycle that is not Delivered back to
backlog/, marks the cycle Closed, regenerates its ## Bets view, and clears
CURRENT.

Preflight: a cycle must be open (exit 7 otherwise); an already-Closed cycle
is exit 7.

Exit codes: 0 ok · 2 usage · 5 not_found · 6 preflight_failed
            7 state_conflict · 10 io_error

Example:
  orbit cycles cycle close`,
		Args:         usageArgs(cobra.NoArgs),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openCyclesVault()
			if err != nil {
				return err
			}
			cy, err := s.CloseCycle()
			if err != nil {
				return err
			}
			if flagJSON {
				return writeJSON(cmd.OutOrStdout(), cy)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Closed cycle %s — %s (%s)\n", cy.ID, cy.Goal, cy.Release)
			return nil
		},
	}
	return cmd
}

func newCyclesCycleListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Table of every cycle",
		Long: `Lists every cycle: ID | GOAL | RELEASE | STATUS | START | END.

Exit codes: 0 ok · 2 usage · 10 io_error

Example:
  orbit cycles cycle list --json`,
		Args:         usageArgs(cobra.NoArgs),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openCyclesVault()
			if err != nil {
				return err
			}
			rows, err := s.ListCycles()
			if err != nil {
				return err
			}
			if flagJSON {
				if rows == nil {
					rows = []cycles.Cycle{}
				}
				return writeJSON(cmd.OutOrStdout(), rows)
			}
			printCycleTable(cmd.OutOrStdout(), rows)
			return nil
		},
	}
	return cmd
}

func newCyclesCycleShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <C-####>",
		Short: "Print the full cycle note",
		Long: `Human output prints the cycle note verbatim (its markdown is the artifact);
--json emits the typed Cycle including its regenerated Bets view.

Exit codes: 0 ok · 2 usage · 5 not_found · 6 preflight_failed · 10 io_error

Example:
  orbit cycles cycle show C-0001 --json`,
		Args:         usageArgs(cobra.ExactArgs(1)),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openCyclesVault()
			if err != nil {
				return err
			}
			cy, err := s.ShowCycle(args[0])
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if flagJSON {
				return writeJSON(out, cy)
			}
			data, err := os.ReadFile(cy.Path)
			if err != nil {
				return exit.Wrap(exit.IOError, err, "cannot read "+cy.Path)
			}
			_, werr := out.Write(data)
			return werr
		},
	}
	return cmd
}

// printCycleTable renders the aligned ID | GOAL | RELEASE | STATUS | START | END
// table.
func printCycleTable(out io.Writer, rows []cycles.Cycle) {
	fmt.Fprintf(out, "%-7s %-32s %-10s %-8s %-12s %s\n",
		"ID", "GOAL", "RELEASE", "STATUS", "START", "END")
	for _, cy := range rows {
		fmt.Fprintf(out, "%-7s %-32s %-10s %-8s %-12s %s\n",
			cy.ID, truncate(cy.Goal, 30), cy.Release, cy.Status, cy.Start, cy.End)
	}
}
