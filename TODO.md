# Orbit CLI — TODO

Parking lot for CLI work identified while developing the registry/agents.
Registry is at **0.6.0**; the CLI is at **v0.2.0** (2026-09-27). Committed.

> **The CLI rewrite landed in v0.2.0.** The *Target command surface (vNext)*
> section at the bottom is now the shipped interface; its items are ticked.
> Remaining open work: the orphan prune, the optional "update available"
> notice, and non-Linux self-update.

## Orphan prune is the only real infra gap left

Both global update and self-update now work; the outstanding infra item is
deleting assets that leave the manifest.

## `orbit neocortex update` should be global

- [x] Drop the `.neocortex/` setup gate for `update` — done: `update` moved to a
      **top-level** `orbit update` (`internal/cmd/update.go`) with no project gate;
      `orbit neocortex update` remains as a hidden deprecated alias that prints a
      nudge and works anywhere. Verified from a directory with no `.neocortex/`.
- [x] Rationale: `update` only writes **global** state — `~/.config/opencode/{agents,commands}`
      and `~/.config/orbit/neocortex/{cache,deployed.json}`. It does not touch project
      `.neocortex/` data, so it must not require an initialized project.
- [x] Keep the project gate on `install` (and other project-scoped verbs); only `update` becomes global.
- [x] Update the `update` help text and exit-code docs — the global verb lists
      `3/4/10` only (no `not_initialized`).
- [x] Decide whether global `update` still accepts `--registry` / `--ref` overrides — yes, kept
      on both the global verb and the alias.

## Prune orphans during update

- [ ] `UpdateMode` reports files recorded in `deployed.json` but absent from the manifest as
      "orphaned (kept)" (`internal/deploy/deploy.go:215-221`). On a version bump that renames or
      removes assets, the old files linger in `~/.config/opencode`.
- [ ] Delete previously-deployed files that left the manifest and drop their ledger records —
      or gate behind a `--prune` flag to keep the current conservative behavior by default.
- [ ] Context: renaming commands to `neocortex:<action>` (registry 0.5.0) left six stale files
      behind (`issue.md`, `plan.md`, `new-task.md`, `implement.md`, `addenda.md`, `close.md`),
      which had to be cleaned up by hand. **This now bites twice**: v0.2.0 removed the
      `task status` verb, so the shipped surface and any hand-built completions must be re-checked.

## `orbit self update`

- [x] Add `orbit self update` to fetch the latest release of `RTwoStudio/orbit` and replace the
      running binary in place — done (`internal/selfupdate`, `internal/cmd/self.go`).
- [x] Reuse the GitHub Releases logic already in `install.sh`: `releases/latest`, per-OS/arch asset,
      and `checksums.txt` verification (`GH_REPO=RTwoStudio/orbit`) — same env overrides
      (`ORBIT_GH_REPO/API/DL`), same asset naming.
- [x] Compare the release tag against the compiled `version` and no-op when current.
- [x] Handle failure modes: install dir not writable (exit 7, suggests sudo/`ORBIT_BIN_DIR`),
      offline/unreachable (exit 4), checksum mismatch (exit 7, aborts, binary untouched), and a
      same-filesystem temp + atomic `rename(2)` swap (no rollback needed — rename is atomic).
- [x] UX: `orbit self update [--check] [--force]`; `--check` reports current vs latest.
- [ ] Passive "update available" notice elsewhere — optional, not done. Note
      `.github/workflows/release.yml` already cuts releases on `v*` tags.
- [ ] Non-Linux support — Windows refuses with a clear error (a running `.exe` can't be renamed
      over); macOS is blocked only by the release matrix, not the code.

## Refresh stale CLI hints

- [x] `internal/cmd/issue.go:105` printed `fill the concept via /issue` → rewrote the next-step
      hint in the v0.2.0 issue rewrite (`fill the concept (Objective/Detail/Requirements/Scope),
      then: orbit neocortex issue lock`).
- [x] `internal/cmd/task.go:81` printed `fill via /new-task` → rewrote it in the v0.2.0 task
      rewrite (`fill the task, then: orbit neocortex task start <ID>`).
- [x] Both no longer tell humans to run slash commands; hints now match the orchestrator-driven
      flow and cite real CLI verbs.

---

# Target command surface (vNext)

**Status: shipped in CLI v0.2.0 (commit `b0bf97d`).** This is the interface the
rewrite was built against. Design rules:

- **Every work item is an issue.** One numbered ledger (`issues/issue-N/`). Two lanes:
  *default* (unmarked) and *quick* (`Class: quick`).
- **Verb + entity + operand.** Actions are verbs (no `--set=<status>`, no mode flags);
  the title/ref/path/ID is a positional operand; only query/output shaping stays a flag
  (`--lane`, `--status`, `--json`).
- **Status changes only through verbs**; the CLI owns all frontmatter (`Status`, locks, hashes).
- **ACTIVE scoping:** `plan`, `addenda`, `task`, `quick`, `close` target the ACTIVE issue;
  only `issue <verb> [N]` addresses an issue by number. No `--issue` flag exists.
- Lanes share one counter: issue 42 (default) → issue 43 (quick) → issue 44 (default).

## Intake (mode verbs, positional title)

- [x] `orbit neocortex issue new "<title>"`            — interactive; scaffolds
      `issues/issue-N/` (`00-concept.md`, `01-plan.md`, `tasks/`, `notes/`, `addenda/`).
- [x] `orbit neocortex issue import <n|url> "<title>"` — remote; Detail injected VERBATIM.
- [x] `orbit neocortex issue ingest <path> "<title>"`  — file; Detail injected VERBATIM.
- [x] `orbit neocortex quick new "<title>"`            — quick; scaffolds `00-quick.md`
      (`Class: quick`, `Status: Draft`).
- [x] `orbit neocortex quick import <n|url> "<title>"` — quick remote intake (Intent verbatim).
- [x] `orbit neocortex quick ingest <path> "<title>"`  — quick file intake (Intent verbatim).

## Quick transitions (short verbs)

Transition map: `Draft → In Progress → Revise → Rework → Close`; `Rework → In Progress` on resume.

- [x] `orbit neocortex quick start <N>`  — Draft → In Progress (also Rework → In Progress)
- [x] `orbit neocortex quick revise <N>` — In Progress → Revise
- [x] `orbit neocortex quick close <N>`  — Revise → Close; refuses while `## Result` is empty
- [x] `orbit neocortex quick rework <N>` — Revise → Rework
- [x] `orbit neocortex quick promote <N>` — convert to the default lane **in place**:
      generate `00-concept.md` from the quick `## Intent`, add a Detail line noting the
      promotion, create the tree, remove `00-quick.md`. No folder move, no restart.

## Default-lane task transitions (short verbs)

Replaces `task status <ID> --set=<status>`. Same map the CLI already enforces:
`Open → In Progress → Revise → Rework/Close`; `Rework → In Progress` on resume.

- [x] `orbit neocortex task start <ID>`  — Open → In Progress (also Rework → In Progress)
- [x] `orbit neocortex task revise <ID>` — In Progress → Revise
- [x] `orbit neocortex task close <ID>`  — Revise → Close
- [x] `orbit neocortex task rework <ID>` — Revise → Rework

## Retained verbs (positional operands, no `--issue`)

- [x] `issue lock [N]`, `issue switch <N>`, `plan lock [N]`, `addenda new "<title>"`,
      `addenda list`, `addenda show <NN>`, `addenda approve <NN>`, `addenda apply <NN>`,
      `task new <ID> "<Name>"`, `task list`, `task show <ID>`, `task next`,
      `close [N]`, `which`, `status`.

## Listing & query (unified across lanes)

- [x] `orbit neocortex which`               — active issue (any lane)
- [x] `orbit neocortex status`              — active issue overview (lane-aware)
- [x] `orbit neocortex issue list [--lane=quick|full] [--status <s>]`
- [x] `orbit neocortex issue show <N>`      — quick file verbatim, or concept + plan + DAG view
- [x] `orbit neocortex issue switch <N>`
- [x] `orbit neocortex close [N]`           — verify all tasks Close; print report (read-only)

## Frontmatter tokens (CLI-owned set/validate)

- [x] `Class: quick` (absent = default lane) — written by the quick stub; drives lane detection.
- [x] `Source: <url|interactive|file|promoted:quick>` — provenance; `promoted:quick` set by promote.
- [ ] `Decided-By: operator` — **documented only, not enforced.** Ships in the quick stub, but the
      CLI neither validates nor rewrites it yet; finishing this needs the Judger-era guardrails.
      Deferred on purpose (applies to every lane, not just quick).

## Completion (first-class requirement)

- [x] `orbit neocortex <TAB>` completes subcommands (`issue`, `quick`, `task`, `plan`,
      `addenda`, `close`, `which`, `status`, `update`).
- [x] Lane/action completions stop at verbs; entity completions resolve live issue context.
- [x] `orbit completion [bash|zsh|fish|powershell]` installs idempotently (per-shell user dir,
      `--script` / `--uninstall` / `--json`), auto-detecting `$SHELL`.
- [ ] Keep the existing `--completions <bash|zsh|fish|sh>` global flag working for the new tree —
      **superseded by `orbit completion`**; decide whether to keep a `--completions` alias for
      backward compatibility or drop it. (No such flag exists in v0.2.0.)
