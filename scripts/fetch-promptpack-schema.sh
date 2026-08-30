#!/usr/bin/env bash
#
# Fetch the PromptPack JSON Schema from the published spec release into this
# repo's embedded copy. This script is the ONLY sanctioned way to change
# runtime/prompt/schema/promptpack.schema.json.
#
# The embedded copy is a VERBATIM MIRROR of the published release. It is not a
# promptkit-owned document and must never be hand-edited — not to add a field the
# runtime wants, not to delete one the runtime doesn't implement, not to tighten
# an enum. Where the runtime deliberately declines to carry a spec property, that
# is recorded in Go (see deliberateOmission in
# runtime/prompt/validator_spec_parity_test.go), where a reviewer can see it.
#
# Why this rule exists: between 2026-06-15 and 2026-08-30 the embedded copy sat
# 143 leaves away from the spec while claiming to be v1.5.0. It was patched
# in place rather than replaced (#1376 was labelled "vendor v1.5.0" but was a
# 301-insertion diff), so the fixes from promptpack-spec#29 never came back
# across. The result: promptkit rejected packs that were valid against
# promptpack.org — top-level `requires`, `validators[].message`,
# `variables[].binding`, four eval fields, and two eval triggers. The drift went
# unseen for two and a half months because the CI check emitted ::warning:: and
# never failed.
#
# Usage: scripts/fetch-promptpack-schema.sh [DEST_FILE]
#   default DEST_FILE: runtime/prompt/schema/promptpack.schema.json
#   PROMPTPACK_SCHEMA_URL  override the source (default: the published release)
#
set -euo pipefail

URL="${PROMPTPACK_SCHEMA_URL:-https://promptpack.org/schema/latest/promptpack.schema.json}"
DEST="${1:-runtime/prompt/schema/promptpack.schema.json}"

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

echo "Fetching PromptPack schema from ${URL}"
curl -fsSL --max-time 30 "$URL" -o "$tmp"

# Refuse to install anything that isn't a JSON Schema with a version — a captive
# portal or an HTML error page would otherwise be committed as the spec.
python3 - "$tmp" <<'PY'
import json, sys
with open(sys.argv[1]) as f:
    doc = json.load(f)
if "$defs" not in doc or not doc.get("version"):
    sys.exit("fetched document is not a versioned PromptPack schema — refusing to install it")
print(f"  fetched PromptPack schema v{doc['version']}")
PY

mkdir -p "$(dirname "$DEST")"
cp "$tmp" "$DEST"
echo "✓ Wrote ${DEST}"
