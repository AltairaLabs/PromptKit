#!/usr/bin/env bash
#
# Fixture tests for scripts/check-stage-api.sh: each case builds an old and a
# new copy of a tiny runtime module shaped like pipeline/stage, then runs the
# real script (and so the real apidiff and allowlist) over them.
#
# The cases that must fail are the ones apidiff alone does not report: once a
# type moves behind an alias, apidiff sees only "X: changed from X to
# .../internal/core.X", whatever happened to X's fields and methods (#2876).
#
# Run from the repo root: scripts/test-check-stage-api.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CHECK="${SCRIPT_DIR}/check-stage-api.sh"
RT="github.com/AltairaLabs/PromptKit/runtime/v2"

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

pass=0
fail=0

# The type as stage declares it today; a move must keep all of it.
DECLARED='type Scored struct {
	Index int
	Score float64
}

func (s Scored) Weight() float64 { return s.Score }

type Kind int

const KindA Kind = 1

type Iface interface{ Do(int) error }'

# The same declarations as an internal package holds them after the move.
MOVED="package core

${DECLARED}"

ALIASES="package stage

import \"${RT}/pipeline/stage/internal/core\"

type Scored = core.Scored
type Kind = core.Kind

const KindA = core.KindA

type Iface = core.Iface

type Holder struct{ Top Scored }"

OTHER="package other

import \"${RT}/pipeline/stage\"

type Uses struct{ S stage.Scored }"

# tree <dir> <stage.go> [<core.go>]: a runtime module with pipeline/stage,
# pipeline/other and, given core.go, pipeline/stage/internal/core.
tree() {
  local d="$1/runtime"
  mkdir -p "${d}/pipeline/stage" "${d}/pipeline/other"
  printf 'module %s\n\ngo 1.24\n' "${RT}" >"${d}/go.mod"
  printf '%s\n' "$2" >"${d}/pipeline/stage/s.go"
  printf '%s\n' "${OTHER}" >"${d}/pipeline/other/o.go"
  if [ -n "${3:-}" ]; then
    mkdir -p "${d}/pipeline/stage/internal/core"
    printf '%s\n' "$3" >"${d}/pipeline/stage/internal/core/c.go"
  fi
}

# expect <name> <want-exit> <old stage.go> <old core.go> <new stage.go> <new core.go> [<needle>]
expect() {
  local name="$1" want="$2" needle="${7:-}" dir out got=0
  dir="${tmp}/case-$((pass + fail))"
  tree "${dir}/old" "$3" "$4"
  tree "${dir}/new" "$5" "$6"
  out="$(env STAGE_API_OLD_ROOT="${dir}/old" STAGE_API_NEW_ROOT="${dir}/new" \
    STAGE_API_PACKAGES="runtime|${RT}/pipeline/stage runtime|${RT}/pipeline/other" \
    "${CHECK}" 2>&1)" || got=$?
  if [ "${got}" -ne "${want}" ]; then
    echo "FAIL: ${name} — exit ${got}, wanted ${want}"
    printf '%s\n' "${out}" | sed 's/^/      /'
    fail=$((fail + 1))
    return
  fi
  if [ -n "${needle}" ] && ! printf '%s' "${out}" | grep -qF -- "${needle}"; then
    echo "FAIL: ${name} — output missing '${needle}'"
    printf '%s\n' "${out}" | sed 's/^/      /'
    fail=$((fail + 1))
    return
  fi
  echo "ok:   ${name}"
  pass=$((pass + 1))
}

BEFORE="package stage

${DECLARED}

type Holder struct{ Top Scored }"

expect "a pure move passes, in stage and in a package that uses the type" 0 \
  "${BEFORE}" "" "${ALIASES}" "${MOVED}" \
  "Uses.S: changed from ${RT}/pipeline/stage.Scored to ${RT}/pipeline/stage/internal/core.Scored"

expect "a move that drops a field fails" 1 \
  "${BEFORE}" "" "${ALIASES}" "${MOVED/	Index int/}" "Scored.Index: removed"

expect "a move that re-types a field fails" 1 \
  "${BEFORE}" "" "${ALIASES}" "${MOVED/Index int/Index int64}" "Scored.Index: changed from int to int64"

expect "a move that drops a method fails" 1 \
  "${BEFORE}" "" "${ALIASES}" "${MOVED/func (s Scored) Weight() float64 \{ return s.Score \}/}" \
  "Scored.Weight: removed"

expect "a move that changes the underlying type fails" 1 \
  "${BEFORE}" "" "${ALIASES}" "$(printf '%s' "${MOVED}" | sed 's/^type Kind int$/type Kind string/; s/^const KindA Kind = 1$/const KindA Kind = "a"/')" \
  "Kind: changed from int to string"

expect "a move that adds an interface method fails" 1 \
  "${BEFORE}" "" "${ALIASES}" "${MOVED/Do(int) error/Do(int) error; Extra()}" "Iface.Extra: added"

# After the move both trees alias core.X, and apidiff on stage reports nothing
# at all for a change inside core.
expect "a later change to an already-moved type fails" 1 \
  "${ALIASES}" "${MOVED}" "${ALIASES}" "${MOVED/Index int/Index string}" "Scored.Index: changed from int to string"

expect "an already-moved type left alone passes" 0 \
  "${ALIASES}" "${MOVED}" "${ALIASES}" "${MOVED}" "✓ unchanged"

echo
echo "${pass} passed, ${fail} failed"
[ "${fail}" -eq 0 ]
