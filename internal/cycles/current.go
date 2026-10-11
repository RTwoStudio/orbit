package cycles

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/RTwoStudio/orbit/internal/doc"
	"github.com/RTwoStudio/orbit/internal/exit"
	"github.com/RTwoStudio/orbit/internal/fsutil"
	"github.com/RTwoStudio/orbit/internal/logx"
)

// currentCycleRe validates the cycle id held in the CURRENT pointer. T5 owns
// the exported cycle-id API; this local grammar exists only to reject a
// malformed pointer at read time.
var currentCycleRe = regexp.MustCompile(`^C-\d{4,}$`)

// ReadCurrent returns the open cycle id from the vault's CURRENT pointer.
//
// A missing, empty, or malformed pointer is a state_conflict (7) carrying an
// `orbit cycles cycle new` hint. A well-formed pointer to a cycle that is
// missing (its directory is gone) or `Closed` upstream is auto-cleared — with a
// one-line notice — and reported as the same "no open cycle" state_conflict
// (design §6): `CURRENT` is local/gitignored, so mutating it on read is safe.
// A pointer to a present-but-unparseable note stays loud (never a silent clear).
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
	// The cycle's directory is gone: it was closed/deleted upstream. Clear the
	// stale pointer and report no open cycle rather than a dangling conflict.
	if !fsutil.IsDir(s.CycleDir(id)) {
		return "", s.clearStaleCurrent(id, "is gone")
	}
	path := filepath.Join(s.CycleDir(id), id+".md")
	d, err := doc.ParseDoc(path)
	if err != nil {
		// A note that is simply absent (the directory exists but the note has
		// not been written, or a lightweight fixture) is not a parse error:
		// keep the v1 permissive behavior and return the id. Any other error —
		// a file that exists but cannot be parsed — is neither "gone" nor
		// "Closed": stay loud and never clear.
		if os.IsNotExist(err) {
			return id, nil
		}
		return "", exit.New(exit.StateConflict,
			fmt.Sprintf("CURRENT points at unparseable cycle %s: %v", id, err),
			"fix or remove "+path+", then retry")
	}
	if cycleStatusFromFile(d.Get("status")) == CycleClosed {
		return "", s.clearStaleCurrent(id, "was closed upstream")
	}
	return id, nil
}

// clearStaleCurrent empties CURRENT for a pointer to a cycle that is no longer
// open and returns the same "no open cycle" state_conflict an absent CURRENT
// yields, so BetWork/SyncCycle/CloseCycle still refuse and Status still yields a
// nil Current. `why` completes "cycle C-#### <why>".
func (s *Store) clearStaleCurrent(id, why string) error {
	if err := s.ClearCurrent(); err != nil {
		return err
	}
	logx.Info("CURRENT cleared: cycle %s %s", id, why)
	return exit.New(exit.StateConflict,
		fmt.Sprintf("no open cycle — cycle %s %s", id, why),
		"run: orbit cycles cycle new")
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
