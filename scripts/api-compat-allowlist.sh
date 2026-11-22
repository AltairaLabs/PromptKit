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
#   - "<S>: changed from <old> to <new>" where deleting every
#     ".../runtime/vN/pipeline/stage/internal/<pkg>." qualifier from both sides
#     leaves the two identical. That is a type moving out of pipeline/stage into
#     one of its internal packages behind a same-named alias (#2876): apidiff
#     compares named types by package, so the moved type, every const of it and
#     every field holding it report a change although stage.X still names the
#     very same type. Signatures that mention it print identically on both sides
#     ("changed from func(X) to func(X)"), which the same rule accepts.
#     What makes the rule safe is TestInternalTypesReachableFromAPIHaveAliases
#     in runtime/pipeline/stage: it fails unless every internal type the API
#     reaches is aliased by stage under its own name, so a consumer can still
#     name it. A rename (X to internal/x.Y), a re-typed field, a removal or a
#     generic type whose type parameters print differently is still rejected.
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

# The qualifier apidiff prints for a type declared in an internal package of
# pipeline/stage, e.g. "github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core.".
STAGE_INTERNAL_QUALIFIER='github\.com/AltairaLabs/PromptKit/runtime(/v[0-9]+)?/pipeline/stage/internal/[A-Za-z0-9_/]+\.'

# alias_move_only <rest> succeeds when <rest>, the "<old> to <new>" of a
# "changed from" line, reads "T to T" once stage-internal qualifiers are gone.
# The split is found by length, not by searching for " to ", because a type
# can contain " to " itself (a struct field named "to").
alias_move_only() {
  local stripped n half
  stripped="$(printf '%s' "$1" | sed -E "s#${STAGE_INTERNAL_QUALIFIER}##g")"
  n=${#stripped}
  [ $(((n - 4) % 2)) -eq 0 ] && [ "$n" -gt 4 ] || return 1
  half=$(((n - 4) / 2))
  [ "${stripped:half:4}" = " to " ] && [ "${stripped:0:half}" = "${stripped:half+4}" ]
}

while IFS= read -r line; do
  case "$line" in
    "## incompatible changes"*) in_incompatible=1; continue ;;
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
