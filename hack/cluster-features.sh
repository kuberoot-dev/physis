#!/usr/bin/env bash
# On a kuberoot cluster with physis deployed from the chart (image tag taken
# from the running scheduler): (1) the DRA bridge turns a ResourceSlice into
# a Substrate and an adopting agent pod serves it; (2) two agents on
# different nodes exchange a dataset by identity, and an agent outside the
# allowed jurisdiction is refused. Evidence goes to results/.
#   D=out/workshop hack/cluster-features.sh dra|identity
set -euo pipefail
cd "$(dirname "$0")/.."
D=${D:-out/workshop}
K="kubectl --kubeconfig $D/cluster.kubeconfig"
IMAGE=$($K -n physis-system get deploy physis-scheduler -o jsonpath='{.spec.template.spec.containers[0].image}')
agent_pod() { # name zone jurisdiction node extra-args...
  local n=$1 z=$2 j=$3 node=$4; shift 4
  local args=""; for a in "$@"; do args="$args, \"$a\""; done
  cat <<YAML | $K apply -f - >/dev/null
apiVersion: v1
kind: Pod
metadata: {name: ${POD:-$n}, namespace: physis-system, labels: {app: physis-feature}}
spec:
  serviceAccountName: physis-agent
  nodeName: $node
  restartPolicy: Never
  initContainers:
  - name: data
    image: busybox:1.37
    command: [sh, -c, "sh /scripts/fetch-data.sh && cp /cal/best-found.json /data/; mkdir -p /state/objects; head -c 3145728 /dev/urandom > /state/objects/dataset.bin; sha256sum /state/objects/dataset.bin"]
    volumeMounts: [{name: data, mountPath: /data}, {name: state, mountPath: /state}, {name: scripts, mountPath: /scripts}, {name: cal, mountPath: /cal}]
  containers:
  - name: agent
    image: $IMAGE
    command: ["/usr/local/bin/physis-agent", "-name", "$n", "-zone", "$z", "-operator", "feature-$n", "-jurisdiction", "$j", "-slots", "1", "-data", "/data", "-state", "/state", "-namespace", "physis-system", "-key-secret", "agent-key-$n"$args]
    env:
    - {name: POD_NAMESPACE, valueFrom: {fieldRef: {fieldPath: metadata.namespace}}}
    - {name: POD_IP, valueFrom: {fieldRef: {fieldPath: status.podIP}}}
    volumeMounts: [{name: data, mountPath: /data}, {name: state, mountPath: /state}]
  volumes:
  - {name: data, emptyDir: {}}
  - {name: state, emptyDir: {}}
  - {name: cal, configMap: {name: physis-calibration}}
  - {name: scripts, configMap: {name: physis-scripts}}
YAML
}
nodes=($($K get nodes -o jsonpath='{.items[*].metadata.name}'))
case "${1:-}" in
dra)
  cat <<'YAML' | $K apply -f -
apiVersion: resource.k8s.io/v1
kind: ResourceSlice
metadata: {name: rack-cpu}
spec:
  driver: cpu.example.com
  pool: {name: rack-7, generation: 1, resourceSliceCount: 1}
  allNodes: true
  devices:
  - name: cpu0
    attributes:
      physis.kuberoot.dev/physics: {string: cpu}
      physis.kuberoot.dev/zone: {string: FR}
      physis.kuberoot.dev/operator: {string: rack-7}
      physis.kuberoot.dev/jurisdiction: {string: CH}
      physis.kuberoot.dev/activePowerW: {string: "9"}
YAML
  go run ./cmd/physis-dra-bridge -kubeconfig $D/cluster.kubeconfig -once
  agent_pod dra-rack-7-cpu0 FR CH "${nodes[1]}" -physics cpu -adopt
  $K -n physis-system wait --for=condition=Ready pod/dra-rack-7-cpu0 --timeout=300s >/dev/null; sleep 10
  $K create namespace dra-cluster --dry-run=client -o yaml | $K apply -f - >/dev/null
  cat <<'YAML' | $K apply -f - >/dev/null
apiVersion: physis.kuberoot.dev/v1alpha1
kind: Problem
metadata: {name: via-dra, namespace: dra-cluster}
spec: {class: maxcut, instance: {gset: G11}, quality: 0.97, confidence: 0.9, deadlineSeconds: 300, policy: min-energy, jurisdictions: [CH]}
YAML
  sleep 25
  $K get substrate dra-rack-7-cpu0 -o json > results/dra-cluster-substrate.json
  $K -n dra-cluster get problem via-dra -o json > results/dra-cluster-problem.json
  $K get substrate dra-rack-7-cpu0 -o jsonpath='{.metadata.annotations}{"\n"}{range .spec.model[*]}{.name}={.value} {.source}{"\n"}{end}'
  $K -n dra-cluster get problem via-dra -o jsonpath='{.status.phase} by={.status.claimedBy} q={.status.result.quality} receipt={.status.result.receipt}{"\n"}' ;;
identity)
  agent_pod id-b-de DE EU "${nodes[1]}" -physics cpu -listen :7443 -object-jurisdictions EU
  $K -n physis-system wait --for=condition=Ready pod/id-b-de --timeout=300s >/dev/null; sleep 10
  addr=$($K get substrate id-b-de -o jsonpath='{.status.publicKey}')
  ident=$(go run ./cmd/physis-identity-addr "$addr")
  echo "B key $addr endpoint $($K get substrate id-b-de -o jsonpath='{.status.endpoints}')"
  # The receivers first run as ordinary agents, so their identities are
  # substrates with a zone and a jurisdiction; then the same identity (the
  # key kept in its Secret) asks B for the dataset.
  for who in "id-a-fr FR EU ${nodes[2]}" "id-c-gb GB UK ${nodes[3]}"; do
    set -- $who
    agent_pod $1 $2 $3 $4 -physics cpu
  done
  for p in id-a-fr id-c-gb; do $K -n physis-system wait --for=condition=Ready pod/$p --timeout=300s >/dev/null; done; sleep 10
  for who in "id-a-fr FR EU ${nodes[2]}" "id-c-gb GB UK ${nodes[3]}"; do
    set -- $who
    POD=$1-fetch agent_pod $1 $2 $3 $4 -physics cpu -fetch "$ident/dataset.bin" -fetch-out /state/got.bin
  done
  sleep 60
  for p in id-b-de id-a-fr-fetch id-c-gb-fetch; do echo "== $p"; $K -n physis-system logs $p -c agent --tail=4 2>&1 | cut -c1-220; done
  $K -n physis-system logs id-b-de -c data | tail -1 ;;
*) echo "usage: $0 dra|identity"; exit 1 ;;
esac
