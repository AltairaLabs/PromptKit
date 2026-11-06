#!/usr/bin/env bash
# Fail when the diff against BASE adds a Go `// Deprecated:` marker.
#
# PromptKit is a public library: a symbol with no caller here may still be in
# use downstream, and a deprecation trips staticcheck SA1019 in every consumer
# that uses it. Deprecations are decided per symbol by a maintainer, who signals
# the decision with the `deprecation-approved` PR label (see
# .github/workflows/deprecation-gate.yml). A declared field nothing reads is
# wired, not deprecated.
#
# Usage: check-new-deprecations.sh [BASE]   (default origin/main)
set -euo pipefail

BASE="${1:-origin/main}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

added="$(git -C "$ROOT" diff --unified=0 "$BASE"...HEAD -- '*.go' \
  | awk '/^\+\+\+ /{file=substr($0,7); next} /^\+.*\/\/[[:space:]]*Deprecated:/{print file": "substr($0,2)}')"

if [ -n "$added" ]; then
  echo "This change adds Deprecated: markers:"
  echo "$added"
  echo
  echo "Deprecating exported API needs a maintainer's explicit decision for each symbol."
  echo "If that decision has been made, a maintainer applies the 'deprecation-approved' label."
  exit 1
fi
echo "No new Deprecated: markers."
