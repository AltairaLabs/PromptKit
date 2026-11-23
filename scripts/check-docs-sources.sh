#!/usr/bin/env bash
#
# Fail when a docs page's provenance cites a file that does not exist: a path
# in a page's `verified.sources` frontmatter, or in the evidence of its sidecar
# under docs/provenance/ (see docs/REVIEW.md).
#
# docs-stale reports such a path as `missing` but never fails, so a PR that
# moves or deletes a cited file used to pass with only a warning, and every
# later freshness check of that page compared nothing. Repoint the page at the
# file's new path in the same PR.
#
# Usage: scripts/check-docs-sources.sh [ROOT]   (default: this checkout)
# Exits 1 when a cited path is missing, 2 when a sidecar cannot be read.
set -euo pipefail
command -v jq >/dev/null || { echo "FAIL: jq is required" >&2; exit 2; }

ROOT="${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
DOCS="docs/src/content/docs"
PROV="docs/provenance"

# frontmatter_sources prints the `verified.sources` entries of a page, the
# block docs-provenance writes: "verified:", "  sources:", "    - <path>".
frontmatter_sources() {
  awk '
    NR == 1 { if ($0 != "---") exit; next }
    $0 == "---" { exit }
    /^verified:/ { inv = 1; insrc = 0; next }
    inv && /^[^ ]/ { inv = 0; insrc = 0 }
    inv && /^  sources:/ { insrc = 1; next }
    inv && insrc && /^    - / { s = substr($0, 7); sub(/[ \t]*#.*$/, "", s); gsub(/["\047]/, "", s); if (s != "") print s; next }
    inv && /^  [^ ]/ { insrc = 0 }
  ' "$1"
}

missing=0
checked=0
while IFS= read -r page; do
  rel="${page#"${ROOT}/"}"
  sidecar="${ROOT}/${PROV}/${rel#"${DOCS}/"}"
  sidecar="${sidecar%.*}.json"
  # Read outside the loop's input: a failure inside `< <(...)` would be
  # ignored, and an unreadable sidecar would pass as one citing nothing.
  evidence=""
  if [ -f "${sidecar}" ]; then
    if ! evidence="$(jq -r '.claims[]?.evidence[]? | sub(":[0-9]+(-[0-9]+)?$"; "")' "${sidecar}" 2>&1)"; then
      echo "::error file=${sidecar#"${ROOT}/"}::cannot read ${sidecar#"${ROOT}/"}: ${evidence}"
      exit 2
    fi
  fi
  while IFS= read -r src; do
    [ -n "${src}" ] || continue
    checked=$((checked + 1))
    if [ ! -e "${ROOT}/${src}" ]; then
      echo "::error file=${rel}::${rel} cites ${src}, which does not exist"
      missing=$((missing + 1))
    fi
  done < <(
    frontmatter_sources "${page}"
    [ -z "${evidence}" ] || printf '%s\n' "${evidence}"
  )
done < <(find "${ROOT}/${DOCS}" -type f \( -name '*.md' -o -name '*.mdx' \) | sort)

if [ "${missing}" -gt 0 ]; then
  echo "FAIL: ${missing} cited path(s) do not exist. Point each page's verified.sources and its" \
    "${PROV} sidecar at the file's new path."
  exit 1
fi
echo "OK: ${checked} cited paths exist."
