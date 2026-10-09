package cmd

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/RTwoStudio/orbit/internal/cycles"
)

func newCyclesStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Render the board: current cycle + backlog ladder",
		Long: `Renders the status board: the open cycle (or "No open cycle.") and the
labelled Backlog / Pitched / Shelved blocks. Read-only: it never mutates the
vault.

--json emits the typed StatusBoard ({current, backlog}).

Exit codes: 0 ok · 2 usage · 3 config_error · 5 not_found · 10 io_error

Example:
  orbit cycles status --json`,
		Args:         usageArgs(cobra.NoArgs),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openCyclesVault()
			if err != nil {
				return err
			}
			board, err := s.Status()
			if err != nil {
				return err
			}
			if flagJSON {
				return writeJSON(cmd.OutOrStdout(), board)
			}
			printStatusBoard(cmd.OutOrStdout(), board)
			return nil
		},
	}
	return cmd
}

// printStatusBoard renders the human status board.
func printStatusBoard(out io.Writer, board *cycles.StatusBoard) {
	if board.Current == nil {
		fmt.Fprintln(out, "No open cycle.")
	} else {
		c := board.Current
		fmt.Fprintf(out, "Current cycle: %s — %s\n", c.ID, c.Goal)
		fmt.Fprintf(out, "  release: %s · status: %s · %s → %s\n",
			c.Release, c.Status, c.Start, c.End)
		fmt.Fprintln(out, "  bets:")
		printWorkItems(out, c.Bets)
	}
	fmt.Fprintln(out)
	printWorkBlock(out, "Backlog", board.Backlog.Backlog)
	printWorkBlock(out, "Pitched", board.Backlog.Pitched)
	printWorkBlock(out, "Shelved", board.Backlog.Shelved)
}

// printWorkBlock renders one labelled ladder block.
func printWorkBlock(out io.Writer, label string, items []cycles.WorkItem) {
	fmt.Fprintf(out, "%s:\n", label)
	printWorkItems(out, items)
}

// printWorkItems renders one indented bullet per item, or "(none)".
func printWorkItems(out io.Writer, items []cycles.WorkItem) {
	if len(items) == 0 {
		fmt.Fprintln(out, "  (none)")
		return
	}
	for _, it := range items {
		fmt.Fprintf(out, "  %s — %s (%s)\n", it.ID, it.Title, it.Status)
	}
}
