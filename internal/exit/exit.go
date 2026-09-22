// Package exit centralizes exit codes and user-facing error printing.
// All exits must route through Exit — no os.Exit anywhere else — so the
// log wrapper can always record the final exit code.
package exit

import (
	"fmt"
	"os"

	"github.com/RTwoStudio/orbit-cli/internal/ui"
)

// Code is an orbit exit code.
type Code int

const (
	OK                  Code = 0
	General             Code = 1
	Usage               Code = 2
	ConfigError         Code = 3
	RegistryUnreachable Code = 4
	NotFound            Code = 5
	PreflightFailed     Code = 6
	StateConflict       Code = 7
	TamperDetected      Code = 8
	NoActiveRun         Code = 9
	IOError             Code = 10
	NotInitialized      Code = 11
)

// Name returns the canonical exit-code name used in logs and messages.
func (c Code) Name() string {
	switch c {
	case OK:
		return "ok"
	case General:
		return "general"
	case Usage:
		return "usage"
	case ConfigError:
		return "config_error"
	case RegistryUnreachable:
		return "registry_unreachable"
	case NotFound:
		return "not_found"
	case PreflightFailed:
		return "preflight_failed"
	case StateConflict:
		return "state_conflict"
	case TamperDetected:
		return "tamper_detected"
	case NoActiveRun:
		return "no_active_run"
	case IOError:
		return "io_error"
	case NotInitialized:
		return "not_initialized"
	}
	return fmt.Sprintf("unknown(%d)", int(c))
}

// Error is an error carrying an exit code and optional hints.
type Error struct {
	Code    Code
	Message string
	Hints   []string
}

func (e *Error) Error() string { return e.Message }

// New builds a coded error.
func New(code Code, message string, hints ...string) *Error {
	return &Error{Code: code, Message: message, Hints: hints}
}

// Wrap builds a coded error from an underlying cause.
func Wrap(code Code, err error, message string, hints ...string) *Error {
	return &Error{Code: code, Message: fmt.Sprintf("%s: %v", message, err), Hints: hints}
}

// Print renders "error: ..." plus up to two "hint:" lines to stderr.
// Styling follows the ui color policy (auto/always/never + --no-color).
func Print(err error) {
	e, ok := err.(*Error)
	if !ok {
		fmt.Fprintf(os.Stderr, "%s %v\n", ui.Red("error:"), err)
		return
	}
	fmt.Fprintf(os.Stderr, "%s %s\n", ui.Red("error:"), e.Message)
	for i, h := range e.Hints {
		if i >= 2 {
			break
		}
		fmt.Fprintf(os.Stderr, "%s %s\n", ui.Dim("hint:"), h)
	}
}
