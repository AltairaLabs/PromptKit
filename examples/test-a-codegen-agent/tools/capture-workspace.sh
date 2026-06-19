#!/usr/bin/env bash
# Session-end capture hook (a regular runtime exec SessionHook — NOT a promptarena
# flag). Reads the exec-hook request JSON on stdin:
#
#   {"hook":"session","phase":"session_end",
#    "event":{"SessionID":"...","Metadata":{"sandbox_containers":{"<server>":"<cid>"}}}}
#
# and copies each open sandbox's /workspace out to <outdir>/kit/<session>/<server>/
# via `docker cp`. The container IDs come from SessionEvent metadata, which the
# Arena engine populates for docker-source sandboxes.
#
# Arg 1: output base dir (default "out"). Requires `jq` and `docker` on PATH.
set -euo pipefail

outdir="${1:-out}"
payload="$(cat)"

session="$(printf '%s' "$payload" | jq -r '.event.SessionID // "session"')"

printf '%s' "$payload" \
	| jq -r '(.event.Metadata.sandbox_containers // {}) | to_entries[] | "\(.key)\t\(.value)"' \
	| while IFS=$'\t' read -r server cid; do
		[ -z "${cid:-}" ] && continue
		dest="$outdir/kit/$session/$server"
		mkdir -p "$dest"
		if docker cp "$cid:/workspace/." "$dest/" >/dev/null 2>&1; then
			echo "capture: $server -> $dest" >&2
		else
			echo "capture: docker cp failed for $server ($cid)" >&2
		fi
	done
