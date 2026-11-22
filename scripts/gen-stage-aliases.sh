#!/usr/bin/env bash
#
# Regenerate runtime/pipeline/stage/<pkg>_aliases.go: stage's re-exports of
# what moved into stage/internal/<pkg> (#2876), written by
# tools/stage-aliases-gen. It re-exports only the names stage exported at BASE,
# so the helpers an internal package exports for stage's own use stay out of
# stage's API. Once a move has landed, BASE's stage declares those names in the
# alias file itself, so regenerating against origin/main keeps the same set.
#
# Usage: scripts/gen-stage-aliases.sh PKG [BASE]   (BASE default origin/main)
set -euo pipefail
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE

PKG="${1:?usage: $0 PKG [BASE]}"
BASE="${2:-origin/main}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
STAGE="runtime/pipeline/stage"

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT
git -C "${ROOT}" archive "${BASE}" "${STAGE}" | tar -x -C "${tmp}"

GOWORK=off go -C "${ROOT}/tools/stage-aliases-gen" run . \
  -base "${tmp}/${STAGE}" \
  -pkg "${ROOT}/${STAGE}/internal/${PKG}" \
  -import "github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/${PKG}" \
  -out "${ROOT}/${STAGE}/${PKG}_aliases.go"
