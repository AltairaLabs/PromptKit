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

# Pin the repository gomarkdoc links symbols against. Without these it infers
# them from the checkout, and a shallow detached CI checkout cannot resolve a
# default branch -- so CI emitted link-free headings while any full local clone
# emitted linked ones. The committed pages then drifted from whichever
# environment last regenerated them, and the drift check was unwinnable in the
# other. Pinning makes the output identical everywhere.
REPO_FLAGS=(
  --repository.url "https://github.com/AltairaLabs/PromptKit"
  --repository.default-branch "main"
  --repository.path "/runtime"
)

# pkg | filename | title | sidebar-order
MAP=(
  "./types|types|Types|7"
  "./providers|providers|Providers|3"
  "./tools|tools|Tools|9"
  "./mcp|mcp|MCP|10"
  "./hooks|hooks|Hooks|5"
  "./evals|evals|Evals|20"
  "./statestore|statestore|State Store|6"
  "./storage|storage|Storage|11"
  "./a2a|a2a|A2A|4"
  "./metrics|metrics|Metrics|12"
  "./telemetry|telemetry|Telemetry|13"
  "./logger|logging|Logging|8"
  "./pipeline/stage|pipeline|Pipeline|2"
  "./streaming|streaming|Streaming|14"
  "./tts|tts|TTS|15"
  "./audio|audio|Audio|16"
  "./variables|variables|Variables|17"
  "./memory|memory|Memory|18"
  "./memory/corpus|memory-corpus|Memory Corpus|19"
)

mkdir -p "$OUT"
for row in "${MAP[@]}"; do
  IFS='|' read -r pkg file title order <<<"$row"
  go -C "$ROOT/runtime" run "$GOMARKDOC" "${REPO_FLAGS[@]}" --output "$TMP/$file.md" "$pkg"
  {
    printf -- '---\ntitle: %s\nsidebar:\n  order: %s\n---\n' "$title" "$order"
    cat "$TMP/$file.md"
  } > "$OUT/$file.md"
  echo "generated $file.md from $pkg"
done
