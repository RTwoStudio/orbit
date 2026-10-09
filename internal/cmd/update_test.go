package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RTwoStudio/orbit/internal/config"
	"github.com/RTwoStudio/orbit/internal/domain"
	"github.com/RTwoStudio/orbit/internal/registry"
)

// writeCmdDomainFixture writes a domain tree + valid manifest under
// <root>/<d.Base>. Shared shape with the registry package fixture, kept local
// so the cmd tests exercise the real FetchAll path.
func writeCmdDomainFixture(t *testing.T, root string, d domain.Domain, files map[string]string) {
	t.Helper()
	m := registry.Manifest{Version: "0.1.0", Layout: 1}
	for rel, content := range files {
		e := registry.FileEntry{Path: rel, SHA256: registry.SHA256Hex([]byte(content))}
		switch {
		case strings.HasPrefix(rel, "stubs/"):
			m.Files.Stubs = append(m.Files.Stubs, e)
		case strings.HasPrefix(rel, "opencode/agents/"):
			m.Files.OpenCode.Agents = append(m.Files.OpenCode.Agents, e)
		case strings.HasPrefix(rel, "opencode/commands/"):
			m.Files.OpenCode.Commands = append(m.Files.OpenCode.Commands, e)
		default:
			m.Files.Root = append(m.Files.Root, e)
		}
	}
	dir := filepath.Join(root, d.Base)
	for rel, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// withFixtureFetcher serves root as the registry for every domain.
func withFixtureFetcher(t *testing.T, root string) {
	t.Helper()
	orig := newFetcher
	newFetcher = func(cfg *config.Config, d domain.Domain, urlFlag, refFlag string) registry.Fetcher {
		return registry.DirFetcher{Root: root, Domain: d}
	}
	t.Cleanup(func() { newFetcher = orig })
}

func runUpdateForTest(t *testing.T) (string, error) {
	t.Helper()
	cmd := newUpdateCmd()
	var out, errb bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errb)
	cmd.SetArgs([]string{"--registry", "https://example.invalid/registry.git"})
	err := cmd.Execute()
	return out.String() + errb.String(), err
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func TestUpdateSkipsAbsentCycles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()
	writeCmdDomainFixture(t, root, domain.NeoCortex, map[string]string{
		"stubs/00-concept.stub.md":     "{{ISSUE_ID}}",
		"opencode/agents/neocortex.md": "# neocortex\n",
	})
	withFixtureFetcher(t, root)

	out, err := runUpdateForTest(t)
	if err != nil {
		t.Fatalf("update returned error: %v", err)
	}
	if !strings.Contains(out, "skipped") || !strings.Contains(out, "cycles") {
		t.Errorf("expected a cycles skip step; got:\n%s", out)
	}

	// NeoCortex refreshed byte-identically (same paths), cycles absent.
	if !exists(filepath.Join(home, ".config", "orbit", "neocortex", "cache", "manifest.json")) {
		t.Error("neocortex cache not populated")
	}
	if exists(filepath.Join(home, ".config", "orbit", "cycles", "cache", "manifest.json")) {
		t.Error("cycles cache must not be created when the domain is absent")
	}
	if !exists(filepath.Join(home, ".config", "opencode", "agents", "neocortex.md")) {
		t.Error("neocortex asset not deployed")
	}
}

func TestUpdateRefreshesBothDomains(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()
	writeCmdDomainFixture(t, root, domain.NeoCortex, map[string]string{
		"stubs/00-concept.stub.md":     "{{ISSUE_ID}}",
		"opencode/agents/neocortex.md": "# neocortex\n",
	})
	writeCmdDomainFixture(t, root, domain.Cycles, map[string]string{
		"stubs/work.stub.md":        "{{WORK_ID}}",
		"stubs/cycle.stub.md":       "{{RELEASE}} {{CYCLE_ID}}",
		"opencode/agents/cycles.md": "# cycles\n",
	})
	withFixtureFetcher(t, root)

	if _, err := runUpdateForTest(t); err != nil {
		t.Fatalf("update returned error: %v", err)
	}

	for _, d := range domain.All {
		if !exists(filepath.Join(home, ".config", "orbit", d.Name, "cache", "manifest.json")) {
			t.Errorf("%s cache not populated", d.Name)
		}
		if !exists(filepath.Join(home, ".config", "orbit", d.Name, "deployed.json")) {
			t.Errorf("%s ledger not written", d.Name)
		}
	}
	// Both domains deploy into the shared opencode dir.
	if !exists(filepath.Join(home, ".config", "opencode", "agents", "neocortex.md")) {
		t.Error("neocortex asset not deployed")
	}
	if !exists(filepath.Join(home, ".config", "opencode", "agents", "cycles.md")) {
		t.Error("cycles asset not deployed")
	}
}

func TestUpdateRejectsBadCyclesStub(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cases := []struct {
		name   string
		stub   string
		body   string
		substr string
	}{
		{"unknown token", "work.stub.md", "{{NOPE}}", "stub validation failed"},
		{"unknown stub", "mystery.stub.md", "{{WORK_ID}}", "stub validation failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeCmdDomainFixture(t, root, domain.NeoCortex, map[string]string{
				"stubs/00-concept.stub.md":     "{{ISSUE_ID}}",
				"opencode/agents/neocortex.md": "# neocortex\n",
			})
			writeCmdDomainFixture(t, root, domain.Cycles, map[string]string{
				"stubs/" + tc.stub:          tc.body,
				"opencode/agents/cycles.md": "# cycles\n",
			})
			withFixtureFetcher(t, root)

			_, err := runUpdateForTest(t)
			if err == nil {
				t.Fatalf("expected %s failure", tc.name)
			}
			if !strings.Contains(err.Error(), tc.substr) {
				t.Errorf("err = %q, want substring %q", err.Error(), tc.substr)
			}
		})
	}
}
