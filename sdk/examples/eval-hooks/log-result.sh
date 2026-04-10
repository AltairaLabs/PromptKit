#!/usr/bin/env bash
# log-result.sh — example bash-based eval result consumer.
#
# Reads one JSON-encoded eval result from stdin (as produced by ExecEvalHook
# in main.go), prints a one-line summary to stderr so the caller sees it,
# and appends the raw JSON to eval-results.ndjson for later analysis.
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
out_file="${script_dir}/eval-results.ndjson"

payload=$(cat)

# Extract a few interesting fields with grep/sed — no jq dependency.
eval_id=$(printf '%s' "${payload}" | sed -n 's/.*"eval_id":"\([^"]*\)".*/\1/p')
eval_type=$(printf '%s' "${payload}" | sed -n 's/.*"type":"\([^"]*\)".*/\1/p')
score=$(printf '%s' "${payload}" | sed -n 's/.*"score":\([0-9.]*\).*/\1/p')

printf '[log-result.sh] eval_id=%s type=%s score=%s\n' \
  "${eval_id:-?}" "${eval_type:-?}" "${score:-?}" >&2

printf '%s\n' "${payload}" >> "${out_file}"
