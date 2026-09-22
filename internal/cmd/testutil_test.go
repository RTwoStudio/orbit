package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RTwoStudio/orbit-cli/internal/config"
	"github.com/RTwoStudio/orbit-cli/internal/exit"
	"github.com/RTwoStudio/orbit-cli/internal/fsutil"
	"github.com/RTwoStudio/orbit-cli/internal/logx"
	"github.com/RTwoStudio/orbit-cli/internal/registry"
)

// fixtureRepo is a mutable copy of testdata/registry-fixture in a temp dir.
type fixtureRepo struct {
	Root    string
	Version string
	Fetches int
}

var (
	fixtureSrcOnce sync.Once
	fixtureSrc     string
	fixtureSrcErr  error
)

// fixtureSource resolves the committed fixture ONCE (before any t.Chdir
// in the same process can change the working directory).
func fixtureSource() string {
	fixtureSrcOnce.Do(func() {
		fixtureSrc, fixtureSrcErr = filepath.Abs(filepath.Join("..", "..", "testdata", "registry-fixture"))
	})
	if fixtureSrcErr != nil {
		panic(fixtureSrcErr)
	}
	return fixtureSrc
}

func newFixtureRepo(t *testing.T) *fixtureRepo {
	t.Helper()
	root := t.TempDir()
	if err := copyDir(fixtureSource(), root); err != nil {
		t.Fatal(err)
	}
	return &fixtureRepo{Root: root, Version: "0.1.0"}
}

// setVersion rewrites manifest version + rehashes every file.
func (f *fixtureRepo) setVersion(v string) {
	f.Version = v
	runManifest(f.Root, v)
}

func (f *fixtureRepo) fetcher() registry.DirFetcher {
	return registry.DirFetcher{Root: f.Root}
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if fi.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// harness isolates HOME, cwd, log dir, config, and the fetcher per test.
type harness struct {
	home    string
	project string
	repo    *fixtureRepo
	stdout  *bytes.Buffer
	stderr  *bytes.Buffer
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	logx.Close()
	t.Cleanup(logx.Close)

	home := t.TempDir()
	project := t.TempDir()
	repo := newFixtureRepo(t)

	t.Setenv("HOME", home)
	t.Setenv("ORBIT_LOG_DIR", filepath.Join(home, "log"))
	t.Setenv("ORBIT_DEBUG", "")
	t.Setenv("ORBIT_REGISTRY_TOKEN", "")
	os.Unsetenv("ORBIT_REGISTRY_URL")
	os.Unsetenv("ORBIT_OPENCODE_DIR")

	cfgDir := filepath.Join(home, ".config", "orbit")
	fsutil.EnsureDir(cfgDir)
	os.WriteFile(filepath.Join(cfgDir, "config.yml"), []byte(fmt.Sprintf(
		"registry:\n  url: %s\n  ref: main\n", repo.Root)), 0o644)

	prevFetcher := newFetcher
	newFetcher = func(cfg *config.Config, urlFlag, refFlag string) registry.Fetcher {
		repo.Fetches++
		return repo.fetcher()
	}
	t.Cleanup(func() { newFetcher = prevFetcher })

	t.Chdir(project)
	return &harness{
		home:    home,
		project: project,
		repo:    repo,
		stdout:  &bytes.Buffer{},
		stderr:  &bytes.Buffer{},
	}
}

func (h *harness) run(t *testing.T, args ...string) error {
	t.Helper()
	h.stdout.Reset()
	h.stderr.Reset()
	logx.Close()
	// Mirror cmd.Run()'s mandatory logging wrapper so start/end lines land
	// in the log exactly as they do in production.
	start := time.Now()
	logx.Info("start cmd=%q version=%s pid=%d", strings.Join(append([]string{"orbit"}, args...), " "), version, os.Getpid())
	root := NewRootCmd()
	root.SetOut(h.stdout)
	root.SetErr(h.stderr)
	root.SetArgs(args)
	err := root.Execute()
	code := 0
	switch e := err.(type) {
	case *exit.Error:
		code = int(e.Code)
	case nil:
	default:
		if isUsageError(e) {
			err = exit.New(exit.Usage, e.Error())
			code = int(exit.Usage)
		} else {
			code = 1
		}
	}
	logx.Info("end cmd=%q exit=%d duration=%s", strings.Join(append([]string{"orbit"}, args...), " "), code, time.Since(start).Round(time.Millisecond))
	if ee, ok := err.(*exit.Error); ok {
		return ee
	}
	return err
}

func (h *harness) runOK(t *testing.T, args ...string) {
	t.Helper()
	if err := h.run(t, args...); err != nil {
		t.Fatalf("orbit %s: unexpected error: %v\nstdout: %s\nstderr: %s",
			strings.Join(args, " "), err, h.stdout, h.stderr)
	}
}

func (h *harness) runCode(t *testing.T, want exit.Code, args ...string) *exit.Error {
	t.Helper()
	err := h.run(t, args...)
	ee, ok := err.(*exit.Error)
	if !ok {
		t.Fatalf("orbit %s: expected exit %d, got err=%v\nstdout: %s\nstderr: %s",
			strings.Join(args, " "), want, err, h.stdout, h.stderr)
	}
	if ee.Code != want {
		t.Fatalf("orbit %s: exit = %d (%s), want %d\nmsg: %s\nstdout: %s",
			strings.Join(args, " "), ee.Code, ee.Code.Name(), want, ee.Message, h.stdout)
	}
	return ee
}

func (h *harness) cacheDir() string {
	return filepath.Join(h.home, ".config", "orbit", "neocortex", "cache")
}

func (h *harness) opencodeDir() string {
	return filepath.Join(h.home, ".config", "opencode")
}

// fillConcept simulates the agent filling the concept (removes Agent
// comments, fills placeholders).
func (h *harness) fillConcept(t *testing.T) {
	t.Helper()
	path := filepath.Join(h.project, ".neocortex", "issues", "issue-1", "00-concept.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	s = replaceBetween(s, "## Objective", "## Detail",
		"\nBuild a streaming API endpoint. Done when clients can stream events.\n\n")
	s = replaceBetween(s, "## Detail", "## Requirements",
		"\nRaw spec content injected here.\n\n")
	s = replaceBetween(s, "## Requirements", "## Scope & Boundaries",
		"\n- [x] Streaming endpoint\n- [x] Auth\n\n")
	s = replaceBetween(s, "## Scope & Boundaries", "",
		"\n- **In scope:** endpoint\n- **Out of scope:** SDK\n")
	os.WriteFile(path, []byte(s), 0o644)
}

// fillPlan simulates the agent filling the plan + writing a Task DAG.
func (h *harness) fillPlan(t *testing.T, dagLines ...string) {
	t.Helper()
	path := filepath.Join(h.project, ".neocortex", "issues", "issue-1", "01-plan.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	s = replaceBetween(s, "## Architectural Decisions", "## Open Questions",
		"\n- **Q: Which transport?** → **SSE** — simplest\n\n")
	s = replaceBetween(s, "## Open Questions", "## Detail", "\n")
	s = replaceBetween(s, "## Detail", "## Task DAG", "\nnotes\n\n")
	dag := "\n" + strings.Join(dagLines, "\n") + "\n"
	s = replaceBetween(s, "## Task DAG", "## Amendments", dag)
	os.WriteFile(path, []byte(s), 0o644)
}

func (h *harness) fillAddenda(t *testing.T, nn int, deltas ...string) {
	t.Helper()
	entries, _ := os.ReadDir(filepath.Join(h.project, ".neocortex", "issues", "issue-1", "addenda"))
	var path string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), fmt.Sprintf("%02d-", nn)) {
			path = filepath.Join(h.project, ".neocortex", "issues", "issue-1", "addenda", e.Name())
		}
	}
	if path == "" {
		t.Fatalf("addenda %02d not found", nn)
	}
	data, _ := os.ReadFile(path)
	s := string(data)
	s = replaceBetween(s, "## Reasoning", "## Impact Analysis", "\nNew info arrived.\n\n")
	s = replaceBetween(s, "## Impact Analysis", "## Plan Changes", "\nAffects T1/T2.\n\n")
	s = replaceBetween(s, "## Plan Changes", "## Orchestrator Approval",
		"\n"+strings.Join(deltas, "\n")+"\n\n")
	s = strings.Replace(s, "- [ ] Approved by Orchestrator", "- [x] Approved by Orchestrator", 1)
	os.WriteFile(path, []byte(s), 0o644)
}

// replaceBetween replaces content strictly between two headings (or EOF
// when end is empty), keeping the headings.
func replaceBetween(s, start, end, replacement string) string {
	i := strings.Index(s, start+"\n")
	if i < 0 {
		return s
	}
	bodyStart := i + len(start) + 1
	rest := s[bodyStart:]
	if end == "" {
		return s[:bodyStart] + replacement
	}
	j := strings.Index(rest, "\n"+end+"\n")
	if j < 0 {
		return s[:bodyStart] + replacement
	}
	return s[:bodyStart] + replacement + rest[j:]
}
