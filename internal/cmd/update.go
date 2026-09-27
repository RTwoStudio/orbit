package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/RTwoStudio/orbit/internal/config"
	"github.com/RTwoStudio/orbit/internal/deploy"
	"github.com/RTwoStudio/orbit/internal/prompt"
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
// `orbit neocortex update` alias.
func runUpdate(cmd *cobra.Command, registryURL, ref string, prune bool) error {
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
			fmt.Fprintf(cmd.ErrOrStderr(), "%s → declined (non-interactive; run: orbit update --yes to accept)\n", msg)
			return false
		}
		return prompt.Confirm(msg, def, false)
	}
	decisions, err := deploy.UpdateMode(fc, ocDir, deployed, ask, prune)
	for _, d := range decisions {
		steps = append(steps, StepReport{Step: "deploy " + d.Path, Action: d.Action,
			Detail: d.Detail, Path: d.Target})
	}
	if err != nil {
		printSummary(cmd, "update", steps)
		return err
	}

	printSummary(cmd, "update", steps)
	deployedVersion := deployedVersionOf(deployed, fc.Manifest.Version)
	final := fmt.Sprintf("Deployed version: %s (registry: %s)", deployedVersion, fc.Manifest.Version)
	fmt.Fprintln(cmd.OutOrStderr(), final)
	return nil
}
