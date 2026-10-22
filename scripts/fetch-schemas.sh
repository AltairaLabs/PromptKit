#!/usr/bin/env bash
#
# Fetch the config-file JSON schemas promptarena generates and publishes. They
# are promptarena's: PromptKit neither commits nor validates against them, and
# checks its own config against its Go types. The docs site still serves them at
# the stable promptkit.altairalabs.ai/schemas URL, so `make schemas-host` fetches
# them into docs/public at build time.
#
# Usage: scripts/fetch-schemas.sh DEST_DIR
#   PROMPTARENA_SCHEMA_REF   git ref to fetch from (default: main)
#
set -euo pipefail

REF="${PROMPTARENA_SCHEMA_REF:-main}"
BASE="https://raw.githubusercontent.com/AltairaLabs/promptarena/${REF}/schemas/v1alpha1"
DEST="${1:?usage: fetch-schemas.sh DEST_DIR}"

FILES=(
  arena.json
  eval.json
  logging.json
  persona.json
  promptconfig.json
  provider.json
  runtime-config.json
  scenario.json
  tool.json
  common/assertions.json
  common/media.json
  common/metadata.json
)

mkdir -p "$DEST/common"
echo "Fetching schemas from AltairaLabs/promptarena@${REF} -> ${DEST}"
for f in "${FILES[@]}"; do
  curl -fsSL "$BASE/$f" -o "$DEST/$f"
done
echo "✓ Fetched ${#FILES[@]} schema files"
