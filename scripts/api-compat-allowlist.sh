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
# compiling. gorelease also counts two changes as incompatible that do not do
# that in practice, and this is the whole list:
#
#   - "<T>: old is comparable, new is not". A struct gained a slice, map or func
#     field, so `a == b` and map[T] no longer compile. That happens whenever a
#     protocol type grows a list or an extension map (MCP 2026-07-28 did, #2106),
#     and these are data types callers decode into, not types they compare.
#   - "<C>: value changed from X to Y". A constant's value moved, e.g.
#     mcp.ProtocolVersion advancing to the newest revision. Code still compiles;
#     tracking the newest value is what such a constant is for.
#
# Anything else under "incompatible changes" — removed, renamed or re-typed API
# — still fails the release. Add a pattern here only with the same argument:
# the change cannot stop a plausible consumer from compiling.
#
# Usage: gorelease ... | scripts/api-compat-allowlist.sh
set -euo pipefail

accepted=()
rejected=()
in_incompatible=0

while IFS= read -r line; do
  case "$line" in
    "## incompatible changes"*) in_incompatible=1; continue ;;
    "#"*|"") in_incompatible=0; continue ;;
  esac
  [ "$in_incompatible" -eq 1 ] || continue

  if [[ "$line" =~ ^[A-Za-z0-9_.]+:\ old\ is\ comparable,\ new\ is\ not$ ]] ||
     [[ "$line" =~ ^[A-Za-z0-9_.]+:\ value\ changed\ from\ .+\ to\ .+$ ]]; then
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
