#!/usr/bin/env bash
#
# Fail when the published modules of one release would disagree about a shared
# dependency version.
#
# v1.8.0 shipped with runtime requiring k8s.io/apimachinery v0.36.4 while pkg
# and sdk required v0.37.0 (#1920). Go's minimal version selection then picks
# the higher for a consumer taking both, producing a combination neither module
# was tested against — the build failed inside k8s.io/api, which nothing here
# had ever compiled against.
#
# Nothing caught it because every check ran inside the Go workspace, and a
# workspace unifies the build list into one consistent set. The skew exists
# only for a consumer taking the modules separately, which is the one shape
# `go test ./...` at the repo root can never reproduce.
#
set -euo pipefail

MODULES=(runtime pkg sdk server/a2a)

declare -a rows=()
for m in "${MODULES[@]}"; do
  while read -r dep ver; do
    [ -z "$dep" ] && continue
    rows+=("$dep|$ver|$m")
  done < <(awk '/^require|^\t/ {
      line=$0
      gsub(/\/\/.*/, "", line)
      n = split(line, f, /[ \t]+/)
      path=""; ver=""
      for (i = 1; i <= n; i++) {
        if (f[i] ~ /^v[0-9]/) { ver=f[i]; path=f[i-1]; break }
      }
      # Sibling PromptKit modules are exempt: the in-repo go.mod files carry
      # stale requires plus a local replace, and the release pipeline rewrites
      # them to the release version at tag time. Their skew here is expected.
      if (path != "" && path ~ /\./ && path !~ /AltairaLabs\/PromptKit/) print path, ver
    }' "$m/go.mod")
done

fail=0
# group by dependency; a dependency required at two different versions is skew
printf '%s\n' "${rows[@]}" | sort | awk -F'|' '
  { seen[$1] = seen[$1] " " $2 "(" $3 ")"; vers[$1 "|" $2] = 1 }
  END {
    for (d in seen) {
      n = 0
      for (k in vers) { split(k, p, "|"); if (p[1] == d) n++ }
      if (n > 1) printf "::error::%s is required at %d different versions:%s\n", d, n, seen[d]
    }
  }' > /tmp/skew.$$ || true

if [ -s /tmp/skew.$$ ]; then
  cat /tmp/skew.$$
  rm -f /tmp/skew.$$
  echo "::error::"
  echo "::error::The modules of one release must agree on shared dependencies."
  echo "::error::A consumer taking both gets MVS's highest pick, which is a"
  echo "::error::combination no module here was tested against."
  echo "::error::Fix: go -C <module> get <dep>@<agreed-version> && go mod tidy"
  exit 1
fi
rm -f /tmp/skew.$$

echo "✓ ${#MODULES[@]} modules agree on every shared dependency version"
