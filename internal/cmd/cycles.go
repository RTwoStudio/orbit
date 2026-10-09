package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/RTwoStudio/orbit/internal/config"
	"github.com/RTwoStudio/orbit/internal/cycles"
	"github.com/RTwoStudio/orbit/internal/exit"
)

// flagVault is the cycles group's persistent --vault override. It is
// inherited by every cycles subcommand; an empty value falls back to the
// configured vault.dir.
var flagVault string

func newCyclesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cycles",
		Short: "Cycles planning domain: work items, cycles, and the status board",
		Long: `Manage the Cycles planning and commitment domain.

Work items are captured, shaped, and bet into time-boxed cycles. The vault's
Markdown is the source of truth; NeoCortex consumes the handoff for execution.

Command groups:
  install   Bootstrap the vault: cache + deploy assets + create Cycles/ (alias: init)
  work      Capture and move work items (new/shape/bet/shelve/unshelve/deliver/list/show)
  cycle     Open and close cycles (new/close/list/show)
  status    Render the board: current cycle + backlog ladder

Exit codes:
  0 ok  1 general  2 usage  3 config_error  4 registry_unreachable
  5 not_found  6 preflight_failed  7 state_conflict  10 io_error
  11 not_initialized`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				// Unknown verb — mirror cobra's unknown-command error.
				return exit.New(exit.Usage,
					fmt.Sprintf("unknown command %q for %q", args[0], cmd.CommandPath()),
					"run: orbit cycles --help to see the verb tree")
			}
			return cmd.Help()
		},
	}
	cmd.PersistentFlags().StringVar(&flagVault, "vault", "",
		"vault root override (default: config vault.dir)")
	cmd.AddCommand(
		newCyclesInstallCmd(),
		newCyclesWorkCmd(),
		newCyclesCycleCmd(),
		newCyclesStatusCmd(),
	)
	return cmd
}

// resolveVault resolves the vault root for the cycles domain: the trimmed
// --vault override when set, else config vault.dir. A fully empty result is a
// config_error (3). The rule lives here so openCyclesVault and `cycles install`
// share exactly one implementation.
func resolveVault(cfg *config.Config) (string, error) {
	vault := flagVault
	if strings.TrimSpace(vault) == "" {
		vault = cfg.Vault.Dir
	}
	if strings.TrimSpace(vault) == "" {
		return "", exit.New(exit.ConfigError,
			"vault.dir is not configured",
			"set vault.dir in "+config.UserPath(flagConfig),
			"or pass --vault <dir>")
	}
	return vault, nil
}

// openCyclesVault resolves the vault root (--vault > config vault.dir) and
// returns a Store rooted at <vaultRoot>/Cycles. An empty resolved vault is a
// config_error (3).
func openCyclesVault() (*cycles.Store, error) {
	cfg, err := config.Load(flagConfig)
	if err != nil {
		return nil, err
	}
	vault, err := resolveVault(cfg)
	if err != nil {
		return nil, err
	}
	return cycles.Open(vault), nil
}
