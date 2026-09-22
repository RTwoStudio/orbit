// Package prompt implements the TTY/flag/CI prompt policy (§11):
// --yes → default; non-TTY → default + announce; TTY → real prompt.
package prompt

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/orbit-sh/orbit-cli/internal/logx"
	"golang.org/x/term"
)

// Confirm asks a yes/no question. yesFlag forces the default; a non-TTY
// stdin takes the default and prints the decision taken.
func Confirm(msg string, def bool, yesFlag bool) bool {
	if yesFlag {
		return def
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprintf(os.Stderr, "%s [y/n] → %s (non-interactive default)\n", msg, yn(def))
		return def
	}
	suffix := " [Y/n] "
	if !def {
		suffix = " [y/N] "
	}
	fmt.Fprintf(os.Stderr, "%s%s", msg, suffix)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return def
	}
	ans := strings.ToLower(strings.TrimSpace(line))
	switch ans {
	case "y", "yes":
		return true
	case "n", "no":
		return false
	case "":
		return def
	}
	logx.Debug("unrecognized prompt answer %q — using default", ans)
	return def
}

func yn(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
