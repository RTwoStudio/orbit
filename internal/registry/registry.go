// Package registry implements fetching the remote asset registry,
// manifest parsing, sha256 verification, and the global cache.
package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// FileEntry is one deployable asset in the manifest.
type FileEntry struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// OpenCodeFiles groups agents and commands.
type OpenCodeFiles struct {
	Agents   []FileEntry `json:"agents"`
	Commands []FileEntry `json:"commands"`
}

// Files is the manifest's file inventory.
type Files struct {
	Root     []FileEntry   `json:"root"`
	Stubs    []FileEntry   `json:"stubs"`
	OpenCode OpenCodeFiles `json:"opencode"`
}

// Manifest is the registry's version anchor.
type Manifest struct {
	Version string `json:"version"`
	Layout  int    `json:"layout"`
	Files   Files  `json:"files"`
}

// AllEntries returns every file entry in the manifest.
func (m *Manifest) AllEntries() []FileEntry {
	out := make([]FileEntry, 0, 16)
	out = append(out, m.Files.Root...)
	out = append(out, m.Files.Stubs...)
	out = append(out, m.Files.OpenCode.Agents...)
	out = append(out, m.Files.OpenCode.Commands...)
	return out
}

// ParseManifest parses and structurally validates manifest.json.
func ParseManifest(data []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("manifest.json: %w", err)
	}
	if m.Version == "" {
		return nil, fmt.Errorf("manifest.json: missing version")
	}
	if m.Layout != 1 {
		return nil, fmt.Errorf("manifest.json: unsupported layout %d", m.Layout)
	}
	for _, e := range m.AllEntries() {
		if e.Path == "" {
			return nil, fmt.Errorf("manifest.json: empty path entry")
		}
		// Path traversal guard: entries are relative to neocortex/.
		if filepath.IsAbs(e.Path) || strings.Contains(e.Path, "..") {
			return nil, fmt.Errorf("manifest.json: unsafe path %q", e.Path)
		}
		if len(e.SHA256) != 64 {
			return nil, fmt.Errorf("manifest.json: bad sha256 for %s", e.Path)
		}
	}
	return &m, nil
}

// SHA256Hex returns the hex sha256 of b.
func SHA256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// Fetcher abstracts registry acquisition so tests use local fixtures.
type Fetcher interface {
	// Fetch returns the raw bytes of a file relative to the neocortex/
	// folder. Paths that escape the folder must be refused.
	Fetch(relPath string) ([]byte, error)
}

// DirFetcher serves a registry from a local directory (tests + fallback).
type DirFetcher struct {
	// Root is the repo root; all fetches are scoped to Root/neocortex.
	Root string
}

func (d DirFetcher) Fetch(relPath string) ([]byte, error) {
	clean := filepath.Clean(relPath)
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
		return nil, fmt.Errorf("refusing path outside neocortex/: %s", relPath)
	}
	full := filepath.Join(d.Root, "neocortex", clean)
	// Double-check containment after joining.
	neocortexRoot := filepath.Join(d.Root, "neocortex")
	absFull, _ := filepath.Abs(full)
	absRoot, _ := filepath.Abs(neocortexRoot)
	if !strings.HasPrefix(absFull, absRoot+string(os.PathSeparator)) {
		return nil, fmt.Errorf("refusing path outside neocortex/: %s", relPath)
	}
	return os.ReadFile(full)
}

// Fetched is a verified snapshot of the registry.
type Fetched struct {
	Manifest *Manifest
	// Content maps manifest-relative path → bytes, all hash-verified.
	Content map[string][]byte
}

// FetchAll fetches the manifest plus every file it lists, verifying each
// file's sha256. Any mismatch is a registry-integrity failure.
func FetchAll(f Fetcher) (*Fetched, error) {
	raw, err := f.Fetch("manifest.json")
	if err != nil {
		return nil, fmt.Errorf("cannot fetch manifest.json: %w", err)
	}
	manifest, err := ParseManifest(raw)
	if err != nil {
		return nil, fmt.Errorf("registry integrity: %w", err)
	}
	fc := &Fetched{Manifest: manifest, Content: map[string][]byte{"manifest.json": raw}}
	for _, e := range manifest.AllEntries() {
		data, err := f.Fetch(e.Path)
		if err != nil {
			return nil, fmt.Errorf("cannot fetch %s: %w", e.Path, err)
		}
		got := SHA256Hex(data)
		if got != e.SHA256 {
			return nil, fmt.Errorf(
				"registry integrity: sha256 mismatch for %s (manifest %s…, fetched %s…) — the registry is corrupted or the manifest is stale",
				e.Path, e.SHA256[:12], got[:12])
		}
		fc.Content[e.Path] = data
	}
	return fc, nil
}

// SemVerValid reports whether v parses as strict semver.
func SemVerValid(v string) error {
	sv, err := semver.NewVersion(v)
	if err != nil {
		return fmt.Errorf("%q is not valid semver", v)
	}
	// Strict: require major.minor.patch (no bare "1" or "1.2").
	if sv.Prerelease() == "" && sv.Metadata() == "" {
		if !strings.Contains(v, ".") || len(strings.Split(v, ".")) != 3 {
			return fmt.Errorf("%q is not valid semver", v)
		}
		for _, p := range strings.Split(v, ".") {
			if p == "" {
				return fmt.Errorf("%q is not valid semver", v)
			}
		}
	}
	return nil
}

// WalkDir is a small helper for fixture builders.
func WalkDir(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			out = append(out, path)
		}
		return nil
	})
	return out, err
}
