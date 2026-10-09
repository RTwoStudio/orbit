package cycles

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/RTwoStudio/orbit/internal/doc"
	"github.com/RTwoStudio/orbit/internal/exit"
	"github.com/RTwoStudio/orbit/internal/fsutil"
	"github.com/RTwoStudio/orbit/internal/logx"
	"github.com/RTwoStudio/orbit/internal/scaffold"
)

// WorkItem is the shared read shape of a work note, reused by T5's status
// board and T6's `--json` rendering. The json tags are part of that contract.
type WorkItem struct {
	ID              string   `json:"id"`
	Title           string   `json:"title"`
	Scope           string   `json:"scope"`
	Status          string   `json:"status"`
	Appetite        string   `json:"appetite"`
	Cycle           string   `json:"cycle"`
	Project         string   `json:"project"`
	NeocortexIssue  string   `json:"neocortex_issue"`
	Path            string   `json:"path"`
	Created         string   `json:"created"`
	RegistryVersion string   `json:"registry_version"`
	History         []string `json:"history"`
}

const (
	appetiteBig   = "big"
	appetiteSmall = "small"
)

// NewWork creates a work note in backlog/ as Backlog with an empty history,
// rendered from the cached work.stub.md using only the registered tokens.
func (s *Store) NewWork(title, scope string) (*WorkItem, error) {
	title = strings.TrimSpace(title)
	scope = strings.TrimSpace(scope)
	if title == "" {
		return nil, exit.New(exit.PreflightFailed, "title must not be empty")
	}
	if scope == "" {
		return nil, exit.New(exit.PreflightFailed, "scope must not be empty",
			"usage: orbit cycles work new \"<title>\" --scope <scope>")
	}
	if !s.IsInitialized() {
		return nil, exit.New(exit.NotInitialized,
			fmt.Sprintf("vault is not initialized: %s is missing", s.CyclesDir()),
			"run: orbit cycles install")
	}

	stub, version, err := s.workStub()
	if err != nil {
		return nil, err
	}
	id, err := s.NextWorkID()
	if err != nil {
		return nil, err
	}
	slug := doc.SanitizeSlug(title)
	path := WorkPath(s.BacklogDir(), id, slug)

	rendered, err := scaffold.Render(stub, map[string]string{
		"WORK_ID":          id,
		"WORK_TITLE":       title,
		"SCOPE":            scope,
		"DATE":             time.Now().UTC().Format(time.RFC3339),
		"REGISTRY_VERSION": version,
	})
	if err != nil {
		return nil, exit.Wrap(exit.General, err, "cannot render work stub")
	}
	if err := fsutil.AtomicWrite(path, rendered, 0o644); err != nil {
		return nil, exit.Wrap(exit.IOError, err, "cannot write "+path)
	}
	d, err := doc.ParseDoc(path)
	if err != nil {
		return nil, exit.Wrap(exit.General, err, "cannot parse rendered work note")
	}
	logx.Info("work created id=%s scope=%s", id, scope)
	return workItemFromDoc(d), nil
}

// ShapeWork moves a Backlog item to Pitched: it validates the appetite and
// requires the four shape sections (comments ignored) to be non-empty, then
// stamps status + appetite and appends one history entry in place.
func (s *Store) ShapeWork(id, appetite string) (*WorkItem, error) {
	d, err := s.findAndParse(id)
	if err != nil {
		return nil, err
	}
	from := workStatusFromFile(d.Get("status"))
	if from != WorkBacklog {
		return nil, exit.New(exit.StateConflict,
			fmt.Sprintf("work %s is %s — shape requires Backlog", d.Get("id"), canonicalStatus(d.Get("status"))))
	}

	appetite = strings.ToLower(strings.TrimSpace(appetite))
	if appetite != appetiteBig && appetite != appetiteSmall {
		return nil, exit.New(exit.PreflightFailed,
			fmt.Sprintf("invalid appetite %q — valid: big, small", appetite))
	}
	for _, name := range []string{"Problem", "Solution Sketch", "Rabbit Holes", "No-gos"} {
		if doc.StripHTMLComments(doc.Section(d.Body, name)) == "" {
			return nil, exit.New(exit.PreflightFailed,
				fmt.Sprintf("%s: ## %s is empty — fill the shape sections before shaping", d.Path, name),
				"fill Problem, Solution Sketch, Rabbit Holes and No-gos, then retry")
		}
	}
	if err := CheckWorkTransition(from, WorkPitched); err != nil {
		return nil, err
	}

	d.Set("status", WorkPitched.FileValue())
	d.Set("appetite", appetite)
	d.AppendToList("history", historyLine(from, WorkPitched, "appetite: "+appetite))
	if err := d.Save(); err != nil {
		return nil, exit.Wrap(exit.IOError, err, "cannot rewrite "+d.Path)
	}
	logx.Info("work shaped id=%s appetite=%s", d.Get("id"), appetite)
	return workItemFromDoc(d), nil
}

// BetWork commits a Pitched item to the open cycle: it requires a valid
// CURRENT pointer, moves the file into cycles/C-####/, and stamps status +
// cycle.
func (s *Store) BetWork(id string) (*WorkItem, error) {
	d, err := s.findAndParse(id)
	if err != nil {
		return nil, err
	}
	from := workStatusFromFile(d.Get("status"))
	if err := CheckWorkTransition(from, WorkBet); err != nil {
		return nil, err
	}
	cycle, err := s.ReadCurrent()
	if err != nil {
		return nil, err
	}
	if err := s.moveWorkTo(d, s.CycleDir(cycle)); err != nil {
		return nil, err
	}
	d.Set("status", WorkBet.FileValue())
	d.Set("cycle", cycle)
	d.AppendToList("history", historyLine(from, WorkBet, "cycle: "+cycle))
	if err := d.Save(); err != nil {
		return nil, exit.Wrap(exit.IOError, err, "cannot rewrite "+d.Path)
	}
	s.refreshCycleBetsBestEffort(cycle)
	logx.Info("work bet id=%s cycle=%s", d.Get("id"), cycle)
	return workItemFromDoc(d), nil
}

// ShelveWork parks a work item: it moves it back to backlog/ when it lives in
// a cycle, clears the cycle field, and stamps Shelved.
func (s *Store) ShelveWork(id string) (*WorkItem, error) {
	d, err := s.findAndParse(id)
	if err != nil {
		return nil, err
	}
	from := workStatusFromFile(d.Get("status"))
	if err := CheckWorkTransition(from, WorkShelved); err != nil {
		return nil, err
	}
	oldCycle := d.Get("cycle")
	if filepath.Clean(filepath.Dir(d.Path)) != filepath.Clean(s.BacklogDir()) {
		if err := s.moveWorkTo(d, s.BacklogDir()); err != nil {
			return nil, err
		}
	}
	d.Set("status", WorkShelved.FileValue())
	d.Set("cycle", "")
	detail := ""
	if oldCycle != "" {
		detail = "cycle: " + oldCycle
	}
	d.AppendToList("history", historyLine(from, WorkShelved, detail))
	if err := d.Save(); err != nil {
		return nil, exit.Wrap(exit.IOError, err, "cannot rewrite "+d.Path)
	}
	s.refreshCycleBetsBestEffort(oldCycle)
	logx.Info("work shelved id=%s", d.Get("id"))
	return workItemFromDoc(d), nil
}

// UnshelveWork returns a Shelved item to Pitched. It stays in backlog/.
func (s *Store) UnshelveWork(id string) (*WorkItem, error) {
	d, err := s.findAndParse(id)
	if err != nil {
		return nil, err
	}
	from := workStatusFromFile(d.Get("status"))
	if from != WorkShelved {
		return nil, exit.New(exit.StateConflict,
			fmt.Sprintf("work %s is %s — unshelve requires Shelved", d.Get("id"), canonicalStatus(d.Get("status"))))
	}
	if err := CheckWorkTransition(from, WorkPitched); err != nil {
		return nil, err
	}
	d.Set("status", WorkPitched.FileValue())
	d.AppendToList("history", historyLine(from, WorkPitched, ""))
	if err := d.Save(); err != nil {
		return nil, exit.Wrap(exit.IOError, err, "cannot rewrite "+d.Path)
	}
	logx.Info("work unshelved id=%s", d.Get("id"))
	return workItemFromDoc(d), nil
}

// DeliverWork marks a Bet item Delivered. It stays in its cycle folder as that
// cycle's permanent history; the status is terminal.
func (s *Store) DeliverWork(id string) (*WorkItem, error) {
	d, err := s.findAndParse(id)
	if err != nil {
		return nil, err
	}
	from := workStatusFromFile(d.Get("status"))
	if err := CheckWorkTransition(from, WorkDelivered); err != nil {
		return nil, err
	}
	cycle := d.Get("cycle")
	detail := ""
	if cycle != "" {
		detail = "cycle: " + cycle
	}
	d.Set("status", WorkDelivered.FileValue())
	d.AppendToList("history", historyLine(from, WorkDelivered, detail))
	if err := d.Save(); err != nil {
		return nil, exit.Wrap(exit.IOError, err, "cannot rewrite "+d.Path)
	}
	s.refreshCycleBetsBestEffort(cycle)
	logx.Info("work delivered id=%s", d.Get("id"))
	return workItemFromDoc(d), nil
}

// ListWork returns every work item across backlog/ and all cycles/C-*/,
// optionally filtered by status (case-insensitive) and scope (exact), sorted
// by numeric id. Read-only.
func (s *Store) ListWork(status, scope string) ([]WorkItem, error) {
	var want WorkStatus
	if strings.TrimSpace(status) != "" {
		ws, err := ParseWorkStatus(status)
		if err != nil {
			return nil, err
		}
		want = ws
	}
	scope = strings.TrimSpace(scope)

	files, err := s.workFiles()
	if err != nil {
		return nil, exit.Wrap(exit.IOError, err, "cannot scan cycles dirs")
	}
	var items []WorkItem
	for _, p := range files {
		d, err := doc.ParseDoc(p)
		if err != nil {
			continue // a read view tolerates an unparseable note
		}
		it := workItemFromDoc(d)
		if want != "" && workStatusFromFile(it.Status) != want {
			continue
		}
		if scope != "" && it.Scope != scope {
			continue
		}
		items = append(items, *it)
	}
	sort.SliceStable(items, func(i, j int) bool {
		ni, _ := workIDNum(items[i].ID)
		nj, _ := workIDNum(items[j].ID)
		return ni < nj
	})
	return items, nil
}

// ShowWork returns one work item anywhere it lives. Read-only.
func (s *Store) ShowWork(id string) (*WorkItem, error) {
	d, err := s.findAndParse(id)
	if err != nil {
		return nil, err
	}
	return workItemFromDoc(d), nil
}

// findAndParse resolves a work id then parses its note.
func (s *Store) findAndParse(id string) (*doc.Doc, error) {
	path, err := s.FindWork(id)
	if err != nil {
		return nil, err
	}
	d, err := doc.ParseDoc(path)
	if err != nil {
		return nil, exit.Wrap(exit.PreflightFailed, err, "cannot parse work note "+path)
	}
	return d, nil
}

// moveWorkTo moves a work note to toDir (same filename), updating d.Path. A
// rename preserves bytes; a cross-device rename falls back to write+remove.
func (s *Store) moveWorkTo(d *doc.Doc, toDir string) error {
	if filepath.Clean(filepath.Dir(d.Path)) == filepath.Clean(toDir) {
		return nil
	}
	if err := fsutil.EnsureDir(toDir); err != nil {
		return exit.Wrap(exit.IOError, err, "cannot create "+toDir)
	}
	dst := filepath.Join(toDir, filepath.Base(d.Path))
	if err := os.Rename(d.Path, dst); err != nil {
		data, rerr := os.ReadFile(d.Path)
		if rerr != nil {
			return exit.Wrap(exit.IOError, rerr, "cannot move work note")
		}
		if werr := fsutil.AtomicWrite(dst, data, 0o644); werr != nil {
			return exit.Wrap(exit.IOError, werr, "cannot move work note")
		}
		if rerr := os.Remove(d.Path); rerr != nil && !os.IsNotExist(rerr) {
			return exit.Wrap(exit.IOError, rerr, "cannot remove old work note")
		}
	}
	d.Path = dst
	return nil
}

// workItemFromDoc builds the shared read shape from a parsed note.
func workItemFromDoc(d *doc.Doc) *WorkItem {
	history := d.GetStringList("history")
	if history == nil {
		history = []string{}
	}
	return &WorkItem{
		ID:              d.Get("id"),
		Title:           d.Get("title"),
		Scope:           d.Get("scope"),
		Status:          canonicalStatus(d.Get("status")),
		Appetite:        d.Get("appetite"),
		Cycle:           d.Get("cycle"),
		Project:         d.Get("project"),
		NeocortexIssue:  d.Get("neocortex-issue"),
		Path:            d.Path,
		Created:         d.Get("created"),
		RegistryVersion: d.Get("registry-version"),
		History:         history,
	}
}

// canonicalStatus normalizes a raw status for display; unknown values pass
// through unchanged.
func canonicalStatus(raw string) string {
	if ws := workStatusFromFile(raw); ws != "" {
		return ws.FileValue()
	}
	return raw
}

// historyLine formats one history entry:
//
//	2026-10-09T15:04:00Z  From → To (detail)
func historyLine(from, to WorkStatus, detail string) string {
	line := fmt.Sprintf("%s  %s → %s",
		time.Now().UTC().Format(time.RFC3339), from.FileValue(), to.FileValue())
	if detail != "" {
		line += " (" + detail + ")"
	}
	return line
}
