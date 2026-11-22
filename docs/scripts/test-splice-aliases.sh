#!/usr/bin/env bash
#
# Fixture tests for docs/scripts/splice-aliases.py.
#
# testdata/splice-aliases holds gomarkdoc pages of a scratch module shaped like
# the pipeline/stage split (#2876): stage.md for a facade aliasing
# internal/core's Scored and Kind, core.md for internal/core, other-core.md for
# a second internal package also named core. want.md is the spliced page.
# stage-typed.md lists typed constants and an example under its aliases;
# want-typed.md is it spliced. stage-vars.md re-exports core-vars.md's vars;
# want-vars.md is it spliced.
#
# Run from anywhere: docs/scripts/test-splice-aliases.sh
set -euo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SPLICE=(python3 -I "${DIR}/splice-aliases.py")
DATA="${DIR}/testdata/splice-aliases"

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

pass=0
fail=0
ok() { echo "ok:   $1"; pass=$((pass + 1)); }
bad() { echo "FAIL: $1"; fail=$((fail + 1)); }

# Catches: a splice that drops the fields, the methods, the index entries or
# the source links, or that leaks internal/core's own functions (Helper).
"${SPLICE[@]}" "${DATA}/stage.md" "${DATA}/core.md" >"${tmp}/got.md"
if cmp -s "${DATA}/want.md" "${tmp}/got.md"; then
  ok "aliases render the internal types' fields and methods"
else
  bad "aliases render the internal types' fields and methods"
  diff -u "${DATA}/want.md" "${tmp}/got.md" | sed 's/^/      /' || true
fi

# Catches: a splice that drops what the facade lists under an alias's own
# section: its typed constants (they render with the internal values instead)
# and its examples, which only the facade's tests have.
"${SPLICE[@]}" "${DATA}/stage-typed.md" "${DATA}/core.md" >"${tmp}/typed.md"
if cmp -s "${DATA}/want-typed.md" "${tmp}/typed.md"; then
  ok "typed constants and examples under an alias survive"
else
  bad "typed constants and examples under an alias survive"
  diff -u "${DATA}/want-typed.md" "${tmp}/typed.md" | sed 's/^/      /' || true
fi

# Catches: re-exported vars, and consts declared as one literal, rendered as
# the name they re-export ("ErrX = core.ErrX") instead of their value; a const
# declared as iota or as an expression (they mean nothing out of their group),
# a multi-line initializer and stage's own var left alone.
"${SPLICE[@]}" "${DATA}/stage-vars.md" "${DATA}/core-vars.md" >"${tmp}/vars.md"
if cmp -s "${DATA}/want-vars.md" "${tmp}/vars.md"; then
  ok "re-exported vars show the internal initializer"
else
  bad "re-exported vars show the internal initializer"
  diff -u "${DATA}/want-vars.md" "${tmp}/vars.md" | sed 's/^/      /' || true
fi

# Catches: a page with no internal pages to splice being rewritten at all.
"${SPLICE[@]}" "${DATA}/stage.md" >"${tmp}/same.md"
if cmp -s "${DATA}/stage.md" "${tmp}/same.md"; then
  ok "without internal pages the page is unchanged"
else
  bad "without internal pages the page is unchanged"
fi

# Catches: pages keyed by package name, where the second "core" would replace
# the first and splice an unrelated type.
if out="$("${SPLICE[@]}" "${DATA}/stage.md" "${DATA}/core.md" "${DATA}/other-core.md" 2>&1)"; then
  bad "two internal packages named core are refused (exit 0)"
elif printf '%s' "${out}" | grep -qF "stage/internal/other/core are all named core"; then
  ok "two internal packages named core are refused"
else
  bad "two internal packages named core are refused: ${out}"
fi

echo
echo "${pass} passed, ${fail} failed"
[ "${fail}" -eq 0 ]
