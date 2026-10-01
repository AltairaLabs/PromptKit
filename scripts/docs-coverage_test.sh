#!/usr/bin/env bash
# Tests for scripts/docs-coverage.sh
set -u
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$REPO_ROOT/scripts/docs-coverage.sh"
FAILURES=0

# tree: two surface files under api/; reference/a.md cites only one of them.
tree() {
	local d; d=$(mktemp -d)
	mkdir -p "$d/api/sub" "$d/docs/src/content/docs/reference" "$d/scripts"
	printf '# public surface\napi/*.go\n' > "$d/scripts/docs-surface.txt"
	echo "package v1" > "$d/api/covered.go"
	echo "package v1" > "$d/api/newkind.go"
	printf -- '---\ntitle: A\nverified:\n  commit: abc\n  sources:\n    - api/covered.go\n---\n' \
		> "$d/docs/src/content/docs/reference/a.md"
	echo "$d"
}

run_case() {
	local name="$1" d="$2" want="$3"
	local out; out=$(bash "$SCRIPT" --root "$d" 2>&1); local rc=$?
	if [ "$rc" != 0 ]; then echo "FAIL - $name (exit $rc)"; FAILURES=1; return; fi
	if [ "$want" = EMPTY ]; then [ -z "$out" ] && { echo "ok   - $name"; return; }
	elif [ "$out" = "$want" ]; then echo "ok   - $name"; return; fi
	echo "FAIL - $name (want: $want)"; echo "$out" | sed 's/^/       /'; FAILURES=1
}

d=$(tree)
run_case "uncited surface file is uncovered" "$d" "uncovered api/newkind.go"

d=$(tree); printf '# intentionally undocumented\napi/newkind.go\n' > "$d/scripts/docs-coverage-ignore.txt"
run_case "ignored file is not reported" "$d" EMPTY

d=$(tree); printf '# a comment only\n\n' > "$d/scripts/docs-coverage-ignore.txt"
run_case "comment lines ignore nothing" "$d" "uncovered api/newkind.go"

d=$(tree); echo "package v1" > "$d/api/newkind_test.go"
run_case "test files are not surface" "$d" "uncovered api/newkind.go"

d=$(tree); echo "package v1" > "$d/api/sub/deep.go"; echo 'api/**/*.go' >> "$d/scripts/docs-surface.txt"
run_case "recursive globs reach nested files" "$d" "$(printf 'uncovered api/newkind.go\nuncovered api/sub/deep.go')"

d=$(tree); rm "$d/scripts/docs-surface.txt"
run_case "no surface list reports nothing" "$d" EMPTY

exit $FAILURES
