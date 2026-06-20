#!/usr/bin/env bash
# Session-end capture hook (a regular runtime exec SessionHook — NOT a promptarena
# flag). Reads the exec-hook request JSON on stdin:
#
#   {"hook":"session","phase":"session_end","event":{"Metadata":{
#      "artifacts_dir":"<abs path Arena created for this run>",
#      "sandbox_containers":{"<server>":"<cid>"}}}}
#
# Arena owns WHERE artifacts go (artifacts_dir, derived from config/output dir and
# created for us). This hook owns WHAT: it `docker cp`s each sandbox's /workspace
# into artifacts_dir/<server>/ and records {name, description, filename} per
# capture in artifacts_dir/manifest.json. The report joins Arena's base with the
# filename to build the link. Requires `jq` and `docker` on PATH.
set -euo pipefail

payload="$(cat)"
artdir="$(printf '%s' "$payload" | jq -r '.event.Metadata.artifacts_dir // empty')"
if [ -z "$artdir" ]; then
	echo "capture: no artifacts_dir in session metadata; skipping" >&2
	exit 0
fi
mkdir -p "$artdir"

captured=()
# Process substitution (not a pipe) so the captured[] array survives the loop.
while IFS=$'\t' read -r server cid; do
	[ -z "${cid:-}" ] && continue
	dest="$artdir/$server"
	mkdir -p "$dest"
	if docker cp "$cid:/workspace/." "$dest/" >/dev/null 2>&1; then
		captured+=("$server")
		echo "capture: $server -> $dest" >&2
	else
		echo "capture: docker cp failed for $server ($cid)" >&2
	fi
done < <(printf '%s' "$payload" |
	jq -r '(.event.Metadata.sandbox_containers // {}) | to_entries[] | "\(.key)\t\(.value)"')

# Record name/description/filename only — Arena owns the base path.
if [ ${#captured[@]} -gt 0 ]; then
	{
		printf '{"artifacts":['
		for i in "${!captured[@]}"; do
			[ "$i" -gt 0 ] && printf ','
			printf '{"name":"Captured workspace","description":"Files the agent wrote in /workspace, captured at session end","filename":"%s"}' \
				"${captured[$i]}"
		done
		printf ']}'
	} >"$artdir/manifest.json"
fi
