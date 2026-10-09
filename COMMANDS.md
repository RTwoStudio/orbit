# Orbit CLI — Complete Command Reference

**Version:** v0.2.0 · **Registry:** neocortex 0.7.0 · cycles 0.1.0

Every command, every operand, every flag. For the narrative of what changed
from v0.1.0, see the changelog section at the bottom.

---

## Conventions

```
orbit <group> <subgroup> <verb> [operands...] [flags]
```

- **Operands are positional:** `<title>`, `<n|url>`, `<path>`, `<ID>`, `<NN>`, `[n]`.
- **ACTIVE scoping:** `plan`, `addenda`, `task`, `quick`, and `close` act on the
  **ACTIVE** issue (`.neocortex/ACTIVE`). Only `issue <verb> [N]` addresses an
  issue by number. Cross-issue work: `orbit neocortex issue switch <N>` first.
- **Two lanes, one ledger:** every work item is an issue under one counter
  (`issues/0001-issue/`).
  - *default* (unmarked) — the full protocol: concept → plan → task DAG.
  - *quick* (`Class: quick`) — one-sitting work in a single `00-quick.md`.
- **Two domains:** `neocortex` (project-rooted engineering execution, under
  `.neocortex/`) and `cycles` (vault-rooted planning & commitment, under
  `<vault.dir>/Cycles/`). The handoff is one-way; Cycles reads `.neocortex/`
  as evidence only and never mutates it.
- **The CLI owns frontmatter.** Status changes happen only through verbs; never
  edit `Status`, locks, or hashes by hand.

### Global flags (available on every command)

| Flag | Type | Meaning |
|---|---|---|
| `--config <path>` | string | user config path (default `~/.config/orbit/config.yml`; or `ORBIT_CONFIG`) |
| `--json` | bool | machine-readable JSON on stdout (human text on stderr) |
| `--no-color` | bool | disable colored output |
| `-y, --yes` | bool | auto-confirm prompts (accept defaults) |
| `-h, --help` | bool | help for the command |
| `-v, --version` | bool | (root only) print the version |

### Exit codes

| Code | Name | Meaning |
|---|---|---|
| 0 | ok | success (incl. "no runnable task") |
| 1 | general | unexpected internal error |
| 2 | usage | bad flags/args |
| 3 | config_error | missing/invalid config |
| 4 | registry_unreachable | network / ref / auth / registry integrity |
| 5 | not_found | file / issue / task / addenda missing |
| 6 | preflight_failed | validation refused (names the failing item) |
| 7 | state_conflict | illegal transition / already locked / duplicate |
| 8 | tamper_detected | hash mismatch on a locked artifact |
| 9 | no_active_run | no usable ACTIVE pointer |
| 10 | io_error | permissions, disk, etc. |
| 11 | not_initialized | wrong setup verb for this directory |

---

# Top-level commands

## `orbit update`

Refresh **global** registry assets: the registry cache, opencode
agents/commands, and `deployed.json`. Writes only `~/.config/*` — works in any
directory, initialized project or not (no project setup gate).

| Flag | Type | Meaning |
|---|---|---|
| `--registry <url>` | string | one-shot registry URL override (this invocation only; never persisted) |
| `--ref <branch>` | string | one-shot branch/tag override |
| `--prune` | bool | delete orphaned deployed assets (locally modified ones are kept; user files never touched) |

Behavior: registry newer (semver) → prompt (TTY); non-TTY without `--yes`
declines and lists pending updates. Same version but drifted content → prompt,
default NO. Recorded but absent from the manifest → `orphaned (kept)`.

`--prune` deletes orphans and drops their `deployed.json` records, but **only**
when the on-disk file still matches the sha256 the CLI recorded. A locally
modified orphan is kept (only its ledger entry is dropped). Files that were
never in `deployed.json` — your own commands — are never touched.

Exit codes: 0 · 3 · 4 · 10.

```sh
orbit update --yes
orbit update --prune
```

The one-shot override is **not** written to config: precedence is
`--registry` flag > `ORBIT_REGISTRY_URL` env > `config.yml` > embedded default.

The registry accepts **GitHub and GitLab** URLs. Git clone is preferred; when
git is unavailable the tarball fallback supports both hosts
(`api.github.com/.../tarball/<ref>` and
`gitlab.com/api/v4/projects/<enc>/repository/archive.tar.gz?sha=<ref>`).

## `orbit self update`

Replace the **running binary** with the latest GitHub release.

| Flag | Type | Meaning |
|---|---|---|
| `--check` | bool | report current vs latest and exit; change nothing |
| `--force` | bool | reinstall even if same version or a newer build is present |

Env overrides (mirroring `install.sh`): `ORBIT_GH_REPO`, `ORBIT_GH_API`,
`ORBIT_GH_DL`, `ORBIT_GITHUB_TOKEN` (private repos / rate limits),
`ORBIT_BIN_DIR` (binary directory override).

How it works: resolve latest tag → download the per-OS/arch asset + verify
`checksums.txt` → extract the binary to a temp file **on the same filesystem**
→ atomic `rename(2)` over the current binary. Legal on a running executable:
the old inode stays alive until the process exits, so the **next** invocation
is the new version. Refuses to downgrade without `--force`; aborts on checksum
mismatch with the binary untouched; errors when the install dir isn't writable.
Linux only (a running Windows `.exe` cannot be replaced in place).

Exit codes: 0 · 4 · 7 · 10.

```sh
orbit self update            # update if a newer release exists
orbit self update --check    # report current vs latest
orbit self update --force    # reinstall even if same or newer
```

## `orbit completion [bash|zsh|fish|powershell]`

Install a shell completion script for the detected (or named) shell,
idempotently. Auto-detects `$SHELL` when no shell is given.

| Flag | Type | Meaning |
|---|---|---|
| `--script` | bool | print the raw completion script to stdout instead of installing |
| `--uninstall` | bool | remove an installed completion script |

Install dirs (overridable via env): `ZSH_COMPLETION_DIR` →
`~/.zsh/completions/_orbit`; `BASH_COMPLETION_USER_DIR` →
`~/.local/share/bash-completion/completions/orbit`; `FISH_COMPLETION_DIR` →
`~/.config/fish/completions/orbit.fish`; `POWERSHELL_COMPLETION_DIR` → a `.ps1`.

**Automatic install:** `install.sh` runs `orbit completion` at the end of the
install (silent, non-fatal), and the first `orbit neocortex install` offers it
once (interactive TTY only; skipped when a completion file already exists).
Set `ORBIT_NO_COMPLETION=1` to opt out of both.

Exit codes: 0 · 2 · 7 · 10.

```sh
orbit completion                 # install for $SHELL (or bash)
orbit completion zsh             # install for zsh explicitly
orbit completion fish --script   # print the script, do not install
orbit completion --uninstall
```

---

# `orbit neocortex`

## Setup & overview

### `orbit neocortex install`

First-contact setup: fetch the registry → populate the cache → deploy opencode
agents/commands → bootstrap the project tree (`.neocortex/`, `.gitignore`
entry, `NEOCORTEX.md` copy-if-missing).

| Flag | Type | Meaning |
|---|---|---|
| `--registry <url>` | string | one-shot registry URL override |
| `--ref <branch>` | string | one-shot branch/tag override |
| `--global-only` | bool | skip the project gate and bootstrap; only cache + opencode deployment |

Gate: refuses when `.neocortex/` already exists (exit 11) — install is
role-locked to uninitialized projects; use `orbit update` afterwards.

Exit codes: 0 · 3 · 4 · 7 · 10 · 11.

### `orbit neocortex which`

Print the absolute path of the ACTIVE issue directory.

Exit codes: 0 · 5 · 9 · 10.

### `orbit neocortex status`

Render the ACTIVE issue's full overview, **lane-aware**:
- quick → the file path and quick status;
- default → concept/plan status, addenda counts, and the effective Task DAG
  with per-task statuses and both-direction edges (Blocked By / Blocks).

Exit codes: 0 · 5 · 6 · 9.

### `orbit neocortex update` *(hidden / deprecated)*

Alias for `orbit update`; prints a deprecation nudge to stderr then does the
same work. Same flags (`--registry`, `--ref`). Hidden from `--help`.

---

## `orbit neocortex issue` — default-lane lifecycle

### `issue new "<title>"`

Interactive intake. Scaffolds `issues/0001-issue/` with `00-concept.md`,
`01-plan.md`, `tasks/`, `notes/`, `addenda/`, and sets ACTIVE. The concept's
Detail is an agent-instruction placeholder.

Exit codes: 0 · 4 · 6 · 7 · 10.

### `issue import <n|url> "<title>"`

Remote intake. Detail is injected **verbatim**. Supports:
- **GitHub:** `https://github.com/<o>/<r>/issues/<n>` (or `/pull/<n>`)
- **GitLab:** `https://gitlab.com/<group>/<proj>/-/issues/<n>` (or `/merge_requests/<n>`)
- any other URL → fetched as-is (raw GET)

15s timeout; the token is read from the env var named in config under
`tokens.github` / `tokens.gitlab` (GitHub: `Authorization: Bearer`; GitLab:
`PRIVATE-TOKEN`). GitLab self-hosted: set `ORBIT_GITLAB_URL`. Nothing is
written on failure.

Exit codes: 0 · 3 · 4 · 6 · 7 · 10.

### `issue ingest <path> "<title>"`

Local-file intake. Detail is injected **verbatim**; `Source:` records
`file:<path>`.

Exit codes: 0 · 5 · 6 · 7 · 10.

### `issue lock [n]`

Hash-lock the concept (one-way `Draft → Locked`). Defaults to ACTIVE; pass `n`
to target another. Preflights: `Status: Draft`, no `<!-- Agent:` placeholders,
no `{{ }}` tokens. Records `Locked-At` and `Lock-Hash` (sha256 of the body).

Exit codes: 0 · 5 · 6 · 7 · 8 · 9 · 10.

### `issue list [--lane=quick|full] [--status <s>]`

Table of **all** issues across both lanes.

| Flag | Type | Meaning |
|---|---|---|
| `--lane` | string | filter: `quick` or `full` |
| `--status` | string | case-insensitive; matches concept/plan (full) or quick status (quick) |

Columns: `ID · LANE · STATUS · TASKS · TITLE · ACTIVE`.

Exit codes: 0 · 2 · 10.

### `issue show <n>`

Quick run → prints `00-quick.md` verbatim. Default-lane issue → statuses,
addenda counts, and the task DAG.

Exit codes: 0 · 5 · 6.

### `issue switch <n>`

Point ACTIVE at an existing issue. (This is how you work across issues, since
the other groups always target ACTIVE.)

Exit codes: 0 · 5 · 10.

---

## `orbit neocortex quick` — light lane

Lifecycle: `Draft → In Progress → Revise → Rework → Close`
(`Rework → In Progress` on resume).

### Intake

| Command | Source | Injected as |
|---|---|---|
| `quick new "<title>"` | conversation | agent placeholder |
| `quick import <n\|url> "<title>"` | GitHub/GitLab issue/PR | fetched text, verbatim |
| `quick ingest <path> "<title>"` | local file | file bytes, verbatim |

Each scaffolds `issues/0001-issue/00-quick.md` (`Class: quick`, `Status: Draft`)
and sets ACTIVE.

Exit codes: 0 · 3 · 4 · 5 · 6 · 7 · 10.

### Transitions

| Command | Effect |
|---|---|
| `quick start <n>` | Draft → In Progress (also Rework → In Progress) |
| `quick revise <n>` | In Progress → Revise |
| `quick close <n>` | Revise → Close; **refuses while `## Result` is empty** |
| `quick rework <n>` | Revise → Rework |

Each appends a `<RFC3339>  from → to` line to the file's CLI log comment.

Exit codes: 0 · 5 · 6 · 7 · 10.

### `quick promote <n>`

Convert to the default lane **in place**: render `00-concept.md` from the quick
`Intent` (Detail notes the promotion), create `01-plan.md` + `tasks/` +
`addenda/` + `notes/`, remove `00-quick.md`. No folder move, no restart.

Exit codes: 0 · 4 · 5 · 7 · 10.

---

## `orbit neocortex plan`

### `plan lock [n]`

Hash-lock `01-plan.md` (one-way). Preflight order:
1. **concept hash verified first** (tamper → exit 8);
2. plan exists and `Status: Draft`;
3. no `<!-- Agent:` placeholders, no `{{ }}` tokens;
4. `## Open Questions` has no unticked `- [ ]` items;
5. `## Task DAG` parses (≥1 task, unique IDs, deps exist, topologically valid).

Prints the DAG summary on success.

Exit codes: 0 · 5 · 6 · 7 · 8 · 9 · 10.

---

## `orbit neocortex addenda` — change a locked plan

The **only** sanctioned way to change a locked plan. Delta grammar (in
`## Plan Changes`):

```
- ADD [T5] Task name (Blocked by: T2)
- REMOVE [T3] Task name — reason
- MODIFY [T2] New name (Blocked by: T1) — what changes
```

### `addenda new "<title>"`

Scaffold `NN-<slug>.md` (requires plan Locked + concept hash ok).

Exit codes: 0 · 5 · 6 · 8 · 9 · 10.

### `addenda list`

`N · Title · Status · Created · Applied-At` for the ACTIVE issue.

Exit codes: 0 · 5 · 9 · 10.

### `addenda show <NN>`

Print the addenda file.

Exit codes: 0 · 5 · 9 · 10.

### `addenda approve <NN>`

`Draft → Approved` (one-way). Requires no placeholders and ≥1 parseable delta.

Exit codes: 0 · 5 · 6 · 7 · 9.

### `addenda apply <NN>`

Apply an Approved addenda to the locked plan: rewrite the Task DAG, append an
Amendments line, re-chain the plan hash, stamp touched task files, mark the
addenda Applied. Prints the before/after DAG.

Exit codes: 0 · 5 · 6 · 7 · 8 · 9 · 10.

---

## `orbit neocortex task` — JIT task lifecycle

Tasks are created Just-In-Time from the plan's effective DAG (original rows as
amended by applied addenda). All verbs target the ACTIVE issue.

### `task new <ID> "<Name>"`

Preflights: plan Locked + concept hash ok; ID syntax `^T[0-9]+$`; ID exists in
the effective DAG; the file must not already exist (else exit 7, reporting the
current status + remaining placeholder count, so the agent resumes instead of
recreating). Dependency warnings never block.

Exit codes: 0 · 5 · 6 · 7 · 8 · 9 · 10.

### Transitions

| Command | Effect |
|---|---|
| `task start <ID>` | Open → In Progress (also Rework → In Progress) |
| `task revise <ID>` | In Progress → Revise (requires non-empty Completion Notes + all Verification boxes ticked) |
| `task close <ID>` | Revise → Close (orchestrator-owned) |
| `task rework <ID>` | Revise → Rework |

Success appends a CLI log line.

Exit codes: 0 · 5 · 6 · 7 · 9.

### `task list`

`ID · NAME · STATUS · DEPENDSON · ORIGIN · DEP STATUS`.

Exit codes: 0 · 5 · 6 · 9.

### `task show <ID>`

Print `tasks/<ID>.md` verbatim.

Exit codes: 0 · 5 · 9.

### `task next`

First Open task whose every dependency is Close. Exit 0 with a friendly
message when nothing is runnable (**not** an error).

Exit codes: 0 · 5 · 6 · 9.

---

## `orbit neocortex close [n]`

Verify a default-lane issue is complete: plan Locked and **every** task in the
effective DAG is Close. Prints the report and points at the commit step.
**Read-only** — it never mutates files. Refuses a quick run (points at
`quick close <n>`).

Exit codes: 0 · 5 · 6 · 7 · 9.

---

# `orbit cycles` — planning & commitment domain

The vault-rooted planning layer that sits beside the project-rooted `neocortex`
domain. Work items are captured, shaped, and bet into time-boxed cycles; the
vault's Markdown is the source of truth, and NeoCortex consumes the one-way
handoff for execution. The vault root resolves as `--vault` (one-shot override)
> `config.vault.dir`.

The `cycles` group and every subcommand carry the persistent `--vault <dir>`
flag; `--json` (global) is first-class on every read verb.

Lifecycle: work `Backlog → Pitched → Bet → Delivered` (`Shelved` reachable from
the pre-bet/bet stages; `unshelve` returns to `Pitched`); cycles `Open →
Closed`. Illegal moves are `state_conflict` (7). Files move only across the
`backlog/` ↔ `cycles/C-####/` boundary; every other change is frontmatter-only.

## Setup

### `orbit cycles install` (alias `init`)

First-contact setup: fetch the registry → populate the cycles cache → deploy
the cycles opencode agent/commands → bootstrap `<vault>/Cycles/` (`CYCLES.md`,
empty `CURRENT`, `backlog/`, `cycles/`). Copy-if-missing only, so re-running is
a no-op and a locally edited `CYCLES.md` is preserved. Vault-gated (not
project-gated): it never touches a project's `.neocortex/`.

Exit codes: 0 · 3 · 4 · 7 · 10.

```sh
orbit cycles install                       # use config vault.dir
orbit cycles init --vault ~/orbit-vault     # explicit vault root
```

## `orbit cycles work` — work items

### `work new "<title>" --scope <s>`

Create a `W-####` work item in `backlog/` as `Backlog`.

| Flag | Type | Meaning |
|---|---|---|
| `--scope` | string | project surface (required; e.g. `delijan-driver-app`) |

Preflight: title + scope non-empty (exit 6); vault initialized (exit 11).
Exit codes: 0 · 2 · 4 · 6 · 10 · 11.

### `work shape <W-####> --appetite big|small`

`Backlog → Pitched`, stamping the appetite.

| Flag | Type | Meaning |
|---|---|---|
| `--appetite` | string | `big` \| `small` (required) |

Preflight: item is `Backlog` (else exit 7); appetite valid (exit 6); the
`## Problem`, `## Solution Sketch`, `## Rabbit Holes`, and `## No-gos` sections
are all non-empty (exit 6).
Exit codes: 0 · 2 · 5 · 6 · 7 · 10.

### `work bet|shelve|unshelve|deliver <W-####>`

| Verb | Transition | Preflight |
|---|---|---|
| `bet` | `Pitched → Bet` (moves into `cycles/C-####/`) | item is `Pitched`; a current cycle is open (else exit 7) |
| `shelve` | `Backlog`/`Pitched`/`Bet` → `Shelved` (returns to `backlog/` if committed) | legal source status (else exit 7) |
| `unshelve` | `Shelved → Pitched` | item is `Shelved` (else exit 7) |
| `deliver` | `Bet → Delivered` (terminal; stays in the cycle folder) | item is `Bet` (else exit 7); human-only by convention |

Exit codes: 0 · 2 · 5 · 6 · 7 · 10.

### `work list [--status <s>] [--scope <s>]`

Table `ID · TITLE · STATUS · SCOPE · CYCLE` over `backlog/` and every cycle.
`--status` is case-insensitive; `--scope` is an exact match. `--json` emits the
typed slice.

Exit codes: 0 · 2 · 6 · 10.

### `work show <W-####>`

Human output prints the note verbatim; `--json` emits the typed `WorkItem`.

Exit codes: 0 · 2 · 5 · 6 · 10.

## `orbit cycles cycle` — cycles

### `cycle new "<goal>" --release <semver> [--start <date>] [--end <date>]`

Open the next `C-####` cycle and point `CURRENT` at it.

| Flag | Type | Meaning |
|---|---|---|
| `--release` | string | strict `MAJOR.MINOR.PATCH` (required) |
| `--start` | string | `YYYY-MM-DD` (default: today UTC) |
| `--end` | string | `YYYY-MM-DD` (default: start + 6 weeks) |

Preflight: goal non-empty (exit 6); valid release (exit 6); vault initialized
(exit 11); no cycle already open (exit 7); valid dates, end ≥ start (exit 6).
Exit codes: 0 · 2 · 4 · 6 · 7 · 10 · 11.

### `cycle close`

Close the open cycle: every non-`Delivered` bet is auto-shelved back to
`backlog/` as `Shelved` (recording the cycle in its `history`), the cycle is
marked `Closed`, and `CURRENT` is cleared. Human-only by convention.

Preflight: a cycle is open and not already `Closed` (else exit 7).
Exit codes: 0 · 2 · 5 · 6 · 7 · 10.

### `cycle list`

Table `ID · GOAL · RELEASE · STATUS · START · END`; `--json` emits the slice.

Exit codes: 0 · 2 · 10.

### `cycle show <C-####>`

Human output prints the cycle note verbatim; `--json` emits the typed `Cycle`
including its regenerated `## Bets` view.

Exit codes: 0 · 2 · 5 · 6 · 10.

## `orbit cycles status`

Render the board: the open cycle (or "No open cycle.") plus the labelled
`Backlog` / `Pitched` / `Shelved` blocks. Read-only. `--json` emits the typed
`StatusBoard` (`{current, backlog}`).

Exit codes: 0 · 2 · 3 · 5 · 10.

---

# Flag summary (non-global)

| Command | Flags |
|---|---|
| `orbit update` | `--registry <url>`, `--ref <branch>`, `--prune` |
| `orbit self update` | `--check`, `--force` |
| `orbit completion` | `--script`, `--uninstall` |
| `orbit neocortex install` | `--registry`, `--ref`, `--global-only` |
| `orbit neocortex issue list` | `--lane=quick\|full`, `--status <s>` |
| `orbit cycles` (group + all subcommands) | `--vault <dir>` (persistent) |
| `orbit cycles work new` | `--scope <s>` (required) |
| `orbit cycles work shape` | `--appetite big\|small` (required) |
| `orbit cycles work list` | `--status <s>`, `--scope <s>` |
| `orbit cycles cycle new` | `--release <semver>` (required), `--start <date>`, `--end <date>` |
| `orbit cycles work show` / `cycle show` | `--json` |
| all other commands | operands only (+ global flags) |

---

# Common flows

```sh
# Quick lane (one-sitting work)
orbit neocortex quick new "Retry limit on the client"
orbit neocortex quick start 5          # implement…
orbit neocortex quick revise 5         # after filling the checklist + Result
orbit neocortex quick close 5          # refused while ## Result is empty

# Full lane
orbit neocortex issue new "Add streaming API"
orbit neocortex issue lock             # after filling the concept
orbit neocortex plan lock              # after filling plan + Task DAG
orbit neocortex task new T1 "Implement parser"
orbit neocortex task start T1          # → task revise T1 → task close T1
orbit neocortex close                  # verify + report

# Pivot a locked plan
orbit neocortex addenda new "Add retry queue"
orbit neocortex addenda approve 1
orbit neocortex addenda apply 1

# Intake from elsewhere
orbit neocortex quick import 42 "Retry limit on the client"
orbit neocortex issue ingest spec.md "Add streaming API"

# Cycles (planning & commitment)
orbit cycles install                                          # bootstrap <vault.dir>/Cycles/
orbit cycles work new "Voice search" --scope delijan-driver-app
orbit cycles work shape W-0001 --appetite small              # after filling the shape sections
orbit cycles cycle new "Driver search hardening" --release 0.3.0
orbit cycles work bet W-0001
orbit cycles work deliver W-0001                             # human-only
orbit cycles work new "Offline mode" --scope delijan-driver-app
orbit cycles work shape W-0002 --appetite big
orbit cycles work bet W-0002
orbit cycles cycle close                                     # W-0002 → backlog/ (Shelved); CURRENT cleared
orbit cycles status --json                                   # board: current cycle + backlog ladder

# Housekeeping
orbit update --yes                     # refresh registry assets (global)
orbit self update                      # update the binary
orbit completion                       # install tab-completion
```

---

# v0.1.0 → v0.2.0 changelog

**Grammar change**
- Intake mode is a verb, not a flag: `new` / `import` / `ingest`.
- Title is a positional operand, not `--title`.
- Status changes are verbs: `task`/`quick` `start|revise|close|rework`, no
  `--set=<status>`.
- `--issue=<n>` removed everywhere; use positional `[N]` on `issue` verbs or
  `issue switch <N>`. All other groups target ACTIVE.

**New**
- **Quick lane** (`quick new|import|ingest|start|revise|close|rework|promote`),
  single `00-quick.md`.
- **`close`** verb (read-only completion verification).
- **`orbit update`** made global (no project gate); `neocortex update` kept as
  a deprecated alias. Added `--prune` for orphaned deployed assets (shasafe:
  never deletes locally modified or user-owned files).
- **`orbit self update`** — atomic in-place binary replacement from GitHub
  releases.
- **`orbit completion`** — installs shell completion for bash/zsh/fish/powershell.
- **GitLab support** — remote `import`/`ingest` and the registry tarball
  fallback handle GitLab (`gitlab.com/api/v4`, `PRIVATE-TOKEN`,
  `{title,description}`), alongside GitHub.
- **YAML config** — the installer seeds `config.yml` (was `config.json`);
  `.json` stays loadable for back-compat.
- Lane-aware `issue list` (`--lane`, `--status`), `issue show`, and `status`.

**Breaking (pre-v1, intentional)**
- `--title`, `--interactive`, `--from-remote`, `--from-file` removed.
- `task status --set=` removed.
- `--issue=<n>` removed.
- `orbit neocortex update` deprecated.

**Version**
- CLI `0.1.0 → 0.2.0`; registry `00-quick.stub.md` gained a `{{DETAIL}}` token
  (manifest rehashed at the already-published `0.6.0`).
