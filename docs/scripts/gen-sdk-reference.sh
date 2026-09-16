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

# Pin the repository gomarkdoc links symbols against. Without these it infers
# them from the checkout, and a shallow detached CI checkout cannot resolve a
# default branch -- so CI emitted link-free headings while any full local clone
# emitted linked ones. The committed pages then drifted from whichever
# environment last regenerated them, and the drift check was unwinnable in the
# other. Pinning makes the output identical everywhere.
REPO_URL="https://github.com/AltairaLabs/PromptKit"

# module | pkg | filename | title | sidebar-order
MAP=(
  "sdk|.|conversation-manager|Conversation|2"
  "sdk|./agui|ag-ui|AG-UI Integration|6"
  "server/a2a|.|a2a-server|A2A Server|7"
)

mkdir -p "$OUT"
for row in "${MAP[@]}"; do
  IFS='|' read -r module pkg file title order <<<"$row"
  go -C "$ROOT/$module" run "$GOMARKDOC" \
    --repository.url "$REPO_URL" \
    --repository.default-branch "main" \
    --repository.path "/$module" \
    --output "$TMP/$file.md" "$pkg"
  {
    printf -- '---\ntitle: %s\nsidebar:\n  order: %s\n---\n' "$title" "$order"
    cat "$TMP/$file.md"
  } > "$OUT/$file.md"
  echo "generated $file.md from $module:$pkg"
done
