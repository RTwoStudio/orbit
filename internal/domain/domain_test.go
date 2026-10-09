package domain

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/RTwoStudio/orbit/internal/scaffold"
)

func TestCanonicalDomains(t *testing.T) {
	if NeoCortex.Name != "neocortex" || NeoCortex.Base != "neocortex" {
		t.Errorf("NeoCortex = %+v, want {neocortex neocortex}", NeoCortex)
	}
	if Cycles.Name != "cycles" || Cycles.Base != "cycles" {
		t.Errorf("Cycles = %+v, want {cycles cycles}", Cycles)
	}
	if len(All) != 2 || All[0] != NeoCortex || All[1] != Cycles {
		t.Errorf("All = %+v, want [NeoCortex Cycles]", All)
	}
	if !IsRequired(NeoCortex) {
		t.Error("NeoCortex must be required")
	}
	if IsRequired(Cycles) {
		t.Error("Cycles must be optional")
	}
	if !IsKnownBase("neocortex") || !IsKnownBase("cycles") || IsKnownBase("nope") {
		t.Error("IsKnownBase mismatch")
	}
}

func TestNeoCortexPathsByteIdentical(t *testing.T) {
	home, _ := os.UserHomeDir()
	wantCache := filepath.Join(home, ".config", "orbit", "neocortex", "cache")
	if got := NeoCortex.CacheDir(); got != wantCache {
		t.Errorf("NeoCortex.CacheDir() = %q, want %q", got, wantCache)
	}
	wantDeployed := filepath.Join(home, ".config", "orbit", "neocortex", "deployed.json")
	if got := NeoCortex.DeployedPath(); got != wantDeployed {
		t.Errorf("NeoCortex.DeployedPath() = %q, want %q", got, wantDeployed)
	}
}

func TestCyclesPaths(t *testing.T) {
	home, _ := os.UserHomeDir()
	wantCache := filepath.Join(home, ".config", "orbit", "cycles", "cache")
	if got := Cycles.CacheDir(); got != wantCache {
		t.Errorf("Cycles.CacheDir() = %q, want %q", got, wantCache)
	}
	wantDeployed := filepath.Join(home, ".config", "orbit", "cycles", "deployed.json")
	if got := Cycles.DeployedPath(); got != wantDeployed {
		t.Errorf("Cycles.DeployedPath() = %q, want %q", got, wantDeployed)
	}
	if NeoCortex.CacheDir() == Cycles.CacheDir() {
		t.Error("neocortex and cycles cache dirs must differ")
	}
	if NeoCortex.DeployedPath() == Cycles.DeployedPath() {
		t.Error("neocortex and cycles deployed paths must differ")
	}
}

func TestCyclesStubTokensRegistered(t *testing.T) {
	if err := scaffold.ValidateStub("work.stub.md", []byte(
		"{{WORK_ID}} {{WORK_TITLE}} {{SCOPE}} {{DATE}} {{REGISTRY_VERSION}}")); err != nil {
		t.Errorf("work stub rejected: %v", err)
	}
	if err := scaffold.ValidateStub("cycle.stub.md", []byte(
		"{{RELEASE}} {{CYCLE_ID}} {{CYCLE_GOAL}} {{START_DATE}} {{END_DATE}} {{DATE}} {{REGISTRY_VERSION}}")); err != nil {
		t.Errorf("cycle stub rejected: %v", err)
	}
	if err := scaffold.ValidateStub("work.stub.md", []byte("{{NOPE}}")); err == nil {
		t.Error("expected unknown-token rejection for work.stub.md")
	}
	if err := scaffold.ValidateStub("cycle.stub.md", []byte("{{TASK_ID}}")); err == nil {
		t.Error("expected unknown-token rejection for cycle.stub.md")
	}
}
