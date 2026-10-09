// Package registry implements fetching the remote asset registry,
// manifest parsing, sha256 verification, and the global cache.
package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Masterminds/semver/v3"

	"github.com/RTwoStudio/orbit/internal/domain"
)

// ErrDomainAbsent reports that a registry does not carry a requested domain
// folder (no <base>/manifest.json). It is the only registry-fetch outcome that
// `orbit update` treats as an optional soft skip; everything else remains a
// registry_unreachable failure.
var ErrDomainAbsent = errors.New("registry domain absent")

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
		// Path traversal guard: entries are relative to the domain folder.
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
	// Fetch returns the raw bytes of a file relative to the configured
	// domain's folder. Paths that escape the folder must be refused.
	Fetch(relPath string) ([]byte, error)
}

// DirFetcher serves one domain of a registry from a local directory
// (tests + fallback).
type DirFetcher struct {
	// Root is the repo root; all fetches are scoped to Root/<Domain.Base>.
	Root   string
	Domain domain.Domain
}

func (d DirFetcher) Fetch(relPath string) ([]byte, error) {
	base := d.Domain.Base
	clean := filepath.Clean(relPath)
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
		return nil, fmt.Errorf("refusing path outside %s/: %s", base, relPath)
	}
	domainRoot := filepath.Join(d.Root, base)
	full := filepath.Join(domainRoot, clean)
	// Double-check containment after joining.
	absFull, _ := filepath.Abs(full)
	absRoot, _ := filepath.Abs(domainRoot)
	if !strings.HasPrefix(absFull, absRoot+string(os.PathSeparator)) {
		return nil, fmt.Errorf("refusing path outside %s/: %s", base, relPath)
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
// file's sha256. Any mismatch is a registry-integrity failure. A manifest that
// is genuinely missing (no domain folder) surfaces as ErrDomainAbsent.
func FetchAll(f Fetcher) (*Fetched, error) {
	raw, err := f.Fetch("manifest.json")
	if err != nil {
		if errors.Is(err, ErrDomainAbsent) {
			return nil, err
		}
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: no manifest.json", ErrDomainAbsent)
		}
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
