#!/usr/bin/env bash
#
# Fails when the lines a change adds or edits are not caught by their tests:
# diff-scoped mutation testing with gremlins (github.com/go-gremlins/gremlins).
#
# Coverage says a line ran. A mutant says whether a test noticed it was wrong:
# gremlins flips a condition (`==` to `!=`, `<` to `<=`, `&&` to `||`, `+` to
# `-`) on a changed line, reruns that package's tests, and records the mutant
# KILLED if a test failed, LIVED if every test still passed. A LIVED mutant on
# a line you wrote means no test pins what that line decides.
#
# Scope: only non-test .go files this change adds or modifies in the published
# modules (runtime, sdk, pkg, server/a2a), and within them only the changed
# lines. Each changed package is one gremlins run, with the package's own
# directory as the calling dir and files in subdirectories excluded, so a
# package is mutated by its own run only.
#
# Score: caught / (caught + lived), where caught is KILLED or TIMED OUT.
# NOT COVERED mutants are listed but not scored: Go's coverage profile has no
# block for a `switch { case cond: }` expression, so gremlins reports every
# mutant there as NOT COVERED even when tests run it. Lines no test runs are
# the coverage gate's job. NOT VIABLE mutants (ones that do not compile) are
# left out.
#
# Usage: scripts/check-mutation-diff.sh [base-ref]
#   base-ref        merge base to diff against (default origin/main). The
#                   working tree is compared, so uncommitted edits count.
#   MUTATION_THRESHOLD  minimum score in percent (default 75).
#   GREMLINS        gremlins binary (default: `gremlins` on PATH, else
#                   installed at the pinned version into a temp GOBIN).
#   MUTATION_WORKERS    parallel mutants per package (default 4; each worker
#                   copies the module, so more is not faster on a laptop).
set -Eeuo pipefail

# Every failure says why. A command that fails with no message of its own
# (a jq parse, a git plumbing call) would otherwise end the gate with only an
# exit code; -E carries this into functions and command substitutions.
# shellcheck disable=SC2154 # rc is assigned inside the trap string
trap 'rc=$?; echo "FAIL: ${BASH_SOURCE[0]##*/} line ${LINENO}: \`${BASH_COMMAND}\` exited ${rc}" >&2' ERR

unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE

GREMLINS_VERSION="v0.6.0"
BASE="${1:-origin/main}"
# 75%: measured on five merged PRs from 2026-11 (#2540, #2543, #2545, #2551,
# #2553), all written test-first against the caller's path. Four scored
# 87-96%; #2553 scored 69%, and its survivors include a real untested branch
# (fragment_path.go: the fs.ErrNotExist wrap when baseDir is empty). The gate
# sits between them, leaving room for the equivalent mutants seen there.
THRESHOLD="${MUTATION_THRESHOLD:-75}"
ROOT="$(git rev-parse --show-toplevel)"
MODULES="runtime sdk pkg server/a2a"
# A mutant's test run may take this many times the package's coverage run
# before it counts as TIMED OUT (and so caught). gremlins defaults to 3; with
# parallel workers contending for CPU that times out mutants that merely ran
# slowly, which would count them as caught when no test failed.
TIMEOUT_COEFFICIENT="${MUTATION_TIMEOUT_COEFFICIENT:-10}"

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

if [ -z "${GREMLINS:-}" ]; then
  if command -v gremlins >/dev/null 2>&1; then
    GREMLINS="$(command -v gremlins)"
  else
    env GOBIN="${tmp}/bin" go install "github.com/go-gremlins/gremlins/cmd/gremlins@${GREMLINS_VERSION}" ||
      { echo "FAIL: could not install gremlins ${GREMLINS_VERSION} (output above)" >&2; exit 1; }
    GREMLINS="${tmp}/bin/gremlins"
  fi
fi

# merge-base prints nothing when the two commits share no history (a shallow
# clone), so say what was looked for.
MERGE_BASE="$(git -C "${ROOT}" merge-base HEAD "${BASE}")" || {
  echo "FAIL: no merge base between HEAD and ${BASE}: is ${BASE} fetched, with full history (fetch-depth: 0)?" >&2
  exit 1
}

# Changed non-test Go source, minus what has no tests of its own to judge:
# testdata, nested example modules, generated code.
changed_files() {
  local mod
  for mod in ${MODULES}; do
    git -C "${ROOT}" diff --name-only --no-renames --diff-filter=AM "${MERGE_BASE}" -- "${mod}/*.go"
  done | while IFS= read -r f; do
    case "${f}" in
      *_test.go | */testdata/* | */examples/* | runtime/packspec/*) continue ;;
    esac
    [ -f "${ROOT}/${f}" ] || continue
    # The marker may sit anywhere before the package clause.
    awk '/^package /{exit} /^\/\/ Code generated .* DO NOT EDIT\.$/{g=1; exit} END{exit !g}' "${ROOT}/${f}" && continue
    echo "${f}"
  done
}

# module_of prints the published module (from MODULES) that owns dir.
module_of() {
  local dir="$1" mod best=""
  for mod in ${MODULES}; do
    case "${dir}/" in
      "${mod}/"*) [ "${#mod}" -gt "${#best}" ] && best="${mod}" ;;
    esac
  done
  echo "${best}"
}

# modfile_flags prints a GOFLAGS value for a module whose go.mod has relative
# `replace` directives. gremlins runs each mutant in a copy of the module under
# a temp dir, where `../runtime` no longer resolves, so the copy reads an
# alternate go.mod whose replaces are absolute. The tree's own go.mod is never
# written.
modfile_flags() {
  local mod="$1" dir="${tmp}/modfile/$1"
  local olds news i
  olds="$(go -C "${ROOT}/${mod}" mod edit -json | jq -r '.Replace[]? | select(.New.Path | startswith(".")) | .Old.Path')" || return 1
  [ -z "${olds}" ] && return 0
  mkdir -p "${dir}"
  cp "${ROOT}/${mod}/go.mod" "${dir}/go.mod"
  cp "${ROOT}/${mod}/go.sum" "${dir}/go.sum"
  news="$(go -C "${ROOT}/${mod}" mod edit -json | jq -r '.Replace[]? | select(.New.Path | startswith(".")) | .New.Path')" || return 1
  i=1
  for old in ${olds}; do
    new="$(echo "${news}" | sed -n "${i}p")"
    go mod edit -replace "${old}=$(cd "${ROOT}/${mod}/${new}" && pwd)" "${dir}/go.mod" || return 1
    i=$((i + 1))
  done
  echo " -modfile=${dir}/go.mod"
}

files="$(changed_files)"
if [ -z "${files}" ]; then
  echo "OK:   no changed non-test Go source in ${MODULES}; nothing to mutate."
  exit 0
fi
pkgs="$(echo "${files}" | xargs -n1 dirname | sort -u)"

start="$(date +%s)"
: > "${tmp}/mutants.tsv"
n=0
for pkg in ${pkgs}; do
  mod="$(module_of "${pkg}")"
  flags="$(modfile_flags "${mod}")" || { echo "FAIL: could not rewrite ${mod}/go.mod replaces" >&2; exit 1; }
  n=$((n + 1))
  out="${tmp}/result-${n}.json"
  log="${tmp}/gremlins-${n}.log"
  echo "---- ${pkg}"
  # Not --silent: that also hides the output of gremlins' coverage run, so a
  # failing test ended the gate with only "impossible to executeCoverage".
  # The log is printed when gremlins fails.
  args=(unleash --diff "${MERGE_BASE}" --invert-logical --exclude-files '/'
    --timeout-coefficient "${TIMEOUT_COEFFICIENT}" --output "${out}")
  args+=(--workers "${MUTATION_WORKERS:-4}")
  # diff.relative makes gremlins' own `git diff` name files relative to the
  # package dir, the same form it uses for the files it walks; without it no
  # changed line matches and every mutant is skipped. GOWORK=off makes the
  # coverage run resolve modules as the mutant runs in the copies do.
  # -count=1: gremlins times each mutant against the coverage run, and a
  # cached coverage run takes milliseconds, which times out every mutant.
  # -trimpath: each worker tests a copy of the module at its own temp path,
  # and without it the build cache keys on that path, so every worker
  # recompiles the module cold and its mutants time out (and count as caught).
  (
    cd "${ROOT}/${pkg}"
    env GOWORK=off GOFLAGS="-count=1 -trimpath${flags}" \
      GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=diff.relative GIT_CONFIG_VALUE_0=true \
      "${GREMLINS}" "${args[@]}" "${ROOT}/${pkg}"
  ) > "${log}" 2>&1 || {
    # Its coverage run tests ./... under the package dir with GOWORK=off and
    # -trimpath, so a test can fail here that passes in the Test job.
    echo "FAIL: gremlins failed on ${pkg}; its output (the last 200 lines):" >&2
    tail -n 200 "${log}" >&2
    exit 1
  }
  # gremlins writes no report for a package with no mutable tokens on any line.
  [ -f "${out}" ] || continue
  jq -r --arg pkg "${pkg}" '.files[]? as $f | $f.mutations[]?
    | "\(.status)\t\($pkg)/\($f.file_name):\(.line):\(.column)\t\(.type)"' "${out}" >> "${tmp}/mutants.tsv" || {
    echo "FAIL: could not read the gremlins report for ${pkg} (${out}); gremlins output:" >&2
    tail -n 200 "${log}" >&2
    exit 1
  }
done
elapsed=$(($(date +%s) - start))

count() { awk -F'\t' -v s="$1" '$1 == s' "${tmp}/mutants.tsv" | wc -l | tr -d ' '; }
timedout="$(count "TIMED OUT")"
killed=$(($(count KILLED) + timedout))
lived="$(count LIVED)"
uncovered="$(count "NOT COVERED")"
judged=$((killed + lived))

echo ""
awk -F'\t' '$1 == "LIVED" || $1 == "NOT COVERED" { printf "%-12s %s\t%s\n", $1, $2, $3 }' "${tmp}/mutants.tsv" | sort -k2
echo ""
echo "packages: $(echo "${pkgs}" | wc -l | tr -d ' ')  mutants: caught ${killed} (${timedout} timed out), lived ${lived}, not covered ${uncovered} (not scored)  (${elapsed}s)"

if [ "${judged}" -eq 0 ]; then
  if [ "${uncovered}" -gt 0 ]; then
    echo "OK:   no tested mutants; ${uncovered} on lines no test runs are left to the coverage gate."
  else
    echo "OK:   no mutable changed lines (no conditions or arithmetic to flip)."
  fi
  exit 0
fi
# A TIMED OUT mutant counts as caught (a test hung on it). Many of them means
# the runs were starved of CPU, not that tests caught anything, and the score
# would be inflated.
if [ $((timedout * 4)) -gt "${judged}" ]; then
  echo "FAIL: ${timedout} of ${judged} mutants timed out, so the score is unreliable."
  echo "Rerun with fewer workers: MUTATION_WORKERS=2 make check-mutation"
  exit 1
fi
score=$((killed * 100 / judged))
# One survivor never fails the gate on its own: some mutants are equivalent
# (`err == nil || !isX(err)` to `&&` changes nothing when isX(nil) is false),
# and on a small change one of them alone would sink the percentage.
if [ "${score}" -lt "${THRESHOLD}" ] && [ "${lived}" -gt 1 ]; then
  echo "FAIL: ${score}% of mutants on changed lines were caught; the gate is ${THRESHOLD}%."
  echo ""
  echo "Each LIVED line above is a condition or expression this change added that"
  echo "can be flipped with every test still green. Add a test that asserts the"
  echo "value a caller sees when that decision goes the other way, then rerun:"
  echo "make check-mutation"
  exit 1
fi
echo "OK:   ${score}% of mutants on changed lines were caught (gate ${THRESHOLD}%)."
