#!/usr/bin/env bash
# Generate the SDK-owned reference docs from Go source via gomarkdoc.
# Unlike the runtime reference (single module), these packages live in
# different modules, so gomarkdoc must be invoked from each module's dir.
# Frontmatter is prepended (Astro needs it first); gomarkdoc's banner follows.
# Usage: gen-sdk-reference.sh [OUT_DIR]
set -euo pipefail

ROOT="$(git rev-parse --show-toplevel)"
OUT="${1:-$ROOT/docs/src/content/docs/sdk/reference}"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
GOMARKDOC="github.com/princjef/gomarkdoc/cmd/gomarkdoc@v1.1.0"

# module | pkg | filename | title | sidebar-order
MAP=(
  "sdk|.|conversation-manager|Conversation|2"
  "sdk|./agui|ag-ui|AG-UI Integration|8"
  "server/a2a|.|a2a-server|A2A Server|7"
)

mkdir -p "$OUT"
for row in "${MAP[@]}"; do
  IFS='|' read -r module pkg file title order <<<"$row"
  go -C "$ROOT/$module" run "$GOMARKDOC" --output "$TMP/$file.md" "$pkg"
  {
    printf -- '---\ntitle: %s\nsidebar:\n  order: %s\n---\n' "$title" "$order"
    cat "$TMP/$file.md"
  } > "$OUT/$file.md"
  echo "generated $file.md from $module:$pkg"
done
