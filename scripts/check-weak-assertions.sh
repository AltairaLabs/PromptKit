#!/usr/bin/env bash
#
# Fails when a commit adds a test whose assertions cannot fail — one asserting
# only NoError/Nil/NotNil, which passes whether or not the code under test
# produced the right answer while still counting toward the coverage gate.
#
# Nine such tests were written during the guardrail work (#1678/#1683/#1684);
# one went green against a bug that had been observed minutes earlier and would
# have closed the issue as unreproducible.
#
# New findings only: the ~678 pre-existing ones are a separate, deliberate
# cleanup and must not block unrelated work. Mirrors the --new-from-rev
# convention golangci-lint uses in the pre-commit hook.
#
# Run from the repo root: scripts/check-weak-assertions.sh [base-ref]
set -euo pipefail

# Some execution contexts (e.g. worktree pre-commit hooks — see
# reference_worktree_gitdir_golangci) leak GIT_DIR/GIT_WORK_TREE/GIT_INDEX_FILE
# into subprocesses, which silently redirects every git command below at the
# wrong repo. Unset defensively so `git worktree add`/`merge-base` always
# resolve against the repo this script actually lives in.
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE

BASE="${1:-origin/main}"
ROOT="$(git rev-parse --show-toplevel)"
SCAN="go -C ${ROOT}/tools/seamscan run ."

# Findings on the merge base, then on the working tree; anything only in the
# latter is new. Subject+kind identifies a finding stably across line moves,
# so that pair is what the diff runs on; file+line is kept alongside on the
# head side only, purely to report where to look — a scan can carry the same
# subject in several files (sdk/examples/*/smoke_test.go all define
# TestSmoke), so the kind+subject tuple alone doesn't say which one to open.
# Paths passed to seamscan are absolute so `go -C` (which changes the process
# working directory, not just the module lookup) doesn't make them resolve
# against tools/seamscan instead of the tree we mean to scan.
tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

git -C "${ROOT}" worktree add --quiet --detach "${tmp}/base" "$(git -C "${ROOT}" merge-base HEAD "${BASE}")"
${SCAN} weak-assertions "${tmp}/base/runtime" "${tmp}/base/sdk" "${tmp}/base/pkg" 2>/dev/null \
  | grep -oE '"kind": "[^"]+"|"subject": "[^"]+"' | paste - - | sort > "${tmp}/base_ks.txt" || true
git -C "${ROOT}" worktree remove --force "${tmp}/base"

${SCAN} weak-assertions "${ROOT}/runtime" "${ROOT}/sdk" "${ROOT}/pkg" 2>/dev/null \
  | grep -oE '"kind": "[^"]+"|"file": "[^"]+"|"line": [0-9]+|"subject": "[^"]+"' \
  | paste - - - - > "${tmp}/head_full.txt"
cut -f1,4 "${tmp}/head_full.txt" | sort > "${tmp}/head_ks.txt"

# Split from the `if` deliberately: with set -e, `if cmd && [ ... ]` would
# swallow a genuine comm failure as "no findings", which would make the guard
# silently useless — the exact failure mode this whole plan exists to prevent.
new="$(comm -13 "${tmp}/base_ks.txt" "${tmp}/head_ks.txt" || true)"
if [ -n "${new}" ]; then
  while IFS= read -r ks; do
    [ -z "${ks}" ] && continue
    awk -F'\t' -v ks="${ks}" -v OFS='\t' '
      $1"\t"$4 == ks {
        file = $2; sub(/^"file": "/, "", file); sub(/"$/, "", file)
        line = $3; sub(/^"line": /, "", line)
        kind = $1; sub(/^"kind": "/, "", kind); sub(/"$/, "", kind)
        subj = $4; sub(/^"subject": "/, "", subj); sub(/"$/, "", subj)
        printf "FAIL: %s:%s\t%s\t%s\n", file, line, kind, subj
      }' "${tmp}/head_full.txt"
  done <<< "${new}"
  echo ""
  echo "These tests assert only that a call did not fail, so they pass whether or"
  echo "not the code produced the right answer. Name the mutation each assertion"
  echo "would catch — break the implementation in your head and check the test"
  echo "notices — then strengthen it, or delete the test."
  exit 1
fi

echo "OK:   no new tests with unfalsifiable assertions."
