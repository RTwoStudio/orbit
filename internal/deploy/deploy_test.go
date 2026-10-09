package deploy

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/RTwoStudio/orbit/internal/domain"
	"github.com/RTwoStudio/orbit/internal/registry"
)

func TestDeployedPathIsolation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if got, want := DeployedPath(domain.NeoCortex), filepath.Join(home, ".config", "orbit", "neocortex", "deployed.json"); got != want {
		t.Errorf("NeoCortex DeployedPath = %q, want %q", got, want)
	}
	if got, want := DeployedPath(domain.Cycles), filepath.Join(home, ".config", "orbit", "cycles", "deployed.json"); got != want {
		t.Errorf("Cycles DeployedPath = %q, want %q", got, want)
	}
}

func cyclesFetched() *registry.Fetched {
	const rel = "opencode/agents/cycles.md"
	content := []byte("# cycles orchestrator\n")
	return &registry.Fetched{
		Manifest: &registry.Manifest{Version: "0.1.0", Layout: 1, Files: registry.Files{
			OpenCode: registry.OpenCodeFiles{Agents: []registry.FileEntry{{Path: rel, SHA256: registry.SHA256Hex(content)}}},
		}},
		Content: map[string][]byte{rel: content},
	}
}

func TestInstallModePerDomainIsolation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ocDir := t.TempDir()

	decs, err := InstallMode(cyclesFetched(), domain.Cycles, ocDir, LoadDeployed(domain.Cycles))
	if err != nil {
		t.Fatalf("InstallMode cycles: %v", err)
	}
	if len(decs) != 1 || decs[0].Action != "installed" {
		t.Fatalf("decisions = %+v, want one installed", decs)
	}
	if _, err := os.Stat(filepath.Join(ocDir, "agents", "cycles.md")); err != nil {
		t.Fatalf("cycles agent not deployed: %v", err)
	}

	if _, ok := LoadDeployed(domain.Cycles)["opencode/agents/cycles.md"]; !ok {
		t.Error("cycles ledger missing the deployed asset")
	}
	if len(LoadDeployed(domain.NeoCortex)) != 0 {
		t.Error("cycles install wrote to the neocortex ledger")
	}
	if _, err := os.Stat(DeployedPath(domain.NeoCortex)); !os.IsNotExist(err) {
		t.Error("neocortex deployed.json must not be created by a cycles install")
	}
}

func TestUpdateModePerDomainIsolation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ocDir := t.TempDir()

	ask := func(string, bool) bool { return true }
	decs, err := UpdateMode(cyclesFetched(), domain.Cycles, ocDir, LoadDeployed(domain.Cycles), ask, false)
	if err != nil {
		t.Fatalf("UpdateMode cycles: %v", err)
	}
	if len(decs) != 1 || decs[0].Action != "installed" {
		t.Fatalf("decisions = %+v, want one installed", decs)
	}
	if len(LoadDeployed(domain.Cycles)) != 1 {
		t.Error("cycles ledger should hold exactly one record")
	}
	if len(LoadDeployed(domain.NeoCortex)) != 0 {
		t.Error("cycles update wrote to the neocortex ledger")
	}
}
