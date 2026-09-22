#!/usr/bin/env bash
# Regenerates testdata/registry-fixture/neocortex/manifest.json from the
# fixture files (mirrors the registry repo's `make manifest`).
set -euo pipefail
cd "$(dirname "$0")/.."
FIXTURE="testdata/registry-fixture/neocortex"

hash_entry() {
  local path="$1"
  printf '    { "path": "%s", "sha256": "%s" }' "$path" "$(sha256sum "$FIXTURE/$path" | cut -d' ' -f1)"
}

{
  printf '{\n  "version": "%s",\n  "layout": 1,\n  "files": {\n' "${1:-0.1.0}"
  printf '    "root": [\n'
  hash_entry "NEOCORTEX.md"
  printf '\n    ],\n'
  printf '    "stubs": [\n'
  first=1
  for f in 00-concept.stub.md 01-plan.stub.md addenda.stub.md task.stub.md; do
    [ $first -eq 0 ] && printf ',\n'
    hash_entry "stubs/$f"
    first=0
  done
  printf '\n    ],\n'
  printf '    "opencode": {\n      "agents": [\n'
  first=1
  for f in neocortex.md neocortex-planner.md neocortex-task-creator.md neocortex-implementer.md; do
    [ $first -eq 0 ] && printf ',\n'
    hash_entry "opencode/agents/$f"
    first=0
  done
  printf '\n      ],\n      "commands": [\n'
  first=1
  for f in issue.md plan.md new-task.md implement.md addenda.md close.md; do
    [ $first -eq 0 ] && printf ',\n'
    hash_entry "opencode/commands/$f"
    first=0
  done
  printf '\n      ]\n    }\n  }\n}\n'
} > "$FIXTURE/manifest.json"
echo "manifest written: $FIXTURE/manifest.json"
