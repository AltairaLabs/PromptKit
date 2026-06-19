#!/usr/bin/env bash
# Non-gating metric: counts idiom traps + per-scenario assertion adequacy.
# Always exits 0 (exit 2 only on missing dir) so it never fails the run.
set -u
kit="${1:-}"
[ -n "$kit" ] && [ -d "$kit" ] || { echo "usage: idiom-traps.sh <kitdir>" >&2; exit 2; }

traps=0; trap_list=""
note() { traps=$((traps+1)); trap_list="$trap_list \"$1\""; }

# Trap 1: mock-responses.yaml keyed on spec.id instead of metadata.name.
# Heuristic: any scenario spec.id that appears as a top-level key under
# scenarios: in a mock-responses.yaml is a likely mis-key.
for mock in $(find "$kit" -type f -name 'mock-responses.yaml'); do
  for idfile in $(find "$kit" -type f -name '*.scenario.yaml'); do
    sid="$(grep -E '^\s*id:' "$idfile" | head -1 | sed -E 's/.*id:\s*//; s/["'\'' ]//g')"
    [ -n "$sid" ] && grep -qE "^\s*${sid}:" "$mock" && note "mock-key-uses-spec-id:$sid"
  done
done

# Trap 2: min_score/max_score on an inner eval (not on a type: assertion wrapper).
# Heuristic: a min_score/max_score line whose nearest preceding type: is not "assertion".
for f in $(find "$kit" -type f -name '*.yaml'); do
  if grep -qE '^\s*(min_score|max_score):' "$f"; then
    # crude: if the file has min/max_score but no "type: assertion", flag it.
    grep -qE '^\s*type:\s*assertion' "$f" || note "threshold-on-eval:$(basename "$f")"
  fi
done

# Adequacy: scenarios with zero assertions.
scn=0; no_assert=0
for f in $(find "$kit" -type f -name '*.scenario.yaml'); do
  scn=$((scn+1))
  grep -qE '^\s*assertions:' "$f" || no_assert=$((no_assert+1))
done

printf '{"idiom_traps": %d, "scenarios": %d, "scenarios_without_assertions": %d, "traps": [%s ]}\n' \
  "$traps" "$scn" "$no_assert" "$trap_list"
exit 0
