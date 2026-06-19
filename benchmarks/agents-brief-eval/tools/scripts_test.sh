#!/usr/bin/env bash
# Tests for unused-files.sh and idiom-traps.sh. Run: bash scripts_test.sh
set -u
here="$(cd "$(dirname "$0")" && pwd)"
fail=0
check() { # desc, expected_rc, actual_rc
  if [ "$2" != "$3" ]; then echo "FAIL: $1 (want rc=$2 got rc=$3)"; fail=1; else echo "ok: $1"; fi
}

bash "$here/unused-files.sh" "$here/testdata/good-kit"; check "good-kit has no orphans" 0 $?
bash "$here/unused-files.sh" "$here/testdata/trap-kit"; check "trap-kit has an orphan" 1 $?
bash "$here/unused-files.sh" /no/such/dir; check "missing dir errors" 2 $?

out="$(bash "$here/idiom-traps.sh" "$here/testdata/trap-kit")"; rc=$?
check "idiom-traps exits 0" 0 $rc
echo "$out" | grep -q '"idiom_traps": 1' && echo "ok: trap counted" || { echo "FAIL: trap not counted: $out"; fail=1; }

exit $fail
