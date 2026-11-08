#!/usr/bin/env bash
# Deploys physis into the two-node kuberoot cluster booted by
# kuberoot-local-multi.sh. containerd pulls plain HTTP only from localhost, so
# every node runs a registry on its loopback and the image is pushed to each.
# Agents are spread over both machines (agentSpread=DoNotSchedule).
set -euo pipefail
cd "$(dirname "$0")/.."
D=out/kuberoot-2n
K="kubectl --kubeconfig $D/cluster.kubeconfig"
$K get nodes -o wide
mkdir -p $D/layer/usr/local/bin
for c in physis-scheduler physis-agent; do
  GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o $D/layer/usr/local/bin/$c ./cmd/$c
done
tar -C $D/layer -cf $D/layer.tar usr
port=5101
for n in $($K get nodes -o jsonpath='{.items[*].metadata.name}'); do
  cat <<YAML | $K apply -f -
apiVersion: v1
kind: Pod
metadata: {name: registry-$n, namespace: kube-system, labels: {app: node-registry}}
spec:
  nodeName: $n
  hostNetwork: true
  containers:
  - name: registry
    image: registry:2
    env: [{name: REGISTRY_HTTP_ADDR, value: "127.0.0.1:5001"}]
YAML
  $K -n kube-system wait --for=condition=Ready pod/registry-$n --timeout=300s
  $K -n kube-system port-forward pod/registry-$n $port:5001 > $D/pf-$n.log 2>&1 &
  PF=$!; sleep 3
  go run github.com/google/go-containerregistry/cmd/crane@v0.20.2 append --platform linux/arm64 \
    -b gcr.io/distroless/static-debian12:nonroot -f $D/layer.tar -t localhost:$port/physis:dev --insecure
  kill $PF; port=$((port+1))
done
$K apply -f deploy/kubepkg/physis/charts/physis/crds/physis-crds.yaml
helm template physis deploy/kubepkg/physis/charts/physis --namespace physis-system \
  --set image=localhost:5001/physis:dev --set agentSpread=DoNotSchedule \
  | { $K create namespace physis-system --dry-run=client -o yaml; echo ---; cat; } | $K apply -f -
sleep 2; $K -n physis-system rollout status deploy/physis-scheduler --timeout=600s
$K -n physis-system get pods -o wide
