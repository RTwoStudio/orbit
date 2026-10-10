package cycles

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/RTwoStudio/orbit/internal/doc"
	"github.com/RTwoStudio/orbit/internal/exit"
	"github.com/RTwoStudio/orbit/internal/fsutil"
	"github.com/RTwoStudio/orbit/internal/logx"
	"github.com/RTwoStudio/orbit/internal/scaffold"
)

// DefaultCycleWindow is the fallback cycle length when no end date is supplied:
// six weeks (42 days), added to the start date.
const DefaultCycleWindow = 6 * 7 * 24 * time.Hour

// releaseRe is the strict release grammar accepted at `cycle new`:
// MAJOR.MINOR.PATCH with no prefix, suffix, prerelease, or build metadata.
var releaseRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// bareDateLayout is the start/end date format (design §5), matching the
// `--start <date>` / `--end <date>` CLI flags.
const bareDateLayout = "2006-01-02"

// Cycle is the shared read shape of a cycle note, reused by T6's `--json`
// rendering. The json tags are part of that contract. Bets is the regenerated
// read view over the work committed to the cycle.
type Cycle struct {
	ID              string         `json:"id"`
	Goal            string         `json:"goal"`
	Release         string         `json:"release"`
	Status          string         `json:"status"`
	Start           string         `json:"start"`
	End             string         `json:"end"`
	Project         string         `json:"project"`
	Repo            string         `json:"repo"`
	Provider        string         `json:"provider"`
	Milestones      map[string]int `json:"milestone"`
	Created         string         `json:"created"`
	RegistryVersion string         `json:"registry_version"`
	Path            string         `json:"path"`
	Bets            []WorkItem     `json:"bets"`
}

// NewCycle creates the next cycle, renders its note from the cached
// cycle.stub.md, points CURRENT at it, and regenerates an empty ## Bets view.
//
// Preflight: a non-empty goal, a strict MAJOR.MINOR.PATCH release, and an
// initialized vault. At most one cycle may be open at a time, so an existing
// open cycle is a state_conflict (7). start defaults to UTC today; end
// defaults to start + DefaultCycleWindow. Both are bare YYYY-MM-DD dates and
// end must not precede start (preflight_failed 6).
func (s *Store) NewCycle(goal, release, start, end string) (*Cycle, error) {
	goal = strings.TrimSpace(goal)
	release = strings.TrimSpace(release)
	if goal == "" {
		return nil, exit.New(exit.PreflightFailed, "cycle goal must not be empty")
	}
	if !releaseRe.MatchString(release) {
		return nil, exit.New(exit.PreflightFailed,
			fmt.Sprintf("invalid release %q — expected MAJOR.MINOR.PATCH (e.g. 0.3.0)", release))
	}
	if !s.IsInitialized() {
		return nil, exit.New(exit.NotInitialized,
			fmt.Sprintf("vault is not initialized: %s is missing", s.CyclesDir()),
			"run: orbit cycles install")
	}
	if _, err := s.ReadCurrent(); err == nil {
		return nil, exit.New(exit.StateConflict,
			"a cycle is already open",
			"close it first: orbit cycles cycle close")
	}

	startT, err := resolveStart(start)
	if err != nil {
		return nil, err
	}
	endT, err := resolveEnd(end, startT)
	if err != nil {
		return nil, err
	}
	if endT.Before(startT) {
		return nil, exit.New(exit.PreflightFailed,
			fmt.Sprintf("end date %s precedes start date %s",
				endT.Format(bareDateLayout), startT.Format(bareDateLayout)))
	}

	stub, version, err := s.cycleStub()
	if err != nil {
		return nil, err
	}
	id, err := s.NextCycleID()
	if err != nil {
		return nil, err
	}
	rendered, err := scaffold.Render(stub, map[string]string{
		"RELEASE":          release,
		"CYCLE_ID":         id,
		"CYCLE_GOAL":       goal,
		"START_DATE":       startT.Format(bareDateLayout),
		"END_DATE":         endT.Format(bareDateLayout),
		"DATE":             time.Now().UTC().Format(time.RFC3339),
		"REGISTRY_VERSION": version,
	})
	if err != nil {
		return nil, exit.Wrap(exit.General, err, "cannot render cycle stub")
	}
	if err := fsutil.EnsureDir(s.CycleDir(id)); err != nil {
		return nil, exit.Wrap(exit.IOError, err, "cannot create "+s.CycleDir(id))
	}
	path := filepath.Join(s.CycleDir(id), id+".md")
	if err := fsutil.AtomicWrite(path, rendered, 0o644); err != nil {
		return nil, exit.Wrap(exit.IOError, err, "cannot write "+path)
	}
	if err := s.WriteCurrent(id); err != nil {
		return nil, err
	}
	d, err := doc.ParseDoc(path)
	if err != nil {
		return nil, exit.Wrap(exit.General, err, "cannot parse rendered cycle note")
	}
	if err := s.refreshBets(d, id); err != nil {
		return nil, err
	}
	if err := d.Save(); err != nil {
		return nil, exit.Wrap(exit.IOError, err, "cannot rewrite "+d.Path)
	}
	logx.Info("cycle opened id=%s release=%s", id, release)
	cy := s.cycleFromDoc(d, id)
	cy.Bets = []WorkItem{}
	return cy, nil
}

// CloseCycle closes the open cycle: every work note in its folder that is not
// Delivered is shelved back to backlog/ (reusing T4's ShelveWork, whose history
// entry names this cycle), the cycle is marked Closed, and CURRENT is cleared.
//
// No open cycle or an already-Closed cycle is a state_conflict (7).
func (s *Store) CloseCycle() (*Cycle, error) {
	id, err := s.ReadCurrent()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(s.CycleDir(id), id+".md")
	d, err := doc.ParseDoc(path)
	if err != nil {
		return nil, exit.Wrap(exit.PreflightFailed, err, "cannot parse cycle note "+path)
	}
	from := cycleStatusFromFile(d.Get("status"))
	if from == CycleClosed {
		return nil, exit.New(exit.StateConflict,
			fmt.Sprintf("cycle %s is already Closed", id))
	}
	if err := CheckCycleTransition(from, CycleClosed); err != nil {
		return nil, err
	}

	files, err := s.cycleWorkFiles(id)
	if err != nil {
		return nil, err
	}
	for _, p := range files {
		wd, err := doc.ParseDoc(p)
		if err != nil {
			continue // tolerate an unparseable note; the note stays put
		}
		in := workItemFromDoc(wd)
		if in.Status == string(WorkDelivered) {
			continue
		}
		if _, err := s.ShelveWork(in.ID); err != nil {
			return nil, err
		}
	}

	d.Set("status", CycleClosed.FileValue())
	if err := s.refreshBets(d, id); err != nil {
		return nil, err
	}
	if err := d.Save(); err != nil {
		return nil, exit.Wrap(exit.IOError, err, "cannot rewrite "+d.Path)
	}
	if err := s.ClearCurrent(); err != nil {
		return nil, err
	}
	logx.Info("cycle closed id=%s", id)
	cy := s.cycleFromDoc(d, id)
	cy.Bets = []WorkItem{}
	return cy, nil
}

// ListCycles returns every cycle's metadata (no Bets), sorted by numeric id.
// Read-only; a missing ledger is an empty result.
func (s *Store) ListCycles() ([]Cycle, error) {
	entries, err := os.ReadDir(s.LedgerDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, exit.Wrap(exit.IOError, err, "cannot read "+s.LedgerDir())
	}
	var out []Cycle
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		if _, ok := cycleIDNum(id); !ok {
			continue
		}
		path := filepath.Join(s.LedgerDir(), id, id+".md")
		d, err := doc.ParseDoc(path)
		if err != nil {
			continue // a read view tolerates an unparseable note
		}
		out = append(out, *s.cycleFromDoc(d, id))
	}
	sort.SliceStable(out, func(i, j int) bool {
		ni, _ := cycleIDNum(out[i].ID)
		nj, _ := cycleIDNum(out[j].ID)
		return ni < nj
	})
	return out, nil
}

// ShowCycle returns one cycle including its regenerated Bets view. A malformed
// id is a preflight_failed (6); an unknown cycle is a not_found (5).
func (s *Store) ShowCycle(id string) (*Cycle, error) {
	id, err := ParseCycleID(id)
	if err != nil {
		return nil, err
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
	cy := s.cycleFromDoc(d, id)
	bets, err := s.listCycleWork(id)
	if err != nil {
		return nil, err
	}
	if bets == nil {
		bets = []WorkItem{}
	}
	cy.Bets = bets
	return cy, nil
}

// cycleWorkFiles returns every work-note path (W-*.md) inside a cycle folder.
// A missing cycle folder is an empty result.
func (s *Store) cycleWorkFiles(id string) ([]string, error) {
	dir := s.CycleDir(id)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, exit.Wrap(exit.IOError, err, "cannot read "+dir)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		if !workIDRe.MatchString(workIDFromFilename(e.Name())) {
			continue
		}
		out = append(out, filepath.Join(dir, e.Name()))
	}
	return out, nil
}

// listCycleWork parses a cycle's committed work notes, sorted by numeric id.
// Read-only and tolerant of unparseable notes.
func (s *Store) listCycleWork(id string) ([]WorkItem, error) {
	files, err := s.cycleWorkFiles(id)
	if err != nil {
		return nil, err
	}
	var items []WorkItem
	for _, p := range files {
		d, err := doc.ParseDoc(p)
		if err != nil {
			continue
		}
		items = append(items, *workItemFromDoc(d))
	}
	sort.SliceStable(items, func(i, j int) bool {
		ni, _ := workIDNum(items[i].ID)
		nj, _ := workIDNum(items[j].ID)
		return ni < nj
	})
	return items, nil
}

// refreshBets regenerates the cycle note's `## Bets` read view in place from
// the work notes currently committed to the cycle. The rest of the body is
// preserved byte-exact.
func (s *Store) refreshBets(d *doc.Doc, cycleID string) error {
	items, err := s.listCycleWork(cycleID)
	if err != nil {
		return err
	}
	d.Body = replaceSection(d.Body, "Bets", renderBets(items))
	return nil
}

// refreshCycleBetsBestEffort regenerates a cycle note's Bets view after a work
// membership change. A missing or unparseable cycle note must never fail the
// caller's work verb, so every failure here is swallowed.
func (s *Store) refreshCycleBetsBestEffort(cycleID string) {
	if cycleID == "" {
		return
	}
	path := filepath.Join(s.CycleDir(cycleID), cycleID+".md")
	d, err := doc.ParseDoc(path)
	if err != nil {
		return
	}
	if err := s.refreshBets(d, cycleID); err != nil {
		logx.Debug("bets refresh failed cycle=%s: %v", cycleID, err)
		return
	}
	if err := d.Save(); err != nil {
		logx.Debug("bets refresh save failed cycle=%s: %v", cycleID, err)
	}
}

// cycleFromDoc builds the shared read shape from a parsed cycle note.
func (s *Store) cycleFromDoc(d *doc.Doc, id string) *Cycle {
	return &Cycle{
		ID:              d.Get("id"),
		Goal:            d.Get("goal"),
		Release:         d.Get("release"),
		Status:          canonicalCycleStatus(d.Get("status")),
		Start:           d.Get("start"),
		End:             d.Get("end"),
		Project:         d.Get("project"),
		Repo:            d.Get("repo"),
		Provider:        d.Get("provider"),
		Milestones:      stringMapToIntMap(d.GetStringMap("milestone")),
		Created:         d.Get("created"),
		RegistryVersion: d.Get("registry-version"),
		Path:            d.Path,
	}
}

// stringMapToIntMap converts a mapping key's string values to ints, tolerating
// malformed values as 0. A nil input yields an empty (non-nil) map so the read
// shape always renders as a JSON object rather than null.
func stringMapToIntMap(m map[string]string) map[string]int {
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = atoiOrZero(v)
	}
	return out
}

// canonicalCycleStatus normalizes a raw status for display; unknown values
// pass through unchanged.
func canonicalCycleStatus(raw string) string {
	if cs := cycleStatusFromFile(raw); cs != "" {
		return cs.FileValue()
	}
	return raw
}

// resolveStart parses an explicit bare start date, defaulting to UTC today.
func resolveStart(start string) (time.Time, error) {
	start = strings.TrimSpace(start)
	if start == "" {
		now := time.Now().UTC()
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC), nil
	}
	return parseBareDate("start", start)
}

// resolveEnd parses an explicit bare end date, defaulting to start + window.
func resolveEnd(end string, start time.Time) (time.Time, error) {
	end = strings.TrimSpace(end)
	if end == "" {
		return start.Add(DefaultCycleWindow), nil
	}
	return parseBareDate("end", end)
}

// parseBareDate parses a bare YYYY-MM-DD date. Malformed input is a
// preflight_failed (6).
func parseBareDate(field, value string) (time.Time, error) {
	t, err := time.Parse(bareDateLayout, value)
	if err != nil {
		return time.Time{}, exit.New(exit.PreflightFailed,
			fmt.Sprintf("invalid %s date %q — expected YYYY-MM-DD", field, value))
	}
	return t, nil
}

// renderBets formats the `## Bets` read view: one bullet per committed work
// note, e.g. `- W-0003 — Cancellation flow (Bet)`.
func renderBets(items []WorkItem) string {
	var b strings.Builder
	for i, it := range items {
		if i > 0 {
			b.WriteString("\n")
		}
		title := it.Title
		if title == "" {
			title = it.ID
		}
		fmt.Fprintf(&b, "- %s — %s (%s)", it.ID, title, it.Status)
	}
	return b.String()
}

// replaceSection replaces the inner text of a "## <name>" section in body,
// preserving every other byte. The section runs from just after its heading
// line to the start of the next heading (or end of body). The heading line is
// retained; `inner` is written on its own indented block, or the section is
// left empty when inner is "".
func replaceSection(body []byte, name, inner string) []byte {
	heading := []byte("## " + name + "\n")
	start := bytes.Index(body, heading)
	if start < 0 {
		return body
	}
	contentStart := start + len(heading)
	next := len(body)
	pos := contentStart
	for pos < len(body) {
		lineEnd := bytes.IndexByte(body[pos:], '\n')
		if lineEnd < 0 {
			if doc.IsHeading(body[pos:]) {
				next = pos
			}
			break
		}
		lineEnd += pos + 1
		if doc.IsHeading(body[pos:lineEnd]) {
			next = pos
			break
		}
		pos = lineEnd
	}

	var out bytes.Buffer
	out.Write(body[:contentStart])
	out.WriteString("\n")
	if inner != "" {
		out.WriteString(inner)
		out.WriteString("\n\n")
	}
	out.Write(body[next:])
	return out.Bytes()
}
