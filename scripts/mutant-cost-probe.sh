#!/usr/bin/env bash
#
# Estimate what one Mutation Gate mutant of a package costs.
#
# gremlins tests every mutant in its own copy of the module: it changes one
# token in one file, then runs that package's tests, which recompiles the
# package, everything in it that the tests link, vets it and links the test
# binary before a single test runs. For runtime/pipeline/stage that build is
# most of the ~23 CPU-seconds a mutant costs (#2876), and splitting the package
# is meant to shrink it. This measures it, so a split can be judged by numbers
# rather than by line counts.
#
# The probe copies the module to a temp dir (the tree is never written), warms
# the build cache once, then for each sample appends a distinct no-op line to
# one non-test file of the package, which invalidates exactly what a mutant
# does, and times:
#   build   go test -count=1 -run '^$' -trimpath ./PKG   (compile + vet + link)
#   full    go test -count=1 -trimpath ./PKG             (the same, then every test)
# with the flags check-mutation-diff.sh gives gremlins (GOWORK=off, -trimpath,
# PROMPTKIT_MUTATION_RUN). Wall and CPU (user+sys) seconds are printed per
# sample, then their means and the effective wall per mutant on a runner:
#   max(full wall / workers, full CPU / vCPUs)
# that is, either the workers' own latency or the runner's CPU is the limit.
#
# Usage: scripts/mutant-cost-probe.sh MODULE PKG [FILE]
#   MODULE   module dir relative to the repo root (runtime, sdk, pkg, server/a2a)
#   PKG      package dir relative to the module (./pipeline/stage)
#   FILE     the file to append to, relative to PKG (default: its first
#            non-test .go file)
# Environment:
#   PROBE_SAMPLES  samples per measurement (default 3)
#   PROBE_WORKERS  gremlins workers to estimate for (default 4, the CI value)
#   PROBE_VCPUS    runner vCPUs to estimate for (default 4, ubuntu-latest)
#
# Example: scripts/mutant-cost-probe.sh runtime ./pipeline/stage
set -euo pipefail
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE

MODULE="${1:?usage: $0 MODULE PKG [FILE]}"
PKG="${2:?usage: $0 MODULE PKG [FILE]}"
FILE="${3:-}"
SAMPLES="${PROBE_SAMPLES:-3}"
WORKERS="${PROBE_WORKERS:-4}"
VCPUS="${PROBE_VCPUS:-4}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

# The copy sits at the same depth below the temp dir as the module below the
# repo root, and its relative `replace` directives (sdk -> ../runtime) are made
# absolute, as check-mutation-diff.sh does for gremlins' copies.
copy="${tmp}/${MODULE}"
mkdir -p "$(dirname "${copy}")"
cp -R "${ROOT}/${MODULE}" "${copy}"
for old in $(go -C "${copy}" mod edit -json | jq -r '.Replace[]? | select(.New.Path | startswith(".")) | .Old.Path'); do
  new="$(go -C "${copy}" mod edit -json | jq -r --arg o "${old}" '.Replace[] | select(.Old.Path == $o) | .New.Path')"
  go -C "${copy}" mod edit -replace "${old}=$(cd "${ROOT}/${MODULE}/${new}" && pwd)"
done

dir="${copy}/${PKG#./}"
[ -d "${dir}" ] || { echo "no such package dir: ${MODULE}/${PKG#./}" >&2; exit 1; }
if [ -z "${FILE}" ]; then
  for f in "${dir}"/*.go; do
    case "${f}" in *_test.go) continue ;; esac
    FILE="$(basename "${f}")"
    break
  done
fi
target="${dir}/${FILE}"
[ -f "${target}" ] || { echo "no such file: ${PKG}/${FILE}" >&2; exit 1; }

export GOWORK=off PROMPTKIT_MUTATION_RUN=1

# measure <label> <go test args...>: prints "wall cpu" in seconds. Bash's own
# `time` counts the CPU of every process the go command waits for (compile,
# vet, link, the test binary), which is what a runner pays for.
measure() {
  local label="$1" TIMEFORMAT='%R %U %S'
  shift
  if ! { time go -C "${copy}" test "$@" "${PKG}" >"${tmp}/${label}.log" 2>&1; } 2>"${tmp}/${label}.time"; then
    echo "go test $* ${PKG} failed:" >&2
    tail -n 40 "${tmp}/${label}.log" >&2
    exit 1
  fi
  awk '{ printf "%.2f %.2f\n", $1, $2 + $3 }' "${tmp}/${label}.time"
}

mutate() { printf '\nvar _ = %d // mutant-cost-probe\n' "$1" >>"${target}"; }

echo "Probing ${MODULE}/${PKG#./} (mutating ${FILE}), ${SAMPLES} samples per measurement."
echo "Warming the build cache..."
measure warm -count=1 -run '^$' -trimpath >/dev/null

printf '%-8s %-6s %8s %8s\n' sample kind wall_s cpu_s
n=0
build_w=0 build_c=0 full_w=0 full_c=0
for i in $(seq 1 "${SAMPLES}"); do
  n=$((n + 1)); mutate "${n}"
  read -r w c < <(measure "build-${i}" -count=1 -run '^$' -trimpath)
  printf '%-8s %-6s %8s %8s\n' "${i}" build "${w}" "${c}"
  build_w="$(awk -v a="${build_w}" -v b="${w}" 'BEGIN { print a + b }')"
  build_c="$(awk -v a="${build_c}" -v b="${c}" 'BEGIN { print a + b }')"

  n=$((n + 1)); mutate "${n}"
  read -r w c < <(measure "full-${i}" -count=1 -trimpath)
  printf '%-8s %-6s %8s %8s\n' "${i}" full "${w}" "${c}"
  full_w="$(awk -v a="${full_w}" -v b="${w}" 'BEGIN { print a + b }')"
  full_c="$(awk -v a="${full_c}" -v b="${c}" 'BEGIN { print a + b }')"
done

awk -v s="${SAMPLES}" -v bw="${build_w}" -v bc="${build_c}" -v fw="${full_w}" -v fc="${full_c}" \
  -v w="${WORKERS}" -v v="${VCPUS}" 'BEGIN {
  bw /= s; bc /= s; fw /= s; fc /= s
  eff = fw / w; if (fc / v > eff) eff = fc / v
  printf "\nmean per mutant:  build %.2fs wall / %.2fs CPU,  full %.2fs wall / %.2fs CPU\n", bw, bc, fw, fc
  printf "tests alone:      %.2fs wall / %.2fs CPU\n", fw - bw, fc - bc
  printf "effective wall per mutant with %d workers on %d vCPUs: %.2fs (%d mutants in 30 min)\n",
    w, v, eff, 1800 / eff
}'
