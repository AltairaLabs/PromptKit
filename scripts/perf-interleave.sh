#!/usr/bin/env bash
# Throughput check relative to a baseline measured on the same runner.
#
#   perf-interleave.sh <label> <port> <abs-floor> [server flags...]
#
# Runs bin/bench-promptkit-base and bin/bench-promptkit alternately
# (base, head, base, head, ...) so runner drift hits both sides alike, then
# fails if head's median throughput is under PERF_MIN_PCT (default 85) percent
# of base's, or under PERF_SANITY_RPS (default 600) outright.
#
# When the base binary is missing or will not run (say the benchmarks did not
# exist at the base revision) the check falls back to the absolute
# <abs-floor> and says so. Worst-case head figures go to $PERF_HEAD_OUT
# (default /tmp/perf-head.txt) as "<rss> <cpu> <started> <dropped> <goroutines> <errors>", so
# the caller can apply its own ceilings.
#
# Env: ROUNDS (default 2), BASE_BIN, HEAD_BIN, EVENT_STORE_WAIT (see perf-run.sh).
set -uo pipefail

LABEL=$1 PORT=$2 ABS_FLOOR=$3
shift 3
ROUNDS=${ROUNDS:-2}
BASE_BIN=${BASE_BIN:-bin/bench-promptkit-base}
HEAD_BIN=${HEAD_BIN:-bin/bench-promptkit}
MIN_PCT=${PERF_MIN_PCT:-85}
SANITY=${PERF_SANITY_RPS:-600}
OUT=${PERF_HEAD_OUT:-/tmp/perf-head.txt}
HERE=$(dirname "$0")

# perf-run.sh stops its own server; make a signal here reach it promptly.
trap 'exit 130' INT
trap 'exit 143' TERM

BASE_RPS="" HEAD_RPS="" HEAD_LINES=""
use_base=0
[ -x "$BASE_BIN" ] && use_base=1

for i in $(seq 1 "$ROUNDS"); do
  if [ "$use_base" = 1 ]; then
    if line=$("$HERE/perf-run.sh" "$BASE_BIN" "$LABEL" "$PORT" "$@" | grep '^RESULT'); then
      rps=$(echo "$line" | awk '{print $2}')
      if awk -v r="$rps" 'BEGIN { exit !(r > 0) }'; then
        BASE_RPS="$BASE_RPS $rps"
        echo "round $i base: $rps req/s"
      else
        use_base=0
      fi
    else
      use_base=0
    fi
    [ "$use_base" = 0 ] && echo "::warning::Base binary did not produce a result; falling back to the absolute floor"
  fi
  line=$("$HERE/perf-run.sh" "$HEAD_BIN" "$LABEL" "$PORT" "$@" | grep '^RESULT') || {
    echo "::error::Head benchmark run failed"
    exit 1
  }
  HEAD_LINES="$HEAD_LINES
$line"
  HEAD_RPS="$HEAD_RPS $(echo "$line" | awk '{print $2}')"
  echo "round $i head: $(echo "$line" | awk '{print $2}') req/s"
done

# Worst case across head runs: highest RSS/CPU/dropped/goroutines/errors,
# fewest started pipelines.
echo "$HEAD_LINES" | awk 'NF {
  if ($3 != "-" && $3 > rss) rss = $3
  if ($4 != "-" && $4 > cpu) cpu = $4
  if ($5 != "-" && $5 > d) d = $5
  if ($6 != "-" && $6 > g) g = $6
  if ($7 != "-" && (s == "" || $7 < s)) s = $7
  if ($8 > e) e = $8
} END { print rss+0, cpu+0, (s == "" ? "-" : s), d+0, g+0, e+0 }' > "$OUT"
echo "Worst-case head run (rss MB, cpu %, started, dropped, goroutines, request errors): $(cat "$OUT")"

if [ "$use_base" = 1 ]; then
  "$HERE/perf-compare.sh" "$MIN_PCT" "$SANITY" "$BASE_RPS" "$HEAD_RPS"
else
  echo "No usable baseline: checking the absolute floor of ${ABS_FLOOR} req/s instead"
  "$HERE/perf-compare.sh" 100 "$ABS_FLOOR" "$ABS_FLOOR" "$HEAD_RPS"
fi
