package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/RTwoStudio/orbit/internal/exit"
	"github.com/RTwoStudio/orbit/internal/fsutil"
)

const compMarker = "# orbit shell completion"

// completionTarget describes where a shell's completion file lives and what
// loading it means for the user.
type completionTarget struct {
	shell    string
	path     string // resolved install path ("" = unsupported for auto-install)
	loadLine string // instruction printed after install
}

func newCompletionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate or install a shell completion script for orbit",
		Long: `With no argument (or with 'install'), installs completion for the
detected shell (or the one you name) into that shell's user completion
directory, idempotently. This is what the NeoCortex workflow expects:
neocortex quick <TAB> lists the verbs; quick start <TAB> lists open quick
runs.

With a shell name and --script, writes the raw completion script to stdout
instead (for manual sourcing).

Supported shells: bash, zsh, fish, powershell.

Exit codes: 0 ok · 2 usage · 7 state_conflict · 10 io_error

Examples:
  orbit completion                 # install for $SHELL (or bash)
  orbit completion zsh             # install for zsh explicitly
  orbit completion fish --script   # print the script, do not install
  orbit completion --uninstall`,
		Args:         usageArgs(cobra.MaximumNArgs(1)),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			switch {
			case compUninstall:
				return uninstallCompletion(cmd, args)
			case compScript:
				return writeRawCompletion(cmd, args)
			default:
				return installCompletion(cmd, args)
			}
		},
	}
	cmd.Flags().BoolVar(&compUninstall, "uninstall", false, "remove an installed completion script")
	cmd.Flags().BoolVar(&compScript, "script", false, "print the raw script to stdout instead of installing")
	return cmd
}

var (
	compUninstall bool
	compScript    bool
)

// detectedShell returns the explicit shell argument, else $SHELL's base name.
func detectedShell(args []string) (string, error) {
	shell := ""
	if len(args) == 1 {
		shell = args[0]
	} else {
		shell = filepath.Base(os.Getenv("SHELL"))
	}
	switch shell {
	case "bash", "zsh", "fish", "powershell":
		return shell, nil
	case "":
		return "", exit.New(exit.Usage,
			"cannot detect a shell ($SHELL is empty) — name one: orbit completion bash|zsh|fish|powershell")
	default:
		return "", exit.New(exit.Usage,
			fmt.Sprintf("unsupported shell %q — supported: bash, zsh, fish, powershell", shell))
	}
}

// generateCompletion renders the completion script for a shell into w.
func generateCompletion(root *cobra.Command, shell string, w io.Writer) error {
	switch shell {
	case "bash":
		return root.GenBashCompletionV2(w, true)
	case "zsh":
		return root.GenZshCompletion(w)
	case "fish":
		return root.GenFishCompletion(w, true)
	case "powershell":
		return root.GenPowerShellCompletionWithDesc(w)
	}
	return exit.New(exit.Usage, fmt.Sprintf("unsupported shell %q", shell))
}

// completionTargetFor resolves the install path + post-install hint.
func completionTargetFor(shell string) (completionTarget, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return completionTarget{}, exit.Wrap(exit.IOError, err, "cannot resolve home directory")
	}
	switch shell {
	case "zsh":
		base := os.Getenv("ZSH_COMPLETION_DIR")
		if base == "" {
			base = filepath.Join(home, ".zsh", "completions")
		}
		return completionTarget{
			shell: shell,
			path:  filepath.Join(base, "_orbit"),
			loadLine: fmt.Sprintf(
				"ensure it is on your fpath, e.g. add to ~/.zshrc:\n  fpath=(%s $fpath)\n  autoload -Uz compinit && compinit", base),
		}, nil
	case "bash":
		// Bash completion works without an explicit source line.
		base := os.Getenv("BASH_COMPLETION_USER_DIR")
		if base == "" {
			base = filepath.Join(home, ".local", "share", "bash-completion", "completions")
		}
		return completionTarget{
			shell: shell,
			path:  filepath.Join(base, "orbit"),
			loadLine: "bash-completion loads this automatically; otherwise add to ~/.bashrc:\n" +
				"  source " + filepath.Join(base, "orbit"),
		}, nil
	case "fish":
		base := os.Getenv("FISH_COMPLETION_DIR")
		if base == "" {
			base = filepath.Join(home, ".config", "fish", "completions")
		}
		return completionTarget{
			shell:    shell,
			path:     filepath.Join(base, "orbit.fish"),
			loadLine: "fish loads this automatically in new sessions",
		}, nil
	case "powershell":
		base := os.Getenv("POWERSHELL_COMPLETION_DIR")
		if base == "" {
			base = filepath.Join(home, ".config", "orbit", "completions")
		}
		return completionTarget{
			shell:    shell,
			path:     filepath.Join(base, "orbit.ps1"),
			loadLine: "dot-source it from your $PROFILE:\n  . " + filepath.Join(base, "orbit.ps1"),
		}, nil
	}
	return completionTarget{}, exit.New(exit.Usage, fmt.Sprintf("unsupported shell %q", shell))
}

func installCompletion(cmd *cobra.Command, args []string) error {
	shell, err := detectedShell(args)
	if err != nil {
		return err
	}
	target, err := completionTargetFor(shell)
	if err != nil {
		return err
	}
	var buf strings.Builder
	if err := generateCompletion(cmd.Root(), shell, &buf); err != nil {
		return err
	}
	if err := fsutil.AtomicWrite(target.path, []byte(buf.String()), 0o644); err != nil {
		return exit.Wrap(exit.IOError, err, "cannot write "+target.path)
	}
	out := cmd.OutOrStdout()
	if flagJSON {
		return writeJSON(out, map[string]any{"shell": shell, "path": target.path, "action": "installed"})
	}
	fmt.Fprintf(out, "Installed %s completion → %s\n", shell, target.path)
	if target.loadLine != "" {
		fmt.Fprintf(out, "  %s\n", target.loadLine)
	}
	fmt.Fprintln(out, "  start a new shell (or re-source) to pick it up.")
	return nil
}

func uninstallCompletion(cmd *cobra.Command, args []string) error {
	shell, err := detectedShell(args)
	if err != nil {
		return err
	}
	target, err := completionTargetFor(shell)
	if err != nil {
		return err
	}
	if !fsutil.Exists(target.path) {
		return exit.New(exit.StateConflict,
			fmt.Sprintf("no completion installed at %s", target.path))
	}
	if err := os.Remove(target.path); err != nil {
		return exit.Wrap(exit.IOError, err, "cannot remove "+target.path)
	}
	out := cmd.OutOrStdout()
	if flagJSON {
		return writeJSON(out, map[string]any{"shell": shell, "path": target.path, "action": "removed"})
	}
	fmt.Fprintf(out, "Removed %s completion (%s)\n", shell, target.path)
	return nil
}

func writeRawCompletion(cmd *cobra.Command, args []string) error {
	shell, err := detectedShell(args)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "%s\n", compMarker)
	return generateCompletion(cmd.Root(), shell, out)
}
