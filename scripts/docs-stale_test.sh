#!/usr/bin/env bash
# Tests for scripts/docs-stale.sh (line-level staleness).
set -u
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$REPO_ROOT/scripts/docs-stale.sh"
FAILURES=0
P=docs/src/content/docs/reference
G=(-c user.email=t@t -c user.name=t)

# repo: temp git repo with a 30-line src/x.go and a 30-line public-surface file api/foo.go.
repo() {
	local d; d=$(mktemp -d)
	mkdir -p "$d/src" "$d/api" "$d/scripts" "$d/$P" "$d/docs/provenance/reference"
	seq 1 30 | sed 's/^/line /' > "$d/src/x.go"
	seq 1 30 | sed 's/^/field /' > "$d/api/foo.go"
	echo "api/*.go" > "$d/scripts/docs-surface.txt"
	git -C "$d" init -q && git -C "$d" "${G[@]}" add -A && git -C "$d" "${G[@]}" commit -q -m c1
	echo "$d $(git -C "$d" rev-parse HEAD)"
}

# page <dir> <name> <commit> <sources-csv> [evidence...]: page + sidecar (sidecar only if evidence given).
page() {
	local d="$1" n="$2" c="$3" srcs="$4"; shift 4
	{
		echo "---"; echo "title: $n"
		if [ -n "$c" ]; then
			echo "verified:"; echo "  commit: $c"; echo "  sources:"
			IFS=, read -ra S <<<"$srcs"; for s in "${S[@]}"; do [ -n "$s" ] && echo "    - $s"; done
		fi
		echo "---"; echo; echo "Body."
	} > "$d/$P/$n.md"
	if [ $# -gt 0 ]; then
		local ev; ev=$(printf '"%s",' "$@"); ev="[${ev%,}]"
		printf '{"page":"%s","commit":"%s","claims":[{"id":1,"claim":"c","verdict":"true","evidence":%s}]}\n' \
			"$P/$n.md" "$c" "$ev" > "$d/docs/provenance/reference/$n.json"
	fi
}

edit_line() { sed -i.bak "$2s/.*/changed/" "$1" && rm -f "$1.bak"; }

run_case() {
	local name="$1" d="$2" want="$3"; shift 3
	local out; out=$(bash "$SCRIPT" --root "$d" "$@" 2>&1); local rc=$?
	if [ "$rc" != 0 ]; then echo "FAIL - $name (exit $rc)"; echo "$out" | sed 's/^/       /'; FAILURES=1; return; fi
	if [ "$want" = EMPTY ]; then
		[ -z "$out" ] && { echo "ok   - $name"; return; }
	elif grep -qE -- "$want" <<<"$out"; then echo "ok   - $name"; return; fi
	echo "FAIL - $name (want /$want/)"; echo "$out" | sed 's/^/       /'; FAILURES=1
}

read -r d c <<<"$(repo)"; page "$d" a "$c" src/x.go src/x.go:2
run_case "unchanged source is fresh" "$d" EMPTY

read -r d c <<<"$(repo)"; page "$d" a "$c" src/x.go src/x.go:2; edit_line "$d/src/x.go" 20
run_case "change far from the cited line is fresh" "$d" EMPTY

read -r d c <<<"$(repo)"; page "$d" a "$c" src/x.go src/x.go:2; edit_line "$d/src/x.go" 3
run_case "change near the cited line is stale" "$d" "^stale $P/a.md: src/x.go \(cited lines 2\)"

read -r d c <<<"$(repo)"; page "$d" a "$c" src/x.go src/x.go; edit_line "$d/src/x.go" 20
run_case "evidence without a line: any change is stale" "$d" "^stale $P/a.md: src/x.go \(cited without a line\)"

read -r d c <<<"$(repo)"; page "$d" a "$c" src/x.go; edit_line "$d/src/x.go" 20
run_case "no sidecar falls back to file level" "$d" "^stale $P/a.md: src/x.go \(file changed\)"

read -r d c <<<"$(repo)"
page "$d" owner "$c" api/foo.go api/foo.go:2
printf '{"page":"%s","commit":"%s","claims":[{"id":1,"evidence":["api/foo.go:2"]},{"id":2,"evidence":["api/foo.go:3"]}]}\n' "$P/owner.md" "$c" > "$d/docs/provenance/reference/owner.json"
page "$d" other "$c" api/foo.go api/foo.go:2
echo "field new" >> "$d/api/foo.go"
run_case "added surface flags the owning page" "$d" "^stale $P/owner.md: api/foo.go \(surface added\)"
out=$(bash "$SCRIPT" --root "$d"); if grep -q "other.md" <<<"$out"; then echo "FAIL - added surface spares other pages"; FAILURES=1; else echo "ok   - added surface spares other pages"; fi

read -r d c <<<"$(repo)"; page "$d" a "$c" src/x.go src/x.go:2; rm "$d/src/x.go"
run_case "deleted source is missing" "$d" "^missing $P/a.md: src/x.go"

read -r d c <<<"$(repo)"; page "$d" a 0123456789abcdef0123456789abcdef01234567 src/x.go src/x.go:2
run_case "unknown commit is reported" "$d" "^unknown-commit $P/a.md"

read -r d c <<<"$(repo)"; page "$d" a "" ""
run_case "page without verified is unverified" "$d" "^unverified $P/a.md"

read -r d c <<<"$(repo)"; page "$d" a "$c" ""
run_case "empty sources is unverified" "$d" "^unverified $P/a.md"

# PR mode: verified at c1; c2 inserts 5 lines at the top; c3 (the PR) edits the cited line, now shifted.
read -r d c <<<"$(repo)"; page "$d" a "$c" src/x.go src/x.go:10
git -C "$d" "${G[@]}" add -A && git -C "$d" "${G[@]}" commit -q -m page
{ printf 'top\ntop\ntop\ntop\ntop\n'; cat "$d/src/x.go"; } > "$d/t" && mv "$d/t" "$d/src/x.go"
git -C "$d" "${G[@]}" commit -qam c2; b=$(git -C "$d" rev-parse HEAD)
edit_line "$d/src/x.go" 15; git -C "$d" "${G[@]}" commit -qam c3
run_case "PR mode follows shifted lines" "$d" "^stale $P/a.md: src/x.go \(cited lines 10\)" --base "$b"

read -r d c <<<"$(repo)"; page "$d" a "$c" src/x.go src/x.go:10
git -C "$d" "${G[@]}" add -A && git -C "$d" "${G[@]}" commit -q -m page; b=$(git -C "$d" rev-parse HEAD)
edit_line "$d/src/x.go" 25; git -C "$d" "${G[@]}" commit -qam pr
run_case "PR mode ignores unrelated hunks" "$d" EMPTY --base "$b"

exit $FAILURES
