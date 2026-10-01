#!/usr/bin/env bash
# Reports public surface no docs page cites; see scripts/docs-coverage.py.
# Usage: bash scripts/docs-coverage.sh [--root <repo>]
exec python3 "$(dirname "$0")/docs-coverage.py" "$@"
