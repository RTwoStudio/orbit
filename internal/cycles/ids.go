package cycles

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/RTwoStudio/orbit/internal/exit"
)

// compositeWorkIDRe is the canonical owner-namespaced work-id grammar:
//
//	W-<seq>-<owner-slug>
//
// where <seq> is four or more digits and <owner-slug> is the creator's stable
// roster handle ([a-z0-9] words joined by '-'). The capture groups carry the
// numeric sequence and the owner handle.
var compositeWorkIDRe = regexp.MustCompile(`^W-(\d{4,})-([a-z0-9]+(?:-[a-z0-9]+)*)$`)

// legacyWorkIDRe is the pre-v2 bare grammar (`W-####`). It is accepted
// READ-ONLY so an un-migrated vault keeps resolving; the CLI never mints one.
var legacyWorkIDRe = regexp.MustCompile(`^W-(\d{4,})$`)

// isWorkID reports whether tok is a canonical composite id or a legacy bare id.
func isWorkID(tok string) bool {
	return compositeWorkIDRe.MatchString(tok) || legacyWorkIDRe.MatchString(tok)
}

// workIDSplit decomposes a composite or legacy work id into its sequence and
// owner handle. A legacy id yields an empty owner; anything malformed yields
// ok=false.
func workIDSplit(id string) (seq int, owner string, ok bool) {
	id = strings.TrimSpace(id)
	if m := compositeWorkIDRe.FindStringSubmatch(id); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			return 0, "", false
		}
		return n, m[2], true
	}
	if m := legacyWorkIDRe.FindStringSubmatch(id); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			return 0, "", false
		}
		return n, "", true
	}
	return 0, "", false
}

// workIDNum extracts the numeric sequence of a composite or legacy work id.
func workIDNum(id string) (int, bool) {
	seq, _, ok := workIDSplit(id)
	return seq, ok
}

// workIDOwner extracts the owner handle of a composite work id; a legacy bare
// id yields "".
func workIDOwner(id string) string {
	_, owner, _ := workIDSplit(id)
	return owner
}

// ParseWorkID validates and returns a canonical work id: the composite
// `W-<seq>-<owner-slug>` form, or a legacy bare `W-####` (read-compat).
// Anything else is a preflight_failed (6).
func ParseWorkID(id string) (string, error) {
	s := strings.TrimSpace(id)
	if !isWorkID(s) {
		return "", exit.New(exit.PreflightFailed,
			fmt.Sprintf("invalid work id %q — syntax: W-<seq>-<owner-slug> (e.g. W-0001-arash-jafarpour); a legacy bare W-#### still resolves", id))
	}
	return s, nil
}

// workIDFromFilename extracts a note filename's leading ID token by splitting
// on the FIRST ` - ` delimiter (the slug alphabet is [a-z0-9-], so the
// space-padded delimiter can never appear inside a slug):
//
//	"W-0001-arash-jafarpour - voice-search.md" → "W-0001-arash-jafarpour"
//	"W-0001 - voice-search.md"                 → "W-0001" (legacy)
//	"W-0002.md"                                → "W-0002"
func workIDFromFilename(name string) string {
	base := strings.TrimSuffix(name, ".md")
	if i := strings.Index(base, " - "); i >= 0 {
		return base[:i]
	}
	return base
}

// NextWorkID returns the next free composite id for owner: max over that
// owner's ids across backlog/ and every cycles/C-*/ directory, plus one,
// zero-padded to at least four digits. Only the allocating owner's namespace is
// scanned, so two developers minting offline never collide on merge. Legacy
// bare ids carry no owner and never advance a per-owner sequence.
func (s *Store) NextWorkID(owner string) (string, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return "", exit.New(exit.PreflightFailed, "work owner handle must not be empty")
	}
	files, err := s.workFiles()
	if err != nil {
		return "", exit.Wrap(exit.IOError, err, "cannot scan cycles dirs")
	}
	max := 0
	for _, p := range files {
		tok := workIDFromFilename(filepath.Base(p))
		if workIDOwner(tok) != owner {
			continue
		}
		if n, ok := workIDNum(tok); ok && n > max {
			max = n
		}
	}
	return fmt.Sprintf("W-%04d-%s", max+1, owner), nil
}

// cycleIDRe is the canonical cycle-id grammar: C- followed by 4+ digits. The
// capture group carries the numeric part for cycleIDNum.
var cycleIDRe = regexp.MustCompile(`^C-(\d{4,})$`)

// ParseCycleID validates and returns a canonical C-#### id. Anything else is a
// preflight_failed (6).
func ParseCycleID(id string) (string, error) {
	s := strings.TrimSpace(id)
	if !cycleIDRe.MatchString(s) {
		return "", exit.New(exit.PreflightFailed,
			fmt.Sprintf("invalid cycle id %q — syntax: C-#### (C- followed by four or more digits)", id))
	}
	return s, nil
}

// cycleIDNum extracts the numeric part of a canonical C-#### id.
func cycleIDNum(id string) (int, bool) {
	m := cycleIDRe.FindStringSubmatch(id)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, false
	}
	return n, true
}

// cycleIDFromFilename extracts a note filename's leading ID token, e.g.
// "C-0001.md" → "C-0001". It mirrors workIDFromFilename.
func cycleIDFromFilename(name string) string {
	base := strings.TrimSuffix(name, ".md")
	if i := strings.IndexByte(base, ' '); i >= 0 {
		base = base[:i]
	}
	return base
}

// NextCycleID returns the next free C-#### id: max over every cycles/C-*/
// directory name and cycle-note filename matching the grammar, plus one,
// zero-padded to at least four digits. It is monotonic and never reuses an id
// (closed cycles and their delivered history stay on disk).
func (s *Store) NextCycleID() (string, error) {
	max := 0
	entries, err := os.ReadDir(s.LedgerDir())
	if err != nil {
		if os.IsNotExist(err) {
			return "C-0001", nil
		}
		return "", exit.Wrap(exit.IOError, err, "cannot scan "+s.LedgerDir())
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if n, ok := cycleIDNum(e.Name()); ok && n > max {
			max = n
		}
		sub, err := os.ReadDir(filepath.Join(s.LedgerDir(), e.Name()))
		if err != nil {
			continue
		}
		for _, se := range sub {
			if se.IsDir() || !strings.HasSuffix(se.Name(), ".md") {
				continue
			}
			if n, ok := cycleIDNum(cycleIDFromFilename(se.Name())); ok && n > max {
				max = n
			}
		}
	}
	return fmt.Sprintf("C-%04d", max+1), nil
}

// CycleIDConflict names a duplicate leading cycle id and every note path that
// carries it. `paths` is sorted; a group always holds ≥2 entries.
type CycleIDConflict struct {
	ID    string   `json:"id"`
	Paths []string `json:"paths"`
}

// cycleNotePaths returns every cycle-note path under a cycles/<dir>/ folder
// whose leading filename token matches the C-#### grammar. Directory names are
// containers and are never counted, so the canonical one-note layout
// (cycles/C-0007/C-0007.md) is a single cycle, not a self-duplicate. A missing
// ledger is an empty result; an unreadable folder is skipped.
func (s *Store) cycleNotePaths() ([]string, error) {
	entries, err := os.ReadDir(s.LedgerDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, exit.Wrap(exit.IOError, err, "cannot scan "+s.LedgerDir())
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(s.LedgerDir(), e.Name())
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") {
				continue
			}
			if !cycleIDRe.MatchString(cycleIDFromFilename(f.Name())) {
				continue
			}
			out = append(out, filepath.Join(dir, f.Name()))
		}
	}
	sort.Strings(out)
	return out, nil
}

// CycleIDDuplicates scans the ledger for duplicate leading C-#### ids and
// returns one group per duplicated token, ordered by numeric id (each group's
// paths sorted). A clean vault — or a missing ledger — yields nil. It is the
// post-pull/rebase backstop (design §5): a "keep both" merge that lands two
// notes with one id is reported here rather than silently losing a cycle.
func (s *Store) CycleIDDuplicates() ([]CycleIDConflict, error) {
	paths, err := s.cycleNotePaths()
	if err != nil {
		return nil, err
	}
	byID := make(map[string][]string)
	for _, p := range paths {
		tok := cycleIDFromFilename(filepath.Base(p))
		byID[tok] = append(byID[tok], p)
	}
	var out []CycleIDConflict
	for id, ps := range byID {
		if len(ps) < 2 {
			continue
		}
		sort.Strings(ps)
		out = append(out, CycleIDConflict{ID: id, Paths: ps})
	}
	sort.Slice(out, func(i, j int) bool {
		ni, _ := cycleIDNum(out[i].ID)
		nj, _ := cycleIDNum(out[j].ID)
		if ni != nj {
			return ni < nj
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// CheckCycleIDs is the loud post-pull/rebase preflight: it returns a
// state_conflict (7) naming both files when two or more cycle notes share a
// leading C-#### token, or nil on a clean vault. Reads report duplicates;
// `cycle reconcile` is what resolves them.
func (s *Store) CheckCycleIDs() error {
	dups, err := s.CycleIDDuplicates()
	if err != nil {
		return err
	}
	if len(dups) == 0 {
		return nil
	}
	return DuplicateCycleIDError(dups[0])
}

// DuplicateCycleIDError renders the state_conflict (7) for a duplicate group,
// naming every file that carries the id and hinting the pull/reconcile loop.
func DuplicateCycleIDError(c CycleIDConflict) error {
	return exit.New(exit.StateConflict,
		fmt.Sprintf("duplicate cycle id %s in %d notes: %s", c.ID, len(c.Paths), strings.Join(c.Paths, ", ")),
		"run: git pull --rebase, then orbit cycles cycle reconcile")
}
