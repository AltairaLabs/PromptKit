#!/usr/bin/env bash
# Gate 5 (deterministic kit adequacy). The generated kit must be a competent
# test suite, not an empty/hallucinated shell or one with only placeholder
# checks. Inspects the REAL files in the kit dir:
#   1. at least one scenario exists (a YAML with `kind: Scenario`)
#   2. every scenario declares at least one NON-TRIVIAL assertion — not only
#      min_length / max_length / length / is_valid_json / field_presence, which
#      pass for any non-empty response and verify nothing about behaviour.
# Exits 0 when adequate, 1 when inadequate (with a reason), 2 on usage error.
# Deterministic; no LLM. (Runs-offline is already enforced by Gate 3.)
#
# LIMITATION: detection is grep-based (matching the other kit gate scripts) —
# it reads `kind:` / `type:` lines, not a full YAML parse.
set -u
kit="${1:-}"
[ -n "$kit" ] && [ -d "$kit" ] || { echo "usage: kit-quality.sh <kitdir>" >&2; exit 2; }

trivial_re='^(min_length|max_length|length|is_valid_json|json_valid|min_sentences|max_sentences|field_presence)$'

rc=0
scn=0
while IFS= read -r f; do
  grep -qE '^[[:space:]]*kind:[[:space:]]*Scenario[[:space:]]*$' "$f" || continue
  scn=$((scn + 1))
  types="$(grep -E '^[[:space:]]*-?[[:space:]]*type:[[:space:]]' "$f" \
    | sed -E 's/.*type:[[:space:]]*//; s/["'\'' ]//g')"
  if [ -z "$types" ]; then
    echo "inadequate: $(basename "$f") declares no assertions"
    rc=1
    continue
  fi
  nontrivial=0
  while IFS= read -r t; do
    [ -z "$t" ] && continue
    printf '%s\n' "$t" | grep -qiE "$trivial_re" || nontrivial=$((nontrivial + 1))
  done <<< "$types"
  if [ "$nontrivial" -eq 0 ]; then
    echo "inadequate: $(basename "$f") has only trivial assertions: $(printf '%s ' $types)"
    rc=1
  fi
done < <(find "$kit" -type f -name '*.yaml')

if [ "$scn" -eq 0 ]; then
  echo "inadequate: no scenarios found in $kit"
  rc=1
fi

[ "$rc" -eq 0 ] && echo "kit-quality: OK ($scn scenario(s), each with a non-trivial assertion)"
exit $rc
