package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/RTwoStudio/orbit/internal/config"
	"github.com/RTwoStudio/orbit/internal/deploy"
	"github.com/RTwoStudio/orbit/internal/domain"
	"github.com/RTwoStudio/orbit/internal/exit"
	"github.com/RTwoStudio/orbit/internal/fsutil"
	"github.com/RTwoStudio/orbit/internal/logx"
	"github.com/RTwoStudio/orbit/internal/neocortex"
	"github.com/RTwoStudio/orbit/internal/prompt"
	"github.com/RTwoStudio/orbit/internal/registry"
	"github.com/RTwoStudio/orbit/internal/scaffold"
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

// newFetcher builds the registry fetcher for one domain; a var so tests can
// serve a local fixture instead of the network.
var newFetcher = func(cfg *config.Config, d domain.Domain, urlFlag, refFlag string) registry.Fetcher {
	url := cfg.Registry.URL
	if urlFlag != "" {
		url = urlFlag
	}
	ref := cfg.Registry.Ref
	if refFlag != "" {
		ref = refFlag
	}
	return registry.RemoteFetcher{URL: url, Ref: ref, TokenEnv: cfg.Tokens.Registry, Domain: d}
}

// fetchRegistry = install/update steps 1–2: resolve config, fetch, verify.
// A genuinely missing domain surfaces unchanged as registry.ErrDomainAbsent so
// callers can decide required vs. optional; every other failure is mapped to
// registry_unreachable.
func fetchRegistry(cfg *config.Config, d domain.Domain, registryURLFlag, refFlag string) (*registry.Fetched, error) {
	if _, err := cfg.RequireRegistryURL(); err != nil && registryURLFlag == "" {
		return nil, err
	}
	fetcher := newFetcher(cfg, d, registryURLFlag, refFlag)
	fc, err := registry.FetchAll(fetcher)
	if err != nil {
		if errors.Is(err, registry.ErrDomainAbsent) {
			return nil, err
		}
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
		if err := scaffold.ValidateStub(name, fc.Content[e.Path]); err != nil {
			return nil, exit.New(exit.RegistryUnreachable,
				"registry integrity: stub validation failed: "+err.Error())
		}
	}
	return fc, nil
}

// requiredDomainError maps a missing required domain to registry_unreachable,
// preserving the pre-domain-generic failure class (exit 4).
func requiredDomainError(d domain.Domain, err error) error {
	if errors.Is(err, registry.ErrDomainAbsent) {
		return exit.New(exit.RegistryUnreachable,
			fmt.Sprintf("registry is missing the required %s/ domain: %v", d.Base, err),
			"check registry url/ref in "+config.UserPath(flagConfig))
	}
	return err
}

// populateCache = install/update step 3.
func populateCache(fc *registry.Fetched, d domain.Domain) StepReport {
	cached := registry.CacheManifestHash(d)
	incoming := registry.SHA256Hex(fc.Content["manifest.json"])
	dir := config.CacheDir(d)
	if cached != "" && cached == incoming && fsutil.IsDir(dir) {
		return StepReport{Step: cacheStep(d), Action: "up to date", Path: dir}
	}
	if err := registry.CacheSave(fc, d); err != nil {
		return StepReport{Step: cacheStep(d), Action: "failed", Detail: err.Error()}
	}
	logx.Info("cache refreshed domain=%s path=%s version=%s", d.Name, dir, fc.Manifest.Version)
	return StepReport{Step: cacheStep(d), Action: "refreshed", Path: dir,
		Detail: "manifest " + incoming[:12] + "…"}
}

// stepName qualifies a step with its domain for the domain-labeled rows of
// multi-domain verbs; the neocortex rows stay byte-identical to before.
func stepName(d domain.Domain, step string) string {
	if d == domain.NeoCortex {
		return step
	}
	return d.Name + " " + step
}

// cacheStep is the step label for the cache row (unchanged for neocortex).
func cacheStep(d domain.Domain) string {
	return stepName(d, "cache")
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
			fc, err := fetchRegistry(cfg, domain.NeoCortex, registryURL, ref)
			if err != nil {
				return requiredDomainError(domain.NeoCortex, err)
			}
			var steps []StepReport
			steps = append(steps, StepReport{Step: "fetch", Action: "ok",
				Detail: fmt.Sprintf("registry version %s (%d files)", fc.Manifest.Version, len(fc.Content)-1)})

			steps = append(steps, populateCache(fc, domain.NeoCortex))

			ocDir := deploy.OpenCodeDir(cfg.OpenCode.Dir)
			deployed := deploy.LoadDeployed(domain.NeoCortex)
			decisions, err := deploy.InstallMode(fc, domain.NeoCortex, ocDir, deployed)
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
			offerCompletion(cmd)
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

// offerCompletion quietly offers to install shell completion once, after a
// successful project install. It never fails the install: absence of a shell,
// a declined prompt, an opt-out env, or an already-installed file are all
// silently tolerated.
func offerCompletion(cmd *cobra.Command) {
	if os.Getenv("ORBIT_NO_COMPLETION") == "1" {
		return
	}
	shell, err := detectedShell(nil)
	if err != nil {
		return // unknown/undetectable shell — stay silent
	}
	target, err := completionTargetFor(shell)
	if err != nil || fsutil.Exists(target.path) {
		return // already installed (or unsupported) — nothing to do
	}
	if !flagYes {
		if !stdinIsTTY() {
			return // non-interactive: never surprise the caller
		}
		if !prompt.Confirm("Install shell completion for "+shell+"?", true, false) {
			return
		}
	}
	var buf strings.Builder
	if err := generateCompletion(cmd.Root(), shell, &buf); err != nil {
		return
	}
	if err := fsutil.AtomicWrite(target.path, []byte(buf.String()), 0o644); err != nil {
		return
	}
	fmt.Fprintf(cmd.OutOrStdout(), "\nshell completion installed → %s\n", target.path)
	if target.loadLine != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", target.loadLine)
	}
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
	// NEOCORTEX.md is intentionally NOT copied into the project: the workflow
	// docs live in the deployed opencode assets, not the user's repo.
	logx.Info("project bootstrapped path=%s", root)
	return steps
}

func newNeoCortexUpdateCmd() *cobra.Command {
	var (
		registryURL string
		ref         string
	)
	cmd := &cobra.Command{
		Use:    "update",
		Short:  "Deprecated alias for 'orbit update' (global registry refresh)",
		Hidden: true,
		Long: `Deprecated: use 'orbit update' instead. Kept so existing scripts keep
working. Refreshes global assets only; it has NO project setup gate.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.ErrOrStderr(), "deprecated: 'orbit neocortex update' → 'orbit update'")
			return runUpdate(cmd, registryURL, ref, false)
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
