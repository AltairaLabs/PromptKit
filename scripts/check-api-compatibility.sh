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
deferred=()

# gorelease has to run against a CLEAN tree whose sibling requires resolve.
# Neither holds in the working copy: sdk and server/a2a pin their siblings at
# placeholders (server/a2a@v0.0.0) that the release pipeline only rewrites at
# tag time, so before tagging there is nothing to resolve them to.
#
# So each module is analysed in a throwaway worktree at HEAD, with sibling
# requires rewritten to the BASE version and the local `replace` directives
# dropped — which is what a consumer of the published modules actually sees.
# The rewrite is committed inside the worktree so gorelease sees a clean tree.
WORKTREE=$(mktemp -d)
cleanup() {
  git worktree remove --force "$WORKTREE" 2>/dev/null || true
  rm -rf "$WORKTREE" 2>/dev/null || true
}
trap cleanup EXIT

git worktree add --quiet --detach "$WORKTREE" HEAD

for m in "${MODULES[@]}"; do
  echo "── ${m}"

  # Make sibling requires resolvable. The local `replace` directives are KEPT:
  # they point at the sibling in the same tree, which is the only thing that can
  # resolve for a FIRST major release, where no sibling /vN tag exists yet to
  # point at. Dropping them works for a minor and breaks entirely for a major,
  # so the placeholder versions are simply replaced with something well-formed
  # and the replace does the resolving.
  (
    cd "$WORKTREE/$m"
    for sib in runtime pkg sdk server/a2a; do
      [ "$sib" = "$m" ] && continue
      # Match the module path with or without a /vN suffix.
      mod=$(grep -oE "github\.com/AltairaLabs/PromptKit/${sib}(/v[0-9]+)?" go.mod \
            | head -1) || true
      [ -z "$mod" ] && continue
      case "$mod" in
        */v[0-9]*) ;;                                   # a major module: leave the
                                                        # version alone, the replace
                                                        # resolves it
        *) go mod edit -require="${mod}@${BASE}" 2>/dev/null || true ;;
      esac
    done
  )

  out=$(cd "$WORKTREE" && git -c user.email=ci@local -c user.name=ci \
          commit --no-verify -aqm "resolve siblings to ${BASE}" 2>/dev/null;
        GOWORK=off go -C "$WORKTREE/$m" run golang.org/x/exp/cmd/gorelease@latest \
          -base="$BASE" -version="$VERSION" 2>&1 || true)
  echo "$out" | sed 's/^/   /'

  # Key on the VERDICT, not the exit code: gorelease exits non-zero for
  # diagnostics too, and a diagnostic is not a breaking change.
  if echo "$out" | grep -q "is not a valid semantic version"; then
    failed+=("$m")
  elif echo "$out" | grep -q "is a valid semantic version"; then
    echo "   ✓ ${m}: ${VERSION} carries these changes"
  elif [[ "$VERSION" =~ ^v[0-9]+\.0\.0$ ]] \
       && echo "$out" | grep -qE 'unknown revision [^ ]+/v[0-9]+\.0\.0'; then
    # Bounded exception, for a FIRST MAJOR release only.
    #
    # gorelease deliberately ignores `replace` — it analyses what a consumer
    # sees — so a module depending on a sibling at vN.0.0 cannot be checked
    # until that sibling is tagged, and on a first major nothing is tagged yet.
    # The release pipeline tags libraries before tools precisely because of this
    # ordering, so these modules ARE verifiable, just later than this step runs.
    #
    # Narrow on purpose: it applies only when the claimed version is X.0.0 AND
    # the sole obstacle is an unknown sibling revision at X.0.0. Any other
    # no-verdict still fails.
    echo "   ⚠ ${m}: cannot be verified until its sibling is tagged (first major)."
    echo "   ⚠ Verified after the libraries phase, not here."
    deferred+=("$m")
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

if [ ${#deferred[@]} -gt 0 ]; then
  echo "::warning::deferred to the libraries phase (first major): ${deferred[*]}"
fi
echo "✓ $(( ${#MODULES[@]} - ${#deferred[@]} ))/${#MODULES[@]} modules verified against ${VERSION}"
