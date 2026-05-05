#!/usr/bin/env bash
# Emit JSON soft-metrics for the agent's work in /workspace, used as a
# session-level eval via tool_exec → Bash → this script. stdout is the
# only output that reaches EvalResult.Value.
#
# Metrics:
#   total_loc   — total LOC across all *.go files (excluding .git)
#   impl_loc    — total minus test_loc (rough "implementation size")
#   test_loc    — total LOC across *_test.go files
#   files_count — number of *.go files
#
# Both spike bundles seed identically before editing, so per-scenario
# differences in these counts come from the agent's bugfix style. The
# script is git-agnostic — works whether or not the agent initialized
# a repo. Failures emit zero-valued JSON so the report row still
# appears (missing rows distort medians more than zeros).

set -uo pipefail

cd /workspace 2>/dev/null || {
  echo '{"total_loc":0,"impl_loc":0,"test_loc":0,"files_count":0}'
  exit 0
}

count_lines() {
  local pattern="$1"
  local exclude="${2:-}"
  if [[ -n "$exclude" ]]; then
    find . -name "$pattern" -not -name "$exclude" -not -path "./.git/*" -print0 2>/dev/null \
      | xargs -0 -r wc -l 2>/dev/null \
      | awk '/total$/{print $1; found=1} END{if(!found) print 0}' \
      | tail -1
  else
    find . -name "$pattern" -not -path "./.git/*" -print0 2>/dev/null \
      | xargs -0 -r wc -l 2>/dev/null \
      | awk '/total$/{print $1; found=1} END{if(!found) print 0}' \
      | tail -1
  fi
}

count_files() {
  find . -name '*.go' -not -path './.git/*' 2>/dev/null | wc -l | tr -d ' '
}

# When there's only one matching file, wc -l doesn't print a "total"
# line — it just prints "<n> <file>". Handle that case by falling
# through to a single-file count.
single_file_loc() {
  local pattern="$1"
  local exclude="${2:-}"
  local files
  if [[ -n "$exclude" ]]; then
    files=$(find . -name "$pattern" -not -name "$exclude" -not -path "./.git/*" 2>/dev/null)
  else
    files=$(find . -name "$pattern" -not -path "./.git/*" 2>/dev/null)
  fi
  if [[ -z "$files" ]]; then
    echo 0; return
  fi
  echo "$files" | xargs wc -l 2>/dev/null | awk 'END{print $1+0}'
}

total_loc=$(single_file_loc '*.go')
test_loc=$(single_file_loc '*_test.go')
non_test_loc=$(single_file_loc '*.go' '*_test.go')
files_count=$(count_files)

printf '{"total_loc":%d,"impl_loc":%d,"test_loc":%d,"files_count":%d}\n' \
  "${total_loc:-0}" "${non_test_loc:-0}" "${test_loc:-0}" "${files_count:-0}"
