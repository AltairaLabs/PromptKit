#!/usr/bin/env bash
#
# Fail a change that breaks the exported API of runtime/pipeline/stage, or of
# the packages that expose its types.
#
# pipeline/stage is being split into stage/internal/<name> packages behind
# same-named aliases (#2876), one move per PR. Two passes:
#
# 1. apidiff (the analysis gorelease runs) on each package in PACKAGES between
#    the old and the new tree, judged by the release's allowlist,
#    scripts/api-compat-allowlist.sh, which accepts the lines a pure move
#    produces ("X: changed from X to .../stage/internal/core.X").
#
# 2. What pass 1 cannot see. apidiff treats a type from another package as
#    opaque, so once stage.X is an alias of internal/core.X it compares only
#    the type's identity: a move that drops a field, and any later change to
#    core.X's fields, methods or underlying type, report nothing (gorelease
#    skips internal packages altogether). So for every alias X = <pkg>.X into
#    stage/internal in the new tree, X's new declaration is compared with its
#    old one, wherever that lived: stage itself before the move, or the
#    internal package it was aliased from. An incompatible change to X, its
#    fields or its methods fails, unless it is itself only a move.
#
# The PR job (ci.yml, "Stage API Compatibility") runs this against the merge
# base; the release gate (scripts/check-api-compatibility.sh) runs pass 2
# against the previous release.
#
# Usage: scripts/check-stage-api.sh [base-ref]      (default origin/main)
#
# Environment:
#   STAGE_API_OLD_ROOT, STAGE_API_NEW_ROOT  repo roots (holding runtime/, sdk/,
#       ...) to compare instead of the merge base and this checkout
#   STAGE_API_PACKAGES  space-separated "<module dir>|<import path>" entries
#       for pass 1, replacing the default set below
set -euo pipefail
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE

BASE="${1:-origin/main}"
# The checkout this script lives in, not the caller's working directory: run
# from another worktree, the allowlist and tree under test must be this one's.
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ALLOW="${ROOT}/scripts/api-compat-allowlist.sh"
APIDIFF="golang.org/x/exp/cmd/apidiff@v0.0.0-20261109185158-a52e3aef5920"
RT="github.com/AltairaLabs/PromptKit/runtime/v2"
SDK="github.com/AltairaLabs/PromptKit/sdk/v2"
STAGE="${RT}/pipeline/stage"
# stage itself, and every published package whose API carries a stage type.
DEFAULT_PACKAGES="runtime|${STAGE} runtime|${RT}/streaming sdk|${SDK} sdk|${SDK}/session"
read -r -a PACKAGES <<<"${STAGE_API_PACKAGES:-${DEFAULT_PACKAGES}}"

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

OLD="${STAGE_API_OLD_ROOT:-}"
NEW="${STAGE_API_NEW_ROOT:-${ROOT}}"
if [ -z "${OLD}" ]; then
  merge_base="$(git -C "${ROOT}" merge-base "${BASE}" HEAD)"
  echo "Comparing with ${merge_base} (merge base of ${BASE})."
  mkdir -p "${tmp}/base"
  # sdk's go.mod replaces its siblings with ../runtime, ../pkg, ../server/a2a.
  git -C "${ROOT}" archive "${merge_base}" runtime pkg sdk server/a2a | tar -x -C "${tmp}/base"
  OLD="${tmp}/base"
fi

# Each side resolves its siblings through its own go.mod replaces; GOWORK=off
# keeps this checkout's workspace from mixing the two.
export GOWORK=off

# export_data <root> <module dir> <import path>: writes the package's export
# data once per side and prints the file. apidiff -w must run in the module
# that holds the package, or it resolves the path through the module cache.
export_data() {
  local out
  out="${tmp}/$(printf '%s|%s' "$1" "$3" | cksum | cut -d' ' -f1).export"
  [ -f "${out}" ] || (cd "$1/$2" && go run "${APIDIFF}" -allow-internal -w "${out}" "$3")
  echo "${out}"
}

# judge <package> <report>: prints the apidiff report and the allowlist's
# verdict on it; fails when the allowlist does.
judge() {
  local verdict
  echo "$2" | sed 's/^/   /'
  # apidiff lists "- <change>"; the allowlist reads gorelease's shape.
  if verdict="$(printf '# %s\n## incompatible changes\n%s\n' "$1" "$(echo "$2" | sed -n 's/^- //p')" |
    "${ALLOW}")"; then
    echo "${verdict}" | sed 's/^/   /'
    return 0
  fi
  echo "${verdict}" | sed 's/^/   /'
  return 1
}

failed=0

echo "Pass 1: the exported API"
for entry in "${PACKAGES[@]}"; do
  mod="${entry%%|*}" pkg="${entry#*|}"
  echo "── ${pkg}"
  if ! (cd "${OLD}/${mod}" && go list "${pkg}" >/dev/null 2>&1); then
    echo "   new package; nothing to compare"
    continue
  fi
  old_export="$(export_data "${OLD}" "${mod}" "${pkg}")"
  new_export="$(export_data "${NEW}" "${mod}" "${pkg}")"
  report="$(cd "${NEW}/${mod}" && go run "${APIDIFF}" -incompatible "${old_export}" "${new_export}")"
  if [ -z "${report}" ]; then
    echo "   ✓ no incompatible changes"
  elif judge "${pkg}" "${report}"; then
    echo "   ✓ only allowlisted incompatibilities"
  else
    failed=1
  fi
done

# internal_paths <root>: "<package name> <import path>" for every package
# under stage/internal in that tree.
internal_paths() {
  (cd "$1/runtime" && go list -f '{{.Name}} {{.ImportPath}}' "${STAGE}/internal/..." 2>/dev/null) || true
}

# decls <root>: stage's exported type declarations, one per line, as go doc
# prints them ("type X = core.X", "type Y struct{ ... }").
decls() { (cd "$1/runtime" && go doc -short "${STAGE}" 2>/dev/null | grep '^type ') || true; }

# resolve <package name> <paths>: the import paths under stage/internal named so.
resolve() { echo "$2" | awk -v n="$1" '$1 == n { print $2 }'; }

echo
echo "Pass 2: the declarations behind stage's aliases of stage/internal types"
new_internal="$(internal_paths "${NEW}")"
old_internal="$(internal_paths "${OLD}")"
old_decls="$(decls "${OLD}")"
# pairs: "<old import path> <new import path> <type>" per aliased type.
pairs=""
while read -r _ name eq target; do
  [ "${eq}" = "=" ] || continue
  qual="${target%%.*}" tname="${target#*.}"
  [ "${tname}" = "${name}" ] || continue
  for new_path in $(resolve "${qual}" "${new_internal}"); do
    old_line="$(echo "${old_decls}" | awk -v n="${name}" '$2 == n' | head -1)"
    if [ -z "${old_line}" ]; then
      continue # a type new in this change: nothing to compare it with
    fi
    read -r _ _ old_eq old_target <<<"${old_line}"
    if [ "${old_eq}" = "=" ]; then
      old_paths="$(resolve "${old_target%%.*}" "${old_internal}")"
    else
      old_paths="${STAGE}" # declared in stage itself before the move
    fi
    for old_path in ${old_paths}; do
      pairs+="${old_path} ${new_path} ${name}"$'\n'
    done
  done
done <<<"$(decls "${NEW}")"

if [ -z "${pairs}" ]; then
  echo "   no aliases of stage/internal types to compare"
fi
for pair in $(echo "${pairs}" | awk 'NF { print $1 "|" $2 }' | sort -u); do
  old_path="${pair%%|*}" new_path="${pair#*|}"
  names="$(echo "${pairs}" | awk -v o="${old_path}" -v n="${new_path}" '$1 == o && $2 == n { print $3 }')"
  echo "── ${old_path} → ${new_path}: $(echo "${names}" | tr '\n' ' ')"
  old_export="$(export_data "${OLD}" runtime "${old_path}")"
  new_export="$(export_data "${NEW}" runtime "${new_path}")"
  report="$(cd "${NEW}/runtime" && go run "${APIDIFF}" -allow-internal -incompatible "${old_export}" "${new_export}")"
  # Only the aliased types and their fields and methods; anything else the two
  # packages hold is not stage's API ("Holder: removed" just means Holder did
  # not move there).
  report="$(echo "${report}" | awk -v names="$(echo "${names}" | tr '\n' ' ')" '
    BEGIN { n = split(names, a, " "); for (i = 1; i <= n; i++) keep[a[i]] = 1 }
    /^- / { s = substr($0, 3); sub(/^\(\*?/, "", s); match(s, /^[A-Za-z0-9_]+/)
            if (substr(s, RSTART, RLENGTH) in keep) print }')"
  if [ -z "${report}" ]; then
    echo "   ✓ unchanged"
  elif judge "${STAGE}" "${report}"; then
    echo "   ✓ only allowlisted incompatibilities"
  else
    failed=1
  fi
done

if [ "${failed}" -ne 0 ]; then
  echo "::error::This change breaks the exported API of runtime/pipeline/stage (listed above)."
  echo "::error::A type moved into stage/internal keeps its fields, methods and underlying"
  echo "::error::type and gets a same-named alias in stage (type X = core.X); a function"
  echo "::error::gets a wrapper func and a sentinel error a var that re-exports it. See #2876."
  exit 1
fi
