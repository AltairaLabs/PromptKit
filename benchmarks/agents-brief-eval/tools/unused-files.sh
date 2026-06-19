#!/usr/bin/env bash
# Gate 4: every *.yaml under the kit (except config.arena.yaml itself and
# mock-responses.yaml) must be referenced by filename from config.arena.yaml.
set -u
kit="${1:-}"
[ -n "$kit" ] && [ -d "$kit" ] || { echo "usage: unused-files.sh <kitdir>" >&2; exit 2; }
cfg="$kit/config.arena.yaml"
[ -f "$cfg" ] || { echo "no config.arena.yaml in $kit" >&2; exit 2; }

rc=0
while IFS= read -r f; do
  base="$(basename "$f")"
  case "$base" in config.arena.yaml|mock-responses.yaml) continue ;; esac
  if ! grep -q "$base" "$cfg"; then
    echo "orphan: $f (not referenced from config.arena.yaml)"
    rc=1
  fi
done < <(find "$kit" -type f -name '*.yaml')
exit $rc
