#!/usr/bin/env bash
# Fail if the committed runtime reference pages are stale vs Go source.
set -euo pipefail
ROOT="$(git rev-parse --show-toplevel)"
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
"$ROOT/docs/scripts/gen-reference.sh" "$TMP" >/dev/null
if ! diff -ru "$ROOT/docs/src/content/docs/runtime/reference" "$TMP" \
     --exclude='index.md' >/dev/null; then
  echo "❌ Runtime reference is stale — run 'make docs-reference' and commit." >&2
  diff -ru "$ROOT/docs/src/content/docs/runtime/reference" "$TMP" --exclude='index.md' >&2 || true
  exit 1
fi
echo "✅ Runtime reference is up to date."
