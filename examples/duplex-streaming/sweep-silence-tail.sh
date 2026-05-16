#!/usr/bin/env bash
#
# sweep-silence-tail.sh — sweep the silence-tail duration we send to a
# duplex provider after the user's TTS audio ends, and tabulate how the
# provider's behaviour changes with each value. Used to find the
# minimum-acceptable silence tail for a given provider/model.
#
# Why: provider-native turn detection (Gemini ASM, OpenAI Realtime
# server VAD) decides end-of-user-speech from content silence in the
# audio stream. Too short and the provider keeps waiting (or returns
# nothing within our timeout). Too long and the perceived inter-turn
# gap is bigger than it needs to be. The sweet spot is provider-
# specific and not documented.
#
# What this measures, per silence value:
#   - whether all turns completed (binary success per run)
#   - per-turn provider latency (DuplexProviderStage's "latencyMs")
#   - per-turn generationComplete -> turnComplete gap (the
#     post-generation idle gap the provider takes before declaring
#     turn-end)
#
# Usage:
#   cd examples/duplex-streaming
#   ./sweep-silence-tail.sh [scenario] [provider] [silence-ms-list...]
#
# Defaults sweep duplex-scripted-text-openai against gemini-2-flash for
# 250 500 1000 1500 2000 ms. Pass any other space-separated list of ms
# values to override.
#
# Requires .env with provider credentials in this directory.

set -u

SCENARIO="${1:-duplex-scripted-text-openai}"
PROVIDER="${2:-gemini-2-flash}"
shift 2 2>/dev/null || true
if [[ $# -gt 0 ]]; then
    SILENCES=("$@")
else
    SILENCES=(250 500 1000 1500 2000)
fi

ARENA="$(cd "$(dirname "$0")"/../.. && pwd)/bin/promptarena"
LOG_DIR="$(cd "$(dirname "$0")" && pwd)/sweep-logs"
mkdir -p "$LOG_DIR"

# Load .env. Repo-root .env wins (matches the user's interactive
# workflow: `set -a && . /path/to/repo/.env && set +a`); fall back to
# example-local .env if present.
REPO_ROOT="$(cd "$(dirname "$0")"/../.. && pwd)"
for candidate in "$REPO_ROOT/.env" "$(cd "$(dirname "$0")" && pwd)/.env"; do
    if [[ -f "$candidate" ]]; then
        set -a
        # shellcheck disable=SC1090
        . "$candidate"
        set +a
        break
    fi
done

printf "\n%-10s | %-7s | %-12s | %-22s | %s\n" \
    "silenceMs" "ok" "turns_done" "avg_latency_ms" "avg_gen_to_turncomplete_ms"
printf -- '------------------------------------------------------------------------------------\n'

for ms in "${SILENCES[@]}"; do
    log="$LOG_DIR/silence-${ms}.log"

    # Capture this run's structured log directly from stderr (arena's
    # slog handler writes there). -v enables DEBUG so we can see
    # turn-complete + Gemini message lines. Discard stdout (TUI / progress).
    if PROMPTKIT_SILENCE_TAIL_MS="$ms" \
        TTS_CACHE_DIR="$(dirname "$0")/.tts-cache" \
        "$ARENA" run \
        --scenario "$SCENARIO" \
        --provider "$PROVIDER" \
        --ci --formats json \
        -v \
        > /dev/null 2> "$log"; then
        ok=1
    else
        ok=0
    fi

    # Count completed turns. grep -c always prints a number; if the
    # pattern doesn't match it prints 0 and exits non-zero, so we
    # swallow the exit status.
    turns_done=$(grep -c "Duplex turn completed" "$log" 2>/dev/null || true)
    [[ -z "$turns_done" ]] && turns_done=0

    # Average DuplexProviderStage latencyMs across turns.
    avg_lat=$(awk '
        /DuplexProviderStage: calculated turn latency/ {
            for (i = 1; i <= NF; i++) {
                if ($i ~ /latencyMs=/) {
                    split($i, kv, "=");
                    sum += kv[2]; n++;
                }
            }
        }
        END { if (n > 0) printf "%.0f", sum/n; else printf "n/a" }' "$log" 2>/dev/null || echo "n/a")

    # generationComplete -> turnComplete gap, averaged across turns.
    # Uses Python because awk + macOS `date` was fragile around the
    # ISO-8601 timestamp's fractional seconds and timezone.
    avg_gap=$(python3 - "$log" <<'PY' 2>/dev/null || echo "n/a"
import re, sys
from datetime import datetime
try:
    txt = open(sys.argv[1]).read()
except Exception:
    print("n/a"); sys.exit(0)
gens = [datetime.fromisoformat(m.group(1))
        for m in re.finditer(r'time=(\S+).*generationComplete', txt)]
turns = [datetime.fromisoformat(m.group(1))
         for m in re.finditer(r'time=(\S+).*turnComplete\\":', txt)]
n = min(len(gens), len(turns))
if n == 0:
    print("n/a"); sys.exit(0)
ms = [(turns[i] - gens[i]).total_seconds() * 1000 for i in range(n)]
print(f"{int(sum(ms) / n)}")
PY
)

    printf "%-10s | %-7s | %-12s | %-22s | %s\n" \
        "$ms" \
        "$([[ $ok -eq 1 ]] && echo yes || echo no)" \
        "$turns_done" \
        "$avg_lat" \
        "$avg_gap"
done

echo
echo "Per-run logs: $LOG_DIR/silence-<ms>.log"
echo
echo "Read this table as: smallest silenceMs where ok=yes, turns_done=N,"
echo "and avg_gen_to_turncomplete_ms is small (~< 400ms means ASM fired"
echo "promptly, vs. ~6000ms which means it timed out)."
