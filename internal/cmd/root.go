// Package cmd wires the cobra command tree with the logging/exit wrapper.
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/RTwoStudio/orbit-cli/internal/config"
	"github.com/RTwoStudio/orbit-cli/internal/exit"
	"github.com/RTwoStudio/orbit-cli/internal/logx"
	"github.com/RTwoStudio/orbit-cli/internal/ui"
)

// version is set at build time via -ldflags.
var version = "0.1.0"

// Flags shared across the tree.
var (
	flagConfig  string
	flagNoColor bool
	flagYes     bool
	flagJSON    bool
)

// Run wraps Execute with the mandatory logging wrapper: start line before,
// end line after (including panics), all exits through internal/exit.
func Run() {
	defer func() {
		if r := recover(); r != nil {
			logx.Error("panic in %s: %v", os.Args[1:], r)
			endLog(1, time.Now())
			fmt.Fprintln(os.Stderr, "error: unexpected internal failure (see log)")
			os.Exit(int(exit.General))
		}
	}()
	start := time.Now()
	logx.Info("start cmd=%q version=%s pid=%d", strings.Join(os.Args, " "), version, os.Getpid())
	code := execute(start)
	os.Exit(int(code))
}

func execute(start time.Time) exit.Code {
	root := NewRootCmd()
	root.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		return exit.New(exit.Usage, err.Error(), "run with --help to see valid flags")
	})
	err := root.Execute()
	if err == nil {
		endLog(0, start)
		return exit.OK
	}
	if ce, ok := err.(*exit.Error); ok {
		endLog(int(ce.Code), start)
		exit.Print(ce)
		return ce.Code
	}
	if isUsageError(err) {
		endLog(int(exit.Usage), start)
		exit.Print(exit.New(exit.Usage, err.Error(), "run with --help to see usage"))
		return exit.Usage
	}
	endLog(1, start)
	exit.Print(err)
	return exit.General
}

// isUsageError classifies cobra's own arg/command validation errors as
// usage-class (exit 2). Flag parse errors are already wrapped via
// FlagErrorFunc; Args validators go through usageArgs.
func isUsageError(err error) bool {
	msg := err.Error()
	return strings.HasPrefix(msg, "unknown command ") ||
		strings.HasPrefix(msg, "unknown flag: ") ||
		strings.HasPrefix(msg, "unknown shorthand flag: ") ||
		strings.Contains(msg, " arg(s), received ") ||
		strings.Contains(msg, "requires at least ")
}

// usageArgs wraps a cobra positional-args validator so violations carry
// the usage exit code.
func usageArgs(fn cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := fn(cmd, args); err != nil {
			return exit.New(exit.Usage, err.Error(), "run with --help for usage")
		}
		return nil
	}
}

func endLog(code int, start time.Time) {
	logx.Info("end cmd=%q exit=%d duration=%s", strings.Join(os.Args, " "), code, time.Since(start).Round(time.Millisecond))
}

// NewRootCmd builds the full command tree.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:     "orbit",
		Short:   "Orbit CLI — deterministic filesystem engine for the NeoCortex workflow",
		Version: version,
		Long: `orbit is the execution engine of the NeoCortex development workflow.

It manages a remote asset registry (agents, commands, stubs), scaffolds
issue/plan/task artifacts under .neocortex/, and enforces the status state
machine with hash-verified locks.

Run 'orbit neocortex --help' for the full verb tree.`,
		SilenceUsage:  true,
		SilenceErrors: true, // we print via internal/exit
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			// Color policy: config ui.color + NO_COLOR + --no-color.
			// Best-effort config read; unreadable config falls back to auto.
			cfg, _ := config.Load(flagConfig)
			ui.Setup(cfg.UI.Color, flagNoColor)
		},
	}
	p := root.PersistentFlags()
	p.StringVar(&flagConfig, "config", "", "path to user config (default ~/.config/orbit/config.yml)")
	p.BoolVar(&flagNoColor, "no-color", false, "disable colored output")
	p.BoolVarP(&flagYes, "yes", "y", false, "auto-confirm prompts (accept defaults)")
	p.BoolVar(&flagJSON, "json", false, "machine-readable JSON output on stdout (human text on stderr)")

	root.AddCommand(newNeoCortexCmd())
	return root
}

// isTTY reports whether stdout is a terminal.
func isTTY() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// stdinIsTTY reports whether stdin is a real terminal (not /dev/null).
func stdinIsTTY() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// projectName returns the base name of the current working directory.
func projectName() string {
	wd, err := os.Getwd()
	if err != nil {
		return "project"
	}
	return filepath.Base(wd)
}
