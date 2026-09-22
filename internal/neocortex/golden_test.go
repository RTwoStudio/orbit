package neocortex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// goldenRender verifies the four registry stubs render deterministically
// per source mode against golden files in testdata/golden/.
func TestGoldenRenders(t *testing.T) {
	stubDir := filepath.Join("..", "..", "testdata", "registry-fixture", "neocortex", "stubs")
	stubs := map[string]string{
		"00-concept.stub.md": string(mustRead(t, filepath.Join(stubDir, "00-concept.stub.md"))),
		"01-plan.stub.md":    string(mustRead(t, filepath.Join(stubDir, "01-plan.stub.md"))),
		"task.stub.md":       string(mustRead(t, filepath.Join(stubDir, "task.stub.md"))),
		"addenda.stub.md":    string(mustRead(t, filepath.Join(stubDir, "addenda.stub.md"))),
	}

	modes := map[string]map[string]string{
		"interactive": {
			"ISSUE_ID": "7", "ISSUE_TITLE": "Golden issue", "DATE": "2026-09-22T00:00:00Z",
			"SOURCE": "interactive", "REGISTRY_VERSION": "0.1.0",
			"DETAIL": "<!-- Agent: transcribe the Orchestrator's answers here verbatim,\nno embellishment. -->",
		},
		"file": {
			"ISSUE_ID": "7", "ISSUE_TITLE": "Golden issue", "DATE": "2026-09-22T00:00:00Z",
			"SOURCE": "file:/spec.md", "REGISTRY_VERSION": "0.1.0",
			"DETAIL": "verbatim file content",
		},
		"remote": {
			"ISSUE_ID": "7", "ISSUE_TITLE": "Golden issue", "DATE": "2026-09-22T00:00:00Z",
			"SOURCE": "remote:https://github.com/acme/repo/issues/7", "REGISTRY_VERSION": "0.1.0",
			"DETAIL": "remote body",
		},
	}

	for stubName, stubText := range stubs {
		// Task stub needs its own tokens; give every mode a superset.
		extra := map[string]string{
			"TASK_ID": "T2", "TASK_NAME": "Golden task", "DEPENDS_ON": "T1",
			"ORIGIN": "plan", "BLOCKED_BY": "T1", "BLOCKS": "T3",
			"ADDENDA_NUM": "1", "ADDENDA_TITLE": "Golden addenda",
		}
		for mode, base := range modes {
			vals := map[string]string{}
			for k, v := range base {
				vals[k] = v
			}
			for k, v := range extra {
				vals[k] = v
			}
			got, err := Render(stubText, vals)
			if err != nil {
				t.Fatalf("%s × %s: %v", stubName, mode, err)
			}
			goldenPath := filepath.Join("..", "..", "testdata", "golden",
				strings.TrimSuffix(stubName, ".md")+"."+mode+".golden")
			if os.Getenv("ORBIT_UPDATE_GOLDEN") == "1" {
				os.MkdirAll(filepath.Dir(goldenPath), 0o755)
				os.WriteFile(goldenPath, got, 0o644)
				continue
			}
			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("golden missing (%s × %s): %v — regenerate with ORBIT_UPDATE_GOLDEN=1", stubName, mode, err)
			}
			if string(want) != string(got) {
				t.Errorf("golden mismatch for %s × %s:\n--- want ---\n%s\n--- got ---\n%s",
					stubName, mode, want, got)
			}
		}
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
