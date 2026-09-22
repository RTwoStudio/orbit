package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/RTwoStudio/orbit-cli/internal/registry"
)

// runManifest regenerates manifest.json for a fixture repo with the given
// version (mirrors the registry repo's `make manifest` script).
func runManifest(root, version string) {
	neo := filepath.Join(root, "neocortex")
	type entry struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	}
	mk := func(rel string) entry {
		data, err := os.ReadFile(filepath.Join(neo, rel))
		if err != nil {
			panic(err)
		}
		return entry{Path: rel, SHA256: registry.SHA256Hex(data)}
	}
	m := struct {
		Version string         `json:"version"`
		Layout  int            `json:"layout"`
		Files   map[string]any `json:"files"`
	}{
		Version: version,
		Layout:  1,
		Files: map[string]any{
			"root": []entry{mk("NEOCORTEX.md")},
			"stubs": []entry{
				mk("stubs/00-concept.stub.md"),
				mk("stubs/01-plan.stub.md"),
				mk("stubs/addenda.stub.md"),
				mk("stubs/task.stub.md"),
			},
			"opencode": map[string][]entry{
				"agents": {
					mk("opencode/agents/neocortex.md"),
					mk("opencode/agents/neocortex-planner.md"),
					mk("opencode/agents/neocortex-task-creator.md"),
					mk("opencode/agents/neocortex-implementer.md"),
				},
				"commands": {
					mk("opencode/commands/issue.md"),
					mk("opencode/commands/plan.md"),
					mk("opencode/commands/new-task.md"),
					mk("opencode/commands/implement.md"),
					mk("opencode/commands/addenda.md"),
					mk("opencode/commands/close.md"),
				},
			},
		},
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(neo, "manifest.json"), data, 0o644); err != nil {
		panic(fmt.Sprintf("write manifest: %v", err))
	}
}
