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
# New findings only: the ~676 pre-existing ones are a separate, deliberate
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
# latter is new. The comparison key is kind+file+subject — file is part of
# identity, not just display: several files legitimately share a subject
# (sdk/examples/*/smoke_test.go all define TestSmoke), and comparing on
# kind+subject alone means one genuinely new finding makes every other
# same-named pre-existing finding look new too, in every file that happens to
# share the name. Line is deliberately left out of the key (carried on the
# head side only, for display): a comparison that included line would make an
# unrelated edit that merely shifts an existing test's line number look like a
# newly introduced one.
#
# The base scan runs against a throwaway worktree under a temp directory, so
# its absolute file paths never equal the head scan's paths even for a file
# that didn't change — comparing raw absolute paths would make every
# pre-existing finding look new. Both sides are normalized to a path relative
# to the tree that was scanned (stripping the temp-worktree prefix on the base
# side, ${ROOT}/ on the head side) via literal index()/substr(), not a regex
# substitution, so a path containing regex metacharacters can't miscompare.
#
# Paths passed to seamscan are absolute so `go -C` (which changes the process
# working directory, not just the module lookup) doesn't make them resolve
# against tools/seamscan instead of the tree we mean to scan.
tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

relativize_file_field() {
  # Strips a literal "<prefix>/" from the value of a `"file": "..."` grep
  # match, leaving other matched lines (kind/line/subject) untouched. Uses
  # index()/substr() rather than sub()'s regex so the prefix (an arbitrary
  # absolute path) never needs escaping.
  awk -v pfx="$1/" '
    {
      marker = "\"file\": \"" pfx
      if (index($0, marker) == 1) {
        print "\"file\": \"" substr($0, length(marker) + 1)
      } else {
        print
      }
    }'
}

BASE_CHECKOUT="${tmp}/base"
git -C "${ROOT}" worktree add --quiet --detach "${BASE_CHECKOUT}" "$(git -C "${ROOT}" merge-base HEAD "${BASE}")"
${SCAN} weak-assertions "${BASE_CHECKOUT}/runtime" "${BASE_CHECKOUT}/sdk" "${BASE_CHECKOUT}/pkg" 2>/dev/null \
  | grep -oE '"kind": "[^"]+"|"file": "[^"]+"|"subject": "[^"]+"' \
  | relativize_file_field "${BASE_CHECKOUT}" \
  | paste - - - | sort > "${tmp}/base_kfs.txt" || true
git -C "${ROOT}" worktree remove --force "${BASE_CHECKOUT}"

${SCAN} weak-assertions "${ROOT}/runtime" "${ROOT}/sdk" "${ROOT}/pkg" 2>/dev/null \
  | grep -oE '"kind": "[^"]+"|"file": "[^"]+"|"line": [0-9]+|"subject": "[^"]+"' \
  | relativize_file_field "${ROOT}" \
  | paste - - - - > "${tmp}/head_full.txt"
cut -f1,2,4 "${tmp}/head_full.txt" | sort > "${tmp}/head_kfs.txt"

# Split from the `if` deliberately: with set -e, `if cmd && [ ... ]` would
# swallow a genuine comm failure as "no findings", which would make the guard
# silently useless — the exact failure mode this whole plan exists to prevent.
new="$(comm -13 "${tmp}/base_kfs.txt" "${tmp}/head_kfs.txt" || true)"
if [ -n "${new}" ]; then
  while IFS= read -r kfs; do
    [ -z "${kfs}" ] && continue
    # Matches the exact (kind, file, subject) triple identified as new —
    # not just subject+kind — so a shared name in an untouched file never
    # gets pulled in alongside the one genuinely new finding.
    awk -F'\t' -v kfs="${kfs}" -v OFS='\t' '
      $1"\t"$2"\t"$4 == kfs {
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
