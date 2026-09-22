# orbit-cli

The Go execution engine of the [NeoCortex](../registry/README.md) workflow:
a deterministic filesystem + state-machine CLI. Cobra-based. **Contains zero
assets** — no stubs, no prompts, no agent definitions are embedded in the
binary; everything deployable lives in the registry repository and is
consumed via the contract below.

```
orbit neocortex install      # first contact: fetch → cache → deploy → bootstrap
orbit neocortex update       # OTA refresh of global assets (version-gated)
orbit neocortex which        # active issue dir
orbit neocortex status       # active issue overview (DAG + statuses)
orbit neocortex issue        # new | lock | list | switch
orbit neocortex plan         # lock
orbit neocortex addenda      # new | list | show | approve | apply
orbit neocortex task         # new | status | list | show | next
```

## Registry contract (v0.1.0)

The registry is a git repo (default GitHub). Everything deployable lives
under `neocortex/`; `README.md` is repo documentation and is never deployed.

```
<registry-repo>/
├── README.md                    # never deployed
└── neocortex/
    ├── manifest.json            # version anchor: version + layout + sha256 per file
    ├── NEOCORTEX.md             # → project root (copy-if-missing only)
    ├── stubs/*.stub.md          # → global cache (~/.config/orbit/neocortex/cache/stubs/)
    └── opencode/
        ├── agents/*.md          # → <opencode.dir>/agents/  (default ~/.config/opencode)
        └── commands/*.md        # → <opencode.dir>/commands/
```

Mapping rules:

- Everything under `opencode/` maps 1:1 into the opencode dir; future
  subfolders inherit the rule.
- `stubs/` and `NEOCORTEX.md` follow their specific rules above.
- `manifest.json` lists **every** deployable file with its sha256. The CLI
  verifies each file at fetch time; a mismatch is a registry-integrity
  failure (exit 4) and nothing is deployed. The manifest never lists itself.
- `deployed.json` (`~/.config/orbit/neocortex/deployed.json`) records what
  was deployed: relpath → {version, sha256, deployed_at}. Keys mirror
  manifest paths.
- All `path` values are relative to `neocortex/`; paths that escape
  (`../`, absolute) are refused.

Regenerating the manifest is mechanical and must never be hand-edited:

```sh
# registry repo
find neocortex -name '*.md' | sort | xargs sha256sum   # slot into manifest.json
```

The CLI's fixture mirror lives at `testdata/registry-fixture/` with its own
generator: `scripts/make-manifest.sh [version]`.

## Stub placeholder convention

Stubs use `{{UPPER_SNAKE}}` tokens. Each stub has a fixed known-token set,
validated at cache time. A render with unresolved tokens is always an
error — leftovers are never rendered silently. The `<!-- Agent: ... -->`
block marks agent-owned fill-ins; the CLI refuses to lock any artifact that
still contains one.

## Configuration

`~/.config/orbit/config.yml` (overridable via `--config` / `ORBIT_CONFIG`):

```yaml
registry:
  url: https://github.com/RTwoStudio/orbit-registry.git   # default; set "" to require explicit config
  ref: main
  token_env: ORBIT_REGISTRY_TOKEN   # env var NAME holding an access token for PRIVATE registries
opencode:
  dir: ~/.config/opencode
neocortex:
  dir: .neocortex
vault:
  dir: ~/.orbit-vault               # reserved (v0.2.0)
ui:
  color: auto                       # auto (TTY-only) | always | never; NO_COLOR and --no-color also win
```

Env overrides (highest precedence): `ORBIT_REGISTRY_URL`, `ORBIT_OPENCODE_DIR`.

Private registries: set the env var named by `token_env` (e.g. `export
ORBIT_REGISTRY_TOKEN=ghp_…`). It is used for authenticated `git clone` and
API tarball downloads; the token value is never logged or stored.

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
go build -ldflags "-X github.com/orbit-sh/orbit-cli/internal/cmd.version=v0.1.0" -o orbit ./cmd/orbit
go test ./...
go vet ./...
```

## Scope notes (v0.1.0)

- `orbit neocortex close` does **not** exist; the registry ships the
  `/close` command file with a "not implemented" notice for forward
  compatibility. Invoking the verb falls through to cobra's
  unknown-command error.
- Update touches only global state (cache, opencode assets,
  `deployed.json`) — never the project.
- Only install/update ever prompt. Domain commands are deterministic and
  agent-safe.
