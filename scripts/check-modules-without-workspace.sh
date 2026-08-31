#!/usr/bin/env bash
#
# Build and test every module with GOWORK=off, the way a consumer gets it.
#
# This exists because of what the workspace hides. Every check on #1916 ran
# inside the Go workspace, which unifies the four modules into one build list,
# so `runtime` requiring apimachinery v0.36.4 against `sdk` requiring v0.37.0
# was invisible (#1920). Four green module suites could not have caught it:
# the skew only exists for a consumer resolving the modules separately.
#
# `go test ./...` at the repo root can never reproduce that. This can.
#
# Build only, deliberately: the skew failed at BUILD time, inside k8s.io/api, so
# that is the signal. vet here would report pre-existing findings that have
# nothing to do with module resolution, and a guard that cries about unrelated
# things gets muted.
#
set -euo pipefail

MODULES=(runtime pkg sdk server/a2a)
failed=()

for m in "${MODULES[@]}"; do
  echo "── ${m} (GOWORK=off)"
  if ! GOWORK=off go -C "$m" build ./... 2>&1 | sed 's/^/   /'; then
    failed+=("$m build")
    continue
  fi
  echo "   ✓ builds outside the workspace"
done

if [ ${#failed[@]} -gt 0 ]; then
  echo "::error::these modules do not build outside the Go workspace: ${failed[*]}"
  echo "::error::A consumer resolves each module on its own, without the workspace"
  echo "::error::unifying the build list. If it only works in here, it does not work."
  exit 1
fi

echo "✓ ${#MODULES[@]} modules build standalone"
