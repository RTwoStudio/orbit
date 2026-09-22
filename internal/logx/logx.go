// Package logx implements orbit's file logger: pipe-delimited lines,
// size-based rotation, level gating via ORBIT_DEBUG, and best-effort
// fallback to stderr when the log dir is unwritable.
package logx

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	maxSize   = 5 * 1024 * 1024
	maxShifts = 3 // orbit.log.1 .. orbit.log.3
)

var (
	mu       sync.Mutex
	file     *os.File
	disabled bool
)

// Dir returns the active log directory ($ORBIT_LOG_DIR or
// ~/.config/orbit/log).
func Dir() string {
	if d := os.Getenv("ORBIT_LOG_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "orbit", "log")
}

// Path returns the active log file path.
func Path() string {
	d := Dir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "orbit.log")
}

func debugEnabled() bool { return os.Getenv("ORBIT_DEBUG") == "1" }

// open lazily opens the log file, rotating if needed. Best-effort: on any
// failure the logger goes silent (stderr-only handled by callers).
func open() error {
	if file != nil {
		return nil
	}
	dir := Dir()
	if dir == "" {
		disabled = true
		return fmt.Errorf("no log dir")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		disabled = true
		return err
	}
	path := filepath.Join(dir, "orbit.log")
	if fi, err := os.Stat(path); err == nil && fi.Size() >= maxSize {
		rotate(dir)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		disabled = true
		return err
	}
	file = f
	return nil
}

// rotate shifts orbit.log -> orbit.log.1 -> ... -> orbit.log.N (delete last).
func rotate(dir string) {
	// Drop the oldest first, then shift downward from high to low so
	// nothing is clobbered.
	os.Remove(filepath.Join(dir, fmt.Sprintf("orbit.log.%d", maxShifts)))
	for i := maxShifts; i >= 1; i-- {
		src := filepath.Join(dir, "orbit.log")
		if i > 1 {
			src = filepath.Join(dir, fmt.Sprintf("orbit.log.%d", i-1))
		}
		dst := filepath.Join(dir, fmt.Sprintf("orbit.log.%d", i))
		data, err := os.ReadFile(src)
		if err != nil {
			continue
		}
		os.WriteFile(dst, data, 0o644)
	}
	os.Remove(filepath.Join(dir, "orbit.log"))
}

// Write emits one structured line at the given level. DEBUG lines are
// dropped unless ORBIT_DEBUG=1. Never fails the caller.
func Write(level, msg string) {
	if level == "DEBUG" && !debugEnabled() {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	if disabled {
		return
	}
	if err := open(); err != nil {
		return
	}
	ts := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	line := fmt.Sprintf("%s | %-5s | %s\n", ts, level, msg)
	// Single Write syscall to avoid interleaving.
	file.WriteString(line)
}

// Info logs at INFO level.
func Info(format string, args ...any) { Write("INFO", fmt.Sprintf(format, args...)) }

// Error logs at ERROR level.
func Error(format string, args ...any) { Write("ERROR", fmt.Sprintf(format, args...)) }

// Debug logs at DEBUG level (gated).
func Debug(format string, args ...any) { Write("DEBUG", fmt.Sprintf(format, args...)) }

// Close closes the underlying file (for tests).
func Close() {
	mu.Lock()
	defer mu.Unlock()
	if file != nil {
		file.Close()
		file = nil
	}
	disabled = false
}
