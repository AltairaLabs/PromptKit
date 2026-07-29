#!/usr/bin/env bash
#
# Guards issue #1713: a published module tag must not carry a local `replace`
# on a sibling PromptKit module, and every internal `require` must name the
# version being released.
#
# Go ignores `replace` directives in dependency modules but honors `require`, so
# a stale internal require reaches consumers. It stayed invisible for five
# releases because the local replace in pkg/go.mod meant no workspace or CI
# build ever exercised the require line.
#
# Usage:
#   verify-published-gomod.sh <version> <module>...   # fetch from the Go proxy
#   verify-published-gomod.sh --file <path> <version> # check a local go.mod
#
# Modules are repo-relative module directories: runtime, pkg, sdk, server/a2a.
#
# Exit status:
#   0  every go.mod checked is clean (or never propagated — see below)
#   1  at least one published go.mod is wrong
#   2  usage error
#
# A module that never appears on the proxy within the retry budget is reported
# as a warning, not a failure: that is a propagation problem, not a correctness
# one, and the existing release steps already treat propagation as advisory.
set -euo pipefail

PROXY_PREFIX="https://proxy.golang.org/github.com/!altaira!labs/!prompt!kit"
RETRIES="${VERIFY_GOMOD_RETRIES:-10}"
RETRY_SLEEP="${VERIFY_GOMOD_SLEEP:-20}"

INTERNAL_PREFIX='github.com/AltairaLabs/PromptKit/'

read -r -d '' AWK_PROG <<'AWK' || true
BEGIN { block = ""; bad = 0 }

function check_require(s,   n, a) {
	n = split(s, a, /[ \t]+/)
	if (n < 2) return
	if (index(a[1], PREFIX) != 1) return
	if (a[2] != VERSION) {
		printf("  BAD  require %s %s (expected %s)\n", a[1], a[2], VERSION)
		bad++
	}
}

function check_replace(s,   n, a) {
	n = split(s, a, /[ \t]+/)
	if (n < 1 || a[1] == "") return
	if (index(a[1], PREFIX) != 1) return
	printf("  BAD  replace %s (internal replace must be dropped before tagging)\n", a[1])
	bad++
}

{
	line = $0
	sub(/\/\/.*$/, "", line)
	gsub(/^[ \t]+|[ \t]+$/, "", line)
	if (line == "") next

	if (block != "") {
		if (line == ")") { block = "" }
		else if (block == "require") { check_require(line) }
		else if (block == "replace") { check_replace(line) }
		next
	}

	if (line ~ /^require[ \t]*\($/)  { block = "require"; next }
	if (line ~ /^replace[ \t]*\($/)  { block = "replace"; next }
	if (line ~ /^[a-z]+[ \t]*\($/)   { block = "other";   next }
	if (line ~ /^require[ \t]+/)     { sub(/^require[ \t]+/, "", line); check_require(line); next }
	if (line ~ /^replace[ \t]+/)     { sub(/^replace[ \t]+/, "", line); check_replace(line); next }
}

END { exit (bad > 0 ? 1 : 0) }
AWK

usage() {
	sed -n '3,25p' "$0" | sed 's/^# \{0,1\}//'
}

annotate_error() {
	if [ "${GITHUB_ACTIONS:-}" = "true" ]; then
		echo "::error::$1"
	else
		echo "ERROR: $1"
	fi
}

# check_gomod <file> <version> <label>
check_gomod() {
	local file="$1" version="$2" label="$3"
	echo "Checking $label ..."
	if awk -v VERSION="$version" -v PREFIX="$INTERNAL_PREFIX" "$AWK_PROG" "$file"; then
		echo "  OK   no internal replace directives; internal requires all at $version"
		return 0
	fi
	annotate_error "$label go.mod is not release-clean (see BAD lines above)"
	return 1
}

if [ "$#" -lt 2 ]; then
	usage
	exit 2
fi

if [ "$1" = "--file" ]; then
	FILE="$2"
	VERSION="${3:-}"
	if [ -z "$VERSION" ]; then
		usage
		exit 2
	fi
	if [ ! -f "$FILE" ]; then
		annotate_error "no such file: $FILE"
		exit 2
	fi
	check_gomod "$FILE" "$VERSION" "$FILE"
	exit $?
fi

VERSION="$1"
shift

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

failed=0
skipped=0

for module in "$@"; do
	url="$PROXY_PREFIX/$module/@v/$VERSION.mod"
	out="$WORK/$(echo "$module" | tr '/' '_').mod"
	fetched=0

	for attempt in $(seq 1 "$RETRIES"); do
		if curl -f -s "$url" -o "$out"; then
			fetched=1
			break
		fi
		if [ "$attempt" -lt "$RETRIES" ]; then
			echo "  attempt $attempt/$RETRIES: $module@$VERSION not on the Go proxy yet, waiting ${RETRY_SLEEP}s..."
			sleep "$RETRY_SLEEP"
		fi
	done

	if [ "$fetched" -ne 1 ]; then
		echo "WARN: $module@$VERSION never appeared on the Go proxy — go.mod not verified"
		skipped=$((skipped + 1))
		continue
	fi

	if ! check_gomod "$out" "$VERSION" "$module@$VERSION"; then
		failed=$((failed + 1))
	fi
done

echo ""
if [ "$failed" -ne 0 ]; then
	annotate_error "$failed published module(s) at $VERSION have a bad go.mod"
	echo "Tags are immutable — the bad tag cannot be fixed in place. Release a new"
	echo "patch version once release.yml's go.mod cleanup is corrected."
	exit 1
fi

if [ "$skipped" -ne 0 ]; then
	echo "$skipped module(s) not verified (Go proxy propagation); the rest are clean."
	exit 0
fi

echo "All published go.mod files at $VERSION are release-clean."
