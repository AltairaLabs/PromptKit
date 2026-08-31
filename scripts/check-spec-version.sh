#!/usr/bin/env bash
#
# Assert that every PromptPack spec version PromptKit CLAIMS matches the one it
# actually embeds.
#
# PromptKit stated its spec version nowhere at all until this check existed —
# a consumer asking "which PromptPack version does this runtime implement?" had
# to open runtime/prompt/schema/promptpack.schema.json and read the version
# field. It also stated it in four places that had gone five versions stale,
# claiming v1.1.0 against an embedded v1.6.0 — the same failure in the other
# direction: not silence, but a confident wrong answer.
#
# So the embedded schema is the single source of truth. It is a byte-for-byte
# mirror of the published release and cannot be hand-edited (see
# runtime/prompt/schema/schema.go), so every claim follows it, never the
# reverse. --write updates the claims; the sync workflow calls that, so a spec
# bump carries its own documentation.
#
# Mirrors promptpack-spec's scripts/check-version-consistency.mjs, which exists
# for the same reason: RFC-0009 was marked Implemented while its schema fields
# were never added, and no automated check caught it.
#
set -euo pipefail

WRITE=false
if [ "${1:-}" = "--write" ]; then
  WRITE=true
fi

SCHEMA="runtime/prompt/schema/promptpack.schema.json"

# Files stating the CURRENT version, as "<path>:<pattern-kind>".
#
# CHANGELOG.md and docs/local-backlog/ are deliberately absent: their version
# references are historical records that were true when written, and rewriting
# them would falsify the history rather than fix a claim.
#
# docs/src/content/docs/api/sdk.md is absent because it is generated and
# gitignored; it inherits the claim from sdk/doc.go at build time.
CLAIMS=(
  "README.md:badge"
  "sdk/doc.go:prose"
  "sdk/README.md:prose"
  "docs/src/content/docs/sdk/reference/conversation-manager.md:prose"
)

embedded="$(python3 -c "import json;print(json.load(open('$SCHEMA'))['version'])")"
if [ -z "$embedded" ]; then
  echo "::error::$SCHEMA has no top-level version field"
  exit 1
fi

# pattern_for prints the grep pattern matching a claim of that kind.
pattern_for() {
  case "$1" in
    badge) printf 'PromptPack%%20Spec-v[0-9]+\.[0-9]+\.[0-9]+-' ;;
    prose) printf 'PromptPack Specification v[0-9]+\.[0-9]+\.[0-9]+' ;;
    *) echo "::error::unknown claim kind $1" >&2; exit 1 ;;
  esac
}

failed=0
wrote=0

for claim in "${CLAIMS[@]}"; do
  file="${claim%%:*}"
  kind="${claim##*:}"
  pattern="$(pattern_for "$kind")"

  if [ ! -f "$file" ]; then
    echo "::error::$file is listed as stating the spec version but does not exist"
    failed=1
    continue
  fi

  found="$(grep -oE "$pattern" "$file" | head -1 |
           grep -oE '[0-9]+\.[0-9]+\.[0-9]+' || true)"

  if [ -z "$found" ]; then
    echo "::error::$file no longer states the PromptPack spec version."
    echo "::error::Expected text matching: $pattern"
    echo "::error::Either restore the claim, or drop $file from CLAIMS in $0."
    failed=1
    continue
  fi

  if [ "$found" = "$embedded" ]; then
    continue
  fi

  if [ "$WRITE" = true ]; then
    python3 - "$file" "$found" "$embedded" <<'PY'
import sys
path, old, new = sys.argv[1], sys.argv[2], sys.argv[3]
s = open(path).read()
s = s.replace(f"PromptPack%20Spec-v{old}-", f"PromptPack%20Spec-v{new}-")
s = s.replace(f"PromptPack Specification v{old}", f"PromptPack Specification v{new}")
open(path, "w").write(s)
PY
    echo "✓ $file: v$found -> v$embedded"
    wrote=$((wrote + 1))
  else
    echo "::error::$file claims PromptPack spec v$found but the embedded schema is v$embedded."
    failed=1
  fi
done

if [ "$failed" -ne 0 ]; then
  echo "::error::The embedded schema is the source of truth — it mirrors the published"
  echo "::error::release and is never hand-edited. Run: ./scripts/check-spec-version.sh --write"
  exit 1
fi

if [ "$wrote" -gt 0 ]; then
  echo "✓ Restated the PromptPack spec version as v$embedded in $wrote file(s)."
  echo "  sdk/doc.go feeds the generated API reference — run 'make docs-sdk-reference'"
  echo "  and 'make docs-api' if it changed."
  exit 0
fi

echo "✓ PromptKit implements PromptPack spec v$embedded (${#CLAIMS[@]} claims agree)"
