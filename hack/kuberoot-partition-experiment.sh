#!/usr/bin/env bash
# A real network partition on the two-node kuberoot cluster: while a stream of
# problems runs, a privileged pod on the worker takes its cluster link (eth1)
# down for a while and brings it back. Agents on the worker lose the API
# server -- heartbeats, claims and reports -- but keep computing; work planned
# for them and not yet claimed must go elsewhere, and work they already
# claimed must be reported after the link returns. The scheduler is pinned to
# the control plane so that planning continues during the cut.
#   hack/kuberoot-partition-experiment.sh [delay-s] [cut-s]
set -euo pipefail
cd "$(dirname "$0")/.."
D=out/kuberoot-2n
K="kubectl --kubeconfig $D/cluster.kubeconfig"
DELAY=${1:-90}; CUT=${2:-120}
CP=kuberoot-000011; WORKER=kuberoot-000012
$K -n physis-system patch deploy physis-scheduler --type merge -p "{\"spec\":{\"template\":{\"spec\":{\"nodeName\":\"$CP\"}}}}" >/dev/null
$K -n physis-system rollout status deploy/physis-scheduler --timeout=300s
echo "agents on the worker:"; $K -n physis-system get pods -l app=physis-agent -o wide --no-headers | awk -v w=$WORKER '$7==w {print "  " $1}'
go run ./cmd/physis-load -kubeconfig $D/cluster.kubeconfig -namespace ${NS:-load-partition} -n 120 -every 3s \
  -out results/live-kuberoot-2node-partition.csv > $D/load-partition.log 2>&1 &
LOAD=$!
sleep "$DELAY"
cut_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
cat <<YAML | $K apply -f -
apiVersion: v1
kind: Pod
metadata: {name: cut-link, namespace: kube-system}
spec:
  nodeName: $WORKER
  hostNetwork: true
  restartPolicy: Never
  containers:
  - name: cut
    image: busybox:1.37
    securityContext: {privileged: true}
    command: [sh, -c, "date -u; ip link set eth1 down; sleep $CUT; ip link set eth1 up; date -u"]
YAML
echo "cut requested at $cut_at for ${CUT}s"
wait $LOAD
echo "load finished: $(tail -1 $D/load-partition.log)"
$K -n kube-system logs cut-link || true
echo "$cut_at $CUT" > $D/partition-window.txt
