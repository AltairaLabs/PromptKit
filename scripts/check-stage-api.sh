#!/usr/bin/env bash
#
# Fail a pull request that breaks the exported API of runtime/pipeline/stage.
#
# The release gate (scripts/check-api-compatibility.sh) runs gorelease once per
# release, against the previous tag. pipeline/stage is being split into
# internal packages behind aliases (#2876), one move per PR, and a move that
# drops an alias or re-types a field would otherwise be found at release time,
# after several more moves had landed on top of it. This runs the same
# comparison on every PR that touches the package: apidiff (the analysis
# gorelease runs) between the merge base and the working tree, judged by the
# same allowlist the release uses, scripts/api-compat-allowlist.sh.
#
# Usage: scripts/check-stage-api.sh [base-ref]      (default origin/main)
#
# STAGE_API_OLD_RUNTIME / STAGE_API_NEW_RUNTIME name two runtime module
# directories to compare instead of the merge base and the working tree, for
# trying a move out in a scratch copy before making it.
set -euo pipefail
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE

BASE="${1:-origin/main}"
# The checkout this script lives in, not the caller's working directory: run
# from another worktree, the allowlist and tree under test must be this one's.
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
APIDIFF="golang.org/x/exp/cmd/apidiff@v0.0.0-20261109185158-a52e3aef5920"
MODULE="github.com/AltairaLabs/PromptKit/runtime/v2"
PKGS=("${MODULE}/pipeline/stage")

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

OLD="${STAGE_API_OLD_RUNTIME:-}"
NEW="${STAGE_API_NEW_RUNTIME:-${ROOT}/runtime}"
if [ -z "${OLD}" ]; then
  merge_base="$(git -C "${ROOT}" merge-base "${BASE}" HEAD)"
  echo "Comparing the pipeline/stage API with ${merge_base} (merge base of ${BASE})."
  mkdir -p "${tmp}/base"
  git -C "${ROOT}" archive "${merge_base}" runtime | tar -x -C "${tmp}/base"
  OLD="${tmp}/base/runtime"
fi

# The runtime module is a leaf (scripts/check-runtime-is-leaf.sh), so each side
# builds on its own; GOWORK=off keeps the workspace from mixing the two.
export GOWORK=off

# apidiff -w must run in the module that holds the package, or it resolves the
# import path through the module cache instead of the tree being compared.
export_data() { (cd "$1" && go run "${APIDIFF}" -w "$2" "$3"); }

failed=0
for pkg in "${PKGS[@]}"; do
  echo "── ${pkg}"
  if [ ! -d "${OLD}/${pkg#"${MODULE}"/}" ]; then
    echo "   new package; nothing to compare"
    continue
  fi
  export_data "${OLD}" "${tmp}/old.export" "${pkg}"
  export_data "${NEW}" "${tmp}/new.export" "${pkg}"
  report="$(cd "${NEW}" && go run "${APIDIFF}" -incompatible "${tmp}/old.export" "${tmp}/new.export")"
  if [ -z "${report}" ]; then
    echo "   ✓ no incompatible changes"
    continue
  fi
  echo "${report}" | sed 's/^/   /'
  # apidiff lists "- <change>" under "Incompatible changes:"; the allowlist
  # reads gorelease's shape of the same messages.
  if verdict="$({
      echo "# ${pkg}"
      echo "## incompatible changes"
      echo "${report}" | sed -n 's/^- //p'
    } | "${ROOT}/scripts/api-compat-allowlist.sh")"; then
    echo "${verdict}" | sed 's/^/   /'
    echo "   ✓ only allowlisted incompatibilities"
  else
    echo "${verdict}" | sed 's/^/   /'
    failed=1
  fi
done

if [ "${failed}" -ne 0 ]; then
  echo "::error::This PR breaks the exported API of runtime/pipeline/stage (listed above)."
  echo "::error::A type moved into stage/internal needs a same-named alias in stage"
  echo "::error::(type X = core.X), a function a wrapper func, and a sentinel"
  echo "::error::error a var that re-exports it. See #2876."
  exit 1
fi
