#!/usr/bin/env bash
# Generate per-package runtime reference docs from Go source via gomarkdoc.
# Frontmatter is prepended (Astro needs it first); gomarkdoc's banner follows.
# Usage: gen-reference.sh [OUT_DIR]
set -euo pipefail

ROOT="$(git rev-parse --show-toplevel)"
OUT="${1:-$ROOT/docs/src/content/docs/runtime/reference}"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
GOMARKDOC="github.com/princjef/gomarkdoc/cmd/gomarkdoc@v1.1.0"

# pkg | filename | title | sidebar-order
MAP=(
  "./types|types|Types|7"
  "./providers|providers|Providers|3"
  "./tools|tools|Tools|9"
  "./mcp|mcp|MCP|10"
  "./hooks|hooks|Hooks|5"
  "./statestore|statestore|State Store|6"
  "./storage|storage|Storage|11"
  "./a2a|a2a|A2A|4"
  "./metrics|metrics|Metrics|12"
  "./telemetry|telemetry|Telemetry|13"
  "./logger|logging|Logging|8"
  "./pipeline/stage|pipeline|Pipeline|2"
)

mkdir -p "$OUT"
for row in "${MAP[@]}"; do
  IFS='|' read -r pkg file title order <<<"$row"
  go -C "$ROOT/runtime" run "$GOMARKDOC" --output "$TMP/$file.md" "$pkg"
  {
    printf -- '---\ntitle: %s\nsidebar:\n  order: %s\n---\n' "$title" "$order"
    cat "$TMP/$file.md"
  } > "$OUT/$file.md"
  echo "generated $file.md from $pkg"
done
