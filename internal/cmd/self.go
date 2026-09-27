package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/RTwoStudio/orbit/internal/exit"
	"github.com/RTwoStudio/orbit/internal/selfupdate"
)

func newSelfCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "self",
		Short: "Manage the orbit binary itself",
		Long: `Self-management of the orbit binary. Currently: update.

Unlike 'orbit update' (which refreshes registry assets), 'orbit self update'
replaces the running binary with the latest GitHub release. It downloads the
release asset, verifies the checksum, and atomically renames the new binary
over the current one — safe even while orbit is running, because the running
process keeps its old inode until it exits. The next invocation is the new
version.`,
		SilenceUsage: true,
	}
	cmd.AddCommand(newSelfUpdateCmd())
	return cmd
}

func newSelfUpdateCmd() *cobra.Command {
	var (
		check bool
		force bool
	)
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Replace this binary with the latest GitHub release",
		Long: `Fetches the latest release of the orbit repo, verifies its checksum, and
atomically swaps it in place. Works in any directory. Linux only for now;
Windows raises a clear error.

Source overrides (mirroring install.sh): ORBIT_GH_REPO, ORBIT_GH_API,
ORBIT_GH_DL, plus ORBIT_GITHUB_TOKEN for private repos / rate limits.
Binary location: the running executable (symlinks resolved), or
ORBIT_BIN_DIR/<orbit> when set.

Exit codes: 0 ok (incl. already current) · 4 registry_unreachable
            7 state_conflict (not writable / downgrade refused / Windows)
            10 io_error

Examples:
  orbit self update            # update if a newer release exists
  orbit self update --check    # report current vs latest, change nothing
  orbit self update --force    # reinstall even if same or newer`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := selfupdate.DefaultConfig(version)
			if err != nil {
				return err
			}
			client := selfupdate.DefaultClient()

			info, err := cfg.Lookup(client)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()

			if check {
				if flagJSON {
					return writeJSON(out, info)
				}
				switch {
				case info.UpToDate:
					fmt.Fprintf(out, "orbit %s is up to date (latest %s)\n", info.Current, info.Latest)
				case info.Newer:
					fmt.Fprintf(out, "orbit %s is newer than latest release %s\n", info.Current, info.Latest)
				default:
					fmt.Fprintf(out, "update available: %s → %s\n", info.Current, info.Latest)
				}
				return nil
			}

			if info.UpToDate && !force {
				if flagJSON {
					return writeJSON(out, map[string]any{"status": "up_to_date", "version": info.Current})
				}
				fmt.Fprintf(out, "orbit %s is already the latest version — nothing to do\n", info.Current)
				return nil
			}
			if info.Newer && !force {
				return exit.New(exit.StateConflict,
					fmt.Sprintf("installed %s is newer than latest release %s — refusing to downgrade", info.Current, info.Latest),
					"pass --force to replace it anyway")
			}

			from := info.Current
			if !flagJSON {
				fmt.Fprintf(out, "updating orbit %s → %s (%s)\n", from, info.Latest, info.Asset)
			}
			if err := cfg.Apply(client, info, force); err != nil {
				return err
			}
			if flagJSON {
				return writeJSON(out, map[string]any{
					"status": "updated", "from": from, "to": info.Latest, "path": info.BinPath,
				})
			}
			fmt.Fprintf(out, "orbit %s installed at %s\n", info.Latest, info.BinPath)
			fmt.Fprintln(out, "  (the running process keeps the old inode; the next run uses the new binary)")
			return nil
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "report current vs latest and exit; change nothing")
	cmd.Flags().BoolVar(&force, "force", false, "reinstall even if same version or a newer build is present")
	return cmd
}

var _ = exit.OK
