package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/orbit-sh/orbit-cli/internal/config"
	"github.com/orbit-sh/orbit-cli/internal/deploy"
	"github.com/orbit-sh/orbit-cli/internal/exit"
	"github.com/orbit-sh/orbit-cli/internal/fsutil"
	"github.com/orbit-sh/orbit-cli/internal/logx"
	"github.com/orbit-sh/orbit-cli/internal/neocortex"
	"github.com/orbit-sh/orbit-cli/internal/prompt"
	"github.com/orbit-sh/orbit-cli/internal/registry"
)

// StepReport is one row of the install/update summary table.
type StepReport struct {
	Step   string `json:"step"`
	Action string `json:"action"` // installed | up to date | refused | skipped | declined | warned | orphaned | refreshed
	Detail string `json:"detail,omitempty"`
	Path   string `json:"path,omitempty"`
}

const gateHintInstall = "run: orbit neocortex update"
const gateHintUpdate = "run: orbit neocortex install first"

// setupGate implements §5.0: role-lock install ↔ update.
func setupGate(cmdName string) error {
	initialized := neocortex.IsInitialized()
	switch {
	case cmdName == "install" && initialized:
		return exit.New(exit.NotInitialized,
			"project already initialized (.neocortex/ exists) — "+gateHintInstall)
	case cmdName == "update" && !initialized:
		return exit.New(exit.NotInitialized,
			"no .neocortex/ in this directory — "+gateHintUpdate)
	}
	return nil
}

// newFetcher builds the registry fetcher; a var so tests can serve a
// local fixture instead of the network.
var newFetcher = func(cfg *config.Config, urlFlag, refFlag string) registry.Fetcher {
	url := cfg.Registry.URL
	if urlFlag != "" {
		url = urlFlag
	}
	ref := cfg.Registry.Ref
	if refFlag != "" {
		ref = refFlag
	}
	return registry.RemoteFetcher{URL: url, Ref: ref, TokenEnv: cfg.Registry.TokenEnv}
}

// fetchRegistry = install/update steps 1–2: resolve config, fetch, verify.
func fetchRegistry(cfg *config.Config, registryURLFlag, refFlag string) (*registry.Fetched, error) {
	if _, err := cfg.RequireRegistryURL(); err != nil && registryURLFlag == "" {
		return nil, err
	}
	fetcher := newFetcher(cfg, registryURLFlag, refFlag)
	fc, err := registry.FetchAll(fetcher)
	if err != nil {
		logx.Error("registry fetch failed: %v", err)
		return nil, exit.New(exit.RegistryUnreachable, err.Error(),
			"check registry url/ref/token in "+config.UserPath(flagConfig),
			"network or auth issue — verify access to the registry")
	}
	if err := registry.SemVerValid(fc.Manifest.Version); err != nil {
		return nil, exit.New(exit.RegistryUnreachable,
			"registry integrity: manifest version is invalid semver: "+err.Error())
	}
	// Validate every stub against its known token set at cache time (§10).
	for _, e := range fc.Manifest.Files.Stubs {
		name := filepath.Base(e.Path)
		if err := neocortex.ValidateStub(name, fc.Content[e.Path]); err != nil {
			return nil, exit.New(exit.RegistryUnreachable,
				"registry integrity: stub validation failed: "+err.Error())
		}
	}
	return fc, nil
}

// populateCache = install/update step 3.
func populateCache(fc *registry.Fetched) StepReport {
	cached := registry.CacheManifestHash()
	incoming := registry.SHA256Hex(fc.Content["manifest.json"])
	dir := config.CacheDir()
	if cached != "" && cached == incoming && fsutil.IsDir(dir) {
		return StepReport{Step: "cache", Action: "up to date", Path: dir}
	}
	if err := registry.CacheSave(fc); err != nil {
		return StepReport{Step: "cache", Action: "failed", Detail: err.Error()}
	}
	logx.Info("cache refreshed path=%s version=%s", dir, fc.Manifest.Version)
	return StepReport{Step: "cache", Action: "refreshed", Path: dir,
		Detail: "manifest " + incoming[:12] + "…"}
}

func newInstallCmd() *cobra.Command {
	var (
		registryURL string
		ref         string
		globalOnly  bool
	)
	cmd := &cobra.Command{
		Use:   "install",
		Short: "First-contact setup: fetch registry, deploy agents, bootstrap project",
		Long: `Performs first-contact setup for the NeoCortex workflow.

Preflight (setup gate): refuses when .neocortex/ already exists —
install is role-locked to uninitialized projects (exit 11). Use
'orbit neocortex update' afterwards, or --global-only to fix global
assets without touching the project.

Steps (each idempotent, independently reported):
  1. Resolve config (registry.url REQUIRED — exit 3 when absent)
  2. Fetch registry + verify manifest + per-file sha256 (exit 4 on mismatch)
  3. Populate the global cache (~/.config/orbit/neocortex/cache)
  4. Deploy opencode agents/commands (existing files are NEVER overwritten)
  5. Bootstrap the project: .neocortex tree, .gitignore entry, NEOCORTEX.md
     (copy-if-missing only)

Exit codes: 0 ok · 3 config_error · 4 registry_unreachable · 7 state_conflict
            10 io_error · 11 not_initialized

Example:
  orbit neocortex install --registry https://github.com/acme/orbit-registry`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !globalOnly {
				if err := setupGate("install"); err != nil {
					return err
				}
			}
			cfg, err := config.Load(flagConfig)
			if err != nil {
				return err
			}
			fc, err := fetchRegistry(cfg, registryURL, ref)
			if err != nil {
				return err
			}
			var steps []StepReport
			steps = append(steps, StepReport{Step: "fetch", Action: "ok",
				Detail: fmt.Sprintf("registry version %s (%d files)", fc.Manifest.Version, len(fc.Content)-1)})

			steps = append(steps, populateCache(fc))

			ocDir := deploy.OpenCodeDir(cfg.OpenCode.Dir)
			deployed := deploy.LoadDeployed()
			decisions, err := deploy.InstallMode(fc, ocDir, deployed)
			for _, d := range decisions {
				steps = append(steps, StepReport{Step: "deploy " + d.Path, Action: d.Action,
					Detail: d.Detail, Path: d.Target})
			}
			if err != nil {
				printSummary(cmd, "install", steps)
				return err
			}

			if !globalOnly {
				steps = append(steps, bootstrapProject(fc)...)
			} else {
				steps = append(steps, StepReport{Step: "project", Action: "skipped",
					Detail: "--global-only"})
			}

			printSummary(cmd, "install", steps)
			logx.Info("install complete version=%s", fc.Manifest.Version)
			return nil
		},
	}
	cmd.Flags().StringVar(&registryURL, "registry", "", "one-shot registry URL override")
	cmd.Flags().StringVar(&ref, "ref", "", "one-shot branch/tag override")
	cmd.Flags().BoolVar(&globalOnly, "global-only", false,
		"skip the project gate and bootstrap; only cache + opencode deployment")
	return cmd
}

// bootstrapProject = install step 5.
func bootstrapProject(fc *registry.Fetched) []StepReport {
	var steps []StepReport
	root := neocortex.Root()
	// Tree: ACTIVE (empty), CONVENTIONS.md stub (once), issues/.
	created := false
	if !fsutil.IsDir(root) {
		for _, d := range []string{root, neocortex.IssuesDir()} {
			if err := fsutil.EnsureDir(d); err != nil {
				steps = append(steps, StepReport{Step: "project", Action: "failed", Detail: err.Error()})
				return steps
			}
		}
		if err := os.WriteFile(filepath.Join(root, "ACTIVE"), nil, 0o644); err != nil {
			steps = append(steps, StepReport{Step: "project", Action: "failed", Detail: err.Error()})
			return steps
		}
		steps = append(steps, StepReport{Step: "project tree", Action: "installed",
			Detail: ".neocortex/ (ACTIVE, CONVENTIONS.md stub, issues/)"})
		created = true
	}
	convPath := filepath.Join(root, "CONVENTIONS.md")
	if !fsutil.Exists(convPath) {
		stub := "# Project Conventions\n\nUser-owned. The CLI creates this file ONCE and never touches it again.\n\n- <record project-specific conventions here>\n"
		os.WriteFile(convPath, []byte(stub), 0o644)
	}
	if created {
		steps = append(steps, StepReport{Step: "CONVENTIONS.md", Action: "installed", Path: convPath})
	}
	// .gitignore (check-before-append, idempotent).
	if err := fsutil.GitignoreAppend(".", ".neocortex/"); err != nil {
		steps = append(steps, StepReport{Step: ".gitignore", Action: "warned", Detail: err.Error()})
	} else {
		steps = append(steps, StepReport{Step: ".gitignore", Action: "up to date", Detail: ".neocortex/ entry ensured"})
	}
	// NEOCORTEX.md copy-if-missing (project root).
	neoPath := "NEOCORTEX.md"
	if fsutil.Exists(neoPath) {
		steps = append(steps, StepReport{Step: "NEOCORTEX.md", Action: "up to date",
			Detail: "exists — never touched, not even compared"})
	} else {
		if data, ok := fc.Content["NEOCORTEX.md"]; ok {
			if err := fsutil.AtomicWrite(neoPath, data, 0o644); err != nil {
				steps = append(steps, StepReport{Step: "NEOCORTEX.md", Action: "failed", Detail: err.Error()})
			} else {
				steps = append(steps, StepReport{Step: "NEOCORTEX.md", Action: "installed"})
			}
		} else {
			steps = append(steps, StepReport{Step: "NEOCORTEX.md", Action: "warned", Detail: "missing from registry"})
		}
	}
	logx.Info("project bootstrapped path=%s", root)
	return steps
}

func newUpdateCmd() *cobra.Command {
	var (
		registryURL string
		ref         string
	)
	cmd := &cobra.Command{
		Use:   "update",
		Short: "OTA refresh of global assets from the registry (version-gated)",
		Long: `Refreshes global assets: registry cache, opencode agents/commands,
and deployed.json. NO project-side filesystem changes.

Preflight (setup gate): requires an initialized project (.neocortex/)
— update is role-locked to initialized projects (exit 11 when absent).

Deploy decisions per agents/commands file:
  - registry newer (semver) → prompt (TTY); --yes accepts all;
    non-TTY without --yes treats as NO and lists pending updates
  - same version but drifted content → warn + prompt, default NO
  - recorded but absent from manifest → reported as orphaned (kept)

Exit codes: 0 ok · 3 config_error · 4 registry_unreachable ·
            10 io_error · 11 not_initialized

Example:
  orbit neocortex update --yes`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := setupGate("update"); err != nil {
				return err
			}
			cfg, err := config.Load(flagConfig)
			if err != nil {
				return err
			}
			fc, err := fetchRegistry(cfg, registryURL, ref)
			if err != nil {
				return err
			}
			var steps []StepReport
			steps = append(steps, StepReport{Step: "fetch", Action: "ok",
				Detail: fmt.Sprintf("registry version %s", fc.Manifest.Version)})
			steps = append(steps, populateCache(fc))

			ocDir := deploy.OpenCodeDir(cfg.OpenCode.Dir)
			deployed := deploy.LoadDeployed()
			ask := func(msg string, def bool) bool {
				if flagYes {
					return def
				}
				if !stdinIsTTY() {
					fmt.Fprintf(cmd.ErrOrStderr(), "%s → declined (non-interactive; run: orbit neocortex update --yes to accept)\n", msg)
					return false
				}
				return prompt.Confirm(msg, def, false)
			}
			decisions, err := deploy.UpdateMode(fc, ocDir, deployed, ask)
			for _, d := range decisions {
				steps = append(steps, StepReport{Step: "deploy " + d.Path, Action: d.Action,
					Detail: d.Detail, Path: d.Target})
				logx.Info("deploy decision path=%s action=%s detail=%q", d.Path, d.Action, d.Detail)
			}
			if err != nil {
				printSummary(cmd, "update", steps)
				return err
			}

			printSummary(cmd, "update", steps)
			deployedVersion := deployedVersionOf(deployed, fc.Manifest.Version)
			final := fmt.Sprintf("Deployed version: %s (registry: %s)", deployedVersion, fc.Manifest.Version)
			fmt.Fprintln(cmd.OutOrStderr(), final)
			logx.Info("update complete %s", final)
			return nil
		},
	}
	cmd.Flags().StringVar(&registryURL, "registry", "", "one-shot registry URL override")
	cmd.Flags().StringVar(&ref, "ref", "", "one-shot branch/tag override")
	return cmd
}

func deployedVersionOf(d deploy.Deployed, registryVersion string) string {
	seen := map[string]bool{}
	for _, rec := range d {
		if !seen[rec.Version] {
			seen[rec.Version] = true
		}
	}
	if len(seen) == 1 {
		for v := range seen {
			return v
		}
	}
	if len(seen) == 0 {
		return registryVersion
	}
	// Mixed versions: report the registry version (after update all new writes are at it).
	return registryVersion
}

// printSummary renders the step table (human on stdout/TTY, JSON when --json).
func printSummary(cmd *cobra.Command, verb string, steps []StepReport) {
	if flagJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		enc.Encode(map[string]any{"command": "neocortex " + verb, "steps": steps})
		return
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "\n%s summary:\n", verb)
	for _, s := range steps {
		detail := ""
		if s.Detail != "" {
			detail = " — " + s.Detail
		}
		fmt.Fprintf(out, "  %-12s %-14s %s%s\n", s.Action, s.Step, s.Path, detail)
	}
}
