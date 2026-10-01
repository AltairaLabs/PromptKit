#!/usr/bin/env bash
# Tests for scripts/docs-provenance.py
set -u
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$REPO_ROOT/scripts/docs-provenance.py"
FAILURES=0
P=docs/src/content/docs/reference/a.md

# fx: temp git repo with a page and a few source files; prints its dir.
fx() {
	local d; d=$(mktemp -d)
	mkdir -p "$d/docs/src/content/docs/reference" "$d/.github/workflows" "$d/internal/db" "$d/src"
	printf -- '---\ntitle: A\n---\n\nBody.\n' > "$d/$P"
	echo "on: push" > "$d/.github/workflows/ci.yml"
	echo "select 1;" > "$d/internal/db/001.sql"
	echo "package x" > "$d/src/x.go"; echo "package y" > "$d/src/y.go"
	git -C "$d" init -q; git -C "$d" -c user.email=t@t -c user.name=t add -A
	git -C "$d" -c user.email=t@t -c user.name=t commit -q -m c1
	echo "$d"
}
ok() { echo "ok   - $1"; }
bad() { echo "FAIL - $1"; FAILURES=1; }

# dot-prefixed paths and .sql evidence are recorded
d=$(fx)
printf '{"results":[{"id":1,"verdict":"true","evidence":[".github/workflows/ci.yml:1","internal/db/001.sql:1"]}]}' > "$d/s3.json"
(cd "$d" && python3 "$SCRIPT" "$P" missing.json s3.json HEAD >/dev/null)
grep -q -- '- .github/workflows/ci.yml' "$d/$P" && ok "dot-prefixed path recorded" || bad "dot-prefixed path recorded"
grep -q -- '- internal/db/001.sql' "$d/$P" && ok ".sql evidence recorded" || bad ".sql evidence recorded"

# .tsx paths, [param] route segments and Dockerfile.<variant> are recorded whole
mkdir -p "$d/app/[name]"; echo "x" > "$d/app/[name]/page.tsx"; echo "FROM x" > "$d/Dockerfile.cli"
git -C "$d" -c user.email=t@t -c user.name=t add -A; git -C "$d" -c user.email=t@t -c user.name=t commit -q -m c2
printf '{"results":[{"id":1,"verdict":"true","evidence":["app/[name]/page.tsx:1","Dockerfile.cli:1"]}]}' > "$d/s3.json"
(cd "$d" && python3 "$SCRIPT" "$P" missing.json s3.json HEAD >/dev/null)
grep -qF -- '- app/[name]/page.tsx' "$d/$P" && ok ".tsx route path recorded" || bad ".tsx route path recorded"
grep -qF -- '- Dockerfile.cli' "$d/$P" && ok "Dockerfile variant recorded" || bad "Dockerfile variant recorded"

# a ref like HEAD is resolved to a full sha
sha=$(git -C "$d" rev-parse HEAD)
grep -q "commit: $sha" "$d/$P" && ok "commit ref resolved to sha" || bad "commit ref resolved to sha"

# runs from any working directory
d=$(fx)
printf '{"results":[{"id":1,"verdict":"true","evidence":["src/x.go:1"]}]}' > "$d/s3.json"
(cd "$d/docs" && python3 "$SCRIPT" "$P" ../missing.json ../s3.json HEAD >/dev/null 2>&1)
grep -q -- '- src/x.go' "$d/$P" && ok "works from a subdirectory" || bad "works from a subdirectory"

# --merge keeps untouched claims' sources and replaces re-verified ones by id
d=$(fx)
printf '{"results":[{"id":1,"verdict":"true","evidence":["src/x.go:1"]},{"id":2,"verdict":"true","evidence":["src/y.go:1"]}]}' > "$d/full.json"
(cd "$d" && python3 "$SCRIPT" "$P" missing.json full.json HEAD >/dev/null)
printf '{"results":[{"id":2,"verdict":"false","evidence":["src/y.go:2"]}]}' > "$d/part.json"
(cd "$d" && python3 "$SCRIPT" --merge "$P" missing.json part.json HEAD >/dev/null)
if grep -q -- '- src/x.go' "$d/$P" && grep -q -- '- src/y.go' "$d/$P" &&
	python3 -c "import json,sys;c={x['id']:x for x in json.load(open('$d/docs/provenance/reference/a.json'))['claims']};sys.exit(0 if c[2]['verdict']=='false' and c[1]['verdict']=='true' else 1)"; then
	ok "--merge keeps untouched claims"
else bad "--merge keeps untouched claims"; fi

exit $FAILURES
