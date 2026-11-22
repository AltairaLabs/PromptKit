#!/usr/bin/env bash
#
# Decide whether a module's "incompatible" gorelease verdict breaks consumers.
#
# Reads one module's gorelease output on stdin. Exits 0 when every line under
# "## incompatible changes" is one of the kinds below, and 1 otherwise, printing
# the lines it accepted and the lines it did not.
#
# The release gate exists to stop what broke Omnia on v1.8.0 (#1921): removed
# symbols and re-typed fields shipped as a minor, so consumers stopped
# compiling. gorelease (and apidiff, which it runs) also counts changes as
# incompatible that do not do that in practice, and this is the whole list:
#
#   - "<T>: old is comparable, new is not". A struct gained a slice, map or func
#     field, so `a == b` and map[T] no longer compile. That happens whenever a
#     protocol type grows a list or an extension map (MCP 2026-07-28 did, #2106),
#     and these are data types callers decode into, not types they compare.
#   - "<C>: value changed from X to Y". A constant's value moved, e.g.
#     mcp.ProtocolVersion advancing to the newest revision. Code still compiles;
#     tracking the newest value is what such a constant is for.
#   - "<S>: changed from <old> to <new>" that is only a type moving out of
#     pipeline/stage into one of its internal packages behind a same-named
#     alias (#2876). apidiff compares named types by package, so the moved
#     type, every const of it and every field or parameter holding it report a
#     change although stage.X still names it. Inside pipeline/stage the old
#     side prints X and the new .../pipeline/stage/internal/<pkg>.X; in any
#     other package the old side prints .../pipeline/stage.X. The line is
#     accepted when, with the new side's internal qualifier deleted (in stage)
#     or rewritten to the stage qualifier (elsewhere), both sides are equal.
#     Signatures print identically on both sides ("changed from func(X) to
#     func(X)"), which the same rule accepts.
#     This rule cannot see the moved type's own fields, methods or underlying
#     type: apidiff reports the move and stops there. scripts/check-stage-api.sh
#     compares each moved type with its old self, and both the PR job and the
#     release gate run it; TestInternalTypesReachableFromAPIHaveAliases makes
#     sure every internal type the API reaches has a stage alias. A rename (X
#     to internal/x.Y), a re-typed field, a removal or a generic type whose type
#     parameters print differently is still rejected here.
#
# Anything else under "incompatible changes" — removed, renamed or re-typed API
# — still fails the release. Add a pattern here only with the same argument:
# the change cannot stop a plausible consumer from compiling.
#
# Usage: gorelease ... | scripts/api-compat-allowlist.sh
#        (scripts/check-stage-api.sh feeds it apidiff output in the same shape)
set -euo pipefail

accepted=()
rejected=()
in_incompatible=0

# The qualifier apidiff prints for pipeline/stage and for a type declared in
# one of its internal packages. Anchored so it cannot match inside a longer
# path (example.com/github.com/AltairaLabs/...).
QUAL_START='(^|[^A-Za-z0-9._/-])'
STAGE_PATH='github\.com/AltairaLabs/PromptKit/runtime(/v[0-9]+)?/pipeline/stage'
STAGE_INTERNAL="${STAGE_PATH}/internal/[A-Za-z0-9_/]+\."
current_pkg=""

# alias_move_only <rest> succeeds when <rest>, the "<old> to <new>" of a
# "changed from" line in package current_pkg, reads "T to T" once the
# stage-internal qualifiers are normalized: deleted inside pipeline/stage,
# where the old side prints a stage type unqualified, and rewritten to the
# pipeline/stage qualifier in every other package, where it prints qualified.
# The split is found by length, not by searching for " to ", because a type
# can contain " to " itself (a struct field named "to").
alias_move_only() {
  local stripped n half
  if [[ "$current_pkg" =~ ^${STAGE_PATH}$ ]]; then
    stripped="$(printf '%s' "$1" | sed -E "s#${QUAL_START}${STAGE_INTERNAL}#\1#g")"
  else
    stripped="$(printf '%s' "$1" |
      sed -E "s#${QUAL_START}(${STAGE_PATH})/internal/[A-Za-z0-9_/]+\.#\1\2.#g")"
  fi
  n=${#stripped}
  [ $(((n - 4) % 2)) -eq 0 ] && [ "$n" -gt 4 ] || return 1
  half=$(((n - 4) / 2))
  [ "${stripped:half:4}" = " to " ] && [ "${stripped:0:half}" = "${stripped:half+4}" ]
}

while IFS= read -r line; do
  case "$line" in
    "## incompatible changes"*) in_incompatible=1; continue ;;
    "# "*) current_pkg="${line#\# }"; in_incompatible=0; continue ;;
    "#"*|"") in_incompatible=0; continue ;;
  esac
  [ "$in_incompatible" -eq 1 ] || continue

  if [[ "$line" =~ ^[A-Za-z0-9_.]+:\ old\ is\ comparable,\ new\ is\ not$ ]] ||
     [[ "$line" =~ ^[A-Za-z0-9_.]+:\ value\ changed\ from\ .+\ to\ .+$ ]]; then
    accepted+=("$line")
  elif [[ "$line" =~ ^[A-Za-z0-9_.()*]+:\ changed\ from\ (.+)$ ]] &&
       alias_move_only "${BASH_REMATCH[1]}"; then
    accepted+=("$line")
  else
    rejected+=("$line")
  fi
done

if [ ${#rejected[@]} -gt 0 ]; then
  echo "breaking changes:"
  printf '  %s\n' "${rejected[@]}"
  exit 1
fi
if [ ${#accepted[@]} -eq 0 ]; then
  # An incompatible verdict with nothing listed is not something this script
  # understands, so it must not wave it through.
  echo "no incompatible changes found in the output, so the verdict is unexplained"
  exit 1
fi
echo "accepted (cannot break a consumer's build):"
printf '  %s\n' "${accepted[@]}"
