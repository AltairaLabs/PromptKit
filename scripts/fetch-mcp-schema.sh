#!/usr/bin/env bash
#
# Fetch the official MCP JSON Schemas for the protocol revisions PromptKit
# claims into runtime/mcp/testdata/spec/<revision>/schema.json. This script is
# the ONLY sanctioned way to change those files.
#
# The client is dual-era, so it claims two revisions, both read from
# runtime/mcp/types.go:
#   - ProtocolVersion: the stateless revision it speaks with modern servers;
#   - LegacyProtocolVersion: the newest handshake revision, for servers that
#     predate it.
# Those constants are the claims; the mirrors follow them, and
# runtime/mcp/spec_parity_test.go grades the wire types against the mirrors.
# Moving a constant without re-running this script fails that test, so a
# claim cannot move without the spec it names.
#
# The copies are VERBATIM MIRRORS of the published schemas. Never hand-edit
# them — not to add a field the client wants, not to delete one it doesn't
# implement. Where a wire type deliberately does not carry a spec property,
# record a specOmission in runtime/mcp/spec_parity_test.go, where a reviewer
# sees it. Grading our types against a document we edit makes the guard
# circular; that is how the PromptPack copy drifted 143 leaves from its spec
# (see scripts/fetch-promptpack-schema.sh).
#
# Why this exists: the client claimed 2025-06-18 while dropping that
# revision's CallToolResult.structuredContent, so a spec-conformant server's
# result reached the model as "Operation completed successfully" (#2099).
# Nothing compared the wire types to the spec they claimed.
#
# Usage: scripts/fetch-mcp-schema.sh [DEST_DIR]
#   default DEST_DIR: runtime/mcp/testdata/spec
#   MCP_SCHEMA_BASE_URL  override the source (default: the spec repo on GitHub)
#
set -euo pipefail

TYPES="runtime/mcp/types.go"
read_const() {
  sed -nE "s/^const $1 = \"([0-9]{4}-[0-9]{2}-[0-9]{2})\"$/\\1/p" "$TYPES"
}
REVISIONS=("$(read_const ProtocolVersion)" "$(read_const LegacyProtocolVersion)")
for rev in "${REVISIONS[@]}"; do
  if [ -z "$rev" ]; then
    echo "::error::could not read ProtocolVersion and LegacyProtocolVersion from $TYPES" >&2
    exit 1
  fi
done

BASE="${MCP_SCHEMA_BASE_URL:-https://raw.githubusercontent.com/modelcontextprotocol/modelcontextprotocol/main/schema}"
SPEC_DIR="${1:-runtime/mcp/testdata/spec}"

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

for rev in "${REVISIONS[@]}"; do
  url="${BASE}/${rev}/schema.json"
  echo "Fetching MCP ${rev} schema from ${url}"
  curl -fsSL --max-time 30 "$url" -o "$tmp"

  # Refuse to install anything that isn't an MCP schema — an HTML error page
  # or a captive portal would otherwise be committed as the spec.
  python3 - "$tmp" <<'PY'
import json, sys
with open(sys.argv[1]) as f:
    doc = json.load(f)
defs = doc.get("definitions") or doc.get("$defs") or {}
for required in ("CallToolResult", "Tool", "Implementation"):
    if required not in defs:
        sys.exit(f"fetched document has no {required} definition — refusing to install it")
print(f"  fetched MCP schema with {len(defs)} definitions")
PY

  mkdir -p "${SPEC_DIR}/${rev}"
  cp "$tmp" "${SPEC_DIR}/${rev}/schema.json"
  chmod 644 "${SPEC_DIR}/${rev}/schema.json"
  echo "✓ Wrote ${SPEC_DIR}/${rev}/schema.json"
done

# Only the claimed revisions are kept: another directory would be another
# claim.
for dir in "${SPEC_DIR}"/*/; do
  rev="$(basename "$dir")"
  if [ "$rev" != "${REVISIONS[0]}" ] && [ "$rev" != "${REVISIONS[1]}" ]; then
    rm -rf "$dir"
    echo "✓ Removed ${SPEC_DIR}/${rev}: no longer claimed"
  fi
done
