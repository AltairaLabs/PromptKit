#!/usr/bin/env bash
#
# Fail a release whose Go API changes do not match the version bump it claims.
#
# v1.8.0 removed nine exported symbols and re-typed several struct fields, and
# shipped as a MINOR. Omnia could not build against it: the released PromptArena
# called two of the removed methods (#1921). Nothing in the pipeline looked at
# the API surface, so the first signal was a downstream ticket.
#
# This is not a warning. A breaking change in a minor is a bug, not a policy: if
# the API must break, that is a major version, and the Go module-path change
# that entails is the friction that stops it happening casually.
#
# gorelease compares each module against its previous release and exits non-zero
# when the claimed version cannot carry the changes found.
#
# Usage: scripts/check-api-compatibility.sh vX.Y.Z [previous-vX.Y.Z]
set -euo pipefail

VERSION="${1:?usage: $0 <new-version> [base-version]}"
BASE="${2:-}"

if [ -z "$BASE" ]; then
  # Previous release tag on the root module, newest first, excluding this one.
  BASE=$(git tag -l 'v[0-9]*.[0-9]*.[0-9]*' --sort=-v:refname \
         | grep -v "^${VERSION}$" | head -1)
fi

if [ -z "$BASE" ]; then
  echo "::warning::no previous release tag found; skipping API compatibility check"
  exit 0
fi

echo "Comparing the published API against ${BASE}, claiming ${VERSION}."
echo

MODULES=(runtime pkg sdk server/a2a)
failed=()

for m in "${MODULES[@]}"; do
  echo "── ${m}"
  # Each submodule is tagged with its path prefix, so its base tag is
  # "<module>/<version>" while the root module's is bare.
  if GOWORK=off go -C "$m" run golang.org/x/exp/cmd/gorelease@latest \
       -base="$BASE" -version="$VERSION" 2>&1 | sed 's/^/   /'; then
    echo "   ✓ ${m}: ${VERSION} carries these changes"
  else
    failed+=("$m")
  fi
  echo
done

if [ ${#failed[@]} -gt 0 ]; then
  echo "::error::API changes in these modules are incompatible with ${VERSION}: ${failed[*]}"
  echo "::error::"
  echo "::error::gorelease reports the exact symbols above. Either:"
  echo "::error::  - restore what was removed, keeping the old name as a wrapper, or"
  echo "::error::  - release this as a MAJOR version, which for Go means a new"
  echo "::error::    module path (/v2) and a deliberate migration for every consumer."
  echo "::error::"
  echo "::error::Shipping it as a minor is what broke Omnia on v1.8.0 (#1921)."
  exit 1
fi

echo "✓ ${#MODULES[@]} modules: the API changes are compatible with ${VERSION}"
