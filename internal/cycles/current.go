package cycles

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/RTwoStudio/orbit/internal/exit"
	"github.com/RTwoStudio/orbit/internal/fsutil"
)

// currentCycleRe validates the cycle id held in the CURRENT pointer. T5 owns
// the exported cycle-id API; this local grammar exists only to reject a
// malformed pointer at read time.
var currentCycleRe = regexp.MustCompile(`^C-\d{4,}$`)

// ReadCurrent returns the open cycle id from the vault's CURRENT pointer.
// Read-only: T5 owns WriteCurrent/ClearCurrent.
//
// A missing, empty, malformed, or dangling pointer is a state_conflict (7)
// carrying a `orbit cycles cycle new` hint.
func (s *Store) ReadCurrent() (string, error) {
	data, err := os.ReadFile(s.CurrentPath())
	if err != nil {
		if os.IsNotExist(err) {
			return "", exit.New(exit.StateConflict,
				"no open cycle — bet requires a current cycle",
				"run: orbit cycles cycle new")
		}
		return "", exit.Wrap(exit.IOError, err, "cannot read "+s.CurrentPath())
	}
	id := strings.TrimSpace(string(data))
	if id == "" {
		return "", exit.New(exit.StateConflict,
			"no open cycle — CURRENT is empty",
			"run: orbit cycles cycle new")
	}
	if !currentCycleRe.MatchString(id) {
		return "", exit.New(exit.StateConflict,
			fmt.Sprintf("CURRENT holds %q, which is not a cycle id", id),
			"run: orbit cycles cycle new")
	}
	if !fsutil.IsDir(s.CycleDir(id)) {
		return "", exit.New(exit.StateConflict,
			fmt.Sprintf("CURRENT points to missing cycle %s", id),
			"run: orbit cycles cycle new")
	}
	return id, nil
}

// WriteCurrent points CURRENT at an existing cycle. The id must be a valid
// C-#### and its cycle directory must already exist.
func (s *Store) WriteCurrent(id string) error {
	id, err := ParseCycleID(id)
	if err != nil {
		return err
	}
	if !fsutil.IsDir(s.CycleDir(id)) {
		return exit.New(exit.StateConflict,
			fmt.Sprintf("cannot set CURRENT to missing cycle %s", id),
			"run: orbit cycles cycle new")
	}
	if err := fsutil.AtomicWrite(s.CurrentPath(), []byte(id+"\n"), 0o644); err != nil {
		return exit.Wrap(exit.IOError, err, "cannot write "+s.CurrentPath())
	}
	return nil
}

// ClearCurrent empties the CURRENT pointer. The file stays on disk (the
// canonical "no open cycle" representation). A missing file is tolerated —
// AtomicWrite recreates it empty.
func (s *Store) ClearCurrent() error {
	if err := fsutil.AtomicWrite(s.CurrentPath(), []byte(""), 0o644); err != nil {
		return exit.Wrap(exit.IOError, err, "cannot write "+s.CurrentPath())
	}
	return nil
}
