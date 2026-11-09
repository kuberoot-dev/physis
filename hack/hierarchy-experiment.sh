#!/usr/bin/env bash
# Hierarchical planning on the local stand (hack/run-local.sh up): replaces
# the single scheduler with a global one and site schedulers for DE and FR,
# submits a mixed stream, then stops the global level and submits work
# pinned to DE, which the DE site must plan on its own.
set -euo pipefail
cd "$(dirname "$0")/.."
OUT=out/stand; KC=$OUT/kubeconfig
K="kubectl --kubeconfig $KC"
go build -o $OUT/bin/physis-scheduler ./cmd/physis-scheduler
[ -f $OUT/scheduler.pid ] && kill "$(cat $OUT/scheduler.pid)" 2>/dev/null || true
start() { # name args...
  local n=$1; shift
  nohup $OUT/bin/physis-scheduler -kubeconfig $KC -learn results/learn.json -grid-cache $OUT/grid-cache "$@" > $OUT/logs/$n.log 2>&1 &
  echo $! > $OUT/$n.pid
}
start sched-global -role global
start sched-site-de -role site -site-zone DE
start sched-site-fr -role site -site-zone FR
sleep 20
$K create namespace hierarchy2 --dry-run=client -o yaml | $K apply -f - >/dev/null
submit() { # name zones-json
  cat <<YAML | $K apply -f - >/dev/null
apiVersion: physis.kuberoot.dev/v1alpha1
kind: Problem
metadata: {name: $1, namespace: hierarchy2}
spec: {class: maxcut, instance: {gset: G11}, quality: 0.97, confidence: 0.9, deadlineSeconds: 600, policy: min-carbon, zones: $2}
YAML
}
for i in $(seq 1 12); do submit mix-$i '[DE, FR, PL, GB]'; done
sleep 40
echo "== with the global level"
$K -n hierarchy2 get problems -o jsonpath='{range .items[*]}{.metadata.name} {.status.phase} zone={.status.plan.zone} by={.status.plannedBy} on={.status.claimedBy}{"\n"}{end}'
kill "$(cat $OUT/sched-global.pid)"; date -u +%FT%TZ > $OUT/global-stopped.txt
sleep 20
for i in $(seq 1 6); do submit de-$i '[DE]'; submit any-$i '[DE, FR, PL, GB]'; done
sleep 40
echo "== global stopped at $(cat $OUT/global-stopped.txt)"
$K -n hierarchy2 get problems -o jsonpath='{range .items[*]}{.metadata.name} {.status.phase} zone={.status.plan.zone} by={.status.plannedBy} on={.status.claimedBy}{"\n"}{end}' | grep -E "^(de|any)-"
$K -n hierarchy2 get problems -o json > results/hierarchy-problems.json
