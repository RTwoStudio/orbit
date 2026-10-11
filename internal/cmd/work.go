package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/RTwoStudio/orbit/internal/cycles"
	"github.com/RTwoStudio/orbit/internal/exit"
)

func newCyclesWorkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "work",
		Short: "Capture and move work items (new/shape/bet/shelve/unshelve/deliver/sync/list/show)",
		Long: `Manage work items across the flat workflow:

  Backlog → Pitched → Bet → Delivered, with Shelved reachable from the
  pre-bet/bet stages; unshelve returns a Shelved item to Pitched.

Work ids are owner-namespaced: W-<seq>-<owner-slug> (e.g.
W-0001-arash-jafarpour). A legacy bare W-#### still resolves read-only.

Subcommands:
  new "<title>" --scope <s>                Capture a Backlog item
  shape <W-####-<owner-slug>> --appetite big|small   Backlog → Pitched (needs filled shape)
  bet <W-####-<owner-slug>> --assignee <handle>[,<handle>...]   Pitched → Bet into the open cycle
  shelve <W-####-<owner-slug>>             Park an item (→ Shelved)
  unshelve <W-####-<owner-slug>>           Shelved → Pitched
  deliver <W-####-<owner-slug>>            Bet → Delivered (terminal)
  sync <W-####-<owner-slug>> [--project <dir>]   Sync a Bet's issue to the forge (network)
  list [--status <s>] [--scope <s>]        Table of work items (--json)
  show <W-####-<owner-slug>>               Print the work note (--json)

Exit codes:
  0 ok  2 usage  3 config_error  4 registry_unreachable  5 not_found
  6 preflight_failed  7 state_conflict  10 io_error  11 not_initialized`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return exit.New(exit.Usage,
					fmt.Sprintf("unknown command %q for %q", args[0], cmd.CommandPath()),
					"run: orbit cycles work --help to see the verb tree")
			}
			return cmd.Help()
		},
	}
	cmd.AddCommand(
		newCyclesWorkNewCmd(),
		newCyclesWorkShapeCmd(),
		newCyclesWorkBetCmd(),
		newCyclesWorkShelveCmd(),
		newCyclesWorkUnshelveCmd(),
		newCyclesWorkDeliverCmd(),
		newCyclesWorkSyncCmd(),
		newCyclesWorkListCmd(),
		newCyclesWorkShowCmd(),
	)
	return cmd
}

func newCyclesWorkNewCmd() *cobra.Command {
	var scope string
	cmd := &cobra.Command{
		Use:   `new "<title>" --scope <scope>`,
		Short: "Capture a new work item in the backlog (Backlog)",
		Long: `Creates a work item in the vault's backlog/ with the next free
W-<seq>-<owner-slug> id for the local developer. The handle is resolved from
the vault clone's git identity matched against Cycles/developers.yml.

Preflight: title and scope must be non-empty (exit 6); the vault must be
initialized (exit 11); the cycles registry cache must be populated (exit 4);
the roster must be present and the git identity must match an entry (exit 6).

Exit codes: 0 ok · 2 usage · 4 registry_unreachable · 6 preflight_failed
            10 io_error · 11 not_initialized

Example:
  orbit cycles work new "Cancellation flow" --scope delijan-driver-app`,
		Args:         usageArgs(cobra.ExactArgs(1)),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openCyclesVault()
			if err != nil {
				return err
			}
			item, err := s.NewWork(args[0], scope)
			if err != nil {
				return err
			}
			if flagJSON {
				return writeJSON(cmd.OutOrStdout(), item)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created %s (%s)\n", item.ID, item.Status)
			return nil
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "", "project scope (required)")
	return cmd
}

func newCyclesWorkShapeCmd() *cobra.Command {
	var appetite string
	cmd := &cobra.Command{
		Use:   "shape <W-####-<owner-slug>> --appetite big|small",
		Short: "Move a Backlog item to Pitched (requires the shape sections)",
		Long: `Stamps status Pitched and the chosen appetite.

Preflight: the item must be Backlog (exit 7 otherwise); appetite must be
big or small (exit 6); Problem and Solution Sketch must both be filled
(exit 6).

Exit codes: 0 ok · 2 usage · 5 not_found · 6 preflight_failed
            7 state_conflict · 10 io_error

Example:
  orbit cycles work shape W-0003-arash-jafarpour --appetite small`,
		Args:         usageArgs(cobra.ExactArgs(1)),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openCyclesVault()
			if err != nil {
				return err
			}
			item, err := s.ShapeWork(args[0], appetite)
			if err != nil {
				return err
			}
			if flagJSON {
				return writeJSON(cmd.OutOrStdout(), item)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s → %s\n", item.ID, item.Status)
			return nil
		},
	}
	cmd.Flags().StringVar(&appetite, "appetite", "", "appetite: big | small (required)")
	return cmd
}

func newCyclesWorkBetCmd() *cobra.Command {
	var assignees []string
	cmd := &cobra.Command{
		Use:   "bet <W-####-<owner-slug>> --assignee <handle>[,<handle>...]",
		Short: "Commit a Pitched item to the open cycle (Pitched → Bet)",
		Long: `Pitched → Bet: moves the item into the open cycle and stamps its
` + "`assignees:`" + ` list. At least one --assignee is required; every handle
must exist in the developer roster (Cycles/developers.yml). --assignee accepts a
comma-separated list and may be repeated; handles are deduped, first-seen order
preserved.

Preflight: the item must be Pitched (exit 7 otherwise); CURRENT must point at
an open cycle (exit 7 without one); at least one assignee is required (exit 6);
an empty or unknown handle is exit 6.

Exit codes: 0 ok · 2 usage · 5 not_found · 6 preflight_failed
            7 state_conflict · 10 io_error

Example:
  orbit cycles work bet W-0003-arash-jafarpour --assignee arash-jafarpour,seyed-mahdi-rostami`,
		Args:         usageArgs(cobra.ExactArgs(1)),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openCyclesVault()
			if err != nil {
				return err
			}
			item, err := s.BetWork(args[0], assignees)
			if err != nil {
				return err
			}
			if flagJSON {
				return writeJSON(cmd.OutOrStdout(), item)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s → %s\n", item.ID, item.Status)
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&assignees, "assignee", nil,
		"roster handle(s) to assign (comma-separated, repeatable; required)")
	return cmd
}

func newCyclesWorkShelveCmd() *cobra.Command {
	return newCyclesWorkVerbCmd("shelve", "Park a pre-bet/bet item (→ Shelved)",
		"Preflight: the item must be Backlog, Pitched, or Bet (exit 7 otherwise).\nAn item living in a cycle is moved back to backlog/.",
		(*cycles.Store).ShelveWork)
}

func newCyclesWorkUnshelveCmd() *cobra.Command {
	return newCyclesWorkVerbCmd("unshelve", "Return a Shelved item to Pitched (Shelved → Pitched)",
		"Preflight: the item must be Shelved (exit 7 otherwise). It stays in backlog/.",
		(*cycles.Store).UnshelveWork)
}

func newCyclesWorkDeliverCmd() *cobra.Command {
	return newCyclesWorkVerbCmd("deliver", "Mark a Bet item Delivered (terminal)",
		"Preflight: the item must be Bet (exit 7 otherwise). The note stays in\nits cycle folder as that cycle's permanent history.",
		(*cycles.Store).DeliverWork)
}

// newCyclesWorkVerbCmd builds one single-item transition verb: it resolves the
// id, applies the Store method, and renders the resulting *WorkItem.
func newCyclesWorkVerbCmd(verb, short, preflight string, method func(*cycles.Store, string) (*cycles.WorkItem, error)) *cobra.Command {
	return &cobra.Command{
		Use:   verb + " <W-####-<owner-slug>>",
		Short: short,
		Long: preflight + `

Exit codes: 0 ok · 2 usage · 5 not_found · 6 preflight_failed
            7 state_conflict · 10 io_error

Example:
  orbit cycles work ` + verb + ` W-0003-arash-jafarpour`,
		Args:         usageArgs(cobra.ExactArgs(1)),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openCyclesVault()
			if err != nil {
				return err
			}
			item, err := method(s, args[0])
			if err != nil {
				return err
			}
			if flagJSON {
				return writeJSON(cmd.OutOrStdout(), item)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s → %s\n", item.ID, item.Status)
			return nil
		},
	}
}

func newCyclesWorkListCmd() *cobra.Command {
	var status, scope string
	cmd := &cobra.Command{
		Use:   "list [--status <s>] [--scope <s>]",
		Short: "Table of work items across backlog/ and every cycle",
		Long: `Lists every work item: ID | TITLE | STATUS | SCOPE | CYCLE.

--status   filter (case-insensitive): Backlog | Pitched | Bet | Shelved | Delivered
--scope    filter by exact scope match

Exit codes: 0 ok · 2 usage · 6 preflight_failed · 10 io_error

Example:
  orbit cycles work list --status Pitched --json`,
		Args:         usageArgs(cobra.NoArgs),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openCyclesVault()
			if err != nil {
				return err
			}
			items, err := s.ListWork(status, scope)
			if err != nil {
				return err
			}
			if flagJSON {
				if items == nil {
					items = []cycles.WorkItem{}
				}
				return writeJSON(cmd.OutOrStdout(), items)
			}
			printWorkTable(cmd.OutOrStdout(), items)
			return nil
		},
	}
	cmd.Flags().StringVar(&status, "status", "", "filter by status (case-insensitive)")
	cmd.Flags().StringVar(&scope, "scope", "", "filter by exact scope")
	return cmd
}

func newCyclesWorkShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <W-####-<owner-slug>>",
		Short: "Print the full work note",
		Long: `Human output prints the work note verbatim (its markdown is the artifact);
--json emits the typed WorkItem. A legacy bare W-#### is accepted read-only.

Exit codes: 0 ok · 2 usage · 5 not_found · 6 preflight_failed · 10 io_error

Example:
  orbit cycles work show W-0003-arash-jafarpour --json`,
		Args:         usageArgs(cobra.ExactArgs(1)),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openCyclesVault()
			if err != nil {
				return err
			}
			item, err := s.ShowWork(args[0])
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if flagJSON {
				return writeJSON(out, item)
			}
			data, err := os.ReadFile(item.Path)
			if err != nil {
				return exit.Wrap(exit.IOError, err, "cannot read "+item.Path)
			}
			_, werr := out.Write(data)
			return werr
		},
	}
	return cmd
}

// printWorkTable renders the aligned ID | TITLE | STATUS | SCOPE | CYCLE table.
// The ID column is wide enough for a composite owner-namespaced id.
func printWorkTable(out io.Writer, items []cycles.WorkItem) {
	fmt.Fprintf(out, "%-30s %-32s %-10s %-22s %s\n",
		"ID", "TITLE", "STATUS", "SCOPE", "CYCLE")
	for _, it := range items {
		fmt.Fprintf(out, "%-30s %-32s %-10s %-22s %s\n",
			it.ID, truncate(it.Title, 30), it.Status, truncate(it.Scope, 20), it.Cycle)
	}
}
