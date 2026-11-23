#!/usr/bin/env bash
#
# Tests scripts/check-docs-sources.sh against a throwaway tree: a page whose
# frontmatter and sidecar cite files that exist passes; a moved source cited
# by either one fails, naming the page and the path.
set -euo pipefail

SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/check-docs-sources.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT
fails=0

page() { # page <root> <sources...>: a verified page citing the given sources
  local root="$1"
  shift
  mkdir -p "${root}/docs/src/content/docs/how-to" "${root}/docs/provenance/how-to" "${root}/src"
  {
    printf -- '---\ntitle: T\nverified:\n  commit: abc\n  sources:\n'
    printf '    - %s\n' "$@"
    printf -- '---\n\nBody citing src/gone.go in prose only.\n'
  } >"${root}/docs/src/content/docs/how-to/p.md"
}

sidecar() { # sidecar <root> <evidence...>
  local root="$1"
  shift
  jq -n --args '{page: "p", commit: "abc", claims: [{id: "c1", evidence: $ARGS.positional}]}' "$@" \
    >"${root}/docs/provenance/how-to/p.json"
}

expect() { # expect <name> <want exit> <want substring> <root>
  local name="$1" want="$2" substr="$3" root="$4" out rc=0
  out="$("${SCRIPT}" "${root}" 2>&1)" || rc=$?
  if [ "${rc}" -ne "${want}" ] || [[ "${out}" != *"${substr}"* ]]; then
    echo "FAIL ${name}: exit ${rc} (want ${want}), output:"
    printf '%s\n' "${out}" | sed 's/^/   /'
    fails=$((fails + 1))
  else
    echo "ok   ${name}"
  fi
}

ok="${tmp}/ok"
page "${ok}" src/a.go src/b.go
sidecar "${ok}" src/a.go:10 src/b.go:3-7
touch "${ok}/src/a.go" "${ok}/src/b.go"
expect "existing sources pass" 0 "OK: 4 cited paths exist." "${ok}"

fm="${tmp}/frontmatter"
page "${fm}" src/a.go src/moved.go
touch "${fm}/src/a.go"
expect "a missing frontmatter source fails" 1 \
  "docs/src/content/docs/how-to/p.md cites src/moved.go, which does not exist" "${fm}"

side="${tmp}/sidecar"
page "${side}" src/a.go
sidecar "${side}" src/a.go:1 src/moved.go:42-44
touch "${side}/src/a.go"
expect "a missing sidecar source fails, its line range stripped" 1 \
  "docs/src/content/docs/how-to/p.md cites src/moved.go, which does not exist" "${side}"

[ "${fails}" -eq 0 ] || { echo "${fails} test(s) failed"; exit 1; }
echo "all check-docs-sources tests passed"
