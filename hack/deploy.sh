#!/usr/bin/env bash
# Deploys physis into a cluster: CRDs, RBAC, calibration, scheduler and one
# agent per line of hack/fleet.tsv.  usage: hack/deploy.sh <kubeconfig> <image>
set -euo pipefail
cd "$(dirname "$0")/.."
KC=$1 IMAGE=$2
k() { kubectl --kubeconfig "$KC" "$@"; }
k apply -f deploy/crds.yaml -f deploy/rbac.yaml
k -n physis-system create configmap physis-calibration --from-file=learn.json=results/learn.json --from-file=best-found.json=results/best-found.json --dry-run=client -o yaml | k apply -f -
k -n physis-system create configmap physis-scripts --from-file=deploy/fetch-data.sh --dry-run=client -o yaml | k apply -f -
k -n physis-system create configmap principals --dry-run=client -o yaml | k apply -f -
sed "s|IMAGE|$IMAGE|g" deploy/physis.yaml | k apply -f -
grep -v '^#' hack/fleet.tsv | while IFS=$'\t' read -r name phys zone op jur slots; do
  sed "s|NAME|$name|g; s|PHYSICS|$phys|g; s|ZONE|$zone|g; s|OPERATOR|$op|g; s|JURISDICTION|$jur|g; s|SLOTS|$slots|g; s|IMAGE|$IMAGE|g" deploy/agent.yaml.tmpl | k apply -f -
done
