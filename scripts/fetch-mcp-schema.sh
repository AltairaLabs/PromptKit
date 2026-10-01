#!/usr/bin/env bash
#
# Fetch the official MCP JSON Schema for the protocol revision PromptKit claims
# into runtime/mcp/testdata/spec/<revision>/schema.json. This script is the ONLY
# sanctioned way to change that file.
#
# The revision is read from mcp.ProtocolVersion in runtime/mcp/types.go — the
# version the client sends in initialize. That constant is the single claim;
# the mirror follows it, and runtime/mcp/spec_parity_test.go grades the wire
# types against the mirror. Bumping the constant without re-running this
# script fails that test, so the claim cannot move without the spec it names.
#
# The copy is a VERBATIM MIRROR of the published schema. Never hand-edit it —
# not to add a field the client wants, not to delete one it doesn't implement.
# Where a wire type deliberately does not carry a spec property, record a
# specOmission in runtime/mcp/spec_parity_test.go, where a reviewer sees it.
# Grading our types against a document we edit makes the guard circular; that
# is how the PromptPack copy drifted 143 leaves from its spec (see
# scripts/fetch-promptpack-schema.sh).
#
# Why this exists: the client claimed 2025-06-18 while dropping that
# revision's CallToolResult.structuredContent, so a spec-conformant server's
# result reached the model as "Operation completed successfully" (#2099).
# Nothing compared the wire types to the spec they claimed.
#
# Usage: scripts/fetch-mcp-schema.sh [DEST_FILE]
#   default DEST_FILE: runtime/mcp/testdata/spec/<revision>/schema.json
#   MCP_SCHEMA_BASE_URL  override the source (default: the spec repo on GitHub)
#
set -euo pipefail

TYPES="runtime/mcp/types.go"
REVISION="$(sed -nE 's/^const ProtocolVersion = "([0-9]{4}-[0-9]{2}-[0-9]{2})"$/\1/p' "$TYPES")"
if [ -z "$REVISION" ]; then
  echo "::error::could not read ProtocolVersion from $TYPES" >&2
  exit 1
fi

BASE="${MCP_SCHEMA_BASE_URL:-https://raw.githubusercontent.com/modelcontextprotocol/modelcontextprotocol/main/schema}"
URL="${BASE}/${REVISION}/schema.json"
SPEC_DIR="runtime/mcp/testdata/spec"
DEST="${1:-${SPEC_DIR}/${REVISION}/schema.json}"

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

echo "Fetching MCP ${REVISION} schema from ${URL}"
curl -fsSL --max-time 30 "$URL" -o "$tmp"

# Refuse to install anything that isn't the MCP schema — an HTML error page or
# a captive portal would otherwise be committed as the spec.
python3 - "$tmp" <<'PY'
import json, sys
with open(sys.argv[1]) as f:
    doc = json.load(f)
defs = doc.get("definitions") or doc.get("$defs") or {}
for required in ("InitializeResult", "CallToolResult", "Tool"):
    if required not in defs:
        sys.exit(f"fetched document has no {required} definition — refusing to install it")
print(f"  fetched MCP schema with {len(defs)} definitions")
PY

# Only the claimed revision is kept: a second directory would be a second
# claim, and the parity test loads exactly one.
if [ $# -eq 0 ]; then
  find "$SPEC_DIR" -mindepth 1 -maxdepth 1 -type d ! -name "$REVISION" -exec rm -rf {} + 2>/dev/null || true
fi

mkdir -p "$(dirname "$DEST")"
cp "$tmp" "$DEST"
chmod 644 "$DEST"
echo "✓ Wrote ${DEST}"
