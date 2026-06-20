#!/usr/bin/env bash
# Session-end capture hook (a regular runtime exec SessionHook — NOT a promptarena
# flag). Reads the exec-hook request JSON on stdin:
#
#   {"hook":"session","phase":"session_end",
#    "event":{"SessionID":"...","Metadata":{"sandbox_containers":{"<server>":"<cid>"}}}}
#
# and copies each open sandbox's /workspace out to <outdir>/kit/<session>/<server>/
# via `docker cp` (container IDs come from SessionEvent metadata). It then writes a
# manifest at <outdir>/artifacts/<session>.json so the HTML report can link the
# captured workspace — the report surfaces whatever a hook declares, it doesn't
# know anything about this sandbox.
#
# Arg 1: output base dir (default "out"). Requires `jq` and `docker` on PATH.
set -euo pipefail

outdir="${1:-out}"
payload="$(cat)"
session="$(printf '%s' "$payload" | jq -r '.event.SessionID // "session"')"

captured=()
# Process substitution (not a pipe) so the captured[] array survives the loop.
while IFS=$'\t' read -r server cid; do
	[ -z "${cid:-}" ] && continue
	dest="$outdir/kit/$session/$server"
	mkdir -p "$dest"
	if docker cp "$cid:/workspace/." "$dest/" >/dev/null 2>&1; then
		captured+=("kit/$session/$server")
		echo "capture: $server -> $dest" >&2
	else
		echo "capture: docker cp failed for $server ($cid)" >&2
	fi
done < <(printf '%s' "$payload" |
	jq -r '(.event.Metadata.sandbox_containers // {}) | to_entries[] | "\(.key)\t\(.value)"')

# Write the artifact manifest the report reads. Paths are relative to outdir so
# the link resolves when report.html (also in outdir) is opened from disk.
if [ ${#captured[@]} -gt 0 ]; then
	mkdir -p "$outdir/artifacts"
	{
		printf '{"artifacts":['
		for i in "${!captured[@]}"; do
			[ "$i" -gt 0 ] && printf ','
			printf '{"label":"Captured workspace","path":"%s"}' "${captured[$i]}"
		done
		printf ']}'
	} >"$outdir/artifacts/$session.json"
fi
