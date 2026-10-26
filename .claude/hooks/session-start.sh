#!/bin/bash
# Cloud-session setup: put the Go toolchain and golangci-lint that CI uses on
# PATH, and install the repo's git hooks.
#
# The container's own Go can be older than the repo's. Commands inside the
# workspace switch to the go.work toolchain on their own, but `go run
# tool@version` follows the tool's go.mod and builds with the container's Go,
# so gomarkdoc rendered the API reference differently from CI and the
# pre-commit drift check failed. A golangci-lint built with an older Go
# refuses to load the config at all.
set -euo pipefail

if [ "${CLAUDE_CODE_REMOTE:-}" != "true" ]; then
  exit 0
fi

cd "$CLAUDE_PROJECT_DIR"

# The versions CI pins: go.work's go line, ci.yml's golangci-lint version.
go_version="go$(awk '$1 == "go" { print $2; exit }' go.work)"
lint_version="$(grep -A3 'golangci-lint-action' .github/workflows/ci.yml |
  awk '$1 == "version:" { print $2; exit }')"

# The toolchain module lands in the module cache, which the container keeps.
goroot="$(GOTOOLCHAIN="$go_version" go env GOROOT)"
export PATH="$goroot/bin:$HOME/.local/bin:$PATH"

if ! golangci-lint version 2>/dev/null | grep -q "version ${lint_version#v} built with $go_version"; then
  GOBIN="$HOME/.local/bin" go install \
    "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$lint_version"
fi

./scripts/install-hooks.sh >/dev/null

if [ -n "${CLAUDE_ENV_FILE:-}" ]; then
  echo "export PATH=\"$goroot/bin:\$HOME/.local/bin:\$PATH\"" >> "$CLAUDE_ENV_FILE"
fi
