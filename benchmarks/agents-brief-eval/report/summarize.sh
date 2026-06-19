#!/usr/bin/env bash
# Summarize an agents-brief-eval run: gate pass-rate per scenario + idiom-trap counts.
# Requires: python3, jq
#
# Usage: summarize.sh [out-dir]
#   out-dir defaults to "out" (relative to CWD, or pass an absolute path).
#
# Report shape (as of PromptKit v1.4.x):
#   Arena writes one JSON file per run combination:
#     <out-dir>/<timestamp>_<provider>_<region>_<scenario>_<run-id>.json
#   and an index.json summary.  There is no report-data.json.
#
#   Each per-run file has these top-level fields (PascalCase):
#     .ScenarioID          string   — e.g. "refund-assistant-mock"
#     .Labels              object   — e.g. {"use_case":"support","complexity":"medium"}
#     .conversation_assertions
#       .passed            bool     — overall pass/fail for this run
#       .failed            int      — number of failing assertions
#       .total             int      — total assertions evaluated
#       .results[]
#         .type            string   — "tool_exec" | "llm_judge_session" | ...
#         .passed          bool
#         .message         string
#         .details
#           .result        string|object — stdout for tool_exec; for idiom-traps
#                          it is a JSON object: {idiom_traps, scenarios,
#                          scenarios_without_assertions, traps:[]}
#
# ## Tool-adoption (not yet captured)
#   The per-run JSON records which MCP tools were called (ToolStats.by_tool) but
#   does NOT record the arguments passed to each tool call.  Determining whether
#   the agent invoked `promptarena explain`, `promptarena schema`, or
#   `promptarena examples show` therefore requires an events-log pass (e.g.
#   streaming the conversation messages for assistant tool-use blocks).
#   This metric is deferred to a follow-up task.

set -eu

out_dir="${1:-out}"

# Resolve to absolute path so we can cd freely
if [[ "$out_dir" != /* ]]; then
  out_dir="$(pwd)/$out_dir"
fi

if [[ ! -d "$out_dir" ]]; then
  echo "ERROR: output directory not found: $out_dir" >&2
  exit 1
fi

# Collect all per-run JSON files (exclude index.json)
shopt -s nullglob
run_files=("$out_dir"/*_*.json)
shopt -u nullglob

if [[ ${#run_files[@]} -eq 0 ]]; then
  echo "No run files found in $out_dir" >&2
  exit 1
fi

echo "== Gate pass-rate per scenario =="
echo "(pass = conversation_assertions.passed == true for that run)"
echo ""

# Build a combined JSON array from all per-run files and summarize with jq.
# We pipe each file's content into a JSON array, then group and report.
{
  echo "["
  first=1
  for f in "${run_files[@]}"; do
    # Skip index.json
    base="$(basename "$f")"
    if [[ "$base" == "index.json" ]]; then
      continue
    fi
    if [[ $first -eq 0 ]]; then
      echo ","
    fi
    first=0
    python3 -c "
import json, sys
with open(sys.argv[1]) as fh:
    d = json.load(fh)
# Extract the fields we need
out = {
    'scenario': d.get('ScenarioID', '?'),
    'labels':   d.get('Labels', {}),
    'passed':   (d.get('conversation_assertions') or {}).get('passed', False),
    'failed':   (d.get('conversation_assertions') or {}).get('failed', 0),
    'total':    (d.get('conversation_assertions') or {}).get('total', 0),
    'results':  (d.get('conversation_assertions') or {}).get('results', []),
}
print(json.dumps(out))
" "$f"
  done
  echo "]"
} | jq -r '
  group_by(.scenario)
  | map({
      scenario: .[0].scenario,
      use_case: (.[0].labels.use_case // "?"),
      complexity: (.[0].labels.complexity // "?"),
      runs: length,
      passed_runs: ([.[] | select(.passed == true)] | length),
      total_assertions: (.[0].total)
    })
  | sort_by(.scenario)
  | .[]
  | "  \(.scenario) [\(.use_case)/\(.complexity)]: \(.passed_runs)/\(.runs) runs passed  (\(.total_assertions) assertions each)"
'

echo ""
echo "== Idiom-trap metric (from Gate: idiom-traps.sh Details) =="
echo "(idiom_traps = hardcoded threshold patterns; scenarios_without_assertions = missing test coverage)"
echo ""

{
  echo "["
  first=1
  for f in "${run_files[@]}"; do
    base="$(basename "$f")"
    if [[ "$base" == "index.json" ]]; then
      continue
    fi
    if [[ $first -eq 0 ]]; then
      echo ","
    fi
    first=0
    python3 -c "
import json, sys
with open(sys.argv[1]) as fh:
    d = json.load(fh)
scenario = d.get('ScenarioID', '?')
results  = (d.get('conversation_assertions') or {}).get('results', [])
idiom_result = None
for r in results:
    details = r.get('details') or {}
    result  = details.get('result')
    if isinstance(result, dict) and 'idiom_traps' in result:
        idiom_result = result
        break
out = {
    'scenario': scenario,
    'idiom':    idiom_result,
}
print(json.dumps(out))
" "$f"
  done
  echo "]"
} | jq -r '
  [.[] | select(.idiom != null)]
  | group_by(.scenario)
  | map({
      scenario: .[0].scenario,
      idiom_traps: (.[0].idiom.idiom_traps // 0),
      scenarios_total: (.[0].idiom.scenarios // 0),
      scenarios_without_assertions: (.[0].idiom.scenarios_without_assertions // 0),
      traps: (.[0].idiom.traps // [])
    })
  | sort_by(.scenario)
  | .[]
  | "  \(.scenario): idiom_traps=\(.idiom_traps)  scenarios=\(.scenarios_total)  without_assertions=\(.scenarios_without_assertions)\(if (.traps | length) > 0 then "  traps=\(.traps | join(","))" else "" end)"
' 2>/dev/null || echo "  (no idiom-trap data in this run — gate may be absent from scenarios)"

echo ""
echo "== Tool-adoption (not yet captured) =="
echo "  ToolStats.by_tool records call counts per tool name (e.g. Bash: N)"
echo "  but does not capture argument text, so promptarena explain/schema/examples"
echo "  cannot be distinguished from other Bash calls.  A follow-up events-log pass"
echo "  is needed to emit this metric."
