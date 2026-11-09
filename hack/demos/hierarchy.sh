#!/usr/bin/env bash
# Screencast: hierarchical planning on the kuberoot cluster. Needs the level
# deployments made by hack/hierarchy-cluster.sh (sched-global, sched-site-de,
# sched-site-fr in physis-system).
set -euo pipefail; cd "$(dirname "$0")/../.."; . hack/demos/lib.sh
NS=hierarchy-demo
k -n physis-system scale deploy physis-scheduler --replicas=0 >/dev/null
for d in sched-global sched-site-de sched-site-fr; do k -n physis-system scale deploy $d --replicas=1 >/dev/null; done
for d in sched-global sched-site-de sched-site-fr; do k -n physis-system rollout status deploy/$d --timeout=300s >/dev/null; until k -n physis-system logs deploy/$d 2>/dev/null | grep -q "grid data loaded"; do sleep 5; done; done
k create namespace $NS --dry-run=client -o yaml | k apply -f - >/dev/null
submit() { cat <<YAML | k apply -f - >/dev/null
apiVersion: physis.kuberoot.dev/v1alpha1
kind: Problem
metadata: {name: $1, namespace: $NS}
spec: {class: maxcut, instance: {gset: G11}, quality: 0.97, confidence: 0.9, deadlineSeconds: 600, policy: min-carbon, zones: $2}
YAML
}
say "Three planning levels announce themselves with Leases: the global level and the sites for DE and FR"
show "k -n physis-system get leases -o custom-columns=LEVEL:.metadata.name,RENEWED:.spec.renewTime | grep physis"
say "Six problems that may run in any zone: the global level picks the zone, the site picks the substrate"
for i in 1 2 3 4 5 6; do submit mix-$i '[DE, FR, PL, GB]'; done; sleep 25
show "k -n $NS get problems -o custom-columns=PROBLEM:.metadata.name,PLANNED_BY:.status.plannedBy,ZONE:.status.plan.zone,SUBSTRATE:.status.claimedBy,PHASE:.status.phase"
say "The centre goes away"
show "k -n physis-system scale deploy sched-global --replicas=0"
sleep 20
say "Work pinned to DE keeps flowing: the DE site plans it on its own"
for i in 1 2 3 4; do submit de-$i '[DE]'; done; sleep 30
show "k -n $NS get problems -o custom-columns=PROBLEM:.metadata.name,PLANNED_BY:.status.plannedBy,ZONE:.status.plan.zone,SUBSTRATE:.status.claimedBy,PHASE:.status.phase | grep -E 'PROBLEM|de-'"
for d in sched-site-de sched-site-fr; do k -n physis-system scale deploy $d --replicas=0 >/dev/null; done
k -n physis-system scale deploy physis-scheduler --replicas=1 >/dev/null
