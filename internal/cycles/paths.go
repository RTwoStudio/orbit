// Package cycles implements the Cycles domain's work-item and cycle
// lifecycle layer.
//
// It is vault-rooted: a Store is opened from an explicit vault root (never
// cwd) and resolves every path under <vaultRoot>/Cycles/. It owns the work and
// cycle status transition tables, W-####/C-#### identity allocation and
// parsing, on-disk lookup, CURRENT read/write, the work verbs
// new/shape/bet/shelve/unshelve/deliver plus list/show, the cycle verbs
// new/close/list/show (including auto-shelve on close), and the status board.
// Cobra wiring and --json rendering are out of scope (T6).
package cycles

import (
	"path/filepath"

	"github.com/RTwoStudio/orbit/internal/fsutil"
)

// Store is a handle to a Cycles vault. Open never touches disk; methods do.
type Store struct {
	// Root is <vaultRoot>/Cycles — the vault's Cycles directory.
	Root string
}

// Open returns a Store rooted at <vaultRoot>/Cycles. No I/O is performed.
func Open(vaultRoot string) *Store {
	return &Store{Root: filepath.Join(vaultRoot, "Cycles")}
}

// CyclesDir returns <vaultRoot>/Cycles.
func (s *Store) CyclesDir() string { return s.Root }

// BacklogDir returns <vaultRoot>/Cycles/backlog — everything not committed to
// a cycle (Backlog, Pitched, Shelved, told apart by status).
func (s *Store) BacklogDir() string { return filepath.Join(s.Root, "backlog") }

// LedgerDir returns <vaultRoot>/Cycles/cycles — the directory holding every
// cycle folder (C-####).
func (s *Store) LedgerDir() string { return filepath.Join(s.Root, "cycles") }

// CycleDir returns <vaultRoot>/Cycles/cycles/<id>.
func (s *Store) CycleDir(id string) string { return filepath.Join(s.LedgerDir(), id) }

// CurrentPath returns <vaultRoot>/Cycles/CURRENT, the open-cycle pointer.
func (s *Store) CurrentPath() string { return filepath.Join(s.Root, "CURRENT") }

// IsInitialized reports whether the vault's Cycles/ directory exists (§5.0
// gate). T4 never creates the layout; that is `orbit cycles install` (T7).
func (s *Store) IsInitialized() bool { return fsutil.IsDir(s.Root) }

// WorkPath builds a work note's stable filename in dir: "W-#### - <slug>.md"
// (or "W-####.md" when slug is empty).
func WorkPath(dir, id, slug string) string {
	name := id + ".md"
	if slug != "" {
		name = id + " - " + slug + ".md"
	}
	return filepath.Join(dir, name)
}
