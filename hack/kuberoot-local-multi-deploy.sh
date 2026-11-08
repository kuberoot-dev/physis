#!/usr/bin/env bash
# Deploys physis into a kuberoot cluster of several nodes: the two-node
# cluster booted by kuberoot-local-multi.sh (default), or any other with
# D=<dir holding cluster.kubeconfig> and ARCH=<amd64|arm64>. containerd pulls plain HTTP only from localhost, so
# every node runs a registry on its loopback and the image is pushed to each.
# Agents are spread over both machines (agentSpread=DoNotSchedule).
set -euo pipefail
cd "$(dirname "$0")/.."
D=${D:-out/kuberoot-2n}; ARCH=${ARCH:-arm64}
K="kubectl --kubeconfig $D/cluster.kubeconfig"
$K get nodes -o wide
mkdir -p $D/layer/usr/local/bin
for c in physis-scheduler physis-agent; do
  GOOS=linux GOARCH=$ARCH CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o $D/layer/usr/local/bin/$c ./cmd/$c
done
tar -C $D/layer -cf $D/layer.tar usr
# A tag per build: nodes keep images by tag, and a reused tag would keep
# running the previous build.
TAG=dev-$(date +%s)
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
  # A port-forward through a remote API server can drop mid-push: retry.
  for try in 1 2 3 4; do
    $K -n kube-system port-forward pod/registry-$n $port:5001 > $D/pf-$n.log 2>&1 &
    PF=$!; sleep 3
    ok=0; go run github.com/google/go-containerregistry/cmd/crane@v0.20.2 append --platform linux/$ARCH \
      -b gcr.io/distroless/static-debian12:nonroot -f $D/layer.tar -t localhost:$port/physis:$TAG --insecure && ok=1
    kill $PF 2>/dev/null || true; port=$((port+1))
    [ $ok = 1 ] && break
  done
  [ $ok = 1 ] || { echo "push to $n failed"; exit 1; }
done
$K apply -f deploy/kubepkg/physis/charts/physis/crds/physis-crds.yaml
helm template physis deploy/kubepkg/physis/charts/physis --namespace physis-system \
  --set image=localhost:5001/physis:$TAG --set agentSpread=DoNotSchedule \
  | { $K create namespace physis-system --dry-run=client -o yaml; echo ---; cat; } | $K apply -f -
sleep 2; $K -n physis-system rollout status deploy/physis-scheduler --timeout=600s
$K -n physis-system get pods -o wide
