#!/usr/bin/env bash
# orbit installer — installs or updates the orbit CLI from GitHub Releases.
#
#   curl -fsSL https://orbit.rtwostudio.ir/install.sh | bash
#
# - No orbit binary present  -> installs the latest release
# - Older binary present     -> updates it to the latest release
# - Same/newer binary        -> leaves it untouched
#
# Env overrides (all optional):
#   ORBIT_GH_REPO    GitHub repo            (default RTwoStudio/orbit)
#   ORBIT_GH_API     API base               (default https://api.github.com)
#   ORBIT_GH_DL      download base          (default https://github.com)
#   ORBIT_BIN_DIR    install dir            (default ~/.local/bin)
set -euo pipefail

GH_REPO="${ORBIT_GH_REPO:-RTwoStudio/orbit}"
GH_API="${ORBIT_GH_API:-https://api.github.com}"
GH_DL="${ORBIT_GH_DL:-https://github.com}"
BIN_DIR="${ORBIT_BIN_DIR:-$HOME/.local/bin}"
CONFIG_DIR="$HOME/.config/orbit"

log() { printf '%s\n' "$*" >&2; }
die() { log "error: $*"; exit 1; }

# --- platform detection -------------------------------------------------
os="$(uname -s)"
case "$os" in
  Linux)  os=linux  ;;
  Darwin) os=darwin ;;
  *) die "unsupported OS: $os (supported: linux, darwin)" ;;
esac
arch="$(uname -m)"
case "$arch" in
  x86_64|amd64)   arch=amd64 ;;
  aarch64|arm64)  arch=arm64 ;;
  *) die "unsupported architecture: $arch (supported: amd64, arm64)" ;;
esac

# --- helpers ------------------------------------------------------------
semver_of() { # "orbit version v0.1.0" -> "0.1.0"
  printf '%s' "$1" | sed -E 's/.*v?([0-9]+\.[0-9]+\.[0-9]+).*/\1/'
}

gt() { # gt X Y  ->  true when semver X > Y
  local IFS=.
  local a=(${1:-0}) b=(${2:-0}) i
  for i in 0 1 2; do
    local x="${a[i]:-0}" y="${b[i]:-0}"
    [ "$x" -gt "$y" ] && return 0
    [ "$x" -lt "$y" ] && return 1
  done
  return 1
}

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

# --- current install? ----------------------------------------------------
current=""
if command -v orbit >/dev/null 2>&1; then
  current="$(semver_of "$(orbit --version 2>/dev/null || true)")"
elif [ -x "$BIN_DIR/orbit" ]; then
  current="$(semver_of "$("$BIN_DIR/orbit" --version 2>/dev/null || true)")"
fi

# --- latest release ------------------------------------------------------
log "resolving latest release of $GH_REPO..."
json="$(curl -fsSL "$GH_API/repos/$GH_REPO/releases/latest")" \
  || die "cannot reach GitHub API — check your network"
latest="$(printf '%s' "$json" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1)"
[ -n "$latest" ] || die "could not determine the latest release tag"

if [ -n "$current" ]; then
  if [ "$current" = "${latest#v}" ]; then
    log "orbit $current is already the latest version — nothing to do"
    exit 0
  fi
  if gt "$current" "${latest#v}"; then
    log "orbit $current is newer than latest release $latest — leaving untouched"
    exit 0
  fi
  log "updating orbit $current -> $latest"
else
  log "installing orbit $latest"
fi

# --- download + verify ---------------------------------------------------
tag="$latest"
asset="orbit_${tag}_${os}_${arch}.tar.gz"
url="$GH_DL/$GH_REPO/releases/download/$tag/$asset"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

log "downloading $asset"
curl -fsSL "$url" -o "$tmp/$asset" || die "download failed: $url"

if curl -fsSL "$GH_DL/$GH_REPO/releases/download/$tag/checksums.txt" -o "$tmp/checksums.txt" 2>/dev/null && [ -s "$tmp/checksums.txt" ]; then
  want="$(awk -v f="$asset" '$2 == f {print $1}' "$tmp/checksums.txt")"
  if [ -n "$want" ]; then
    have="$(sha256_of "$tmp/$asset")"
    [ "$have" = "$want" ] || die "checksum mismatch for $asset (want $want, got $have)"
    log "checksum ok"
  fi
fi

# --- install -------------------------------------------------------------
mkdir -p "$BIN_DIR"
tar -xzf "$tmp/$asset" -C "$tmp"
[ -f "$tmp/orbit_${tag}_${os}_${arch}/orbit" ] || die "unexpected archive layout in $asset"
chmod 0755 "$tmp/orbit_${tag}_${os}_${arch}/orbit"
if [ -w "$BIN_DIR" ]; then
  mv "$tmp/orbit_${tag}_${os}_${arch}/orbit" "$BIN_DIR/orbit"
else
  log "$BIN_DIR is not writable — installing with sudo"
  sudo mv "$tmp/orbit_${tag}_${os}_${arch}/orbit" "$BIN_DIR/orbit"
fi

# --- seed config (never overwrite) --------------------------------------
if [ ! -f "$CONFIG_DIR/config.yml" ] && [ ! -f "$CONFIG_DIR/config.json" ]; then
  mkdir -p "$CONFIG_DIR"
  cat > "$CONFIG_DIR/config.json" <<'EOF'
{
  "registry": { "url": "https://github.com/RTwoStudio/orbit-registry.git", "ref": "main" },
  "opencode": { "dir": "~/.config/opencode" },
  "neocortex": { "dir": ".neocortex" }
}
EOF
  log "seeded $CONFIG_DIR/config.json"
fi

# --- done ----------------------------------------------------------------
case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) log "note: add to PATH —  export PATH=\"$BIN_DIR:\$PATH\"" ;;
esac
log "orbit ${latest#v} installed at $BIN_DIR/orbit"
log "next: orbit neocortex install"
