#!/usr/bin/env bash
# Runs physis on a control plane with no nodes: etcd + kube-apiserver from the
# envtest binaries, the scheduler and one agent per substrate of
# hack/fleet.tsv, all as local processes. Logs and state go to out/stand.
#
#   hack/run-local.sh up      start everything (returns when all agents report)
#   hack/run-local.sh down    stop everything
set -euo pipefail
cd "$(dirname "$0")/.."
OUT=out/stand
ASSETS=$(ls -d .stand/envtest/k8s/* | head -1)
KC=$OUT/kubeconfig
case "${1:-}" in
  up)
    mkdir -p $OUT/bin $OUT/logs
    for c in physis-scheduler physis-agent physis-conformance; do go build -o $OUT/bin/$c ./cmd/$c; done
    go build -o $OUT/bin/stand ./hack/stand
    rm -f $KC
    nohup $OUT/bin/stand -assets "$ASSETS" -out $KC > $OUT/logs/stand.log 2>&1 &
    echo $! > $OUT/stand.pid
    for i in $(seq 60); do [ -s $KC ] && break; sleep 1; done
    kubectl --kubeconfig $KC apply -f deploy/rbac.yaml -f deploy/links.yaml >/dev/null
    kubectl --kubeconfig $KC -n physis-system create configmap principals >/dev/null 2>&1 || true
    nohup $OUT/bin/physis-scheduler -kubeconfig $KC -learn out/calibration/learn.json -grid-cache $OUT/grid-cache > $OUT/logs/scheduler.log 2>&1 &
    echo $! > $OUT/scheduler.pid
    # Agents read the grid cache the scheduler fills; start them after it.
    for i in $(seq 120); do grep -q "grid data loaded" $OUT/logs/scheduler.log 2>/dev/null && break; sleep 5; done
    grep -v '^#' hack/fleet.tsv | while IFS=$'\t' read -r name phys zone op jur slots; do
      mkdir -p $OUT/state/$name
      nohup $OUT/bin/physis-agent -kubeconfig $KC -name "$name" -physics "$phys" -zone "$zone" -operator "$op" \
        -jurisdiction "$jur" -slots "$slots" -data data -state $OUT/state/$name -grid-cache $OUT/grid-cache -code "physis-agent/$(git rev-parse --short HEAD)" \
        > $OUT/logs/agent-$name.log 2>&1 &
      echo $! > $OUT/agent-$name.pid
    done
    for i in $(seq 60); do
      n=$(kubectl --kubeconfig $KC get substrates --no-headers 2>/dev/null | wc -l | tr -d ' ')
      [ "$n" -ge "$(grep -vc '^#' hack/fleet.tsv)" ] && break; sleep 1
    done
    kubectl --kubeconfig $KC get substrates
    echo "nodes in this cluster: $(kubectl --kubeconfig $KC get nodes --no-headers 2>/dev/null | wc -l | tr -d ' ')" ;;
  down)
    for f in $OUT/*.pid; do [ -f "$f" ] && kill "$(cat "$f")" 2>/dev/null || true; rm -f "$f"; done ;;
  *) echo "usage: $0 up|down"; exit 1 ;;
esac
