#!/usr/bin/env bash
#
# Fixture tests for scripts/api-compat-allowlist.sh.
#
# Each case names the mutation it catches: an allowlist that accepts too much
# turns the release gate back off, which is how v1.8.0 broke Omnia (#1921).
#
# Run from the repo root: scripts/test-api-compat-allowlist.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ALLOW="$SCRIPT_DIR/api-compat-allowlist.sh"

pass=0
fail=0

# expect <name> <want-exit> <gorelease-output> [<substring the output must contain>]
expect() {
	local name="$1" want="$2" input="$3" needle="${4:-}"
	local out got=0
	out="$(printf '%s\n' "$input" | "$ALLOW" 2>&1)" || got=$?
	if [ "$got" -ne "$want" ]; then
		echo "FAIL: $name — exit $got, wanted $want"
		printf '%s\n' "$out" | sed 's/^/      /'
		fail=$((fail + 1))
		return
	fi
	if [ -n "$needle" ] && ! printf '%s' "$out" | grep -qF "$needle"; then
		echo "FAIL: $name — output missing '$needle'"
		printf '%s\n' "$out" | sed 's/^/      /'
		fail=$((fail + 1))
		return
	fi
	echo "ok:   $name"
	pass=$((pass + 1))
}

# The real v2.9.1 → v2.10.0 runtime/mcp report (#2106), trimmed.
expect "comparability and constant value changes pass" 0 '# github.com/AltairaLabs/PromptKit/runtime/v2/mcp
## incompatible changes
ClientCapabilities: old is comparable, new is not
Implementation: old is comparable, new is not
ProtocolVersion: value changed from "2025-06-18" to "2026-07-28"
## compatible changes
Annotations: added
Icon: added

# summary
v2.10.0 is not a valid semantic version for this release.' "Implementation: old is comparable"

# Catches: an allowlist that only looks at the first incompatible line.
expect "a removal alongside allowed changes fails" 1 '# github.com/AltairaLabs/PromptKit/runtime/v2/mcp
## incompatible changes
Implementation: old is comparable, new is not
(*Client).ListTools: removed' "(*Client).ListTools: removed"

# Catches: matching "changed" loosely, so a re-typed field slips through.
expect "a re-typed field fails" 1 '# github.com/AltairaLabs/PromptKit/runtime/v2/pipeline
## incompatible changes
ToolPolicy.MaxRounds: changed from int to int64' "changed from int to int64"

# Catches: reading past the incompatible section into compatible changes.
expect "compatible changes are not judged" 0 '# pkg
## incompatible changes
Content: old is comparable, new is not
## compatible changes
Frobnicate: removed-but-listed-as-compatible-for-the-test' "Content: old is comparable"

# Catches: an incompatible section in a second package being skipped.
expect "every package's incompatible section is judged" 1 '# github.com/AltairaLabs/PromptKit/runtime/v2/mcp
## incompatible changes
Content: old is comparable, new is not

# github.com/AltairaLabs/PromptKit/runtime/v2/tools
## incompatible changes
Registry.Register: changed from func(*ToolDescriptor) error to func(*ToolDescriptor)' "Registry.Register"

# Catches: waving through an incompatible verdict the script cannot explain.
expect "a verdict with no listed changes fails" 1 '# summary
v2.10.0 is not a valid semantic version for this release.' "unexplained"

echo
echo "$pass passed, $fail failed"
[ "$fail" -eq 0 ]
