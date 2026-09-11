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

MODULE_PATH=github.com/AltairaLabs/PromptKit
MODULES=(runtime pkg sdk server/a2a)

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

# gorelease needs two things the working copy cannot give it.
#
# 1. A clean tree whose sibling requires resolve. sdk and server/a2a pin their
#    siblings at placeholders (server/a2a@v0.0.0) behind local `replace`
#    directives, and gorelease ignores replaces when it loads the release
#    version ("These directives only apply within the main module").
#
# 2. Siblings at the version BEING RELEASED, not the previous one. The release
#    tags runtime and pkg first and rewrites sdk's requires to VERSION, so what a
#    consumer gets is sdk@VERSION over runtime@VERSION. Pinning the siblings at
#    BASE instead — which is what this script did before this rewrite — made gorelease
#    compile HEAD's sdk against the PREVIOUS runtime, and the first release where
#    sdk used a new runtime symbol failed with "undefined" and no verdict.
#
# So the analysis runs in a throwaway clone of HEAD where every module's sibling
# requires are rewritten to VERSION and the replaces dropped, committed, and the
# siblings tagged at VERSION. GOPRIVATE routes only our module path to a direct
# git fetch, and a git URL redirect points that fetch at the clone, so
# runtime@VERSION resolves to exactly the code being released while every
# third-party dependency still comes from the proxy. The module under analysis
# is left untagged (gorelease refuses a version that already exists) and is
# diffed against the real BASE tag, which the clone inherited.
#
# A private GOMODCACHE keeps the fabricated VERSION out of the developer's real
# module cache, where it would shadow the eventual published module.
REPO=$(git rev-parse --show-toplevel)
WORK=$(mktemp -d)
CLONE="$WORK/clone"
cleanup() { rm -rf "$WORK" 2>/dev/null || true; }
trap cleanup EXIT

git clone --quiet --shared "$REPO" "$CLONE"
git -C "$CLONE" checkout --quiet --detach HEAD

for m in "${MODULES[@]}"; do
  (
    cd "$CLONE/$m"
    for sib in "${MODULES[@]}"; do
      [ "$sib" = "$m" ] && continue
      mod="${MODULE_PATH}/${sib}"
      grep -q "$mod" go.mod || continue
      go mod edit -dropreplace="$mod"
      go mod edit -require="${mod}@${VERSION}"
    done
  )
done
git -C "$CLONE" -c user.email=ci@local -c user.name=ci \
  commit --no-verify -aqm "resolve siblings to ${VERSION}"
for m in "${MODULES[@]}"; do
  git -C "$CLONE" tag "${m}/${VERSION}"
done

# One environment for every gorelease run. -mod=mod lets the go command add the
# sums the rewritten go.mod files lack instead of failing on them; -modcacherw
# leaves the private module cache writable so the cleanup trap can remove it.
export GOWORK=off
export GOFLAGS="-mod=mod -modcacherw"
export GOMODCACHE="$WORK/modcache"
export GOPRIVATE="$MODULE_PATH"
export GIT_CONFIG_COUNT=1
export GIT_CONFIG_KEY_0="url.file://${CLONE}.insteadOf"
export GIT_CONFIG_VALUE_0="https://${MODULE_PATH}"

failed=()
for m in "${MODULES[@]}"; do
  echo "── ${m}"

  # gorelease rejects a VERSION that already exists in the module's own repo,
  # so the module under analysis is untagged for its own run only.
  git -C "$CLONE" tag -d "${m}/${VERSION}" >/dev/null

  out=$(go -C "$CLONE/$m" run golang.org/x/exp/cmd/gorelease@latest \
          -base="$BASE" -version="$VERSION" 2>&1 || true)
  echo "$out" | sed 's/^/   /'

  git -C "$CLONE" tag "${m}/${VERSION}"

  # Key on the VERDICT, not the exit code: gorelease exits non-zero for
  # diagnostics too, and a diagnostic is not a breaking change.
  if echo "$out" | grep -q "is not a valid semantic version"; then
    failed+=("$m")
  elif echo "$out" | grep -q "is a valid semantic version"; then
    echo "   ✓ ${m}: ${VERSION} carries these changes"
  else
    # No verdict is a FAILURE. A gate that passes when it could not look
    # reports success for a release nobody checked.
    echo "::error::${m}: gorelease reached no verdict, so the API is UNVERIFIED."
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
