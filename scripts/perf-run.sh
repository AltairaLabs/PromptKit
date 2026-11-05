#!/usr/bin/env bash
# One benchmark run for the CI "Performance Regression Check" job.
#
#   perf-run.sh <server-binary> <label> <port> [server flags...]
#
# Starts the server (the mock upstream must already be listening on :8081),
# drives it with the harness, stops it, and prints one line on stdout:
#
#   RESULT <req/s> <rss MB> <cpu %> <pipeline.completed> <goroutines>
#
# The last two are only filled in with EVENT_STORE_WAIT=1 (else "-"), which
# waits for every turn's pipeline.completed to reach the store and the
# goroutines to drain. Everything else goes to stderr.
set -euo pipefail

BIN=$1 LABEL=$2 PORT=$3
shift 3
REQUESTS=${REQUESTS:-5000}
CONCURRENCY=${CONCURRENCY:-500}
PACK=${PACK:-benchmarks/frameworks/promptkit/round1/chat.pack.json}

OPENAI_BASE_URL=http://localhost:8081/v1 OPENAI_API_KEY=bench \
  "$BIN" --pack "$PACK" --port "$PORT" "$@" >&2 &
PK_PID=$!
trap 'kill "$PK_PID" 2>/dev/null || true' EXIT
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

COMPLETED=- GOROUTINES=-
if [ "${EVENT_STORE_WAIT:-0}" = 1 ]; then
  # The server closes each conversation after its response, so the last
  # closes can still be running when the harness exits: poll until every
  # turn's pipeline.completed has arrived and the goroutines have drained.
  for _ in $(seq 1 20); do
    # A failed poll counts as "not there yet" rather than ending the run.
    STATS=$(curl -sf "http://localhost:${PORT}/stats" || echo '{}')
    COMPLETED=$(echo "$STATS" | jq '.by_type["pipeline.completed"] // 0')
    GOROUTINES=$(echo "$STATS" | jq '.goroutines // 999999')
    if [ "$COMPLETED" -ge "$REQUESTS" ] && [ "$GOROUTINES" -lt 2000 ]; then
      break
    fi
    sleep 0.5
  done
fi

kill "$PK_PID" 2>/dev/null || true
wait "$PK_PID" 2>/dev/null || true
echo "RESULT ${RPS:-0} ${RSS:--} ${CPU:--} ${COMPLETED} ${GOROUTINES}"
