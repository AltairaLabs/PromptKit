#!/usr/bin/env bash
# Compare head throughput against a baseline measured on the same runner.
#
#   perf-compare.sh <min-pct> <sanity-floor> "<base rps ...>" "<head rps ...>"
#
# Takes the median of each list and fails (exit 1) when head < <min-pct>% of
# base, or when head is under the absolute <sanity-floor> (which catches a
# broken build even if the base is equally slow). Used by the CI
# "Performance Regression Check" job, which interleaves base and head runs so
# runner noise hits both sides alike.
set -euo pipefail

if [ "$#" -ne 4 ]; then
  echo "usage: $0 <min-pct> <sanity-floor> \"<base rps...>\" \"<head rps...>\"" >&2
  exit 2
fi
MIN_PCT=$1
SANITY=$2
BASE_LIST=$3
HEAD_LIST=$4

median() {
  # shellcheck disable=SC2086
  printf '%s\n' $1 | sort -g | awk '{a[NR]=$1} END {
    if (NR == 0) { print "nan"; exit }
    if (NR % 2) print a[(NR+1)/2]; else printf "%.2f\n", (a[NR/2]+a[NR/2+1])/2 }'
}

BASE=$(median "$BASE_LIST")
HEAD=$(median "$HEAD_LIST")
if [ "$BASE" = nan ] || [ "$HEAD" = nan ]; then
  echo "::error::No throughput samples (base: '${BASE_LIST}', head: '${HEAD_LIST}')"
  exit 1
fi

RATIO=$(awk -v h="$HEAD" -v b="$BASE" 'BEGIN { printf "%.1f", 100*h/b }')
echo "Throughput (median): base ${BASE} req/s [${BASE_LIST}], head ${HEAD} req/s [${HEAD_LIST}] = ${RATIO}% of base"

if awk -v h="$HEAD" -v f="$SANITY" 'BEGIN { exit !(h < f) }'; then
  echo "::error::Throughput ${HEAD} req/s is under the absolute sanity floor of ${SANITY} req/s"
  exit 1
fi
if awk -v r="$RATIO" -v m="$MIN_PCT" 'BEGIN { exit !(r < m) }'; then
  echo "::error::Throughput regression: head ${HEAD} req/s is ${RATIO}% of base ${BASE} req/s (minimum ${MIN_PCT}%)"
  exit 1
fi
echo "Throughput within ${MIN_PCT}% of base"
