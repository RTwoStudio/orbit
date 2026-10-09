// Package domain names Orbit's asset domains. A domain is one self-contained
// registry tree (its own manifest.json) served by the shared registry → cache
// → deploy pipeline. Each domain namespaces its global cache and deployed
// ledger (~/.config/orbit/<Name>/…), while its opencode assets still deploy
// into the shared ~/.config/opencode/ tree.
package domain

import (
	"os"
	"path/filepath"
)

// Domain identifies a registry domain.
//
//   - Name is the CLI/cache namespace: `orbit <Name> …` and
//     ~/.config/orbit/<Name>/.
//   - Base is the folder name at the registry repo root that holds the
//     domain's manifest.json.
type Domain struct {
	Name string
	Base string
}

// Canonical domains.
var (
	// NeoCortex is the project-rooted engineering execution domain.
	NeoCortex = Domain{Name: "neocortex", Base: "neocortex"}
	// Cycles is the vault-rooted planning and commitment domain.
	Cycles = Domain{Name: "cycles", Base: "cycles"}
)

// All is the canonical, ordered list of domains orbit knows about.
var All = []Domain{NeoCortex, Cycles}

// Required is the set of domains whose absence makes `orbit update` fail.
// Every other domain in All is optional: a missing registry manifest yields a
// soft "skipped" step instead.
var Required = []Domain{NeoCortex}

// IsRequired reports whether d must be present for `orbit update` to succeed.
func IsRequired(d Domain) bool {
	for _, r := range Required {
		if r == d {
			return true
		}
	}
	return false
}

// IsKnownBase reports whether s names a known registry domain base folder.
func IsKnownBase(s string) bool {
	for _, d := range All {
		if d.Base == s {
			return true
		}
	}
	return false
}

// StateDir is ~/.config/orbit/<Name> — the domain's private state root.
func (d Domain) StateDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "orbit", d.Name)
}

// CacheDir is the domain's global cache: ~/.config/orbit/<Name>/cache.
func (d Domain) CacheDir() string {
	return filepath.Join(d.StateDir(), "cache")
}

// DeployedPath is the domain's deployment ledger:
// ~/.config/orbit/<Name>/deployed.json.
func (d Domain) DeployedPath() string {
	return filepath.Join(d.StateDir(), "deployed.json")
}
