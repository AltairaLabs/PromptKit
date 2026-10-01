#!/usr/bin/env bash
# Reports docs pages whose verified facts may have changed; see scripts/docs-stale.py.
# Usage: bash scripts/docs-stale.sh [--root <repo>] [--base <ref>]
exec python3 "$(dirname "$0")/docs-stale.py" "$@"
