#!/usr/bin/env bash
# Screencast: a real network cut of one worker on the kuberoot cluster while a
# stream of problems runs; work flows around the cut-off node and resumes.
set -euo pipefail; cd "$(dirname "$0")/../.."; . hack/demos/lib.sh
NS=${NS:-partition-demo}; WORKER=${WORKER:-kuberoot-0e556e}; STAMP=$(date +%s)
k create namespace $NS --dry-run=client -o yaml | k apply -f - >/dev/null
subs=$(k -n physis-system get pods -l app=physis-agent -o wide --no-headers | awk -v w=$WORKER '$7==w {print $1}' | sed -E 's/^agent-(.*)-[a-z0-9]+-[a-z0-9]+$/\1/' | paste -sd'|' -)
stream() { for i in $(seq 1 60); do cat <<YAML | k apply -f - >/dev/null
apiVersion: physis.kuberoot.dev/v1alpha1
kind: Problem
metadata: {name: p$i, namespace: $NS}
spec: {class: maxcut, instance: {gset: G11}, quality: 0.97, confidence: 0.9, deadlineSeconds: 600, policy: min-energy}
YAML
sleep 2; done; }
count() { k -n $NS get problems -o jsonpath='{range .items[*]}{.status.claimedBy}{"\n"}{end}' | awk -v re="^($subs)$" 'NF{ if ($0 ~ re) w++; else o++ } END {printf "claimed by the cut-off worker: %d   by the other nodes: %d\n", w, o}'; }
say "Worker $WORKER carries substrates: $(echo $subs | tr '|' ' ')"
say "A stream of 60 problems starts, one every two seconds"
stream & S=$!
sleep 30; show count
say "The worker's only network interface goes down for 60 seconds"
cat <<YAML | k apply -f - >/dev/null
apiVersion: v1
kind: Pod
metadata: {name: cut-demo-$STAMP, namespace: kube-system}
spec:
  nodeName: $WORKER
  hostNetwork: true
  restartPolicy: Never
  containers:
  - name: cut
    image: busybox:1.37
    securityContext: {privileged: true}
    command: [sh, -c, "i=\$(ip route | awk '/^default/ {print \$5}'); r=\$(ip route | grep '^default'); ip link set \$i down; sleep 60; ip link set \$i up; sleep 1; ip route replace \$r"]
YAML
sleep 15; show "k get node $WORKER --no-headers 2>&1 | awk '{print \$1, \$2}'"
sleep 15; show count
sleep 20; show count
say "The link is back; the worker claims work again"
sleep 30; show count
wait $S; sleep 20
show "k -n $NS get problems --no-headers | awk '{print \$2}' | sort | uniq -c"
