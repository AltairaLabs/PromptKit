#!/usr/bin/env bash
#
# Generate the /schemas/latest/ redirect files. Each is a tiny JSON $ref pointing
# at the current versioned schema, so authors can pin
# https://promptkit.altairalabs.ai/schemas/latest/<type>.json and follow the
# latest published version. Regenerated into docs/public at docs-build time.
#
# Usage: scripts/gen-schema-latest-refs.sh [SRC_DIR] [DEST_DIR]
#   SRC_DIR  versioned schemas (default: schemas/v1alpha1)
#   DEST_DIR latest output      (default: docs/public/schemas/latest)
#
set -euo pipefail

SRC="${1:-schemas/v1alpha1}"
DEST="${2:-docs/public/schemas/latest}"
BASE="https://promptkit.altairalabs.ai/schemas/v1alpha1"

mkdir -p "$DEST/common"
(cd "$SRC" && find . -name '*.json' | sed 's|^\./||') | while IFS= read -r rel; do
  mkdir -p "$DEST/$(dirname "$rel")"
  printf '{\n  "$ref": "%s/%s"\n}\n' "$BASE" "$rel" > "$DEST/$rel"
done
echo "✓ Generated latest/ \$ref redirects in $DEST"
