#!/usr/bin/env bash
# One benchmark run for the CI "Performance Regression Check" job.
#
#   perf-run.sh <server-binary> <label> <port> [server flags...]
#
# Starts the server (the mock upstream must already be listening on :8081),
# drives it with the harness, stops it, and prints one line on stdout:
#
#   RESULT <req/s> <rss MB> <cpu %> <dropped> <goroutines> <started> <errors>
#
# <dropped> is pipeline.started - (completed + failed): events that never
# arrived. <goroutines> and <dropped> are only filled in with EVENT_STORE_WAIT=1
# (else "-"), which waits for every started pipeline's terminal event to reach
# the store and the goroutines to drain. <errors> is the harness's count of
# failed requests (e.g. refused or reset at TCP, which never start a pipeline). Everything else goes to stderr.
set -euo pipefail

BIN=$1 LABEL=$2 PORT=$3
shift 3
REQUESTS=${REQUESTS:-5000}
CONCURRENCY=${CONCURRENCY:-500}
PACK=${PACK:-benchmarks/frameworks/promptkit/round1/chat.pack.json}

OPENAI_BASE_URL=http://localhost:8081/v1 OPENAI_API_KEY=bench \
  "$BIN" --pack "$PACK" --port "$PORT" "$@" >&2 &
PK_PID=$!
# Stop the server on every way out: normal exit, failure under set -e, a
# signal. Without the signal traps an interrupted run left it listening.
cleanup() {
  kill "$PK_PID" 2>/dev/null || true
  wait "$PK_PID" 2>/dev/null || true
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
sleep 1
curl -sf "http://localhost:${PORT}/health" > /dev/null

RESULT=$(bin/harness --round round1 --target "http://localhost:${PORT}" \
  --framework "$LABEL" --concurrency "$CONCURRENCY" --requests "$REQUESTS" \
  --framework-pid "$PK_PID" --output "/tmp/bench-results-${LABEL}" 2>&1)
echo "$RESULT" >&2

TABLE_ROW=$(echo "$RESULT" | grep "^| ${LABEL} " | head -1)
RPS=$(echo "$TABLE_ROW" | grep -oE '[0-9.]+ req/s' | awk '{print $1}')
RSS=$(echo "$TABLE_ROW" | grep -oE '[0-9.]+ MB' | awk '{print $1}')
COST_ROW=$(echo "$RESULT" | grep "^| ${LABEL}.*%" | head -1)
CPU=$(echo "$COST_ROW" | grep -oE '[0-9.]+%' | head -1 | tr -d '%')

ERRORS=$(echo "$RESULT" | sed -n 's/^Errors: \([0-9]*\) of.*/\1/p' | head -1)
DROPPED=- GOROUTINES=- STARTED=-
if [ "${EVENT_STORE_WAIT:-0}" = 1 ]; then
  # The server closes each conversation after its response, so the last
  # closes can still be running when the harness exits: poll until every
  # started pipeline has its completed/failed event and the goroutines have
  # drained.
  for _ in $(seq 1 20); do
    # A failed poll counts as "not there yet" rather than ending the run.
    STATS=$(curl -sf "http://localhost:${PORT}/stats" || echo '{}')
    STARTED=$(echo "$STATS" | jq '.by_type["pipeline.started"] // 0')
    DONE=$(echo "$STATS" | jq '(.by_type["pipeline.completed"] // 0) + (.by_type["pipeline.failed"] // 0)')
    GOROUTINES=$(echo "$STATS" | jq '.goroutines // 999999')
    DROPPED=$((STARTED - DONE))
    if [ "$DROPPED" -le 0 ] && [ "$GOROUTINES" -lt 2000 ]; then
      break
    fi
    sleep 0.5
  done
fi

cleanup
echo "RESULT ${RPS:-0} ${RSS:--} ${CPU:--} ${DROPPED} ${GOROUTINES} ${STARTED} ${ERRORS:-0}"
