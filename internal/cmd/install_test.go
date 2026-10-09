package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RTwoStudio/orbit/internal/cycles"
	"github.com/RTwoStudio/orbit/internal/domain"
	"github.com/RTwoStudio/orbit/internal/exit"
	"github.com/RTwoStudio/orbit/internal/fsutil"
)

// cyclesInstallCyclesMD is the fetched root asset install copies into the
// vault. Its exact bytes are the fixture's; the real one lives in
// orbit-registry/cycles/CYCLES.md.
const cyclesInstallCyclesMD = "---\nRegistry-Version: 0.1.0\n---\n\n# CYCLES\n\nfixture quick ref.\n"

// setupCyclesInstall isolates $HOME, primes a hash-valid cycles registry
// fixture (CYCLES.md + both stubs), and writes a temp config with a registry
// URL plus vault.dir. The vault itself is left untouched — creating it is the
// behavior under test.
func setupCyclesInstall(t *testing.T, vault string) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ORBIT_NO_COMPLETION", "1")

	regRoot := t.TempDir()
	writeCmdDomainFixture(t, regRoot, domain.Cycles, map[string]string{
		"CYCLES.md":           cyclesInstallCyclesMD,
		"stubs/work.stub.md":  cyclesCmdWorkStub,
		"stubs/cycle.stub.md": cyclesCmdCycleStub,
	})
	withFixtureFetcher(t, regRoot)

	return writeInstallConfig(t, vault)
}

// writeInstallConfig writes a minimal config.yml with a registry URL and the
// given vault.dir.
func writeInstallConfig(t *testing.T, vault string) string {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "config.yml")
	body := "registry:\n  url: https://example.invalid/registry.git\nvault:\n  dir: " + vault + "\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfgPath
}

// installEnvelope is the --json shape printSummaryAs emits.
type installEnvelope struct {
	Command string       `json:"command"`
	Steps   []StepReport `json:"steps"`
}

func TestCyclesInstallBootstrapsVault(t *testing.T) {
	vault := t.TempDir()
	cfgPath := setupCyclesInstall(t, vault)

	out, _, err := runCycles(t, cfgPath, "cycles", "install", "--json")
	if err != nil {
		t.Fatalf("cycles install --json: %v", err)
	}
	env := decodeJSON[installEnvelope](t, out)
	if env.Command != "cycles install" {
		t.Errorf("json command = %q, want %q", env.Command, "cycles install")
	}
	if len(env.Steps) == 0 {
		t.Fatal("json steps is empty")
	}
	seen := map[string]bool{}
	for _, s := range env.Steps {
		seen[s.Step] = true
	}
	for _, want := range []string{"cycles fetch", "cycles cache", "cycles vault", "cycles CURRENT", "cycles CYCLES.md"} {
		if !seen[want] {
			t.Errorf("json steps missing %q; got %+v", want, env.Steps)
		}
	}

	s := cycles.Open(vault)
	for _, d := range []string{s.CyclesDir(), s.BacklogDir(), s.LedgerDir()} {
		if !fsutil.IsDir(d) {
			t.Errorf("%s is not a directory", d)
		}
	}
	md, err := os.ReadFile(filepath.Join(s.CyclesDir(), "CYCLES.md"))
	if err != nil {
		t.Fatalf("read CYCLES.md: %v", err)
	}
	if string(md) != cyclesInstallCyclesMD {
		t.Errorf("CYCLES.md = %q, want the fetched root asset", md)
	}
	cur, err := os.ReadFile(s.CurrentPath())
	if err != nil {
		t.Fatalf("read CURRENT: %v", err)
	}
	if len(cur) != 0 {
		t.Errorf("CURRENT = %q, want empty", cur)
	}

	// After install, a work verb succeeds against the bootstrapped vault.
	out, _, err = runCycles(t, cfgPath, "cycles", "work", "new", "Voice search", "--scope", "app", "--json")
	if err != nil {
		t.Fatalf("work new after install: %v", err)
	}
	if item := decodeJSON[cycles.WorkItem](t, out); item.ID != "W-0001" {
		t.Errorf("work new id = %q, want W-0001", item.ID)
	}
}

func TestCyclesInstallIdempotentAndCopyIfMissing(t *testing.T) {
	vault := t.TempDir()
	cfgPath := setupCyclesInstall(t, vault)

	if _, _, err := runCycles(t, cfgPath, "cycles", "install"); err != nil {
		t.Fatalf("first install: %v", err)
	}
	s := cycles.Open(vault)
	mdPath := filepath.Join(s.CyclesDir(), "CYCLES.md")

	// Locally edit CYCLES.md: copy-if-missing must preserve it on re-run.
	edited := "---\nRegistry-Version: 0.1.0\n---\n\n# CYCLES\n\nlocally edited.\n"
	if err := fsutil.AtomicWrite(mdPath, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	out, _, err := runCycles(t, cfgPath, "cycles", "install")
	if err != nil {
		t.Fatalf("second install: %v", err)
	}
	if !strings.Contains(out, "install summary:") {
		t.Errorf("human install output missing summary header; got:\n%s", out)
	}
	if !strings.Contains(out, "up to date") {
		t.Errorf("second install must report 'up to date'; got:\n%s", out)
	}
	after, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("read CYCLES.md after re-run: %v", err)
	}
	if string(after) != edited {
		t.Errorf("CYCLES.md was overwritten on re-run: %q", after)
	}
	if cur, _ := os.ReadFile(s.CurrentPath()); len(cur) != 0 {
		t.Errorf("CURRENT rewritten on re-run: %q", cur)
	}
}

func TestCyclesInstallMissingDomain(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ORBIT_NO_COMPLETION", "1")
	// An empty registry root: cycles/manifest.json is absent.
	withFixtureFetcher(t, t.TempDir())

	cfgPath := writeInstallConfig(t, t.TempDir())
	_, _, err := runCycles(t, cfgPath, "cycles", "install")
	wantCode(t, err, exit.RegistryUnreachable)
}

func TestCyclesInstallEmptyVault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ORBIT_NO_COMPLETION", "1")

	cfgPath := filepath.Join(t.TempDir(), "empty.yml")
	body := "registry:\n  url: https://example.invalid/registry.git\nvault:\n  dir: \"\"\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := runCycles(t, cfgPath, "cycles", "install")
	wantCode(t, err, exit.ConfigError)
}

func TestCyclesInstallVaultOverride(t *testing.T) {
	configVault := t.TempDir()
	overrideVault := t.TempDir()
	cfgPath := setupCyclesInstall(t, configVault)

	if _, _, err := runCycles(t, cfgPath, "cycles", "--vault", overrideVault, "install"); err != nil {
		t.Fatalf("install --vault: %v", err)
	}
	if !fsutil.IsDir(filepath.Join(overrideVault, "Cycles")) {
		t.Error("Cycles/ not created under --vault")
	}
	if fsutil.Exists(filepath.Join(configVault, "Cycles")) {
		t.Error("Cycles/ wrongly created under the config vault")
	}
}

func TestCyclesInstallInitAlias(t *testing.T) {
	vault := t.TempDir()
	cfgPath := setupCyclesInstall(t, vault)

	if _, _, err := runCycles(t, cfgPath, "cycles", "init"); err != nil {
		t.Fatalf("cycles init: %v", err)
	}
	if !fsutil.IsDir(cycles.Open(vault).CyclesDir()) {
		t.Error("init did not bootstrap the vault")
	}
}
