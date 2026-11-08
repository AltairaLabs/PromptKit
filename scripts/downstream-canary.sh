#!/usr/bin/env bash
#
# Build and test one PromptKit consumer against THIS PromptKit checkout.
#
# Consumers used to meet a PromptKit change only after the release was tagged:
# promptarena#460 and PromptKit#2525 both shipped in v2.18.0 because nothing
# built a consumer against PromptKit before the tag. This points a consumer's
# checkout at the local modules and runs the part of its own CI that needs no
# API keys, cloud accounts or clusters.
#
#   promptarena   go build ./... ; go test ./... ; go test -race on the engine
#                 packages (its "Race detector" job) ; make test-examples
#                 (schema-checks every example, mock-runs every keyless one).
#                 Left out: schema-gen --check (an additive PromptKit config
#                 field makes its committed schemas stale by design) and the
#                 frontend/npm jobs.
#   deploy-*      The four deploy adapters (vertex, omnia, agentcore, foundry).
#                 Their whole Go CI: go build ./... ; go test -race ./... ;
#                 go vet -tags=integration ./... . Left out: docker builds and
#                 SonarCloud.
#   omnia         go build ./... (every package compiles against this
#                 PromptKit) ; go test -race on every package whose code or
#                 tests import PromptKit directly. Some of those start
#                 Postgres with testcontainers, so Docker must be running.
#                 Left out: the rest of its suite
#                 (envtest-backed controllers, helm, the tagged integration
#                 suite, the dashboard), which needs infrastructure and does
#                 not reach PromptKit directly.
#
# Usage:
#   scripts/downstream-canary.sh <consumer> <checkout> [<promptkit-checkout>]
#
# The consumer checkout is MODIFIED (go.mod, go.sum, bin/). Use a throwaway
# clone:
#   git clone --depth 1 https://github.com/AltairaLabs/promptarena /tmp/pa
#   scripts/downstream-canary.sh promptarena /tmp/pa
#
set -euo pipefail

if [ $# -lt 2 ]; then
  echo "usage: $0 <consumer> <checkout> [<promptkit-checkout>]" >&2
  echo "consumers: promptarena deploy-vertex deploy-omnia deploy-agentcore deploy-foundry omnia" >&2
  exit 2
fi

CONSUMER="$1"
DIR=$(cd "$2" && pwd)
PK=$(cd "${3:-$(dirname "$0")/..}" && pwd)

# Never let a go.work above the checkout (PromptKit's, if the clone sits inside
# this repo) join the build: the canary has to resolve modules the way the
# consumer does.
export GOWORK=off

# Every PromptKit module, /v2 paths. A consumer that requires only some of them
# today gets the rest replaced too, so a future dependency is tested the same
# way instead of silently resolving the published version. Modules the
# consumer pulls in through a released promptarena are replaced as well:
# replace directives apply to the whole build graph.
MODULES=(runtime pkg sdk server/a2a)

echo "── Pointing ${CONSUMER} at ${PK}"
for m in "${MODULES[@]}"; do
  go -C "$DIR" mod edit -replace "github.com/AltairaLabs/PromptKit/${m}/v2=${PK}/${m}"
done
# The local modules can carry dependencies the consumer's go.sum has never
# seen, so the module graph has to be re-resolved.
go -C "$DIR" mod tidy
go -C "$DIR" list -m all | grep 'AltairaLabs/PromptKit' | sed 's/^/   /'

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
    echo "::error::downstream canary (${CONSUMER}): ${name} failed"
    failed+=("${name}")
  fi
}

# The packages of the module whose code or tests import a PromptKit module.
promptkit_importers() {
  go -C "$DIR" list \
    -f '{{.ImportPath}} {{join .Imports " "}} {{join .TestImports " "}} {{join .XTestImports " "}}' ./... |
    awk '/github\.com\/AltairaLabs\/PromptKit\// {print $1}'
}

test_promptkit_importers() {
  local pkgs
  # A go list failure must not shrink the list unseen.
  pkgs=$(promptkit_importers) || return 1
  if [ -z "$pkgs" ]; then
    echo "no package imports PromptKit; the canary would test nothing" >&2
    return 1
  fi
  echo "$(wc -w <<<"$pkgs") packages import PromptKit"
  # shellcheck disable=SC2086 # one package per word
  go -C "$DIR" test -race -count=1 $pkgs
}

# Steps run independently so one red step does not hide the others.
case "$CONSUMER" in
  promptarena)
    step "build" go -C "$DIR" build ./...
    step "unit tests" go -C "$DIR" test ./... -count=1
    step "race (engine)" go -C "$DIR" test -race -count=1 \
      ./arena/engine/... ./arena/stages/... ./arena/turnexecutors/... ./arena/selfplay/...
    step "example packs (mock)" make -C "$DIR" test-examples
    ;;
  deploy-vertex | deploy-omnia | deploy-agentcore | deploy-foundry)
    step "build" go -C "$DIR" build ./...
    step "unit tests (race)" go -C "$DIR" test ./... -race -count=1
    step "vet integration-tagged tests" go -C "$DIR" vet -tags=integration ./...
    ;;
  omnia)
    step "build" go -C "$DIR" build ./...
    step "tests of packages importing PromptKit (race)" test_promptkit_importers
    ;;
  *)
    echo "unknown consumer: ${CONSUMER}" >&2
    exit 2
    ;;
esac

if [ ${#failed[@]} -gt 0 ]; then
  echo "::error::${CONSUMER} does not pass against this PromptKit: ${failed[*]}"
  exit 1
fi
echo "✓ ${CONSUMER} builds and passes its keyless suite against this PromptKit"
