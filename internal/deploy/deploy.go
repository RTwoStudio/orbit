// Package deploy maps registry assets into the opencode dir and global
// cache, with the deployed.json version ledger.
package deploy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RTwoStudio/orbit/internal/exit"
	"github.com/RTwoStudio/orbit/internal/fsutil"
	"github.com/RTwoStudio/orbit/internal/registry"
)

// DeployedRecord is one entry of deployed.json.
type DeployedRecord struct {
	Version    string `json:"version"`
	SHA256     string `json:"sha256"`
	DeployedAt string `json:"deployed_at"`
}

// Deployed is the deployed.json ledger: manifest-relative path → record.
type Deployed map[string]DeployedRecord

// DeployedPath is ~/.config/orbit/neocortex/deployed.json.
func DeployedPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "orbit", "neocortex", "deployed.json")
}

// LoadDeployed reads deployed.json (empty map if absent).
func LoadDeployed() Deployed {
	out := Deployed{}
	if data, err := os.ReadFile(DeployedPath()); err == nil {
		json.Unmarshal(data, &out)
	}
	return out
}

// Save writes deployed.json atomically.
func (d Deployed) Save() error {
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.AtomicWrite(DeployedPath(), data, 0o644)
}

// OpenCodeDir resolves the deployment dir (config may override).
func OpenCodeDir(configured string) string {
	if configured != "" {
		return configured
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "opencode")
}

// TargetPath maps a manifest-relative path ("opencode/agents/x.md") to its
// destination. Everything under opencode/ maps 1:1 into the opencode dir
// (agents/, commands/, future subfolders inherit the rule).
func TargetPath(opencodeDir, relPath string) (string, error) {
	rel := filepath.ToSlash(relPath)
	if !strings.HasPrefix(rel, "opencode/") {
		return "", fmt.Errorf("not an opencode asset: %s", relPath)
	}
	rest := strings.TrimPrefix(rel, "opencode/")
	if strings.Contains(rest, "..") || filepath.IsAbs(rest) {
		return "", fmt.Errorf("unsafe path: %s", relPath)
	}
	return filepath.Join(opencodeDir, filepath.FromSlash(rest)), nil
}

// Decision is the outcome of a per-file deploy decision.
type Decision struct {
	Path   string `json:"path"`
	Target string `json:"target"`
	Action string `json:"action"` // installed | up to date | refused | skipped | declined | orphaned | warned
	Detail string `json:"detail,omitempty"`
	OldSHA string `json:"-"`
	NewSHA string `json:"-"`
}

// InstallMode deploys opencode assets with install semantics: missing →
// write; identical → skip; differs → refuse (state_conflict), never
// overwrite. Only agents/commands entries are deployed here. Deployed
// files are recorded in the ledger so update's version gate has a baseline.
func InstallMode(fc *registry.Fetched, opencodeDir string, deployed Deployed) ([]Decision, error) {
	var out []Decision
	refused := false
	changed := false
	for _, e := range append(append([]registry.FileEntry{}, fc.Manifest.Files.OpenCode.Agents...), fc.Manifest.Files.OpenCode.Commands...) {
		target, err := TargetPath(opencodeDir, e.Path)
		if err != nil {
			return nil, err
		}
		dec := Decision{Path: e.Path, Target: target, NewSHA: e.SHA256}
		existing, err := os.ReadFile(target)
		switch {
		case os.IsNotExist(err):
			if err := fsutil.AtomicWrite(target, fc.Content[e.Path], 0o644); err != nil {
				return nil, exit.Wrap(exit.IOError, err, "cannot write "+target)
			}
			dec.Action = "installed"
		case err != nil:
			return nil, exit.Wrap(exit.IOError, err, "cannot read "+target)
		case registry.SHA256Hex(existing) == e.SHA256:
			dec.Action = "up to date"
		default:
			dec.Action = "refused"
			dec.Detail = "target exists with different content"
			refused = true
		}
		if dec.Action == "installed" || dec.Action == "up to date" {
			deployed[e.Path] = DeployedRecord{
				Version:    fc.Manifest.Version,
				SHA256:     e.SHA256,
				DeployedAt: time.Now().UTC().Format(time.RFC3339),
			}
			changed = true
		}
		out = append(out, dec)
	}
	if changed && !refused {
		if err := deployed.Save(); err != nil {
			return nil, exit.Wrap(exit.IOError, err, "cannot update deployed.json")
		}
	}
	if refused {
		return out, exit.New(exit.StateConflict,
			"one or more opencode assets already exist with different content",
			"remove the refused files and re-run install, or run 'orbit neocortex update' in an initialized project")
	}
	return out, nil
}

// PromptFunc asks a yes/no question; default is the second return.
type PromptFunc func(msg string, def bool) bool

// UpdateMode deploys with the version gate: prompts per file when the
// registry is newer; warns + prompts on same-version content drift;
// reports orphans. --yes accepts all; non-TTY without --yes declines all.
func UpdateMode(fc *registry.Fetched, opencodeDir string, deployed Deployed, prompt PromptFunc) ([]Decision, error) {
	var out []Decision
	entries := append(append([]registry.FileEntry{}, fc.Manifest.Files.OpenCode.Agents...), fc.Manifest.Files.OpenCode.Commands...)
	seen := map[string]bool{}
	changed := false
	for _, e := range entries {
		seen[e.Path] = true
		target, err := TargetPath(opencodeDir, e.Path)
		if err != nil {
			return nil, err
		}
		dec := Decision{Path: e.Path, Target: target, NewSHA: e.SHA256}
		rec, recorded := deployed[e.Path]
		existing, rerr := os.ReadFile(target)
		exists := rerr == nil
		if exists {
			dec.OldSHA = registry.SHA256Hex(existing)
		}

		write := false
		switch {
		case !exists || !recorded:
			// New file (or never recorded): install outright.
			write = true
			dec.Action = "installed"
			if recorded && !exists {
				dec.Detail = "recorded but missing on disk — restored"
			}
		case rec.Version != fc.Manifest.Version && registry.SHA256Hex(existing) != e.SHA256:
			// Version bump pending.
			if prompt(fmt.Sprintf("Update %s %s → %s?", filepath.Base(target), rec.Version, fc.Manifest.Version), true) {
				write = true
				dec.Action = "installed"
			} else {
				dec.Action = "declined"
				dec.Detail = fmt.Sprintf("update later with: orbit neocortex update (%s → %s)", rec.Version, fc.Manifest.Version)
			}
		case rec.Version != fc.Manifest.Version && registry.SHA256Hex(existing) == e.SHA256:
			// Content already matches the new version; just re-record.
			write = true
			dec.Action = "up to date"
			dec.Detail = "content already matches registry"
		case registry.SHA256Hex(existing) != e.SHA256:
			// Same version, drifted content (user edited?).
			if prompt(fmt.Sprintf("%s differs from registry (same version %s) — overwrite?", filepath.Base(target), fc.Manifest.Version), false) {
				write = true
				dec.Action = "installed"
				dec.Detail = "overwrote local modifications"
			} else {
				dec.Action = "warned"
				dec.Detail = "kept local version (differs from registry)"
			}
		default:
			dec.Action = "up to date"
		}

		if write {
			if err := fsutil.AtomicWrite(target, fc.Content[e.Path], 0o644); err != nil {
				return nil, exit.Wrap(exit.IOError, err, "cannot write "+target)
			}
			deployed[e.Path] = DeployedRecord{
				Version:    fc.Manifest.Version,
				SHA256:     e.SHA256,
				DeployedAt: time.Now().UTC().Format(time.RFC3339),
			}
			changed = true
		}
		out = append(out, dec)
	}

	// Orphans: recorded but absent from the new manifest (informational).
	for path := range deployed {
		if !seen[path] {
			out = append(out, Decision{Path: path, Target: TargetOf(path, opencodeDir), Action: "orphaned",
				Detail: "no longer in the registry manifest (left in place)"})
		}
	}
	if changed {
		if err := deployed.Save(); err != nil {
			return nil, exit.Wrap(exit.IOError, err, "cannot update deployed.json")
		}
	}
	return out, nil
}

// TargetOf maps a recorded path for display purposes.
func TargetOf(relPath, opencodeDir string) string {
	t, err := TargetPath(opencodeDir, relPath)
	if err != nil {
		return relPath
	}
	return t
}
