#!/usr/bin/env bash
#
# Fixture tests for scripts/verify-published-gomod.sh.
#
# Each case names the mutation it catches, because a guard that cannot fail is
# worse than no guard — that is exactly how issue #1713 survived five releases.
#
# Run from the repo root: scripts/test-verify-published-gomod.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VERIFY="$SCRIPT_DIR/verify-published-gomod.sh"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

pass=0
fail=0

indent() { awk '{ print "      " $0 }'; }

# expect <name> <want-exit> <version> <gomod-content> [<substring the output must contain>]
expect() {
	local name="$1" want="$2" version="$3" content="$4" needle="${5:-}"
	local file="$WORK/$name.mod"
	local out got=0

	printf '%s' "$content" >"$file"
	out="$("$VERIFY" --file "$file" "$version" 2>&1)" || got=$?

	if [ "$got" -ne "$want" ]; then
		echo "FAIL: $name — exit $got, wanted $want"
		echo "$out" | indent
		fail=$((fail + 1))
		return
	fi
	if [ -n "$needle" ] && ! printf '%s' "$out" | grep -qF "$needle"; then
		echo "FAIL: $name — output missing '$needle'"
		echo "$out" | indent
		fail=$((fail + 1))
		return
	fi
	echo "OK:   $name"
	pass=$((pass + 1))
}

# ---------------------------------------------------------------------------
# Clean go.mod files must pass. Catches a guard that rejects everything, which
# would be indistinguishable from a working guard on the bad fixtures below.
# ---------------------------------------------------------------------------

expect "clean-block-requires" 0 v1.5.8 'module github.com/AltairaLabs/PromptKit/sdk

go 1.26.0

require (
	github.com/AltairaLabs/PromptKit/pkg v1.5.8
	github.com/AltairaLabs/PromptKit/runtime v1.5.8
	github.com/AltairaLabs/PromptKit/server/a2a v1.5.8
	github.com/stretchr/testify v1.11.1
)
'

expect "clean-single-line-require" 0 v1.5.8 'module github.com/AltairaLabs/PromptKit/pkg

go 1.26.0

require github.com/AltairaLabs/PromptKit/runtime v1.5.8
'

# A module with no internal dependencies at all is correct by construction.
# Catches a guard that demands at least one internal require.
expect "no-internal-deps" 0 v1.5.8 'module github.com/AltairaLabs/PromptKit/runtime

go 1.26.0

require (
	github.com/google/uuid v1.6.0
	gopkg.in/yaml.v3 v3.0.1
)
'

# Third-party replaces and requires at other versions are none of our business.
# Catches a guard that matches any `replace`, or that version-checks every require.
expect "external-replace-and-requires-ignored" 0 v1.5.8 'module github.com/AltairaLabs/PromptKit/pkg

go 1.26.0

replace github.com/xeipuuv/gojsonschema => github.com/AltairaLabsFork/gojsonschema v1.0.0

require (
	github.com/AltairaLabs/PromptKit/runtime v1.5.8
	github.com/stretchr/testify v1.11.1
	k8s.io/apimachinery v0.36.3
)
'

# retract blocks contain bare versions that are not requires.
# Catches a parser that treats every block line as a require.
expect "retract-block-ignored" 0 v1.5.8 'module github.com/AltairaLabs/PromptKit/pkg

go 1.26.0

retract v1.4.0 // Published prematurely; use v1.4.1+

retract (
	v1.4.0
	v1.2.3
)

require github.com/AltairaLabs/PromptKit/runtime v1.5.8
'

# ---------------------------------------------------------------------------
# Bad go.mod files must fail, and must name the offender.
# ---------------------------------------------------------------------------

# The pkg/v1.5.7 defect exactly: a masked replace plus the stale require it hid.
expect "issue-1713-shape" 1 v1.5.8 'module github.com/AltairaLabs/PromptKit/pkg

go 1.26.0

replace github.com/AltairaLabs/PromptKit/runtime => ../runtime

require (
	github.com/AltairaLabs/PromptKit/runtime v1.3.5
	github.com/stretchr/testify v1.11.1
)
' "github.com/AltairaLabs/PromptKit/runtime"

# Catches a guard that only inspects require lines.
expect "single-line-internal-replace" 1 v1.5.8 'module github.com/AltairaLabs/PromptKit/pkg

go 1.26.0

replace github.com/AltairaLabs/PromptKit/runtime => ../runtime

require github.com/AltairaLabs/PromptKit/runtime v1.5.8
' "replace"

# Catches a parser that only handles the single-line replace form.
expect "block-internal-replace" 1 v1.5.8 'module github.com/AltairaLabs/PromptKit/sdk

go 1.26.0

replace (
	github.com/AltairaLabs/PromptKit/pkg => ../pkg
	github.com/AltairaLabs/PromptKit/runtime => ../runtime
)

require (
	github.com/AltairaLabs/PromptKit/pkg v1.5.8
	github.com/AltairaLabs/PromptKit/runtime v1.5.8
)
' "replace"

# The runtime -> pkg half of #1713: a plain stale require, no replace masking it.
# Catches a guard that only looks for replaces.
expect "stale-internal-require" 1 v1.5.8 'module github.com/AltairaLabs/PromptKit/runtime

go 1.26.0

require (
	github.com/AltairaLabs/PromptKit/pkg v1.5.3
	github.com/google/uuid v1.6.0
)
' "v1.5.3"

# Catches a guard that skips indirect requires — sdk carries pkg as indirect in
# some example modules, and an indirect stale require is just as wrong.
expect "stale-indirect-require" 1 v1.5.8 'module github.com/AltairaLabs/PromptKit/sdk

go 1.26.0

require (
	github.com/AltairaLabs/PromptKit/runtime v1.5.8
	github.com/AltairaLabs/PromptKit/pkg v1.5.3 // indirect
)
' "v1.5.3"

# Catches a guard that uses substring/prefix matching on the version instead of
# equality — v1.5.80 contains v1.5.8.
expect "version-near-miss" 1 v1.5.8 'module github.com/AltairaLabs/PromptKit/pkg

go 1.26.0

require github.com/AltairaLabs/PromptKit/runtime v1.5.80
' "v1.5.80"

# A pseudo-version is never a release version.
# Catches a guard that only rejects lower semver.
expect "pseudo-version-require" 1 v1.5.8 'module github.com/AltairaLabs/PromptKit/pkg

go 1.26.0

require github.com/AltairaLabs/PromptKit/runtime v0.0.0-20260101000000-abcdef123456
' "v0.0.0-"

# ---------------------------------------------------------------------------
# Argument handling.
# ---------------------------------------------------------------------------

got=0
"$VERIFY" >/dev/null 2>&1 || got=$?
if [ "$got" -eq 0 ]; then
	echo "FAIL: no-arguments — expected non-zero exit"
	fail=$((fail + 1))
else
	echo "OK:   no-arguments"
	pass=$((pass + 1))
fi

got=0
"$VERIFY" --file "$WORK/does-not-exist.mod" v1.5.8 >/dev/null 2>&1 || got=$?
if [ "$got" -eq 0 ]; then
	echo "FAIL: missing-file — expected non-zero exit"
	fail=$((fail + 1))
else
	echo "OK:   missing-file"
	pass=$((pass + 1))
fi

echo ""
if [ "$fail" -ne 0 ]; then
	echo "$fail failed, $pass passed"
	exit 1
fi
echo "All $pass checks passed."
