package deploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RTwoStudio/orbit-cli/internal/registry"
)

func TestTargetPathMapping(t *testing.T) {
	cases := []struct {
		in, want string
		wantErr  bool
	}{
		{"opencode/agents/neocortex.md", filepath.Join("~oc", "agents", "neocortex.md"), false},
		{"opencode/commands/close.md", filepath.Join("~oc", "commands", "close.md"), false},
		// Future subfolders inherit the 1:1 rule.
		{"opencode/plugins/x.md", filepath.Join("~oc", "plugins", "x.md"), false},
		{"stubs/task.stub.md", "", true},
		{"NEOCORTEX.md", "", true},
		{"opencode/../evil.md", "", true},
	}
	for _, c := range cases {
		got, err := TargetPath("~oc", c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("TargetPath(%q) should refuse", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("TargetPath(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("TargetPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDeployedLedgerRoundTrip(t *testing.T) {
	// Redirect HOME for ledger persistence.
	home := t.TempDir()
	t.Setenv("HOME", home)
	d := LoadDeployed()
	d["opencode/agents/x.md"] = DeployedRecord{Version: "0.2.0", SHA256: strings.Repeat("a", 64), DeployedAt: "now"}
	if err := d.Save(); err != nil {
		t.Fatal(err)
	}
	d2 := LoadDeployed()
	if d2["opencode/agents/x.md"].Version != "0.2.0" {
		t.Errorf("round trip failed: %+v", d2)
	}
}

func TestUpdateModeOrphansAndGates(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	oc := filepath.Join(home, "oc")

	// Manifest with one agent + one command; deployed.json knows both plus
	// an orphan.
	fc := &registry.Fetched{
		Manifest: &registry.Manifest{
			Version: "0.2.0",
			Files: registry.Files{
				OpenCode: registry.OpenCodeFiles{
					Agents:   []registry.FileEntry{{Path: "opencode/agents/a.md", SHA256: registry.SHA256Hex([]byte("a-new\n"))}},
					Commands: []registry.FileEntry{{Path: "opencode/commands/c.md", SHA256: registry.SHA256Hex([]byte("c\n"))}},
				},
			},
		},
		Content: map[string][]byte{
			"opencode/agents/a.md":   []byte("a-new\n"),
			"opencode/commands/c.md": []byte("c\n"),
		},
	}
	deployed := Deployed{
		"opencode/agents/a.md":    {Version: "0.1.0", SHA256: registry.SHA256Hex([]byte("a-old\n"))},
		"opencode/commands/c.md":  {Version: "0.1.0", SHA256: registry.SHA256Hex([]byte("c\n"))},
		"opencode/agents/dead.md": {Version: "0.1.0", SHA256: registry.SHA256Hex([]byte("dead\n"))},
	}
	// Pre-existing files: a.md holds the old version (version gate →
	// prompt); c.md already matches the registry content (re-record path).
	os.MkdirAll(filepath.Join(oc, "agents"), 0o755)
	os.MkdirAll(filepath.Join(oc, "commands"), 0o755)
	os.WriteFile(filepath.Join(oc, "agents", "a.md"), []byte("a-old\n"), 0o644)
	os.WriteFile(filepath.Join(oc, "commands", "c.md"), []byte("c\n"), 0o644)

	// Auto-decline all prompts.
	decisions, err := UpdateMode(fc, oc, deployed, func(msg string, def bool) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	byAction := map[string]int{}
	for _, d := range decisions {
		byAction[d.Action]++
	}
	if byAction["declined"] != 1 { // a.md version gate
		t.Errorf("expected 1 declined, got %v", byAction)
	}
	if byAction["up to date"] != 1 { // c.md content already matches
		t.Errorf("expected 1 up to date, got %v", byAction)
	}
	if byAction["orphaned"] != 1 { // dead.md
		t.Errorf("expected 1 orphaned, got %v", byAction)
	}
	// Declined file untouched.
	data, _ := os.ReadFile(filepath.Join(oc, "agents", "a.md"))
	if string(data) != "a-old\n" {
		t.Errorf("declined update was applied: %q", data)
	}

	// Auto-accept: everything applies, ledger saved.
	deployed = Deployed{
		"opencode/agents/a.md":    {Version: "0.1.0", SHA256: registry.SHA256Hex([]byte("a-old\n"))},
		"opencode/commands/c.md":  {Version: "0.1.0", SHA256: registry.SHA256Hex([]byte("c\n"))},
		"opencode/agents/dead.md": {Version: "0.1.0", SHA256: registry.SHA256Hex([]byte("dead\n"))},
	}
	if _, err := UpdateMode(fc, oc, deployed, func(msg string, def bool) bool { return true }); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(oc, "agents", "a.md"))
	if string(data) != "a-new\n" {
		t.Errorf("accepted update not applied: %q", data)
	}
	dj, _ := os.ReadFile(DeployedPath())
	if !strings.Contains(string(dj), "0.2.0") {
		t.Errorf("ledger not updated: %s", dj)
	}
}
