#!/usr/bin/env bash
#
# Fails unless the golangci-lint on PATH is the version CI pins in
# .github/workflows/ci.yml.
#
# Linter versions disagree about what is a finding. A cleanup verified clean
# with v2.14.0 failed CI's v2.12.2 on five findings v2.14.0 does not produce
# (revive empty-block on intentional channel drains, unparam), so a local "0
# issues" is only evidence when it comes from the version CI runs. The cloud
# session-start hook installs that version; this makes every other lint run
# (make verify, the pre-commit hook) refuse a mismatch instead of passing it.
#
# Run from anywhere inside the repo: scripts/check-lint-version.sh
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
pinned="$(grep -A3 'golangci-lint-action' "$root/.github/workflows/ci.yml" |
  awk '$1 == "version:" { print $2; exit }')"
if [ -z "$pinned" ]; then
  echo "check-lint-version: no golangci-lint version pinned in .github/workflows/ci.yml" >&2
  exit 1
fi

if ! command -v golangci-lint > /dev/null 2>&1; then
  echo "check-lint-version: golangci-lint is not installed; CI uses $pinned" >&2
  echo "  go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$pinned" >&2
  exit 1
fi

installed="$(golangci-lint version 2>/dev/null | awk '{ for (i = 1; i < NF; i++) if ($i == "version") { print $(i + 1); exit } }')"
if [ "v${installed#v}" != "$pinned" ]; then
  echo "check-lint-version: golangci-lint ${installed:-unknown} is installed, CI runs $pinned." >&2
  echo "  Findings differ between versions, so a local result would not predict CI's." >&2
  echo "  go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$pinned" >&2
  exit 1
fi
