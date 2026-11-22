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

# What apidiff reported for a real alias move (#2876): similarity.go moved to
# stage/internal/compaction with `type X = compaction.X` and wrapper funcs in
# stage, plus a const, a struct field, a func and an interface method that
# mention the moved types.
Q='github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/compaction'
expect "a type moved behind a same-named alias passes" 0 "# github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage
## incompatible changes
ByOriginalIndex: changed from ByOriginalIndex to $Q.ByOriginalIndex
ScoreKindA: changed from ScoreKind to $Q.ScoreKind
ScoredMessage: changed from ScoredMessage to $Q.ScoredMessage
ScratchHolder.Top: changed from ScoredMessage to $Q.ScoredMessage
ScratchSorter.Sort: changed from func(ByOriginalIndex) *ScoredMessage to func(ByOriginalIndex) *ScoredMessage
ScratchTop: changed from func(ScoredMessages) ScoredMessage to func(ScoredMessages) ScoredMessage
ScratchVar: changed from []ScoredMessage to []ScoredMessage" "ScoredMessage: changed from ScoredMessage"

# A type moving between two internal packages is still the same stage alias.
expect "a type moved between internal packages passes" 0 "# github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage
## incompatible changes
Cue: changed from github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core.Cue to $Q.Cue" "Cue: changed from"

# Catches: an alias rule that ignores what is left after stripping, so any
# change to a moved type's line passes.
expect "a moved type that was also renamed fails" 1 "# github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage
## incompatible changes
ScoredMessage: changed from ScoredMessage to $Q.Scored" "ScoredMessage: changed from ScoredMessage to $Q.Scored"

# Catches: stripping the qualifier and then comparing loosely, so a real
# signature change that also mentions a moved type passes.
expect "a re-typed signature that mentions a moved type fails" 1 "# github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage
## incompatible changes
ScoredMessage: changed from ScoredMessage to $Q.ScoredMessage
NewScored: changed from func(int) ScoredMessage to func(int64) $Q.ScoredMessage" "changed from func(int) ScoredMessage to func(int64)"

# Catches: stripping qualifiers of any internal package, not only stage's.
expect "a move into another package's internal fails" 1 "# github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage
## incompatible changes
Cue: changed from Cue to github.com/AltairaLabs/PromptKit/runtime/v2/internal/cue.Cue" "runtime/v2/internal/cue.Cue"

# Catches: stripping the stage-internal qualifier but leaving a dangling
# prefix match, e.g. a package named stage/internalfoo.
expect "a move into a sibling of stage/internal fails" 1 "# github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage
## incompatible changes
Cue: changed from Cue to github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internalx.Cue" "internalx.Cue"

# A generic alias prints its type parameters without their constraints on the
# new side (seen in the same experiment), so it cannot be told apart from a
# constraint change and stays a manual decision.
expect "a moved generic type fails" 1 "# github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage
## incompatible changes
Pair: changed from Pair[T any] to $Q.Pair[T]" "Pair: changed from Pair[T any]"

# Catches: a "changed from" line with no " to " split being accepted because
# both halves happened to be empty.
expect "a malformed changed-from line fails" 1 "# github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage
## incompatible changes
X: changed from  to " "X: changed from"

# Catches: a moved type whose stage alias was dropped — apidiff says removed.
expect "a moved type without its alias fails" 1 "# github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage
## incompatible changes
ScratchHolder.Top: changed from ScoredMessage to $Q.ScoredMessage
ScoredMessage: removed" "ScoredMessage: removed"

# Catches: waving through an incompatible verdict the script cannot explain.
expect "a verdict with no listed changes fails" 1 '# summary
v2.10.0 is not a valid semantic version for this release.' "unexplained"

echo
echo "$pass passed, $fail failed"
[ "$fail" -eq 0 ]
