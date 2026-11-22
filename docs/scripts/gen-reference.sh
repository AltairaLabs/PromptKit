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

# Facade packages: their page also renders the types they re-export as aliases
# of their internal/ packages' types, with those types' fields and methods
# (splice-aliases.py). pipeline/stage is split into stage/internal/<name>
# behind aliases (#2876); gomarkdoc alone would show each moved type as its
# one-line `type X = core.X`.
SPLICE=("./pipeline/stage")

# internal_dirs <pkg> prints, as ./-relative dirs gomarkdoc accepts, the
# packages under <pkg>/internal/ that <pkg> imports: the ones its aliases can
# name. splice-aliases.py fails rather than guess when two share a name.
internal_dirs() {
  local mod
  # The package's own module: `go list -m` would print every module of the
  # workspace (go.work), and the pattern below would then match nothing.
  mod="$(go -C "$ROOT/runtime" list -f '{{.Module.Path}}' "$1")"
  go -C "$ROOT/runtime" list -f '{{join .Imports "\n"}}' "$1" |
    { grep "^$mod/${1#./}/internal/" || true; } | sed "s#^$mod/#./#"
}

mkdir -p "$OUT"
for row in "${MAP[@]}"; do
  IFS='|' read -r pkg file title order <<<"$row"
  go -C "$ROOT/runtime" run "$GOMARKDOC" "${REPO_FLAGS[@]}" --output "$TMP/$file.md" "$pkg"
  if [[ " ${SPLICE[*]} " == *" $pkg "* ]]; then
    pages=()
    for dir in $(internal_dirs "$pkg"); do
      page="$TMP/internal-${#pages[@]}.md"
      go -C "$ROOT/runtime" run "$GOMARKDOC" "${REPO_FLAGS[@]}" --output "$page" "$dir"
      pages+=("$page")
    done
    if [ ${#pages[@]} -gt 0 ]; then
      python3 -I "$ROOT/docs/scripts/splice-aliases.py" "$TMP/$file.md" "${pages[@]}" > "$TMP/$file.spliced.md"
      mv "$TMP/$file.spliced.md" "$TMP/$file.md"
    fi
  fi
  {
    printf -- '---\ntitle: %s\nsidebar:\n  order: %s\n---\n' "$title" "$order"
    cat "$TMP/$file.md"
  } > "$OUT/$file.md"
  echo "generated $file.md from $pkg"
done
