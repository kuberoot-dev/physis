#!/usr/bin/env bash
# Screencast of the container-free kuberoot node: the node runs only OS
# services, the cluster has no Node objects and no pods, and physis works.
# Needs the node API forwarded to :50031 and the cluster API to :16453
# (see hack/kuberoot-physis-cluster.sh and out/workshop-px/).
set -euo pipefail
cd "$(dirname "$0")/.."
D=out/workshop-px
node() { kubectl --kubeconfig $D/trust/admin.kubeconfig --server https://127.0.0.1:50031 "$@"; }
k() { kubectl --kubeconfig $D/cluster.kubeconfig "$@"; }
say() { printf '\n\033[1;36m# %s\033[0m\n' "$*"; sleep 1.5; }
show() { printf '\033[1;33m$ %s\033[0m\n' "$*"; sleep 0.6; eval "$@"; sleep 2.5; }
say "A kuberoot node of the physis distribution: what runs on it (the node's own API)"
show "node get nodeservices | cut -c1-70"
say "The cluster on this node has no Node objects and no pods"
show "k get nodes; k get pods -A"
say "Yet twenty substrates of different physics are alive, served by agents that are OS services"
show "k get substrates -o custom-columns=NAME:.metadata.name,PHYSICS:.spec.physics,ZONE:.spec.zone | head -21"
k create namespace demo --dry-run=client -o yaml | k apply -f - >/dev/null
cat <<'YAML' | k apply -f - >/dev/null
apiVersion: physis.kuberoot.dev/v1alpha1
kind: Problem
metadata: {name: no-containers, namespace: demo}
spec: {class: maxcut, instance: {gset: G11}, quality: 0.97, confidence: 0.9, deadlineSeconds: 300, policy: min-carbon}
YAML
say "A problem is solved and signed without a single container"
sleep 6
show "k -n demo get problem no-containers -o custom-columns=PHASE:.status.phase,SUBSTRATE:.status.claimedBy,ZONE:.status.plan.zone,QUALITY:.status.result.quality,RECEIPT:.status.result.receipt"
