#!/usr/bin/env bash
# A short scripted run on the local stand for the article's screencast:
# the substrates, one outcome request at three quality targets, the plan the
# scheduler chooses for each, a real run and its signed receipt.
# Record: asciinema rec -c hack/demo.sh out/demo.cast; agg out/demo.cast docs/img/demo.gif
set -euo pipefail
cd "$(dirname "$0")/.."
KC=out/stand/kubeconfig
k() { kubectl --kubeconfig $KC "$@"; }
say() { printf '\n\033[1;36m# %s\033[0m\n' "$*"; sleep 1.5; }
show() { printf '\033[1;33m$ %s\033[0m\n' "$*"; sleep 0.6; eval "$@"; sleep 2.5; }
k create namespace demo --dry-run=client -o yaml | k apply -f - >/dev/null
say "A Kubernetes API with no nodes: twenty substrates of different physics, described by their own agents"
show "k get nodes 2>&1 | head -2; k get substrates -o custom-columns=NAME:.metadata.name,PHYSICS:.spec.physics,ZONE:.spec.zone,EMULATED:.spec.emulated | head -21"
say "One request, three quality targets: classify 1797 digits at the least energy (dry runs, nothing runs)"
for q in 0.95 0.99 1.0; do
  n=demo-q$(echo $q | tr -d .)
  cat <<YAML | k apply -f - >/dev/null
apiVersion: physis.kuberoot.dev/v1alpha1
kind: Problem
metadata: {name: $n, namespace: demo}
spec: {class: classify, instance: {digits: {size: 1797, offset: 0}}, quality: $q, confidence: 0.9, deadlineSeconds: 120, policy: min-energy, dryRun: true}
YAML
done
sleep 6
show "k -n demo get problems -o custom-columns=PROBLEM:.metadata.name,TARGET:.spec.quality,PHYSICS:.status.plan.physics,ALGORITHM:.status.plan.algorithm,EFFORT:.status.plan.effort,JOULES:.status.plan.joules"
say "Now for real: MaxCut on G11, least carbon, only in the EU"
cat <<'YAML' | k apply -f - >/dev/null
apiVersion: physis.kuberoot.dev/v1alpha1
kind: Problem
metadata: {name: demo-run, namespace: demo}
spec: {class: maxcut, instance: {gset: G11}, quality: 0.97, confidence: 0.9, deadlineSeconds: 300, policy: min-carbon, jurisdictions: [EU]}
YAML
sleep 8
show "k -n demo get problem demo-run -o custom-columns=PHASE:.status.phase,SUBSTRATE:.status.claimedBy,ZONE:.status.plan.zone,QUALITY:.status.result.quality,GCO2:.status.result.gco2"
say "Every result carries a receipt signed by the substrate, chained to its previous ones"
show "k -n demo get receipts -o custom-columns=RECEIPT:.metadata.name,SUBSTRATE:.spec.substrate,SEQ:.spec.seq,DEVICE_J:.spec.deviceJoules,KEY:.spec.key | cut -c1-110"
say "Receipts verify offline with the substrate's published key; a tampered copy does not"
show "out/stand/bin/physis-conformance -kubeconfig $KC -run TC-10 2>/dev/null | grep -E 'TC-10|passed'"
