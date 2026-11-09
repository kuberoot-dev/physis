#!/usr/bin/env bash
# Hierarchical planning on a kuberoot cluster where physis runs from the Helm
# chart: the chart's scheduler becomes the global level, two copies become
# the DE and FR sites; a mixed stream goes through the global level, then the
# global level is stopped and DE-pinned work is planned by the DE site alone.
#   D=out/workshop hack/hierarchy-cluster.sh
set -euo pipefail
cd "$(dirname "$0")/.."
D=${D:-out/workshop}; NS=${NS:-hierarchy}
K="kubectl --kubeconfig $D/cluster.kubeconfig"
cat <<'YAML' | $K apply -f - >/dev/null
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata: {name: physis-scheduler-leases, namespace: physis-system}
rules:
- {apiGroups: [coordination.k8s.io], resources: [leases], verbs: [get, create, update]}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata: {name: physis-scheduler-leases, namespace: physis-system}
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: Role, name: physis-scheduler-leases}
subjects: [{kind: ServiceAccount, name: physis-scheduler, namespace: physis-system}]
YAML
# A deployment per level, from the chart's scheduler with -role arguments.
level() { # name args...
  local n=$1; shift
  $K -n physis-system get deploy physis-scheduler -o json | python3 -c '
import json,sys
d=json.load(sys.stdin); name=sys.argv[1]; extra=sys.argv[2:]
for k in ["uid","resourceVersion","creationTimestamp","generation","managedFields","annotations"]: d["metadata"].pop(k,None)
d.pop("status",None); d["metadata"]["name"]=name
d["spec"]["replicas"]=1; d["spec"]["selector"]["matchLabels"]={"app":name}; d["spec"]["template"]["metadata"]["labels"]={"app":name}
c=d["spec"]["template"]["spec"]["containers"][0]; c["command"]=c["command"]+extra
print(json.dumps(d))' "$n" "$@" | $K apply -f - >/dev/null
}
$K -n physis-system scale deploy physis-scheduler --replicas=0 >/dev/null
level sched-global -role global
level sched-site-de -role site -site-zone DE
level sched-site-fr -role site -site-zone FR
for d in sched-global sched-site-de sched-site-fr; do
  $K -n physis-system rollout status deploy/$d --timeout=300s >/dev/null
  # A new scheduler fetches the grid data first; the source rate-limits.
  until $K -n physis-system logs deploy/$d 2>/dev/null | grep -q "grid data loaded"; do sleep 10; done
done
$K create namespace $NS --dry-run=client -o yaml | $K apply -f - >/dev/null
submit() { cat <<YAML | $K apply -f - >/dev/null
apiVersion: physis.kuberoot.dev/v1alpha1
kind: Problem
metadata: {name: $1, namespace: $NS}
spec: {class: maxcut, instance: {gset: G11}, quality: 0.97, confidence: 0.9, deadlineSeconds: 600, policy: min-carbon, zones: $2}
YAML
}
for i in $(seq 1 12); do submit mix-$i '[DE, FR, PL, GB]'; done
sleep 50
$K -n physis-system logs deploy/sched-global > $D/log-$NS-sched-global-before-stop.txt 2>&1 || true
$K -n physis-system scale deploy sched-global --replicas=0 >/dev/null
date -u +%FT%TZ > $D/global-stopped.txt
sleep 30
for i in $(seq 1 6); do submit de-$i '[DE]'; submit any-$i '[DE, FR, PL, GB]'; done
sleep 60
$K -n $NS get problems -o json --show-managed-fields > results/hierarchy-cluster-problems.json
$K -n $NS get problems -o jsonpath='{range .items[*]}{.metadata.name} {.status.phase} by={.status.plannedBy} zone={.status.plan.zone} on={.status.claimedBy}{"\n"}{end}'
for d in sched-global sched-site-de sched-site-fr; do $K -n physis-system logs deploy/$d > $D/log-$NS-$d.txt 2>&1 || true; done
# Back to a single scheduler.
for d in sched-global sched-site-de sched-site-fr; do $K -n physis-system scale deploy $d --replicas=0 >/dev/null; done
$K -n physis-system scale deploy physis-scheduler --replicas=1 >/dev/null
