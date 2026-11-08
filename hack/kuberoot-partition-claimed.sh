#!/usr/bin/env bash
# The second half of the real-partition test: work already claimed when the
# link goes down. The living-culture agent is moved to the worker; a batch it
# needs minutes for is submitted; once it is Running, the worker's cluster
# link (eth1) is taken down for CUT seconds. The agent must finish the work
# during the cut and deliver the report, with its signed receipt, after the
# link returns.
#   hack/kuberoot-partition-claimed.sh [cut-s]
set -euo pipefail
cd "$(dirname "$0")/.."
D=${D:-out/kuberoot-2n}; IFACE=${IFACE:-eth1}
K="kubectl --kubeconfig $D/cluster.kubeconfig"
CUT=${1:-90}; WORKER=${WORKER:-kuberoot-000012}; NS=${NS:-partition-claimed}
$K -n physis-system patch deploy agent-organoid-fr --type merge -p "{\"spec\":{\"template\":{\"spec\":{\"nodeName\":\"$WORKER\"}}}}" >/dev/null
$K -n physis-system rollout status deploy/agent-organoid-fr --timeout=300s
$K -n physis-system get pods -l substrate=organoid-fr -o wide --no-headers
sleep 20  # the new agent registers and heartbeats
$K create namespace $NS --dry-run=client -o yaml | $K apply -f -
cat <<YAML | $K apply -f -
apiVersion: physis.kuberoot.dev/v1alpha1
kind: Problem
metadata: {name: slow-batch, namespace: $NS}
spec: {class: classify, instance: {digits: {size: 100, offset: 0}}, quality: 0.9, confidence: 0.9,
       deadlineSeconds: 900, policy: min-energy, physics: [organoid]}
YAML
until [ "$($K -n $NS get problem slow-batch -o jsonpath='{.status.phase}')" = Running ]; do sleep 1; done
$K -n $NS get problem slow-batch -o jsonpath='claimed by {.status.claimedBy} at {.status.claimedAt}{"\n"}'
cat <<YAML | $K apply -f -
apiVersion: v1
kind: Pod
metadata: {name: cut-link-claimed, namespace: kube-system}
spec:
  nodeName: $WORKER
  hostNetwork: true
  restartPolicy: Never
  containers:
  - name: cut
    image: busybox:1.37
    securityContext: {privileged: true}
    command: [sh, -c, "date -u +%FT%TZ; i=$IFACE; [ \$i = auto ] \u0026\u0026 i=\$(ip route | awk '/^default/ {print \$5}'); r=\$(ip route | grep '^default'); ip link set \$i down; sleep $CUT; ip link set \$i up; sleep 1; [ -n \"\$r\" ] \u0026\u0026 ip route replace \$r; date -u +%FT%TZ"]
YAML
until [ "$($K -n $NS get problem slow-batch -o jsonpath='{.status.phase}' 2>/dev/null)" = Succeeded ]; do sleep 2; done
$K -n kube-system logs cut-link-claimed
$K -n $NS get problem slow-batch -o json --show-managed-fields > $D/partition-claimed.json
r=$($K -n $NS get problem slow-batch -o jsonpath='{.status.result.receipt}')
$K -n $NS get receipt "$r" -o json > $D/partition-claimed-receipt.json
python3 - "$D" <<'EOF'
import json,sys
d=sys.argv[1]; p=json.load(open(d+'/partition-claimed.json')); r=json.load(open(d+'/partition-claimed-receipt.json'))
reported=max(f['time'] for f in p['metadata']['managedFields'] if f.get('subresource')=='status')
print('claimedAt', p['status']['claimedAt']); print('receipt finished', r['spec'].get('finished')); print('status reported', reported)
print('phase', p['status']['phase'], 'quality', p['status']['result']['quality'], 'receipt', r['metadata']['name'])
EOF
