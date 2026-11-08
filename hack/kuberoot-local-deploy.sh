#!/usr/bin/env bash
# Deploys physis into the local kuberoot node booted by kuberoot-local-boot.sh:
# fetches the cluster kubeconfig through the node API, runs a registry on the
# node's loopback (containerd pulls plain HTTP from localhost), pushes the
# physis image into it and installs the Helm chart. physis then runs as pods
# of a kuberoot cluster.
set -euo pipefail
cd "$(dirname "$0")/.."
D=out/kuberoot
NODE="kubectl --kubeconfig $D/trust/admin.kubeconfig --server https://127.0.0.1:50000"
$NODE get kubeconfig admin -o jsonpath='{.kubeconfig}' > $D/cluster.kubeconfig.orig
# The cluster API is forwarded to localhost; keep the certificate's name.
server=$(awk '/server:/ {print $2; exit}' $D/cluster.kubeconfig.orig)
host=$(echo "$server" | sed -E 's|https://([^:/]+).*|\1|')
sed "s|$server|https://127.0.0.1:6443|" $D/cluster.kubeconfig.orig > $D/cluster.kubeconfig
kubectl --kubeconfig $D/cluster.kubeconfig config set-cluster "$(kubectl --kubeconfig $D/cluster.kubeconfig config view -o jsonpath='{.clusters[0].name}')" --tls-server-name="$host" >/dev/null
K="kubectl --kubeconfig $D/cluster.kubeconfig"
$K get nodes -o wide
cat <<YAML | $K apply -f -
apiVersion: v1
kind: Pod
metadata: {name: node-registry, namespace: kube-system, labels: {app: node-registry}}
spec:
  hostNetwork: true
  containers:
  - name: registry
    image: registry:2
    env: [{name: REGISTRY_HTTP_ADDR, value: "127.0.0.1:5001"}]
YAML
$K -n kube-system wait --for=condition=Ready pod/node-registry --timeout=300s
$K -n kube-system port-forward pod/node-registry 5001:5001 > $D/pf.log 2>&1 &
PF=$!; sleep 3
# The image: physis binaries for linux/arm64 as one layer over distroless,
# built without a container engine.
mkdir -p $D/layer/usr/local/bin
for c in physis-scheduler physis-agent; do
  GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o $D/layer/usr/local/bin/$c ./cmd/$c
done
tar -C $D/layer -cf $D/layer.tar usr
go run github.com/google/go-containerregistry/cmd/crane@v0.20.2 append --platform linux/arm64 \
  -b gcr.io/distroless/static-debian12:nonroot -f $D/layer.tar -t localhost:5001/physis:dev --insecure
kill $PF
$K apply -f deploy/kubepkg/physis/charts/physis/crds/physis-crds.yaml
helm template physis deploy/kubepkg/physis/charts/physis --namespace physis-system \
  --set image=localhost:5001/physis:dev | { $K create namespace physis-system --dry-run=client -o yaml; echo ---; cat; } | $K apply -f -
sleep 2; $K -n physis-system rollout status deploy/physis-scheduler --timeout=600s
$K get substrates
