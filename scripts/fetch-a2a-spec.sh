#!/usr/bin/env bash
#
# Fetch the official A2A specification documents for the protocol versions
# PromptKit speaks into runtime/a2a/testdata/spec/<version>/. This script is
# the ONLY sanctioned way to change those files.
#
# runtime/a2a speaks two versions (ProtocolVersion10 and ProtocolVersion03 in
# runtime/a2a/version.go), and each has a different normative document:
#   - 1.0: specification/a2a.proto. A2A 1.0 publishes no JSON schema; its JSON
#     is the ProtoJSON mapping of this file.
#   - 0.3: specification/json/a2a.json, the JSON schema 0.3 published.
# Each is pinned to a release tag of github.com/a2aproject/A2A below. Moving a
# pin is a claim that runtime/a2a speaks that release, so it goes with a run
# of TestA2ASpecParity (runtime/a2a/spec_parity_test.go), which grades the wire
# types against the mirrors.
#
# The copies are VERBATIM MIRRORS. Never hand-edit them — not to add a field
# PromptKit wants, not to delete one it doesn't carry. Where a wire type
# deliberately does not carry a spec field, record a specOmission in
# runtime/a2a/spec_parity_test.go, where a reviewer sees it. Grading our types
# against a document we edit makes the guard circular.
#
# Why this exists: the conformance audit in #2101 found data parts, card
# fields and required fields that had drifted from the spec, and nothing
# compared the Go types to it (#2135).
#
# Usage: scripts/fetch-a2a-spec.sh [DEST_DIR]
#   default DEST_DIR: runtime/a2a/testdata/spec
#   A2A_SPEC_BASE_URL  override the source (default: the spec repo on GitHub)
#
set -euo pipefail

# The release tag each mirrored version is taken from.
V10_TAG="v1.0.1"
V03_TAG="v0.3.0"

BASE="${A2A_SPEC_BASE_URL:-https://raw.githubusercontent.com/a2aproject/A2A}"
SPEC_DIR="${1:-runtime/a2a/testdata/spec}"

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

# fetch URL DEST CHECK...: download URL, refuse it unless it contains every
# CHECK string (an HTML error page or a captive portal would otherwise be
# committed as the spec), and install it at DEST.
fetch() {
  local url="$1" dest="$2"
  shift 2
  echo "Fetching ${url}"
  # GitHub's raw host answers 5xx under load; retry before calling it a failure.
  curl -fsSL --max-time 30 --retry 4 --retry-all-errors --retry-delay 5 "$url" -o "$tmp"
  for want in "$@"; do
    if ! grep -qF -- "$want" "$tmp"; then
      echo "::error::${url} has no '${want}' — refusing to install it" >&2
      exit 1
    fi
  done
  mkdir -p "$(dirname "$dest")"
  cp "$tmp" "$dest"
  chmod 644 "$dest"
  echo "✓ Wrote ${dest}"
}

fetch "${BASE}/${V10_TAG}/specification/a2a.proto" "${SPEC_DIR}/1.0/a2a.proto" \
  "package lf.a2a.v1;" "message AgentCard {" "message Task {"
fetch "${BASE}/${V03_TAG}/specification/json/a2a.json" "${SPEC_DIR}/0.3/a2a.json" \
  '"$schema"' '"AgentCard"' '"TaskStatusUpdateEvent"'

# Only the spoken versions are kept: another directory would be another claim.
for dir in "${SPEC_DIR}"/*/; do
  v="$(basename "$dir")"
  if [ "$v" != "1.0" ] && [ "$v" != "0.3" ]; then
    rm -rf "$dir"
    echo "✓ Removed ${SPEC_DIR}/${v}: not a version runtime/a2a speaks"
  fi
done
