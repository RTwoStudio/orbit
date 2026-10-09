package cycles

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RTwoStudio/orbit/internal/exit"
)

// workFiles returns every candidate work-note path under backlog/ and every
// cycles/C-*/ directory. Missing directories are tolerated (empty result).
func (s *Store) workFiles() ([]string, error) {
	var out []string
	collect := func(dir string) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			if !workIDRe.MatchString(workIDFromFilename(e.Name())) {
				continue
			}
			out = append(out, filepath.Join(dir, e.Name()))
		}
		return nil
	}
	if err := collect(s.BacklogDir()); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(s.LedgerDir())
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "C-") {
			continue
		}
		if err := collect(filepath.Join(s.LedgerDir(), e.Name())); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// FindWork resolves a W-#### to its on-disk path wherever it lives (backlog/
// or any cycles/C-*/). The filename is stable after creation, so matching is
// by the leading ID token, never the full filename.
//
// 0 matches → not_found (5); >1 → state_conflict (7).
func (s *Store) FindWork(id string) (string, error) {
	id, err := ParseWorkID(id)
	if err != nil {
		return "", err
	}
	files, err := s.workFiles()
	if err != nil {
		return "", exit.Wrap(exit.IOError, err, "cannot scan cycles dirs")
	}
	var matches []string
	for _, p := range files {
		if workIDFromFilename(filepath.Base(p)) == id {
			matches = append(matches, p)
		}
	}
	switch len(matches) {
	case 0:
		return "", exit.New(exit.NotFound,
			fmt.Sprintf("work item %s not found", id),
			"list work items with: orbit cycles work list")
	case 1:
		return matches[0], nil
	default:
		return "", exit.New(exit.StateConflict,
			fmt.Sprintf("work item %s exists in %d locations: %s", id, len(matches), strings.Join(matches, ", ")))
	}
}
