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
  for name in "Problem" "Solution Sketch" "Rabbit Holes" "No-gos"; do
    awk -v h="## $name" '{ print } $0 == h { print ""; print "Smoke-filled " h "." }' "$tmp" > "$tmp.2"
    mv "$tmp.2" "$tmp"
  done
  mv "$tmp" "$p"
}

# backlog_path <W-####>: resolve a work note in backlog/ by id prefix.
backlog_path() { printf '%s\n' "$VAULT"/Cycles/backlog/"$1"*.md; }

section() { printf '\n\033[1m== %s ==\033[0m\n' "$1"; }

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
section "summary"
printf '\n  assertions: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -ne 0 ]; then
  echo "  RESULT: FAIL"
  exit 1
fi
echo "  RESULT: PASS"
