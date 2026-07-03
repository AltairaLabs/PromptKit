#!/usr/bin/env bash
#
# Fetch the generated JSON schemas from the promptarena repo (the schema owner)
# into this repo. PromptKit no longer generates the schemas — the config types
# they reflect live in promptarena/arena/arenaconfig (arena, scenario, persona,
# eval, promptconfig, tool) and PromptKit/pkg/config (provider, runtime, logging),
# and promptarena's tools/schema-gen generates them. PromptKit keeps a committed
# copy because pkg/config's validator loads them and docs hosts them at the
# stable promptkit.altairalabs.ai/schemas URL.
#
# Usage: scripts/fetch-schemas.sh [DEST_DIR]   (default: schemas/v1alpha1)
#   PROMPTARENA_SCHEMA_REF   git ref to fetch from (default: main)
#
set -euo pipefail

REF="${PROMPTARENA_SCHEMA_REF:-main}"
BASE="https://raw.githubusercontent.com/AltairaLabs/promptarena/${REF}/schemas/v1alpha1"
DEST="${1:-schemas/v1alpha1}"

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
