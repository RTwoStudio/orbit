package cycles

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/RTwoStudio/orbit/internal/doc"
	"github.com/RTwoStudio/orbit/internal/exit"
	"github.com/RTwoStudio/orbit/internal/logx"
)

// WorkForge is the forge-link payload for a work note: the resolved
// provider/repo plus the git issue and cycle milestone numbers that
// `work sync` records. Issue is the git issue (distinct from neocortex-issue,
// the NeoCortex link); a zero means "not yet synced".
type WorkForge struct {
	Provider  string
	Repo      string
	Issue     int
	Milestone int
}

// CycleForge is the forge-link payload for a cycle note: the single-repo
// convenience scalars (set only when the cycle resolves to one repo) plus the
// per-repo milestone map keyed by `owner/repo`, which is authoritative when a
// cycle spans repos or providers.
type CycleForge struct {
	Project    string
	Repo       string
	Provider   string
	Milestones map[string]int
}

// SetWorkForge writes a work note's forge links (provider/repo/issue/
// milestone) in place, preserving the body and every other frontmatter key,
// then returns the refreshed read shape. It never reads or writes
// neocortex-issue. A negative issue or milestone is a preflight_failed (6).
func (s *Store) SetWorkForge(id string, f WorkForge) (*WorkItem, error) {
	if f.Issue < 0 || f.Milestone < 0 {
		return nil, exit.New(exit.PreflightFailed,
			fmt.Sprintf("issue and milestone must not be negative (issue=%d milestone=%d)", f.Issue, f.Milestone))
	}
	d, err := s.findAndParse(id)
	if err != nil {
		return nil, err
	}
	d.Set("provider", f.Provider)
	d.Set("repo", f.Repo)
	d.SetInt("issue", f.Issue)
	d.SetInt("milestone", f.Milestone)
	if err := d.Save(); err != nil {
		return nil, exit.Wrap(exit.IOError, err, "cannot rewrite "+d.Path)
	}
	logx.Info("work forge set id=%s issue=%d milestone=%d", d.Get("id"), f.Issue, f.Milestone)
	return workItemFromDoc(d), nil
}

// SetCycleForge writes a cycle note's forge links (the project/repo/provider
// scalars and the per-repo milestone map) in place, preserving the body and
// every other frontmatter key, then returns the refreshed read shape. A
// negative milestone is a preflight_failed (6).
func (s *Store) SetCycleForge(id string, f CycleForge) (*Cycle, error) {
	id, err := ParseCycleID(id)
	if err != nil {
		return nil, err
	}
	for _, n := range f.Milestones {
		if n < 0 {
			return nil, exit.New(exit.PreflightFailed,
				fmt.Sprintf("milestone numbers must not be negative (got %d)", n))
		}
	}
	path := filepath.Join(s.CycleDir(id), id+".md")
	d, err := doc.ParseDoc(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, exit.New(exit.NotFound,
				fmt.Sprintf("cycle %s not found", id),
				"list cycles with: orbit cycles cycle list")
		}
		return nil, exit.Wrap(exit.PreflightFailed, err, "cannot parse cycle note "+path)
	}
	d.Set("project", f.Project)
	d.Set("repo", f.Repo)
	d.Set("provider", f.Provider)
	d.SetStringMap("milestone", intMapToStringMap(f.Milestones))
	if err := d.Save(); err != nil {
		return nil, exit.Wrap(exit.IOError, err, "cannot rewrite "+d.Path)
	}
	logx.Info("cycle forge set id=%s milestones=%d", id, len(f.Milestones))
	return s.ShowCycle(id)
}

// intMapToStringMap converts a milestone map to the string map the doc layer
// writes. A nil input yields an empty (non-nil) map, rendering `milestone: {}`.
func intMapToStringMap(m map[string]int) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = strconv.Itoa(v)
	}
	return out
}
