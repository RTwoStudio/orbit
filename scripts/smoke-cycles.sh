#!/usr/bin/env bash
#
# smoke-cycles.sh — end-to-end smoke for the `orbit cycles` domain.
#
# This is verification tooling, not a shipped feature. It builds the current CLI
# from source, serves a snapshot of the real registry working tree over a local
# git URL, and drives the full lifecycle in a throwaway vault — all under an
# isolated HOME so the developer's ~/.config is never touched.
#
# Sequence (per cycles-v1-plan.md §5):
#   install → work new → shape → cycle new → bet → deliver
#           → work new → shape → bet → cycle close (auto-shelve)
#           → list/show/status (human + --json) → update (both domains)
#           → forge sync (stubbed gh/glab: cycle sync → work sync, github
#             + gitlab, idempotency, skip/override, missing-CLI/unauthenticated
#             degradation)
# plus the exit-code contract and a no-neocortex-regression check.
#
# Usage:
#   orbit/scripts/smoke-cycles.sh [--keep]
#
# Env:
#   ORBIT_REGISTRY_DIR   registry checkout to snapshot (default: ../orbit-registry)
#   ORBIT_SMOKE_BIN      use an existing orbit binary instead of building one
#
# Exit: 0 when every assertion passes, 1 otherwise.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ORBIT_SRC="$(cd "$SCRIPT_DIR/.." && pwd)"
REGISTRY_SRC="${ORBIT_REGISTRY_DIR:-$(cd "$ORBIT_SRC/.." && pwd)/orbit-registry}"
REAL_HOME="${HOME:-}"

KEEP=0
[ "${1:-}" = "--keep" ] && KEEP=1

command -v git >/dev/null 2>&1 || { echo "smoke: git is required" >&2; exit 1; }
[ -d "$REGISTRY_SRC/neocortex" ] && [ -d "$REGISTRY_SRC/cycles" ] || {
  echo "smoke: no registry checkout at $REGISTRY_SRC (set ORBIT_REGISTRY_DIR)" >&2
  exit 1
}

TMP="$(mktemp -d "${TMPDIR:-/tmp}/orbit-cycles-smoke.XXXXXX")"
cleanup() {
  if [ "$KEEP" = "1" ]; then
    echo "smoke: workdir kept at $TMP"
  else
    chmod -R u+w "$TMP" 2>/dev/null || true
    rm -rf "$TMP"
  fi
}
trap cleanup EXIT

# --- Isolation: never touch the developer's real config ---------------------
export HOME="$TMP/home"
mkdir -p "$HOME"
export ORBIT_NO_COMPLETION=1
unset ORBIT_CONFIG ORBIT_REGISTRY_URL ORBIT_OPENCODE_DIR ORBIT_GITLAB_URL 2>/dev/null || true

VAULT="$TMP/vault"
OPENCODE_DIR="$TMP/opencode"
REG_REPO="$TMP/registry"
LOG="$TMP/transcript.log"
CONFIG="$TMP/config.yml"

# --- Build the current CLI --------------------------------------------------
ORBIT="${ORBIT_SMOKE_BIN:-$TMP/orbit}"
if [ -z "${ORBIT_SMOKE_BIN:-}" ]; then
  echo "smoke: building orbit from $ORBIT_SRC"
  # Build under the real HOME so we reuse the normal module cache (the
  # isolated HOME above is only for the CLI's ~/.config).
  ( cd "$ORBIT_SRC" && HOME="$REAL_HOME" go build -o "$ORBIT" ./cmd/orbit )
fi
[ -x "$ORBIT" ] || { echo "smoke: orbit binary not executable: $ORBIT" >&2; exit 1; }

# --- Snapshot the registry working tree (includes uncommitted assets) -------
# A plain clone of the live checkout would miss untracked/modified files, so
# copy the tree into a fresh local repo and serve that over file://.
mkdir -p "$REG_REPO"
cp -R "$REGISTRY_SRC/." "$REG_REPO/"
rm -rf "$REG_REPO/.git"
( cd "$REG_REPO" && git init -q -b main \
  && git add -A \
  && git -c user.name=smoke -c user.email=smoke@example.invalid commit -qm "smoke registry snapshot" )

cat > "$CONFIG" <<EOF
registry:
  url: file://$REG_REPO
  ref: main
opencode:
  dir: $OPENCODE_DIR
vault:
  dir: $VAULT
EOF
export ORBIT_CONFIG="$CONFIG"

# Run from an empty dir so project-scoped neocortex commands see no .neocortex/.
cd "$TMP"

# --- Assertion plumbing -----------------------------------------------------
PASS=0
FAIL=0
LAST_RC=0
LAST_OUT=""

green() { printf '\033[32m%s\033[0m' "$1"; }
red()   { printf '\033[31m%s\033[0m' "$1"; }

ok()   { PASS=$((PASS + 1)); printf '  [%s] %s\n' "$(green ok)" "$1"; }
bad()  { FAIL=$((FAIL + 1)); printf '  [%s] %s\n' "$(red FAIL)" "$1"; }

assert_rc()   { [ "$LAST_RC" = "$2" ] && ok "$1 (exit $LAST_RC)" || bad "$1: want exit $2, got $LAST_RC"; }
assert_file() { [ -f "$2" ] && ok "$1 ($2)" || bad "$1: missing file $2"; }
assert_dir()  { [ -d "$2" ] && ok "$1 ($2)" || bad "$1: missing dir $2"; }
assert_empty() { [ ! -s "$2" ] && ok "$1 (empty)" || bad "$1: expected empty $2"; }
assert_has()  { grep -qF -- "$2" "$3" 2>/dev/null && ok "$1" || bad "$1: '$2' not found in $3"; }
assert_out_has() { printf '%s' "$LAST_OUT" | grep -qF -- "$2" && ok "$1" || bad "$1: '$2' not in output"; }

# run <args...>: invoke the CLI, capture stdout/stderr/rc, echo the command.
run() {
  printf '\n$ orbit %s\n' "$*"
  local errf="$TMP/.stderr"
  set +e
  LAST_OUT="$("$ORBIT" "$@" 2>"$errf")"
  LAST_RC=$?
  set -e
  if [ -n "$LAST_OUT" ]; then printf '%s\n' "$LAST_OUT"; fi
  if [ -s "$errf" ]; then sed 's/^/  ! /' "$errf"; fi
  printf '  → exit %s\n' "$LAST_RC"
}

# json_field <key> <json>: first `"key": "value"` string value.
json_field() {
  printf '%s' "$2" | sed -n "s/.*\"$1\": \"\([^\"]*\)\".*/\1/p" | head -n1
}

# fill_shape <path>: give every shape heading non-comment content so `shape`
# passes its preflight. The CLI owns structure; this only fills agent space.
fill_shape() {
  local p="$1" name tmp
  tmp="$(mktemp)"
  cp "$p" "$tmp"
  for name in "Problem" "Solution Sketch"; do
    awk -v h="## $name" '{ print } $0 == h { print ""; print "Smoke-filled " h "." }' "$tmp" > "$tmp.2"
    mv "$tmp.2" "$tmp"
  done
  mv "$tmp" "$p"
}

# backlog_path <W-####>: resolve a work note in backlog/ by id prefix.
backlog_path() { printf '%s\n' "$VAULT"/Cycles/backlog/"$1"*.md; }

section() { printf '\n\033[1m== %s ==\033[0m\n' "$1"; }

# --- Forge-section helpers (verification tooling only) ----------------------
# assert_grep <label> <ERE> <file>: like assert_has but for a regular expression
# (used to match the milestone map's key/value regardless of YAML quoting).
assert_grep() { grep -qE -- "$2" "$3" 2>/dev/null && ok "$1" || bad "$1: pattern '$2' not in $3"; }

# json_num <key> <json>: first `"key": <int>` value (writeJSON indents one
# field per line; the string-only json_field above cannot read numbers).
json_num() { printf '%s' "$2" | sed -n "s/.*\"$1\": \([0-9][0-9]*\).*/\1/p" | head -n1; }

# log_count <fixed-string> <file>: number of matching lines in a stub's argv log
# (`|| true` keeps `set -e` happy when the count is zero).
log_count() { grep -cF -- "$1" "$2" 2>/dev/null || true; }

# run_no_forge <args...>: like run, but the CLI is handed a PATH holding git and
# no forge CLI, so a real gh/glab on the developer's machine can never mask the
# missing-CLI path. The PATH change is scoped to the orbit invocation only.
run_no_forge() {
  printf '\n$ orbit %s  [PATH without gh/glab]\n' "$*"
  local errf="$TMP/.stderr"
  set +e
  LAST_OUT="$(PATH="$FORGE_LESS_BIN" "$ORBIT" "$@" 2>"$errf")"
  LAST_RC=$?
  set -e
  if [ -n "$LAST_OUT" ]; then printf '%s\n' "$LAST_OUT"; fi
  if [ -s "$errf" ]; then sed 's/^/  ! /' "$errf"; fi
  printf '  → exit %s\n' "$LAST_RC"
}

echo "smoke: CLI      = $ORBIT"
echo "smoke: registry = $REGISTRY_SRC (snapshotted)"
echo "smoke: home     = $HOME"
echo "smoke: vault    = $VAULT"

# ---------------------------------------------------------------------------
section "install: bootstrap the vault + deploy the cycles assets"

run cycles install
assert_rc "cycles install" 0
assert_dir "vault Cycles/" "$VAULT/Cycles"
assert_dir "vault backlog/" "$VAULT/Cycles/backlog"
assert_dir "vault cycles/" "$VAULT/Cycles/cycles"
assert_file "vault CURRENT" "$VAULT/Cycles/CURRENT"
assert_empty "CURRENT is empty" "$VAULT/Cycles/CURRENT"
assert_file "vault CYCLES.md" "$VAULT/Cycles/CYCLES.md"
assert_file "cycles agent deployed" "$OPENCODE_DIR/agents/cycles.md"
assert_file "cycles:work command deployed" "$OPENCODE_DIR/commands/cycles:work.md"
assert_file "cycles:cycle command deployed" "$OPENCODE_DIR/commands/cycles:cycle.md"
assert_file "cycles:status command deployed" "$OPENCODE_DIR/commands/cycles:status.md"

# Idempotence: a second install is a no-op.
run cycles install
assert_rc "cycles install (idempotent)" 0

# ---------------------------------------------------------------------------
section "work new → shape → bet-without-cycle (state_conflict)"

run cycles work new "Voice search" --scope delijan-driver-app --json
assert_rc "work new" 0
w1="$(json_field id "$LAST_OUT")"
[ "$w1" = "W-0001" ] && ok "work new id is W-0001" || bad "work new id: want W-0001, got '$w1'"
w1_path="$(json_field path "$LAST_OUT")"
assert_file "W-0001 note on disk" "$w1_path"
assert_has "W-0001 starts Backlog" "status: Backlog" "$w1_path"

fill_shape "$w1_path"
run cycles work shape W-0001 --appetite small --json
assert_rc "work shape" 0
assert_out_has "W-0001 is Pitched" '"status": "Pitched"'

# No cycle open yet: bet must be a state_conflict (exit 7).
run cycles work bet W-0001
assert_rc "work bet without a cycle → 7" 7

# Malformed / unknown id contract.
run cycles work show W-X
assert_rc "malformed id → 6" 6
run cycles work show W-9999
assert_rc "unknown id → 5" 5

# Uninitialized vault contract (exit 11).
EMPTY_VAULT="$TMP/empty-vault"
run cycles --vault "$EMPTY_VAULT" work new "Nope" --scope app
assert_rc "uninitialized vault → 11" 11

# ---------------------------------------------------------------------------
section "cycle new → bet → deliver"

run cycles cycle new "Driver search hardening" --release 0.3.0 --json
assert_rc "cycle new" 0
c1="$(json_field id "$LAST_OUT")"
[ "$c1" = "C-0001" ] && ok "cycle id is C-0001" || bad "cycle id: want C-0001, got '$c1'"
assert_out_has "cycle is Open" '"status": "Open"'
assert_has "CURRENT points at C-0001" "C-0001" "$VAULT/Cycles/CURRENT"

run cycles work bet W-0001 --json
assert_rc "work bet" 0
assert_out_has "W-0001 is Bet" '"status": "Bet"'
assert_out_has "W-0001 cycle is C-0001" '"cycle": "C-0001"'
assert_file "W-0001 moved into the cycle" "$VAULT/Cycles/cycles/C-0001/W-0001 - voice-search.md"

run cycles work deliver W-0001 --json
assert_rc "work deliver" 0
assert_out_has "W-0001 is Delivered" '"status": "Delivered"'

# ---------------------------------------------------------------------------
section "second item → bet → cycle close auto-shelves it"

run cycles work new "Offline mode" --scope delijan-driver-app --json
assert_rc "work new (second)" 0
w2_path="$(json_field path "$LAST_OUT")"
fill_shape "$w2_path"
run cycles work shape W-0002 --appetite big --json
assert_rc "work shape (second)" 0
run cycles work bet W-0002 --json
assert_rc "work bet (second)" 0

run cycles cycle close --json
assert_rc "cycle close" 0
assert_out_has "cycle is Closed" '"status": "Closed"'
assert_empty "CURRENT cleared after close" "$VAULT/Cycles/CURRENT"
assert_has "cycle note is Closed" "status: Closed" "$VAULT/Cycles/cycles/C-0001/C-0001.md"

w2_backlog="$(backlog_path W-0002)"
assert_file "W-0002 auto-shelved to backlog/" "$w2_backlog"
assert_has "W-0002 is Shelved" "status: Shelved" "$w2_backlog"
assert_file "delivered W-0001 stays in the cycle" "$VAULT/Cycles/cycles/C-0001/W-0001 - voice-search.md"
assert_has "delivered W-0001 stays Delivered" "status: Delivered" "$VAULT/Cycles/cycles/C-0001/W-0001 - voice-search.md"

# ---------------------------------------------------------------------------
section "list / show / status (human + --json)"

run cycles work list --json
assert_rc "work list --json" 0
assert_out_has "list has delivered W-0001" '"id": "W-0001"'
assert_out_has "list has shelved W-0002" '"id": "W-0002"'

run cycles work show W-0001 --json
assert_rc "work show --json" 0
assert_out_has "show W-0001 Delivered" '"status": "Delivered"'

run cycles cycle list --json
assert_rc "cycle list --json" 0
assert_out_has "cycle list has C-0001" '"id": "C-0001"'

run cycles cycle show C-0001 --json
assert_rc "cycle show --json" 0
assert_out_has "cycle show C-0001 Closed" '"status": "Closed"'

run cycles status --json
assert_rc "status --json" 0
assert_out_has "no current cycle" '"current": null'
assert_out_has "shelved W-0002 on the board" '"id": "W-0002"'

run cycles status
assert_rc "status (human)" 0
assert_out_has "human status: no open cycle" "No open cycle."
assert_out_has "human status: Shelved block" "Shelved:"

# ---------------------------------------------------------------------------
section "orbit update: refresh BOTH domains + deploy cycles assets"

run update --yes --json
assert_rc "orbit update" 0
assert_out_has "update json command label" '"command": "update"'
assert_file "neocortex cache refreshed" "$HOME/.config/orbit/neocortex/cache/manifest.json"
assert_file "cycles cache refreshed" "$HOME/.config/orbit/cycles/cache/manifest.json"
assert_file "neocortex ledger" "$HOME/.config/orbit/neocortex/deployed.json"
assert_file "cycles ledger" "$HOME/.config/orbit/cycles/deployed.json"
assert_file "neocortex agent deployed" "$OPENCODE_DIR/agents/neocortex.md"
assert_file "cycles agent deployed by update" "$OPENCODE_DIR/agents/cycles.md"
assert_file "cycles:work command deployed by update" "$OPENCODE_DIR/commands/cycles:work.md"
assert_file "cycles:cycle command deployed by update" "$OPENCODE_DIR/commands/cycles:cycle.md"
assert_file "cycles:status command deployed by update" "$OPENCODE_DIR/commands/cycles:status.md"

# ---------------------------------------------------------------------------
section "no neocortex regression"

run neocortex --help
assert_rc "neocortex --help" 0
assert_out_has "neocortex help renders the verb tree" "Manage the NeoCortex development workflow."

run neocortex install --global-only --json
assert_rc "neocortex install --global-only" 0
assert_out_has "neocortex install json label unchanged" '"command": "neocortex install"'

# In a directory with no .neocortex/, `neocortex which` is still no_active_run.
run neocortex which
assert_rc "neocortex which (no project) → 9" 9

# ---------------------------------------------------------------------------
section "forge sync: stub gh/glab, full work/cycle sync flow"

# --- Stub the forge CLIs ----------------------------------------------------
# STUB_BIN sits FIRST on PATH so the stubs shadow any real gh/glab; earlier
# sections never invoked a forge CLI and are unaffected. FORGE_LESS_BIN holds
# git alone, so the missing-CLI path is reachable even on a machine that has a
# real gh/glab installed.
STUB_BIN="$TMP/stub-bin"
FORGE_LESS_BIN="$TMP/forge-less-bin"
mkdir -p "$STUB_BIN" "$FORGE_LESS_BIN"
ln -sf "$(command -v git)" "$FORGE_LESS_BIN/git"
export PATH="$STUB_BIN:$PATH"

cat > "$STUB_BIN/gh" <<'GH_STUB'
#!/usr/bin/env bash
# Stub gh for the cycles forge smoke: logs argv, allocates issue/milestone
# numbers from counter files, and prints the URL/JSON the github wrapper parses.
bin="$(cd "$(dirname "$0")" && pwd)"
log="$bin/gh.log"
printf '%s\n' "$*" >> "$log"
next() { # next <counter-file> -> prints the incremented counter
  local f="$bin/$1" n
  n=$(( $(cat "$f" 2>/dev/null || echo 0) + 1 ))
  printf '%s\n' "$n" > "$f"
  printf '%s' "$n"
}
case "$1 $2" in
  "auth status")
    if [ "${GH_STUB_UNAUTH:-0}" = "1" ]; then
      echo "gh: not logged into any GitHub hosts" >&2
      exit 1
    fi
    echo "Logged in to github.com as smoke"
    exit 0
    ;;
  "issue create")
    n="$(next gh.issue-counter)"
    url="https://github.com/RTwoStudio/orbit/issues/$n"
    printf 'created %s\n' "$url" >> "$log"
    echo "$url"
    exit 0
    ;;
  "issue edit")
    exit 0
    ;;
  "api "*)
    n="$(next gh.ms-counter)"
    title=""
    for a in "$@"; do case "$a" in title=*) title="${a#title=}" ;; esac; done
    printf '{"number":%s,"title":"%s","html_url":"https://github.com/RTwoStudio/orbit/milestone/%s"}\n' "$n" "$title" "$n"
    exit 0
    ;;
esac
echo "gh stub: unexpected args: $*" >&2
exit 1
GH_STUB
chmod +x "$STUB_BIN/gh"

cat > "$STUB_BIN/glab" <<'GLAB_STUB'
#!/usr/bin/env bash
# Stub glab for the cycles forge smoke: logs argv, allocates issue/milestone
# iids, and prints the URL/JSON the gitlab wrapper parses.
bin="$(cd "$(dirname "$0")" && pwd)"
log="$bin/glab.log"
printf '%s\n' "$*" >> "$log"
next() {
  local f="$bin/$1" n
  n=$(( $(cat "$f" 2>/dev/null || echo 0) + 1 ))
  printf '%s\n' "$n" > "$f"
  printf '%s' "$n"
}
case "$1 $2" in
  "auth status")
    if [ "${GLAB_STUB_UNAUTH:-0}" = "1" ]; then
      echo "glab: not authenticated" >&2
      exit 1
    fi
    echo "Logged in to gitlab.com as smoke"
    exit 0
    ;;
  "issue create")
    n="$(next glab.issue-counter)"
    url="https://gitlab.com/rtwo/orbit/-/issues/$n"
    printf 'created %s\n' "$url" >> "$log"
    echo "$url"
    exit 0
    ;;
  "issue update")
    exit 0
    ;;
  "api "*)
    n="$(next glab.ms-counter)"
    title=""
    for a in "$@"; do case "$a" in title=*) title="${a#title=}" ;; esac; done
    printf '{"iid":%s,"title":"%s","web_url":"https://gitlab.com/rtwo/orbit/-/milestones/%s"}\n' "$n" "$title" "$n"
    exit 0
    ;;
esac
echo "glab stub: unexpected args: $*" >&2
exit 1
GLAB_STUB
chmod +x "$STUB_BIN/glab"

# --- Offline temp git projects (forge.Resolve reads git config only) --------
mkdir -p "$TMP/proj-gh" "$TMP/proj-gl"
( cd "$TMP/proj-gh" && git -c init.defaultBranch=main init -q \
  && git remote add origin https://github.com/RTwoStudio/orbit.git )
( cd "$TMP/proj-gl" && git -c init.defaultBranch=main init -q \
  && git remote add origin https://gitlab.com/rtwo/orbit.git )
assert_dir "temp proj-gh created" "$TMP/proj-gh/.git"
assert_dir "temp proj-gl created" "$TMP/proj-gl/.git"

# --- github: cycle sync happy path + idempotency + work sync ---------------
run cycles cycle new "Forge github sync" --release 0.4.0 --json
assert_rc "cycle new (forge gh)" 0
c2="$(json_field id "$LAST_OUT")"
g2="$(json_field goal "$LAST_OUT")"
c2_note="$VAULT/Cycles/cycles/$c2/$c2.md"

run cycles work new "Publish the beta" --scope forge-app --json
assert_rc "work new (forge gh)" 0
w3="$(json_field id "$LAST_OUT")"
w3_backlog="$(json_field path "$LAST_OUT")"
fill_shape "$w3_backlog"
run cycles work shape "$w3" --appetite small --json
assert_rc "work shape (forge gh)" 0
run cycles work bet "$w3" --json
assert_rc "work bet (forge gh)" 0
w3_path="$(json_field path "$LAST_OUT")"
assert_file "forge gh bet sits in the cycle" "$w3_path"
sed -i "s|^project:.*|project: $TMP/proj-gh|" "$w3_path"
assert_has "forge gh bet seeded with project" "project: $TMP/proj-gh" "$w3_path"

# Degradation (missing CLI): no milestone is recorded yet, so cycle sync must
# probe gh and fail with the PATH hint (exit 1).
run_no_forge cycles cycle sync
assert_rc "cycle sync without gh → 1" 1
assert_has "cycle sync missing-CLI hint" "not found on PATH" "$TMP/.stderr"

# Happy path: --json creates exactly one github milestone.
run cycles cycle sync --json
assert_rc "cycle sync --json (github)" 0
assert_out_has "cycle sync json provider github" '"provider": "github"'
assert_out_has "cycle sync json repo" '"repo": "RTwoStudio/orbit"'
assert_out_has "cycle sync json action created" '"action": "created"'
gh_ms="$(json_num number "$LAST_OUT")"
[ -n "$gh_ms" ] && [ "$gh_ms" -gt 0 ] \
  && ok "cycle sync recorded milestone #$gh_ms" \
  || bad "cycle sync milestone number not found in $LAST_OUT"
assert_has "cycle records project scalar" "project: $TMP/proj-gh" "$c2_note"
assert_has "cycle records repo scalar" "repo: RTwoStudio/orbit" "$c2_note"
assert_has "cycle records provider scalar" "provider: github" "$c2_note"
assert_grep "cycle records milestone map entry" "RTwoStudio/orbit: \"?$gh_ms\"?" "$c2_note"
assert_grep "gh milestone gets an RFC3339 due_on" "due_on=[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9:]+Z" "$STUB_BIN/gh.log"

# Idempotency: a re-sync reports unchanged, keeps the number, and never
# creates a second milestone.
ms_log_after_create="$(log_count "api repos/RTwoStudio/orbit/milestones" "$STUB_BIN/gh.log")"
run cycles cycle sync --json
assert_rc "cycle sync --json (github, re-run)" 0
assert_out_has "cycle re-sync action unchanged" '"action": "unchanged"'
assert_out_has "cycle re-sync keeps the number" "\"number\": $gh_ms"
[ "$(log_count "api repos/RTwoStudio/orbit/milestones" "$STUB_BIN/gh.log")" = "$ms_log_after_create" ] \
  && ok "no second milestone create in gh.log" || bad "a second milestone create reached gh.log"

# Human render: header + one milestone row.
run cycles cycle sync
assert_rc "cycle sync (human)" 0
assert_out_has "cycle human header" "Synced cycle $c2 — $g2"
assert_out_has "cycle human row" "RTwoStudio/orbit#$gh_ms (github)"

# work sync: JSON creates, a re-run updates in place, the human line matches.
run cycles work sync "$w3" --json
assert_rc "work sync --json (github)" 0
assert_out_has "work sync json action created" '"action": "created"'
assert_out_has "work sync json provider github" '"provider": "github"'
assert_out_has "work sync json repo" '"repo": "RTwoStudio/orbit"'
assert_out_has "work sync json milestone" "\"milestone\": $gh_ms"
gh_issue="$(json_num issue "$LAST_OUT")"
[ -n "$gh_issue" ] && [ "$gh_issue" -gt 0 ] \
  && ok "work sync recorded issue #$gh_issue" \
  || bad "work sync issue number not found in $LAST_OUT"
assert_has "work note records provider" "provider: github" "$w3_path"
assert_has "work note records repo" "repo: RTwoStudio/orbit" "$w3_path"
assert_has "work note records issue" "issue: $gh_issue" "$w3_path"
assert_has "work note records milestone" "milestone: $gh_ms" "$w3_path"

gh_create_1="$(log_count 'issue create' "$STUB_BIN/gh.log")"
run cycles work sync "$w3" --json
assert_rc "work sync --json (github, re-run)" 0
assert_out_has "work re-sync action updated" '"action": "updated"'
assert_out_has "work re-sync keeps the issue" "\"issue\": $gh_issue"
[ "$(log_count 'issue create' "$STUB_BIN/gh.log")" = "$gh_create_1" ] \
  && ok "no second issue create in gh.log" || bad "a second issue create reached gh.log"
assert_has "re-sync edited/assigned via gh" "issue edit" "$STUB_BIN/gh.log"

run cycles work sync "$w3"
assert_rc "work sync (human)" 0
assert_out_has "work sync human line" "$w3 → issue #$gh_issue (updated), milestone #$gh_ms (RTwoStudio/orbit, github)"

# A bet with no project: is skipped by cycle sync and contributes no milestone.
run cycles work new "Unscoped bet" --scope forge-app --json
assert_rc "work new (unscoped)" 0
w4="$(json_field id "$LAST_OUT")"
w4_backlog="$(json_field path "$LAST_OUT")"
fill_shape "$w4_backlog"
run cycles work shape "$w4" --appetite small --json
assert_rc "work shape (unscoped)" 0
run cycles work bet "$w4" --json
assert_rc "work bet (unscoped)" 0
w4_path="$(json_field path "$LAST_OUT")"
assert_grep "unscoped bet has an empty project:" "^project:$" "$w4_path"

ms_log_before_skip="$(log_count "api repos/RTwoStudio/orbit/milestones" "$STUB_BIN/gh.log")"
run cycles cycle sync --json
assert_rc "cycle sync with a no-project bet" 0
assert_out_has "skip reports the bet id" "\"id\": \"$w4\""
assert_out_has "skip reports the reason" '"reason": "no project: set"'
[ "$(log_count "api repos/RTwoStudio/orbit/milestones" "$STUB_BIN/gh.log")" = "$ms_log_before_skip" ] \
  && ok "skipped bet mints no milestone" || bad "skipped bet minted a milestone"

# work sync --project persists the override and reuses the recorded milestone.
run cycles work sync "$w4" --project "$TMP/proj-gh" --json
assert_rc "work sync --project override" 0
assert_out_has "override action created" '"action": "created"'
assert_out_has "override reuses the recorded milestone" "\"milestone\": $gh_ms"
assert_has "override persists project: into the note" "project: $TMP/proj-gh" "$w4_path"

# Degradation: missing CLI (git-only PATH) and unauthenticated stub.
run_no_forge cycles work sync "$w3"
assert_rc "work sync without gh → 1" 1
assert_has "work sync missing-CLI hint" "not found on PATH" "$TMP/.stderr"

export GH_STUB_UNAUTH=1
run cycles work sync "$w3"
unset GH_STUB_UNAUTH
assert_rc "work sync unauthenticated gh → 1" 1
assert_has "work sync unauth hint" "not authenticated" "$TMP/.stderr"

# --- gitlab: the same flow through glab ------------------------------------
run cycles cycle close --json
assert_rc "cycle close (forge gh)" 0

run cycles cycle new "Forge gitlab sync" --release 0.5.0 --json
assert_rc "cycle new (forge glab)" 0
c3="$(json_field id "$LAST_OUT")"
g3="$(json_field goal "$LAST_OUT")"
c3_note="$VAULT/Cycles/cycles/$c3/$c3.md"

run cycles work new "Ship to gitlab" --scope forge-app --json
assert_rc "work new (forge glab)" 0
w5="$(json_field id "$LAST_OUT")"
w5_backlog="$(json_field path "$LAST_OUT")"
fill_shape "$w5_backlog"
run cycles work shape "$w5" --appetite small --json
assert_rc "work shape (forge glab)" 0
run cycles work bet "$w5" --json
assert_rc "work bet (forge glab)" 0
w5_path="$(json_field path "$LAST_OUT")"
sed -i "s|^project:.*|project: $TMP/proj-gl|" "$w5_path"
assert_has "forge glab bet seeded with project" "project: $TMP/proj-gl" "$w5_path"

# The human run creates (header + a created row); --json re-runs unchanged and
# proves the recorded key + the URL-escaped glab endpoint.
run cycles cycle sync
assert_rc "cycle sync (gitlab, human)" 0
assert_out_has "gitlab cycle header" "Synced cycle $c3 — $g3"
assert_out_has "gitlab created row" "created"
assert_out_has "gitlab milestone row" "rtwo/orbit#"
assert_out_has "gitlab provider row" "(gitlab)"

run cycles cycle sync --json
assert_rc "cycle sync --json (gitlab, unchanged)" 0
assert_out_has "glab milestone provider" '"provider": "gitlab"'
assert_out_has "glab milestone repo" '"repo": "rtwo/orbit"'
gl_ms="$(json_num number "$LAST_OUT")"
[ -n "$gl_ms" ] && [ "$gl_ms" -gt 0 ] \
  && ok "glab cycle recorded milestone #$gl_ms" || bad "glab milestone number not found"
assert_has "gitlab cycle provider" "provider: gitlab" "$c3_note"
assert_has "gitlab cycle repo" "repo: rtwo/orbit" "$c3_note"
assert_grep "gitlab milestone map entry" "rtwo/orbit: \"?$gl_ms\"?" "$c3_note"
assert_has "glab saw the URL-escaped endpoint" "projects/rtwo%2Forbit/milestones" "$STUB_BIN/glab.log"
assert_grep "glab milestone gets a bare due_date" "due_date=[0-9]{4}-[0-9]{2}-[0-9]{2}$" "$STUB_BIN/glab.log"

# work sync through glab: human creates, --json re-runs updated.
run cycles work sync "$w5"
assert_rc "work sync (gitlab, human)" 0
assert_out_has "glab work human line" "$w5 → issue #"

run cycles work sync "$w5" --json
assert_rc "work sync --json (gitlab, updated)" 0
assert_out_has "glab work action updated" '"action": "updated"'
assert_out_has "glab work provider" '"provider": "gitlab"'
assert_out_has "glab work repo" '"repo": "rtwo/orbit"'
assert_out_has "glab work milestone" "\"milestone\": $gl_ms"
gl_issue="$(json_num issue "$LAST_OUT")"
[ -n "$gl_issue" ] && [ "$gl_issue" -gt 0 ] \
  && ok "glab work recorded issue #$gl_issue" || bad "glab issue number not found"
assert_has "work note records gitlab provider" "provider: gitlab" "$w5_path"
assert_has "work note records gitlab issue" "issue: $gl_issue" "$w5_path"
assert_has "glab issue URL uses the /-/issues/ form" "https://gitlab.com/rtwo/orbit/-/issues/$gl_issue" "$STUB_BIN/glab.log"

# ---------------------------------------------------------------------------
section "summary"
printf '\n  assertions: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -ne 0 ]; then
  echo "  RESULT: FAIL"
  exit 1
fi
echo "  RESULT: PASS"
