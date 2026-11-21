#!/usr/bin/env bash
# TEMPORARY diagnostic: measures what one gremlins mutant costs on this runner.
set -uo pipefail
ROOT="$(git rev-parse --show-toplevel)"
PKG=github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage
W="$(mktemp -d)"
export GOWORK=off GOFLAGS="-count=1 -trimpath ${DIAG_EXTRA_GOFLAGS:-}"
echo "nproc=$(nproc)"; lscpu | grep -E 'Model name|^CPU\(s\)|Thread|Core|Socket'
free -g | head -2

tm() { /usr/bin/time -f "%e %U %S" -o "$W/t" "$@" >"$W/out" 2>&1; rc=$?; read -r r u s <"$W/t"; echo "real=${r} cpu=$(echo "$u+$s" | bc) rc=${rc}"; }

cp -r "${ROOT}/runtime" "$W/rt0"
echo "== cold coverage run (gremlins step 1)"; ( cd "$W/rt0/pipeline/stage" && tm go test -cover -coverprofile "$W/c.out" ./... )
echo "== warm unchanged run"; ( cd "$W/rt0/pipeline/stage" && tm go test -failfast "$PKG" && tm go test -failfast "$PKG" )

mutate() { # dir n
  sed -i "30s/config == nil/config == nil \&\& ($2 == $2)/" "$1/pipeline/stage/builder.go"
}
restore() { cp "${ROOT}/runtime/pipeline/stage/builder.go" "$1/pipeline/stage/builder.go"; }

echo "== solo surviving mutant x3"
for i in 1 2 3; do restore "$W/rt0"; mutate "$W/rt0" "$i"; ( cd "$W/rt0/pipeline/stage" && tm go test -failfast "$PKG" ); done
echo "== solo mutant build-only x2 (-c)"
for i in 4 5; do restore "$W/rt0"; mutate "$W/rt0" "$i"; ( cd "$W/rt0/pipeline/stage" && tm go test -c -o "$W/st.test" "$PKG" ); done
echo "== solo test binary run x2"
for i in 1 2; do ( cd "$W/rt0/pipeline/stage" && tm "$W/st.test" -test.failfast -test.count=1 ); done
restore "$W/rt0"

cat >"$W/tx.sh" <<EOF
#!/bin/bash
t=\${1##*/}
/usr/bin/time -f "%e %U %S" -o "$W/tx.\$\$" "\$@"; rc=\$?
read -r r u s < "$W/tx.\$\$"; echo "\$t \$r \$(echo "\$u+\$s" | bc)" >> "$W/tx.log"; rm -f "$W/tx.\$\$"; exit \$rc
EOF
chmod +x "$W/tx.sh"
echo "== per-tool breakdown of one mutant (toolexec, after warm-up)"
( cd "$W/rt0/pipeline/stage" && go test -toolexec "$W/tx.sh" -failfast "$PKG" >/dev/null 2>&1 )
rm -f "$W/tx.log"; mutate "$W/rt0" 99
( cd "$W/rt0/pipeline/stage" && tm go test -toolexec "$W/tx.sh" -failfast "$PKG" )
awk '$2 > 0.05 {print "  tool", $0}' "$W/tx.log"; restore "$W/rt0"

for N in 4 8; do
  echo "== ${N} concurrent workers, 3 rounds of surviving mutants (copies like gremlins)"
  for k in $(seq 1 "$N"); do [ -d "$W/rt$k" ] || cp -r "${ROOT}/runtime" "$W/rt$k"; done
  vmstat 5 >"$W/vm.$N" & vm=$!
  start=$(date +%s.%N); pids=()
  for k in $(seq 1 "$N"); do
    ( for round in 1 2 3; do restore "$W/rt$k"; mutate "$W/rt$k" "$((N*100+k*10+round))"
        ( cd "$W/rt$k/pipeline/stage" && /usr/bin/time -f "w$k r$round %e %U %S" -a -o "$W/conc.$N" go test -failfast "$PKG" >/dev/null 2>&1 ); done ) &
    pids+=($!)
  done
  wait "${pids[@]}"
  end=$(date +%s.%N); kill "$vm"
  echo "  wall=$(echo "$end-$start" | bc) for $((N*3)) mutants => $(echo "scale=2;($end-$start)/($N*3)" | bc)s per mutant"
  awk '{print "  ", $0}' "$W/conc.$N"
  echo "  vmstat (us sy id wa):"; awk 'NR>2 {print "   ", $13, $14, $15, $16}' "$W/vm.$N"
done
