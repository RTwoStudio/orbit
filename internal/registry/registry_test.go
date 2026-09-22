package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// fixtureRegistry builds a real mini-registry on disk with the exact §2
// layout, including manifest hashes, and returns (root, fetcher).
func fixtureRegistry(t *testing.T) (string, DirFetcher) {
	t.Helper()
	root := t.TempDir()
	neo := filepath.Join(root, "neocortex")
	for _, d := range []string{"stubs", filepath.Join("opencode", "agents"), filepath.Join("opencode", "commands")} {
		if err := os.MkdirAll(filepath.Join(neo, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"NEOCORTEX.md":                 "# NeoCortex quick reference\n",
		"stubs/00-concept.stub.md":     "---\nIssue-ID: {{ISSUE_ID}}\n---\n\n# {{ISSUE_TITLE}}\n",
		"stubs/01-plan.stub.md":        "---\nIssue-ID: {{ISSUE_ID}}\n---\n\n# Plan: {{ISSUE_TITLE}}\n",
		"stubs/addenda.stub.md":        "# Addenda {{ADDENDA_NUM}}: {{ADDENDA_TITLE}}\n",
		"stubs/task.stub.md":           "# {{TASK_ID}} — {{TASK_NAME}}\n",
		"opencode/agents/neocortex.md": "# orchestrator\n",
		"opencode/commands/issue.md":   "# /issue\n",
		"opencode/commands/close.md":   "# /close (not implemented in v0.1.0)\n",
	}
	entries := func(names []string) string {
		out := "["
		for i, n := range names {
			data := []byte(files[n])
			h := sha256.Sum256(data)
			if i > 0 {
				out += ","
			}
			out += `{"path":"` + n + `","sha256":"` + hex.EncodeToString(h[:]) + `"}`
		}
		return out + "]"
	}
	manifest := `{
	  "version": "0.1.0",
	  "layout": 1,
	  "files": {
	    "root": ` + entries([]string{"NEOCORTEX.md"}) + `,
	    "stubs": ` + entries([]string{"stubs/00-concept.stub.md", "stubs/01-plan.stub.md", "stubs/addenda.stub.md", "stubs/task.stub.md"}) + `,
	    "opencode": {
	      "agents": ` + entries([]string{"opencode/agents/neocortex.md"}) + `,
	      "commands": ` + entries([]string{"opencode/commands/issue.md", "opencode/commands/close.md"}) + `
	    }
	  }
	}`
	files["manifest.json"] = manifest
	for rel, content := range files {
		full := filepath.Join(neo, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// README.md at repo root — must NOT be part of the manifest.
	os.WriteFile(filepath.Join(root, "README.md"), []byte("repo docs\n"), 0o644)
	return root, DirFetcher{Root: root}
}

func TestFetchAllVerifiesHashes(t *testing.T) {
	_, f := fixtureRegistry(t)
	fc, err := FetchAll(f)
	if err != nil {
		t.Fatalf("FetchAll: %v", err)
	}
	if fc.Manifest.Version != "0.1.0" {
		t.Errorf("version = %q", fc.Manifest.Version)
	}
	if len(fc.Content) != 9 { // 8 manifest files + manifest itself
		t.Errorf("content count = %d", len(fc.Content))
	}
}

func TestFetchAllDetectsCorruption(t *testing.T) {
	root, f := fixtureRegistry(t)
	// Corrupt one deployed file without updating the manifest.
	os.WriteFile(filepath.Join(root, "neocortex", "opencode", "commands", "issue.md"),
		[]byte("tampered\n"), 0o644)
	_, err := FetchAll(f)
	if err == nil {
		t.Fatal("corruption should fail")
	}
	if got := err.Error(); !contains(got, "registry integrity") {
		t.Errorf("error should mention registry integrity: %q", got)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestPathTraversalRefused(t *testing.T) {
	_, f := fixtureRegistry(t)
	if _, err := f.Fetch("../README.md"); err == nil {
		t.Error("traversal path should be refused")
	}
	if _, err := f.Fetch("/etc/passwd"); err == nil {
		t.Error("absolute path should be refused")
	}
}

func TestSemVerValid(t *testing.T) {
	for _, v := range []string{"0.1.0", "1.2.3", "1.0.0-rc.1"} {
		if err := SemVerValid(v); err != nil {
			t.Errorf("%q should be valid: %v", v, err)
		}
	}
	for _, v := range []string{"", "1", "1.2", "a.b.c", "1.2.x"} {
		if err := SemVerValid(v); err == nil {
			t.Errorf("%q should be invalid", v)
		}
	}
}
