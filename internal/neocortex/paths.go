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

// IssueDir returns <root>/issues/issue-<n>.
func IssueDir(n int) string { return filepath.Join(IssuesDir(), fmt.Sprintf("issue-%d", n)) }

// ConceptPath / PlanPath / TasksDir / AddendaDir / NotesDir.
func ConceptPath(n int) string         { return filepath.Join(IssueDir(n), "00-concept.md") }
func PlanPath(n int) string            { return filepath.Join(IssueDir(n), "01-plan.md") }
func TasksDir(n int) string            { return filepath.Join(IssueDir(n), "tasks") }
func AddendaDir(n int) string          { return filepath.Join(IssueDir(n), "addenda") }
func NotesDir(n int) string            { return filepath.Join(IssueDir(n), "notes") }
func TaskPath(n int, id string) string { return filepath.Join(TasksDir(n), id+".md") }

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
	if !isDir(IssueDir(n)) {
		return 0, errActive(fmt.Sprintf("ACTIVE points to nonexistent issue %d", n))
	}
	return n, nil
}

// WriteActive atomically writes the ACTIVE pointer.
func WriteActive(n int) error {
	return os.WriteFile(filepath.Join(Root(), "ACTIVE"), []byte(fmt.Sprintf("%d\n", n)), 0o644)
}

// NextIssueNumber computes max+1 from the issues dir.
func NextIssueNumber() (int, error) {
	max := 0
	entries, err := os.ReadDir(IssuesDir())
	if err == nil {
		for _, e := range entries {
			name := e.Name()
			if !e.IsDir() || !strings.HasPrefix(name, "issue-") {
				continue
			}
			var n int
			if _, err := fmt.Sscanf(name, "issue-%d", &n); err == nil && n > max {
				max = n
			}
		}
	}
	return max + 1, nil
}

func errActive(msg string) error {
	return fmt.Errorf("no active run: %s", msg)
}
