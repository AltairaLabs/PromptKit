#!/usr/bin/env bash
#
# Build and test PromptArena against THIS PromptKit checkout.
#
# PromptArena is PromptKit's main consumer, and it used to meet a PromptKit
# change only after the release was tagged: promptarena#460 and PromptKit#2525
# both shipped in v2.18.0 because nothing built promptarena against PromptKit
# before the tag. This points a promptarena checkout at the local modules and
# runs the part of promptarena's own CI that needs no API keys and no private
# npm packages:
#
#   - go build ./...                 (its "Build & Test" job)
#   - go test ./... -count=1         (its "Build & Test" job)
#   - go test -race on the engine    (its "Race detector (engine)" job)
#   - make test-examples             (its "Test example packs" job: schema-checks
#                                     every example and mock-runs every keyless one)
#
# Left out, deliberately: `schema-gen --check` (an additive PromptKit config
# field makes promptarena's committed schemas stale by design; promptarena
# regenerates on its own schedule) and the frontend/npm jobs (no Go code of
# ours reaches them).
#
# Usage:
#   scripts/downstream-canary.sh <promptarena-checkout> [<promptkit-checkout>]
#
# The promptarena checkout is MODIFIED (go.mod, go.sum, bin/). Use a throwaway
# clone:
#   git clone --depth 1 https://github.com/AltairaLabs/promptarena /tmp/pa
#   scripts/downstream-canary.sh /tmp/pa
#
set -euo pipefail

if [ $# -lt 1 ]; then
  echo "usage: $0 <promptarena-checkout> [<promptkit-checkout>]" >&2
  exit 2
fi

PA=$(cd "$1" && pwd)
PK=$(cd "${2:-$(dirname "$0")/..}" && pwd)

# promptarena is a single module with no go.work of its own. Never let a
# go.work above it (PromptKit's, if the clone sits inside this repo) join the
# build: the canary has to resolve modules the way promptarena does.
export GOWORK=off

# Every PromptKit module, /v2 paths. promptarena requires only runtime and pkg
# today; the other two are replaced too so a future dependency on them is
# tested the same way instead of silently resolving the published version.
MODULES=(runtime pkg sdk server/a2a)

echo "── Pointing promptarena at ${PK}"
for m in "${MODULES[@]}"; do
  go -C "$PA" mod edit -replace "github.com/AltairaLabs/PromptKit/${m}/v2=${PK}/${m}"
done
# The local modules can carry dependencies promptarena's go.sum has never
# seen, so the module graph has to be re-resolved.
go -C "$PA" mod tidy
go -C "$PA" list -m all | grep 'AltairaLabs/PromptKit' | sed 's/^/   /'

failed=()
step() {
  local name="$1"
  shift
  echo "::group::${name}"
  if "$@"; then
    echo "::endgroup::"
    echo "   ✓ ${name}"
  else
    echo "::endgroup::"
    echo "::error::downstream canary: ${name} failed"
    failed+=("${name}")
  fi
}

# Steps run independently so one red step does not hide the others.
step "build" go -C "$PA" build ./...
step "unit tests" go -C "$PA" test ./... -count=1
step "race (engine)" go -C "$PA" test -race -count=1 \
  ./arena/engine/... ./arena/stages/... ./arena/turnexecutors/... ./arena/selfplay/...
step "example packs (mock)" make -C "$PA" test-examples

if [ ${#failed[@]} -gt 0 ]; then
  echo "::error::promptarena does not pass against this PromptKit: ${failed[*]}"
  exit 1
fi
echo "✓ promptarena builds and passes its keyless suite against this PromptKit"
