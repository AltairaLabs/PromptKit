#!/usr/bin/env bash
#
# Assert that the PromptPack spec version PromptKit CLAIMS to implement matches
# the version it ACTUALLY embeds.
#
# PromptKit stated its spec version nowhere at all until this check existed —
# not in the README, not in the docs. A consumer asking "which PromptPack
# version does this runtime implement?" had to open
# runtime/prompt/schema/promptpack.schema.json and read the version field.
#
# Stating it creates a second place to be wrong, so it is stated once (the
# README badge) and pinned here. The embedded schema is the source of truth:
# it is a byte-for-byte mirror of the published release and cannot be
# hand-edited (see runtime/prompt/schema/schema.go), so the badge follows it,
# never the other way round.
#
# Mirrors promptpack-spec's scripts/check-version-consistency.mjs, which exists
# for the same reason: RFC-0009 was marked Implemented while its schema fields
# were never added, and no automated check caught it.
#
# With --write, the badge is rewritten to match the embedded schema instead of
# being checked against it. The sync workflow uses this so the schema, the types
# generated from it and the version PromptKit claims all move in one commit.
set -euo pipefail

WRITE=false
if [ "${1:-}" = "--write" ]; then
  WRITE=true
fi

SCHEMA="runtime/prompt/schema/promptpack.schema.json"
README="README.md"

embedded="$(python3 -c "import json;print(json.load(open('$SCHEMA'))['version'])")"
if [ -z "$embedded" ]; then
  echo "::error::$SCHEMA has no top-level version field"
  exit 1
fi

badge="$(grep -oE 'PromptPack%20Spec-v[0-9]+\.[0-9]+\.[0-9]+-' "$README" | head -1 |
         sed -E 's/.*-v([0-9]+\.[0-9]+\.[0-9]+)-/\1/')"
if [ -z "$badge" ]; then
  echo "::error::$README is missing the PromptPack spec badge"
  echo "::error::Expected a shields.io badge of the form PromptPack%20Spec-vX.Y.Z-"
  exit 1
fi

if [ "$embedded" != "$badge" ] && [ "$WRITE" = true ]; then
  python3 - "$README" "$badge" "$embedded" <<'PY'
import sys
path, old, new = sys.argv[1], sys.argv[2], sys.argv[3]
s = open(path).read()
open(path, "w").write(s.replace(f"PromptPack%20Spec-v{old}-", f"PromptPack%20Spec-v{new}-"))
PY
  echo "✓ README spec badge v$badge -> v$embedded"
  exit 0
fi

if [ "$embedded" != "$badge" ]; then
  echo "::error::PromptKit claims PromptPack spec v$badge but embeds v$embedded."
  echo "::error::The embedded schema is the source of truth — it mirrors the published"
  echo "::error::release and is never hand-edited. Update the README badge to v$embedded."
  exit 1
fi

echo "✓ PromptKit implements PromptPack spec v$embedded (README badge agrees)"
