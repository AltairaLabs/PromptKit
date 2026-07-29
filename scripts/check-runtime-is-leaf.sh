#!/usr/bin/env bash
#
# Guards issue #1713: runtime must not depend on any sibling PromptKit module.
#
# runtime/hooks used to import pkg/config while pkg/config imports
# runtime/{credentials,providers/base,tools,...}, so the two modules required
# each other. Module cycles are legal in Go, but they make the release
# ordering circular: neither tag can be cut with a correct `require` on the
# other without editing both go.mod files in the same commit. That is how
# every published tag ended up with a stale internal require.
#
# The shared binding types now live in runtime/hooks/execconfig, a leaf package
# pkg/config re-exports as aliases. Keep it that way: if runtime needs a type
# that pkg/config also needs, put it in execconfig (or another runtime leaf
# package) rather than importing pkg/config from runtime.
#
# Run from the repo root: scripts/check-runtime-is-leaf.sh
set -euo pipefail

GOMOD="${1:-runtime/go.mod}"

if [ ! -f "$GOMOD" ]; then
	echo "ERROR: no such file: $GOMOD (run from the repo root)"
	exit 2
fi

# Matches a require/replace of another PromptKit module in either the
# single-line or the parenthesised-block form. The `module github.com/...`
# declaration starts with "module " and is deliberately not matched.
PATTERN='^[[:space:]]*(require[[:space:]]+|replace[[:space:]]+)?github\.com/AltairaLabs/PromptKit/'

if grep -nE "$PATTERN" "$GOMOD"; then
	echo ""
	echo "FAIL: $GOMOD depends on a sibling PromptKit module (issue #1713)."
	echo ""
	echo "runtime is the foundation — see runtime/CLAUDE.md. Depending on pkg,"
	echo "sdk or server/a2a re-creates the module cycle that made every published"
	echo "library tag ship a stale internal require."
	echo ""
	echo "If runtime and pkg/config both need a type, declare it in"
	echo "runtime/hooks/execconfig (or another runtime leaf package) and re-export"
	echo "it from pkg/config as an alias."
	exit 1
fi

echo "OK: $GOMOD has no sibling PromptKit dependencies — runtime is a leaf."
