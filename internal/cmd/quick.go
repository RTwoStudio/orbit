package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/RTwoStudio/orbit/internal/config"
	"github.com/RTwoStudio/orbit/internal/exit"
	"github.com/RTwoStudio/orbit/internal/neocortex"
)

func newQuickCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "quick",
		Short: "Light lane: one-sitting work in a single 00-quick.md",
		Long: `The quick lane is for work that fits one sitting: one file, one
implementer dispatch, no plan and no task DAG.

Lifecycle: Draft → In Progress → Revise → Rework → Close
(Rework → In Progress on resume). Close is refused while ## Result is empty.

Subcommands:
  new      Scaffold a quick run (interactive intake) and set ACTIVE
  import   Scaffold from a remote reference (GitHub issue/PR number or URL)
  ingest   Scaffold from a local file (verbatim)
  start    Draft → In Progress (also Rework → In Progress)
  revise   In Progress → Revise
  close    Revise → Close (requires a filled Result)
  rework   Revise → Rework
  promote  Convert to the default lane in place

Exit codes: 0 ok · 3 config_error · 4 registry_unreachable · 5 not_found
            6 preflight_failed · 7 state_conflict · 10 io_error

Example:
  orbit neocortex quick new "Retry limit on the client"`,
		SilenceUsage: true,
	}
	cmd.AddCommand(
		newQuickNewCmd(), newQuickImportCmd(), newQuickIngestCmd(),
		newQuickStartCmd(), newQuickReviseCmd(), newQuickCloseCmd(),
		newQuickReworkCmd(), newQuickPromoteCmd(),
	)
	return cmd
}

func newQuickNewCmd() *cobra.Command {
	return &cobra.Command{
		Use:   `new "<title>"`,
		Short: "Scaffold a quick run (interactive intake) and set it ACTIVE",
		Long: `Creates issues/issue-<n>/00-quick.md with Class: quick and Status:
Draft, then writes ACTIVE. Intent is injected as an agent-instruction
placeholder; the Orchestrator fills Intent/Approach/Checklist through
approved writes.

Exit codes: 0 ok · 4 registry_unreachable · 6 preflight_failed
            7 state_conflict · 10 io_error

Example:
  orbit neocortex quick new "Retry limit on the client"`,
		Args:         usageArgs(cobra.ExactArgs(1)),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runQuickNew(cmd, args[0], neocortex.IssueSource{Mode: "interactive"})
		},
	}
}

func newQuickImportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   `import <n|url> "<title>"`,
		Short: "Scaffold a quick run from a remote reference (Intent verbatim)",
		Long: `Fetches the remote content (GitHub issue/PR → API, 15s timeout; token
via token_env) and injects it VERBATIM into Intent. Nothing is written on
failure (exit 4).

Exit codes: 0 ok · 3 config_error · 4 registry_unreachable
            6 preflight_failed · 7 state_conflict · 10 io_error

Example:
  orbit neocortex quick import 42 "Retry limit on the client"`,
		Args:         usageArgs(cobra.ExactArgs(2)),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(flagConfig)
			if err != nil {
				return err
			}
			src := neocortex.IssueSource{Mode: "remote", RemoteURL: args[0], TokenEnv: cfg.Registry.TokenEnv}
			return runQuickNew(cmd, args[1], src)
		},
	}
}

func newQuickIngestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   `ingest <path> "<title>"`,
		Short: "Scaffold a quick run from a local file (Intent verbatim)",
		Long: `Reads the file and injects its content VERBATIM into Intent.

Exit codes: 0 ok · 5 not_found · 6 preflight_failed · 7 state_conflict
            10 io_error

Example:
  orbit neocortex quick ingest notes.md "Retry limit on the client"`,
		Args:         usageArgs(cobra.ExactArgs(2)),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runQuickNew(cmd, args[1], neocortex.IssueSource{Mode: "file", FilePath: args[0]})
		},
	}
}

func runQuickNew(cmd *cobra.Command, title string, src neocortex.IssueSource) error {
	res, err := neocortex.NewQuick(title, src)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if flagJSON {
		return json.NewEncoder(out).Encode(map[string]any{
			"issue": res.Number, "path": res.Path, "class": "quick",
		})
	}
	fmt.Fprintf(out, "Created quick issue-%d (%s)\n", res.Number, res.Path)
	fmt.Fprintln(out, "Next: fill Intent/Approach/Checklist, then: orbit neocortex quick start <n>")
	return nil
}

func newQuickStartCmd() *cobra.Command {
	return newQuickTransitionCmd("start <n>", "Draft → In Progress (also Rework → In Progress)", neocortex.QuickInProgress)
}

func newQuickReviseCmd() *cobra.Command {
	return newQuickTransitionCmd("revise <n>", "In Progress → Revise", neocortex.QuickRevise)
}

func newQuickCloseCmd() *cobra.Command {
	return newQuickTransitionCmd("close <n>", "Revise → Close (requires a filled Result)", neocortex.QuickClose)
}

func newQuickReworkCmd() *cobra.Command {
	return newQuickTransitionCmd("rework <n>", "Revise → Rework", neocortex.QuickRework)
}

func newQuickTransitionCmd(use, short string, to neocortex.QuickStatus) *cobra.Command {
	return &cobra.Command{
		Use:          use,
		Short:        short,
		Args:         usageArgs(cobra.ExactArgs(1)),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := parseIssueArg(args[0])
			if err != nil {
				return err
			}
			from, err := neocortex.SetQuickStatus(n, to)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if flagJSON {
				return json.NewEncoder(out).Encode(map[string]any{
					"issue": n, "from": from.FileValue(), "to": to.FileValue(),
				})
			}
			fmt.Fprintf(out, "quick issue-%d: %s → %s\n", n, from.FileValue(), to.FileValue())
			return nil
		},
	}
}

func newQuickPromoteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "promote <n>",
		Short: "Convert a quick run to the default lane in place",
		Long: `Converts issue-N from the quick lane to the default lane without
moving it: 00-concept.md is rendered from the quick Intent (Detail notes the
promotion), 01-plan.md and the tasks/addenda/notes dirs are created, and
00-quick.md is removed. Then continue with the normal Issue/Plan procedures.

Exit codes: 0 ok · 4 registry_unreachable · 5 not_found · 7 state_conflict
            10 io_error

Example:
  orbit neocortex quick promote 43`,
		Args:         usageArgs(cobra.ExactArgs(1)),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := parseIssueArg(args[0])
			if err != nil {
				return err
			}
			path, err := neocortex.PromoteQuick(n)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if flagJSON {
				return json.NewEncoder(out).Encode(map[string]any{"issue": n, "path": path})
			}
			fmt.Fprintf(out, "Promoted issue-%d to the default lane — concept created at %s\n", n, path)
			fmt.Fprintln(out, "Next: fill the concept, then: orbit neocortex issue lock")
			return nil
		},
	}
}

var _ = exit.OK
