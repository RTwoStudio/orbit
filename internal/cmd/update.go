package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/RTwoStudio/orbit/internal/config"
	"github.com/RTwoStudio/orbit/internal/deploy"
	"github.com/RTwoStudio/orbit/internal/domain"
	"github.com/RTwoStudio/orbit/internal/prompt"
	"github.com/RTwoStudio/orbit/internal/registry"
)

// newUpdateCmd builds the top-level `orbit update`: a global refresh of the
// registry cache and opencode assets. It writes only ~/.config, never the
// project, so it deliberately has NO .neocortex/ setup gate.
func newUpdateCmd() *cobra.Command {
	var (
		registryURL string
		ref         string
		prune       bool
	)
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Refresh global assets (registry cache + opencode agents/commands)",
		Long: `Refreshes global assets: the registry cache, opencode agents/commands,
and deployed.json. NO project-side filesystem changes — it works in any
directory, initialized project or not.

Deploy decisions per agents/commands file:
  - registry newer (semver) → prompt (TTY); --yes accepts all;
    non-TTY without --yes treats as NO and lists pending updates
  - same version but drifted content → warn + prompt, default NO
  - recorded but absent from manifest → reported as orphaned (kept)

Orphans (--prune):
  Files the CLI deployed that are no longer in the manifest. With --prune they
  are deleted from the opencode dir AND dropped from deployed.json — but only
  when the on-disk file still matches the sha256 the CLI recorded. A locally
  modified orphan is kept (only its ledger entry is dropped). Files never in
  deployed.json are never touched, so your own commands are always safe.

Exit codes: 0 ok · 3 config_error · 4 registry_unreachable · 10 io_error

Example:
  orbit update --yes
  orbit update --prune`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpdate(cmd, registryURL, ref, prune)
		},
	}
	cmd.Flags().StringVar(&registryURL, "registry", "", "one-shot registry URL override")
	cmd.Flags().StringVar(&ref, "ref", "", "one-shot branch/tag override")
	cmd.Flags().BoolVar(&prune, "prune", false, "delete orphaned deployed assets (locally modified ones are kept)")
	return cmd
}

// runUpdate is shared by `orbit update` (global) and the deprecated
// `orbit neocortex update` alias. It refreshes every known domain: neocortex
// is required, cycles optional (a missing cycles/ manifest is a soft skip).
func runUpdate(cmd *cobra.Command, registryURL, ref string, prune bool) error {
	cfg, err := config.Load(flagConfig)
	if err != nil {
		return err
	}
	ocDir := deploy.OpenCodeDir(cfg.OpenCode.Dir)
	ask := func(msg string, def bool) bool {
		if flagYes {
			return def
		}
		if !stdinIsTTY() {
			fmt.Fprintf(cmd.ErrOrStderr(), "%s → declined (non-interactive; run: orbit update --yes to accept)\n", msg)
			return false
		}
		return prompt.Confirm(msg, def, false)
	}

	var (
		steps    []StepReport
		versions []string
	)
	for _, dom := range domain.All {
		domSteps, fc, deployed, err := updateDomain(cfg, dom, registryURL, ref, ocDir, ask, prune)
		steps = append(steps, domSteps...)
		if err != nil {
			if errors.Is(err, registry.ErrDomainAbsent) && !domain.IsRequired(dom) {
				steps = append(steps, StepReport{Step: stepName(dom, "fetch"), Action: "skipped",
					Detail: fmt.Sprintf("no %s/ domain in registry", dom.Base)})
				continue
			}
			printSummary(cmd, "update", steps)
			return requiredDomainError(dom, err)
		}
		versions = append(versions, deployedVersionLine(dom, deployed, fc.Manifest.Version))
	}

	printSummary(cmd, "update", steps)
	for _, v := range versions {
		fmt.Fprintln(cmd.OutOrStderr(), v)
	}
	return nil
}

// updateDomain fetches, caches, and deploys a single domain, returning its
// step rows and the resulting ledger. A real failure (network, integrity)
// keeps its error; a missing optional domain surfaces as
// registry.ErrDomainAbsent for the caller to soft-skip.
func updateDomain(cfg *config.Config, dom domain.Domain, registryURL, ref, ocDir string, ask deploy.PromptFunc, prune bool) ([]StepReport, *registry.Fetched, deploy.Deployed, error) {
	var steps []StepReport
	fc, err := fetchRegistry(cfg, dom, registryURL, ref)
	if err != nil {
		return steps, nil, nil, err
	}
	steps = append(steps, StepReport{Step: stepName(dom, "fetch"), Action: "ok",
		Detail: fmt.Sprintf("registry version %s", fc.Manifest.Version)})
	steps = append(steps, populateCache(fc, dom))

	deployed := deploy.LoadDeployed(dom)
	decisions, err := deploy.UpdateMode(fc, dom, ocDir, deployed, ask, prune)
	for _, d := range decisions {
		steps = append(steps, StepReport{Step: stepName(dom, "deploy "+d.Path), Action: d.Action,
			Detail: d.Detail, Path: d.Target})
	}
	return steps, fc, deployed, err
}

// deployedVersionLine formats the per-domain closing line. NeoCortex keeps the
// exact pre-domain-generic wording; other domains are qualified by name.
func deployedVersionLine(dom domain.Domain, deployed deploy.Deployed, registryVersion string) string {
	v := deployedVersionOf(deployed, registryVersion)
	if dom == domain.NeoCortex {
		return fmt.Sprintf("Deployed version: %s (registry: %s)", v, registryVersion)
	}
	return fmt.Sprintf("Deployed version [%s]: %s (registry: %s)", dom.Name, v, registryVersion)
}
