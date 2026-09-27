// Package neocortex holds the domain logic: paths, frontmatter, status
// enums, DAG parsing, rendering, and all issue/plan/addenda/task commands.
package neocortex

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Root returns the project's .neocortex dir (cwd-relative by default).
func Root() string {
	wd, err := os.Getwd()
	if err != nil {
		return ".neocortex"
	}
	return filepath.Join(wd, ".neocortex")
}

// IsInitialized reports whether the project is initialized (§5.0 gate).
func IsInitialized() bool {
	return isDir(Root())
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// IssuesDir returns <root>/issues.
func IssuesDir() string { return filepath.Join(Root(), "issues") }

// Lane is the issue lane, encoded in the folder suffix and Class frontmatter.
type Lane string

const (
	LaneIssue Lane = "issue" // default lane (full protocol)
	LaneQuick Lane = "quick" // light lane (single 00-quick.md)
)

// IssueDirName builds the folder name: <4-digit N>-<lane> (e.g. 0001-issue).
// 4-digit zero padding keeps lexical order == numeric order through 9999.
func IssueDirName(n int, lane Lane) string {
	return fmt.Sprintf("%04d-%s", n, lane)
}

// IssueDir returns <root>/issues/<NNNN>-issue — the default-lane path.
// Use ResolveIssueDir for a lane-agnostic lookup.
func IssueDir(n int) string { return filepath.Join(IssuesDir(), IssueDirName(n, LaneIssue)) }

// QuickDir returns <root>/issues/<NNNN>-quick.
func QuickDir(n int) string { return filepath.Join(IssuesDir(), IssueDirName(n, LaneQuick)) }

// ResolveIssueDir returns the on-disk dir for issue n regardless of lane.
// Prefers the quick dir when present, else the default, else the default path
// (so callers get a usable path for error messages).
func ResolveIssueDir(n int) string {
	if isDir(QuickDir(n)) {
		return QuickDir(n)
	}
	if isDir(IssueDir(n)) {
		return IssueDir(n)
	}
	return IssueDir(n)
}

// IssueLane returns the lane of an on-disk issue, or "" when neither dir exists.
func IssueLane(n int) Lane {
	if isDir(QuickDir(n)) {
		return LaneQuick
	}
	if isDir(IssueDir(n)) {
		return LaneIssue
	}
	return ""
}

// ConceptPath / PlanPath / TasksDir / AddendaDir / NotesDir resolve through
// ResolveIssueDir so they work for either lane's folder.
func ConceptPath(n int) string { return filepath.Join(ResolveIssueDir(n), "00-concept.md") }
func PlanPath(n int) string    { return filepath.Join(ResolveIssueDir(n), "01-plan.md") }
func TasksDir(n int) string    { return filepath.Join(ResolveIssueDir(n), "tasks") }
func AddendaDir(n int) string  { return filepath.Join(ResolveIssueDir(n), "addenda") }
func NotesDir(n int) string    { return filepath.Join(ResolveIssueDir(n), "notes") }

func TaskPath(n int, id string) string { return filepath.Join(TasksDir(n), id+".md") }

// QuickPath returns the light-lane run file for issue n.
func QuickPath(n int) string { return filepath.Join(QuickDir(n), "00-quick.md") }

// ParseIssueDirName extracts (number, lane) from a folder name like
// "0007-quick". ok is false for anything that is not a valid issue dir.
func ParseIssueDirName(name string) (n int, lane Lane, ok bool) {
	i := strings.IndexByte(name, '-')
	if i <= 0 || i == len(name)-1 {
		return 0, "", false
	}
	num, err := strconv.Atoi(name[:i])
	if err != nil || num <= 0 {
		return 0, "", false
	}
	switch Lane(name[i+1:]) {
	case LaneIssue, LaneQuick:
		return num, Lane(name[i+1:]), true
	}
	return 0, "", false
}

// ReadActive reads the ACTIVE pointer (issue number).
func ReadActive() (int, error) {
	data, err := os.ReadFile(filepath.Join(Root(), "ACTIVE"))
	if err != nil {
		return 0, errActive("ACTIVE missing")
	}
	s := strings.TrimSpace(string(data))
	if s == "" {
		return 0, errActive("ACTIVE is empty")
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, errActive("ACTIVE does not contain an issue number: " + s)
	}
	if !isDir(ResolveIssueDir(n)) {
		return 0, errActive(fmt.Sprintf("ACTIVE points to nonexistent issue %d", n))
	}
	return n, nil
}

// WriteActive atomically writes the ACTIVE pointer.
func WriteActive(n int) error {
	return os.WriteFile(filepath.Join(Root(), "ACTIVE"), []byte(fmt.Sprintf("%d\n", n)), 0o644)
}

// NextIssueNumber computes max+1 across both lanes.
func NextIssueNumber() (int, error) {
	max := 0
	entries, err := os.ReadDir(IssuesDir())
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			if n, _, ok := ParseIssueDirName(e.Name()); ok && n > max {
				max = n
			}
		}
	}
	return max + 1, nil
}

func errActive(msg string) error {
	return fmt.Errorf("no active run: %s", msg)
}
