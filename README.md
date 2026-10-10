# orbit

The Go execution engine of the [NeoCortex](../registry/README.md) workflow:
a deterministic filesystem + state-machine CLI. Cobra-based. **Contains zero
assets** — no stubs, no prompts, no agent definitions are embedded in the
binary; everything deployable lives in the registry repository and is
consumed via the contract below.

```
orbit update                 # refresh global assets (no project needed)
orbit self update            # replace this binary with the latest release
orbit neocortex install      # first contact: fetch → cache → deploy → bootstrap
orbit neocortex which        # active issue dir
orbit neocortex status       # active issue overview (lane-aware)
orbit neocortex issue        # new | import | ingest | lock | list | show | switch
orbit neocortex quick        # new | import | ingest | start | revise | close | rework | promote
orbit neocortex plan         # lock
orbit neocortex addenda      # new | list | show | approve | apply
orbit neocortex task         # new | start | revise | close | rework | list | show | next
orbit neocortex close        # verify a default-lane issue is complete (read-only)
orbit cycles install         # first contact: fetch → cache → deploy → bootstrap the vault
orbit cycles work            # new | shape | bet | shelve | unshelve | deliver | sync | list | show
orbit cycles cycle           # new | sync | close | list | show
orbit cycles status          # board: current cycle + backlog ladder
orbit completion             # install bash/zsh/fish/powershell completion
```

Two lanes, one numbered ledger. Every work item is an **issue**:

- **default** (unmarked) — the full protocol: concept → plan → task DAG.
- **quick** (`Class: quick`) — one-sitting work in a single `00-quick.md`.

Intake selects the mode with a verb and takes a positional title:
`new "<title>"` (interactive), `import <n|url> "<title>"` (remote, injected
verbatim), `ingest <path> "<title>"` (file, injected verbatim). Status changes
are verbs, never `--set`: `quick` and `task` share the short verbs
`start | revise | close | rework`. `plan`, `addenda`, `task`, `quick`, and
`close` act on the ACTIVE issue; `issue <verb> [N]` addresses one by number.

## Domains: Cycles and NeoCortex

Orbit ships two domains, one per level of abstraction:

- **Cycles** — the **planning and commitment** layer. It answers *what should
  we work on, why, and when are we committing to it?* Work items (`W-####`)
  are captured, shaped, and bet into time-boxed cycles (`C-####`) whose
  Markdown lives in a global vault (`<vault.dir>/Cycles/`). It is offline by
  default: only the two explicit sync verbs — `work sync` and `cycle sync` —
  touch the network, mirroring bets and cycles to GitHub/GitLab issues and
  milestones via the `gh`/`glab` CLIs.
- **NeoCortex** — the **engineering execution** layer. It answers *how do we
  actually build it?* Issues, plans, and tasks live in a project's
  `.neocortex/`.

They are two levels of one system, not competing trackers: one planning
tracker (Cycles) and one execution tracker (NeoCortex). The handoff is
**one-way** and read-only across the boundary — Cycles reads a project's
`.neocortex/` only as evidence and never mutates it, and both degrade
gracefully when unlinked. The bridge is the pair `{project dir, issue:int}`
(NeoCortex tasks stay `T1..Tn`; there is no synthetic cross-system ID).
Only a human marks a Cycles item `Delivered`.

## Registry contract (neocortex 0.7.0 · cycles 0.1.0)

The registry is a git repo (default GitHub). Everything deployable lives
under the domain folders — `neocortex/` and the sibling `cycles/` tree;
`README.md` is repo documentation and is never deployed.

```
<registry-repo>/
├── README.md                    # never deployed
├── neocortex/
│   ├── manifest.json            # version anchor: version + layout + sha256 per file
│   ├── NEOCORTEX.md             # workflow reference (kept in the registry; not copied into projects)
│   ├── stubs/*.stub.md          # → global cache (~/.config/orbit/neocortex/cache/stubs/)
│   └── opencode/
│       ├── agents/*.md          # → <opencode.dir>/agents/  (default ~/.config/opencode)
│       └── commands/*.md        # → <opencode.dir>/commands/
└── cycles/
    ├── manifest.json            # cycles version anchor (its own version + sha256 per file)
    ├── CYCLES.md                # → <vault.dir>/Cycles/ (copy-if-missing only)
    ├── stubs/*.stub.md          # → global cache (~/.config/orbit/cycles/cache/stubs/)
    └── opencode/
        ├── agents/cycles.md     # → <opencode.dir>/agents/
        └── commands/cycles:*.md # → <opencode.dir>/commands/
```

Mapping rules:

- Everything under a domain's `opencode/` maps 1:1 into the opencode dir;
  future subfolders inherit the rule.
- `stubs/` follow their domain's cache rule; `NEOCORTEX.md` stays in the
  registry (it is never copied into a project), and `cycles/CYCLES.md` is
  copied into the vault (copy-if-missing only).
- Each domain has its **own** `manifest.json` listing **every** deployable
  file with its sha256. The CLI verifies each file at fetch time; a mismatch
  is a registry-integrity failure (exit 4) and nothing is deployed. A
  manifest never lists itself.
- The global cache and `deployed.json` are **namespaced per domain**
  (`~/.config/orbit/<domain>/…`); `deployed.json` records what was deployed
  (relpath → {version, sha256, deployed_at}) with keys mirroring that
  domain's manifest paths.
- All `path` values are relative to the domain folder (`neocortex/` or
  `cycles/`); paths that escape (`../`, absolute) are refused.

Regenerating the manifests is mechanical and must never be hand-edited. The
registry repo ships one generator that rewrites **both** domains' manifests
(each domain versions independently):

```sh
# in the registry repo
scripts/manifest.sh generate     # rehash both manifests, no version bump
scripts/manifest.sh bump patch   # or minor / major
```

## Stub placeholder convention

Stubs use `{{UPPER_SNAKE}}` tokens. Each stub has a fixed known-token set,
validated at cache time. A render with unresolved tokens is always an
error — leftovers are never rendered silently. The `<!-- Agent: ... -->`
block marks agent-owned fill-ins; the CLI refuses to lock any artifact that
still contains one.

## Installation

```sh
curl -fsSL https://orbit.rtwostudio.ir/install.sh | bash
```

The installer detects OS/arch, resolves the latest GitHub Release of this
repo, verifies the sha256 checksum, and:

- **no `orbit` binary** → installs it to `~/.local/bin`
- **older binary** → updates it to the latest release
- **same/newer binary** → leaves it untouched
- seeds `~/.config/orbit/config.json` (only if no config exists yet)

Releases are cut by pushing a `v*` tag: `.github/workflows/release.yml`
builds linux × amd64/arm64 tarballs + `checksums.txt` and attaches
them to the GitHub Release.

## Configuration

The canonical file is `~/.config/orbit/config.yml` (YAML; overridable via
`--config` / `ORBIT_CONFIG`). A legacy `config.json` is still read when no
`.yml` exists, so existing setups keep working; new installs seed YAML only.

```yaml
registry:
  url: https://github.com/RTwoStudio/orbit-registry.git   # or a GitLab .git URL
  ref: main
tokens:                            # env-var NAMES — the secrets live in your shell
  registry: ORBIT_REGISTRY_TOKEN
  github: ORBIT_GITHUB_TOKEN
  gitlab: ORBIT_GITLAB_TOKEN
opencode:
  dir: ~/.config/opencode
neocortex:
  dir: .neocortex
vault:
  dir: ~/.orbit-vault               # Cycles vault root; the domain lives in <dir>/Cycles/
ui:
  color: auto                       # auto (TTY-only) | always | never; NO_COLOR and --no-color also win
```

Env overrides (highest precedence): `ORBIT_REGISTRY_URL`, `ORBIT_OPENCODE_DIR`,
`ORBIT_GITLAB_URL` (self-hosted GitLab host for issue intake).

**Tokens:** `config.yml` never holds a secret — the `tokens:` block holds the
**names** of environment variables. Export them yourself:

```sh
export ORBIT_REGISTRY_TOKEN=…   # registry repo access
export ORBIT_GITHUB_TOKEN=…     # github.com issue/PR intake
export ORBIT_GITLAB_TOKEN=…     # gitlab.com (or self-hosted) issue/MR intake
```

The registry token is sent as `Authorization: Bearer` to GitHub and
`PRIVATE-TOKEN` to GitLab; token values are never logged or stored.

## License

[Apache-2.0](LICENSE) with an attribution requirement — the same spirit as
OSM's model: **use it freely, but say you use it.** Any redistribution of
orbit (binaries, forks, or products embedding it) must keep the
[NOTICE](NOTICE) file naming RTwo Studio (License §4(d)). The NOTICE file
is the OSI-approved enforcement of "powered by orbit" — no open-source
license can mandate UI badges, but this one makes the credit legally
non-removable.

## Registry access

The asset registry (`RTwoStudio/orbit-registry`) is **private until v1.0.0**.
Until then, users need a read-only fine-grained GitHub PAT:

```sh
export ORBIT_REGISTRY_TOKEN=github_pat_…   # read-only Contents on orbit-registry
```

The token is used for authenticated clone/tarball fetches and is never
logged or stored (see `tokens:` in Configuration).

## Exit codes

| Code | Name | Meaning |
|---|---|---|
| 0 | ok | success (incl. "no runnable task") |
| 1 | general | unexpected internal error |
| 2 | usage | bad flags/args |
| 3 | config_error | missing/invalid config |
| 4 | registry_unreachable | network/ref/auth/registry-integrity |
| 5 | not_found | file/issue/task/addenda missing |
| 6 | preflight_failed | validation refused (names the exact failing item) |
| 7 | state_conflict | illegal transition / already locked / duplicate |
| 8 | tamper_detected | hash mismatch on a locked artifact |
| 9 | no_active_run | no usable ACTIVE pointer |
| 10 | io_error | permissions, disk, etc. |
| 11 | not_initialized | wrong setup verb for this directory |

## Logging

`~/.config/orbit/log/orbit.log` (override: `ORBIT_LOG_DIR`). Pipe-delimited,
greppable. Every invocation writes a `start` and an `end` line; all exits
route through `internal/exit` so no exit path skips logging. `DEBUG` lines
are written only when `ORBIT_DEBUG=1`. Tokens, file bodies, and prompt
answers are never logged.

## Build & test

```sh
go build -ldflags "-X github.com/RTwoStudio/orbit/internal/cmd.version=v0.2.0" -o orbit ./cmd/orbit
go test ./...
go vet ./...
```

## Self-update (binary)

```sh
orbit self update            # update if a newer release exists
orbit self update --check    # report current vs latest; change nothing
orbit self update --force    # reinstall even if same or newer
```

`orbit self update` fetches the latest GitHub release, verifies the asset
against `checksums.txt`, extracts the binary to a temp file **on the same
filesystem** as the current one, and `rename(2)`s it over the running
executable. That rename is why no restart dance is needed: a running
executable can be renamed over on Linux/macOS (the old inode stays alive until
the process exits), so the *next* invocation is the new version. It refuses to
downgrade without `--force`, aborts on checksum mismatch, and errors clearly
when the install dir isn't writable. **Linux only** for now; Windows raises an
error (a running `.exe` can't be replaced in place).

Source overrides (mirroring `install.sh`): `ORBIT_GH_REPO`, `ORBIT_GH_API`,
`ORBIT_GH_DL`, and `ORBIT_GITHUB_TOKEN` for private repos/rate limits. The
binary path comes from `os.Executable()` (symlinks resolved) or
`ORBIT_BIN_DIR/<orbit>`.

`orbit update` (top-level) is different: it refreshes **both domains'**
registry assets (each domain's cache + opencode agents/commands). It writes
only `~/.config`, so it runs in any directory — no project setup gate.
`orbit neocortex update` still works as a deprecated alias. Use `--prune` to
delete orphaned deployed assets (only files the CLI deployed and that you
haven't edited since).

## Registries: GitHub and GitLab

```yaml
registry:
  url: https://github.com/RTwoStudio/orbit-registry.git   # or https://gitlab.com/<group>/<repo>.git
  ref: main
```

Git clone is preferred; when git is unavailable the tarball fallback supports
both hosts. Tokens come from the env vars named under `tokens:` in
`config.yml` (the secret itself is never stored).

Remote issue intake (`issue import` / `quick import`) also supports both:

```sh
orbit neocortex quick import https://github.com/acme/app/issues/42 "Title"
orbit neocortex quick import https://gitlab.com/acme/app/-/issues/42 "Title"
```

Self-hosted GitLab: set `ORBIT_GITLAB_URL=https://gitlab.example.com`.

## Shell completion

```sh
orbit completion                 # install for $SHELL (or bash), idempotently
orbit completion zsh             # install for a named shell
orbit completion fish --script   # print the script instead of installing
orbit completion --uninstall     # remove an installed script
```

Two paths install it automatically, so most users never run this by hand:

- **`install.sh`** calls the freshly-installed binary once (silently, failure
  is non-fatal), so a `curl | bash` install leaves you with completion.
- **First `orbit neocortex install`** offers it once (interactive TTY only;
  `--yes` accepts). It never installs when a completion file already exists.

Opt out of both with `ORBIT_NO_COMPLETION=1` (install.sh honours it too).

Supported: `bash`, `zsh`, `fish`, `powershell`. Each installs into that
shell's user completion directory (`~/.zsh/completions/_orbit`,
`~/.local/share/bash-completion/completions/orbit`,
`~/.config/fish/completions/orbit.fish`, a PowerShell `.ps1`), prints the
one-line load step, and is safe to re-run. Override the target directory with
`ZSH_COMPLETION_DIR`, `BASH_COMPLETION_USER_DIR`, `FISH_COMPLETION_DIR`, or
`POWERSHELL_COMPLETION_DIR`.

## Scope notes (v0.2.0)

- **Verb-first CLI.** Intake is mode verbs (`new`/`import`/`ingest`); status
  changes are short verbs (`quick`/`task` `start|revise|close|rework`). The old
  `--title`/`--interactive`/`--from-remote`/`--from-file`, `task status --set=`,
  and `--issue=<n>` surfaces are **gone** (breaking; pre-v1).
- **Quick lane.** `orbit neocortex quick` runs one-sitting work in a single
  `00-quick.md`. `quick promote <N>` converts it to the default lane in place.
- **`close` exists** and is read-only: it verifies every task is `Close` and
  prints the report. Quick runs close through `quick close <N>`.
- **Cycles domain.** `orbit cycles` adds the vault-rooted planning layer
  (`work`/`cycle`/`status`) beside neocortex; `orbit cycles install` (alias
  `init`) bootstraps `<vault.dir>/Cycles/` idempotently. It stays offline
  except for the opt-in forge sync verbs `work sync` / `cycle sync`
  (`gh`/`glab`), which mirror bets/cycles to GitHub/GitLab issues/milestones.
- Update touches only global state (cache, opencode assets,
  `deployed.json`) — never the project or the vault.
- Only install/update ever prompt. Domain commands are deterministic and
  agent-safe.
