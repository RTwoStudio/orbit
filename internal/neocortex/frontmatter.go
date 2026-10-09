package neocortex

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ListIssues returns all issue numbers present on disk, sorted ascending.
func ListIssues() ([]int, error) {
	entries, err := os.ReadDir(IssuesDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if n, _, ok := ParseIssueDirName(e.Name()); ok {
			out = append(out, n)
		}
	}
	return out, nil
}

// AddendaFilePrefix returns the zero-padded 2-digit NN for addenda files.
func AddendaFilePrefix(n int) string {
	return fmt.Sprintf("%02d", n)
}

// FindAddendaFile locates the file for addenda NN in an issue's addenda dir.
func FindAddendaFile(issue, nn int) (string, error) {
	dir := AddendaDir(issue)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	prefix := AddendaFilePrefix(nn) + "-"
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), prefix) && strings.HasSuffix(e.Name(), ".md") {
			return filepath.Join(dir, e.Name()), nil
		}
	}
	return "", os.ErrNotExist
}
