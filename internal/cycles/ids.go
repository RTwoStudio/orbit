package cycles

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/RTwoStudio/orbit/internal/exit"
)

// workIDRe is the canonical work-id grammar: W- followed by 4+ digits. The
// capture group carries the numeric part for workIDNum.
var workIDRe = regexp.MustCompile(`^W-(\d{4,})$`)

// ParseWorkID validates and returns a canonical W-#### id. Anything else is a
// preflight_failed (6).
func ParseWorkID(id string) (string, error) {
	s := strings.TrimSpace(id)
	if !workIDRe.MatchString(s) {
		return "", exit.New(exit.PreflightFailed,
			fmt.Sprintf("invalid work id %q — syntax: W-#### (W- followed by four or more digits)", id))
	}
	return s, nil
}

// workIDNum extracts the numeric part of a canonical W-#### id.
func workIDNum(id string) (int, bool) {
	m := workIDRe.FindStringSubmatch(id)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, false
	}
	return n, true
}

// workIDFromFilename extracts a note filename's leading ID token, e.g.
// "W-0001 - voice-search.md" → "W-0001", "W-0002.md" → "W-0002".
func workIDFromFilename(name string) string {
	base := strings.TrimSuffix(name, ".md")
	if i := strings.IndexByte(base, ' '); i >= 0 {
		base = base[:i]
	}
	return base
}

// NextWorkID returns the next free W-#### id: max over backlog/ and every
// cycles/C-*/ directory, plus one, zero-padded to at least four digits. It is
// monotonic and never reuses an id (delivered items stay on disk).
func (s *Store) NextWorkID() (string, error) {
	files, err := s.workFiles()
	if err != nil {
		return "", exit.Wrap(exit.IOError, err, "cannot scan cycles dirs")
	}
	max := 0
	for _, p := range files {
		if n, ok := workIDNum(workIDFromFilename(filepath.Base(p))); ok && n > max {
			max = n
		}
	}
	return fmt.Sprintf("W-%04d", max+1), nil
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
