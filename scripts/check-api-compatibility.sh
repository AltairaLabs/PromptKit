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
  out=$(GOWORK=off go -C "$m" run golang.org/x/exp/cmd/gorelease@latest \
          -base="$BASE" -version="$VERSION" 2>&1 || true)
  echo "$out" | sed 's/^/   /'

  # Key on the verdict, NOT the exit code. gorelease also exits non-zero for
  # DIAGNOSTICS — "the following requirements are needed" — which every module
  # carrying a local `replace` to a sibling produces, because the sibling
  # version cannot be resolved from the working tree. Treating that as a
  # breaking change would fail every release for a reason that is not one.
  if echo "$out" | grep -q "is not a valid semantic version"; then
    failed+=("$m")
  elif echo "$out" | grep -q "is a valid semantic version"; then
    echo "   ✓ ${m}: ${VERSION} carries these changes"
  else
    # No verdict is a FAILURE, not a warning. gorelease refuses to run on a
    # dirty tree, among other things, and a gate that passes when it could not
    # look is worse than no gate — it reports success for a release nobody
    # checked.
    echo "::error::${m}: gorelease reached no verdict, so the API is UNVERIFIED."
    echo "::error::Commit or stash local changes and re-run; gorelease refuses"
    echo "::error::to analyse a dirty tree."
    failed+=("$m (unverified)")
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
