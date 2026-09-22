// Package ui resolves the color policy and provides minimal ANSI
// helpers. Policy: ui.color from config ("auto"|"always"|"never"),
// overridden by the NO_COLOR env convention and the --no-color flag.
// "auto" colors only when stdout is a real terminal.
package ui

import (
	"os"
	"strings"

	"golang.org/x/term"
)

var enabled bool

// Setup resolves the color policy once at startup. cfgColor comes from
// config ui.color; noColorFlag is the --no-color flag (always wins).
func Setup(cfgColor string, noColorFlag bool) {
	switch strings.ToLower(strings.TrimSpace(cfgColor)) {
	case "never":
		enabled = false
	case "always":
		enabled = true
	default: // auto (and any unknown value)
		enabled = term.IsTerminal(int(os.Stdout.Fd())) && os.Getenv("NO_COLOR") == ""
	}
	if noColorFlag {
		enabled = false
	}
}

// Enabled reports whether ANSI styling may be emitted.
func Enabled() bool { return enabled }

const (
	ansiRed   = "\x1b[31m"
	ansiDim   = "\x1b[2m"
	ansiBold  = "\x1b[1m"
	ansiReset = "\x1b[0m"
)

func wrap(code, s string) string {
	if !enabled || s == "" {
		return s
	}
	return code + s + ansiReset
}

// Red highlights error markers.
func Red(s string) string { return wrap(ansiRed, s) }

// Dim de-emphasizes secondary output (hints, labels).
func Dim(s string) string { return wrap(ansiDim, s) }

// Bold emphasizes primary output.
func Bold(s string) string { return wrap(ansiBold, s) }
