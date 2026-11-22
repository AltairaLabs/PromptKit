#!/usr/bin/env bash
#
# Fail when a generated runtime/pipeline/stage/<pkg>_aliases.go differs from
# what tools/stage-aliases-gen writes for it (#2876): a hand edit to a
# re-export (its signature, its doc, its body) would otherwise ship unnoticed,
# and be reverted by the next `make stage-aliases`.
#
# Each file is regenerated from the tree itself: the names come from stage as
# it is now, alias file included, so the check needs no history and accepts
# exactly the files the generator would write. A name dropped from or added to
# stage's API is scripts/check-stage-api.sh's business.
#
# Usage: scripts/check-stage-aliases.sh
set -euo pipefail
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
STAGE="${ROOT}/runtime/pipeline/stage"

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

failed=0
checked=0
for file in "${STAGE}"/*_aliases.go; do
  [ -e "${file}" ] || continue
  pkg="$(basename "${file}" _aliases.go)"
  checked=$((checked + 1))
  GOWORK=off go -C "${ROOT}/tools/stage-aliases-gen" run . \
    -base "${STAGE}" \
    -pkg "${STAGE}/internal/${pkg}" \
    -import "github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/${pkg}" \
    -out "${tmp}/${pkg}_aliases.go" >/dev/null
  if cmp -s "${file}" "${tmp}/${pkg}_aliases.go"; then
    echo "✓ runtime/pipeline/stage/${pkg}_aliases.go is what the generator writes"
  else
    echo "::error::runtime/pipeline/stage/${pkg}_aliases.go differs from the generator's output; run: make stage-aliases PKG=${pkg}"
    diff -u "${file}" "${tmp}/${pkg}_aliases.go" | sed 's/^/   /' || true
    failed=1
  fi
done
[ "${checked}" -gt 0 ] || echo "no generated alias files in runtime/pipeline/stage"
exit "${failed}"
