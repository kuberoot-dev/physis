#!/usr/bin/env bash
# Screencast: computing and signing in a WebAssembly (WASI) sandbox, with the
# agent around it doing only the networking (local stand + hack/wasi-device.sh).
set -euo pipefail; cd "$(dirname "$0")/../.."; . hack/demos/lib.sh
D=out/stand; k() { kubectl --kubeconfig out/stand/kubeconfig "$@"; }
say "The device core is a WebAssembly module under wasmtime: no network, no sockets, no Linux, only its own state directory"
show "ps -o command= -p \$(pgrep -f '^wasmtime run' | head -1) | sed 's#'\$PWD'/##g' | cut -c1-140"
say "Its key was made inside the sandbox; the agent only relays it"
show "head -1 out/stand/logs/agent-wasi.log | cut -c21-200"
show "k get substrate wasi-cpu-ch -o custom-columns=NAME:.metadata.name,JURISDICTION:.spec.jurisdiction,KEY:.status.publicKey"
k create namespace wasi-gif2 --dry-run=client -o yaml | k apply -f - >/dev/null
for i in 1 2 3; do cat <<YAML | k apply -f - >/dev/null
apiVersion: physis.kuberoot.dev/v1alpha1
kind: Problem
metadata: {name: sandboxed-$i, namespace: wasi-gif2}
spec: {class: maxcut, instance: {gset: G1}, quality: 0.97, confidence: 0.9, deadlineSeconds: 300, policy: min-energy, jurisdictions: [CH], physics: [cpu]}
YAML
done
say "Three problems allowed only under CH go to it; the sandbox computes and signs"
sleep 25
show "k -n wasi-gif2 get problems -o custom-columns=PROBLEM:.metadata.name,PHASE:.status.phase,SUBSTRATE:.status.claimedBy,QUALITY:.status.result.quality"
say "The receipts verify offline with the key from the sandbox"
show "k -n wasi-gif2 get receipts -o custom-columns=RECEIPT:.metadata.name,SEQ:.spec.seq,KEY:.spec.key | cut -c1-100"
show "out/stand/bin/physis-conformance -kubeconfig out/stand/kubeconfig -run TC-10 2>/dev/null | grep -E 'TC-10|passed'"
