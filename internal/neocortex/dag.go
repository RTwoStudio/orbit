package neocortex

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/RTwoStudio/orbit/internal/exit"
)

// dagLineRe is the strict line grammar (§9):
// - [T1] Name (Blocked by: None|T2, T3) — optional context
var dagLineRe = regexp.MustCompile(
	`^- \[(T[0-9]+)\] (.+?) \(Blocked by: ((?:None|T[0-9]+(?:, T[0-9]+)*))\)(?: — (.*))?$`)

// Task is one DAG row.
type Task struct {
	ID        string
	Name      string
	DependsOn []string
	Context   string
	Line      string // raw source line
}

// DAG is an ordered list of tasks (position = topological order).
type DAG struct {
	Tasks []*Task
}

// skipComments walks lines tracking HTML comment blocks; a line is
// skippable if inside a comment (or itself a bare delimiter).
func skipComments(line string) (bool, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || trimmed == "<!--" || trimmed == "-->" || trimmed == "---" {
		return true, false
	}
	return false, false
}

// ParseDAG parses the body lines of a Task DAG section. Returns a
// preflight_failed error quoting the offending raw line. Lines inside
// HTML comment blocks are ignored.
func ParseDAG(sectionText []byte) (*DAG, error) {
	dag := &DAG{}
	seen := map[string]int{}
	inComment := false
	for _, raw := range strings.Split(string(sectionText), "\n") {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if inComment {
			if strings.Contains(line, "-->") {
				inComment = false
			}
			continue
		}
		if strings.HasPrefix(trimmed, "<!--") {
			if !strings.Contains(line, "-->") {
				inComment = true
			}
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		m := dagLineRe.FindStringSubmatch(trimmed)
		if m == nil {
			return nil, exit.New(exit.PreflightFailed,
				fmt.Sprintf("Task DAG line does not match grammar: %q", trimmed),
				"format: - [T1] Task name (Blocked by: None)",
				"format: - [T2] Task name (Blocked by: T1) — optional context")
		}
		id, name, depsStr, ctx := m[1], m[2], m[3], m[4]
		if strings.TrimSpace(name) == "" {
			return nil, exit.New(exit.PreflightFailed, fmt.Sprintf("Task DAG line has empty name: %q", trimmed))
		}
		var deps []string
		if depsStr != "None" {
			for _, d := range strings.Split(depsStr, ", ") {
				deps = append(deps, d)
			}
		}
		t := &Task{ID: id, Name: name, DependsOn: deps, Context: ctx, Line: trimmed}
		if prev, dup := seen[id]; dup {
			_ = prev
			return nil, exit.New(exit.PreflightFailed, fmt.Sprintf("Task DAG has duplicate ID %s: %q", id, trimmed))
		}
		seen[id] = len(dag.Tasks)
		dag.Tasks = append(dag.Tasks, t)
	}
	if err := dag.Validate(); err != nil {
		return nil, err
	}
	return dag, nil
}

// Validate checks: ≥1 task, deps exist, no self-dep, no dup dep,
// topological order (position(dep) < position(task)).
func (d *DAG) Validate() error {
	if len(d.Tasks) == 0 {
		return exit.New(exit.PreflightFailed, "Task DAG has no tasks (≥1 required)")
	}
	pos := map[string]int{}
	for i, t := range d.Tasks {
		pos[t.ID] = i
	}
	for _, t := range d.Tasks {
		seenDep := map[string]bool{}
		for _, dep := range t.DependsOn {
			if dep == t.ID {
				return exit.New(exit.PreflightFailed,
					fmt.Sprintf("Task DAG self-dependency on %s: %q", t.ID, t.Line))
			}
			if seenDep[dep] {
				return exit.New(exit.PreflightFailed,
					fmt.Sprintf("Task DAG duplicate dependency %s in %q", dep, t.Line))
			}
			seenDep[dep] = true
			p, ok := pos[dep]
			if !ok {
				return exit.New(exit.PreflightFailed,
					fmt.Sprintf("Task DAG dependency %s of %s does not exist: %q", dep, t.ID, t.Line))
			}
			if p > pos[t.ID] {
				return exit.New(exit.PreflightFailed,
					fmt.Sprintf("Task DAG is not topologically valid: %s (line %d) blocks %s (line %d) — %q must come after its dependency",
						dep, p+1, t.ID, pos[t.ID]+1, t.Line))
			}
		}
	}
	return nil
}

// Get returns a task by ID.
func (d *DAG) Get(id string) *Task {
	for _, t := range d.Tasks {
		if t.ID == id {
			return t
		}
	}
	return nil
}

// BlocksOf returns tasks that depend on id (the inverse of DependsOn).
func (d *DAG) BlocksOf(id string) []string {
	var out []string
	for _, t := range d.Tasks {
		for _, dep := range t.DependsOn {
			if dep == id {
				out = append(out, t.ID)
				break
			}
		}
	}
	return out
}

// IDs returns all task IDs in DAG order.
func (d *DAG) IDs() []string {
	out := make([]string, len(d.Tasks))
	for i, t := range d.Tasks {
		out[i] = t.ID
	}
	return out
}
