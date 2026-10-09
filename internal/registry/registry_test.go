package registry

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RTwoStudio/orbit/internal/domain"
)

// writeDomainFixture writes files (domain-relative → content) plus a valid
// manifest.json with correct sha256 for each entry into <root>/<d.Base>.
func writeDomainFixture(t *testing.T, root string, d domain.Domain, files map[string]string) {
	t.Helper()
	m := Manifest{Version: "0.1.0", Layout: 1}
	for rel, content := range files {
		e := FileEntry{Path: rel, SHA256: SHA256Hex([]byte(content))}
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

func TestDirFetcherPerDomain(t *testing.T) {
	root := t.TempDir()
	writeDomainFixture(t, root, domain.NeoCortex, map[string]string{"stubs/task.stub.md": "{{TASK_ID}}"})
	writeDomainFixture(t, root, domain.Cycles, map[string]string{"stubs/work.stub.md": "{{WORK_ID}}"})

	neo, err := FetchAll(DirFetcher{Root: root, Domain: domain.NeoCortex})
	if err != nil {
		t.Fatalf("neocortex FetchAll: %v", err)
	}
	if _, ok := neo.Content["stubs/task.stub.md"]; !ok {
		t.Error("neocortex snapshot missing its own stub")
	}
	if _, ok := neo.Content["stubs/work.stub.md"]; ok {
		t.Error("neocortex snapshot leaked a cycles file")
	}

	cyc, err := FetchAll(DirFetcher{Root: root, Domain: domain.Cycles})
	if err != nil {
		t.Fatalf("cycles FetchAll: %v", err)
	}
	if _, ok := cyc.Content["stubs/work.stub.md"]; !ok {
		t.Error("cycles snapshot missing its own stub")
	}
	if _, ok := cyc.Content["stubs/task.stub.md"]; ok {
		t.Error("cycles snapshot leaked a neocortex file")
	}
}

func TestFetchAllDomainAbsent(t *testing.T) {
	root := t.TempDir()
	writeDomainFixture(t, root, domain.NeoCortex, map[string]string{"stubs/task.stub.md": "{{TASK_ID}}"})
	_, err := FetchAll(DirFetcher{Root: root, Domain: domain.Cycles})
	if !errors.Is(err, ErrDomainAbsent) {
		t.Fatalf("err = %v, want ErrDomainAbsent", err)
	}
}

func TestFetchAllDomainAbsentEmptyRoot(t *testing.T) {
	_, err := FetchAll(DirFetcher{Root: t.TempDir(), Domain: domain.Cycles})
	if !errors.Is(err, ErrDomainAbsent) {
		t.Fatalf("err = %v, want ErrDomainAbsent", err)
	}
}

func TestFetchAllIntegrityFailure(t *testing.T) {
	root := t.TempDir()
	writeDomainFixture(t, root, domain.NeoCortex, map[string]string{"stubs/task.stub.md": "{{TASK_ID}}"})
	tampered := filepath.Join(root, "neocortex", "stubs", "task.stub.md")
	if err := os.WriteFile(tampered, []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := FetchAll(DirFetcher{Root: root, Domain: domain.NeoCortex})
	if err == nil || errors.Is(err, ErrDomainAbsent) {
		t.Fatalf("err = %v, want integrity failure", err)
	}
	if !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Errorf("err = %q, want sha256 mismatch", err.Error())
	}
}

func TestDirFetcherRefusesTraversal(t *testing.T) {
	f := DirFetcher{Root: t.TempDir(), Domain: domain.NeoCortex}
	if _, err := f.Fetch("../outside"); err == nil {
		t.Error("expected traversal refusal")
	}
}

func TestCachePerDomainIsolation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()
	writeDomainFixture(t, root, domain.NeoCortex, map[string]string{"stubs/task.stub.md": "{{TASK_ID}}"})
	writeDomainFixture(t, root, domain.Cycles, map[string]string{"stubs/work.stub.md": "{{WORK_ID}}"})

	neo, err := FetchAll(DirFetcher{Root: root, Domain: domain.NeoCortex})
	if err != nil {
		t.Fatal(err)
	}
	cyc, err := FetchAll(DirFetcher{Root: root, Domain: domain.Cycles})
	if err != nil {
		t.Fatal(err)
	}
	if err := CacheSave(neo, domain.NeoCortex); err != nil {
		t.Fatalf("CacheSave neocortex: %v", err)
	}
	if err := CacheSave(cyc, domain.Cycles); err != nil {
		t.Fatalf("CacheSave cycles: %v", err)
	}

	// Paths are byte-identical to the pre-domain-generic layout for neocortex,
	// and namespaced for cycles.
	if got, want := cacheDir(domain.NeoCortex), filepath.Join(home, ".config", "orbit", "neocortex", "cache"); got != want {
		t.Errorf("neocortex cacheDir = %q, want %q", got, want)
	}
	if got, want := cacheDir(domain.Cycles), filepath.Join(home, ".config", "orbit", "cycles", "cache"); got != want {
		t.Errorf("cycles cacheDir = %q, want %q", got, want)
	}

	gotNeo, err := CacheLoad(domain.NeoCortex)
	if err != nil {
		t.Fatalf("CacheLoad neocortex: %v", err)
	}
	if _, ok := gotNeo.Content["stubs/task.stub.md"]; !ok {
		t.Error("neocortex cache missing its stub")
	}
	gotCyc, err := CacheLoad(domain.Cycles)
	if err != nil {
		t.Fatalf("CacheLoad cycles: %v", err)
	}
	if _, ok := gotCyc.Content["stubs/work.stub.md"]; !ok {
		t.Error("cycles cache missing its stub")
	}
	// No cross-contamination on disk.
	if _, err := os.Stat(filepath.Join(home, ".config", "orbit", "neocortex", "cache", "stubs", "work.stub.md")); !os.IsNotExist(err) {
		t.Error("neocortex cache unexpectedly holds a cycles file")
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "orbit", "cycles", "cache", "stubs", "task.stub.md")); !os.IsNotExist(err) {
		t.Error("cycles cache unexpectedly holds a neocortex file")
	}

	if CacheManifestHash(domain.NeoCortex) == "" || CacheManifestHash(domain.Cycles) == "" {
		t.Error("manifest hashes must be non-empty after save")
	}
	if CacheManifestHash(domain.NeoCortex) == CacheManifestHash(domain.Cycles) {
		t.Error("distinct manifests must hash differently")
	}
}
