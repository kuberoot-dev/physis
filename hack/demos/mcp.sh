#!/usr/bin/env bash
# Screencast: an AI agent's view through the MCP server, against the kuberoot cluster.
set -euo pipefail; cd "$(dirname "$0")/../.."; . hack/demos/lib.sh
k create namespace mcp-demo2 --dry-run=client -o yaml | k apply -f - >/dev/null
go build -o out/physis-mcp ./cmd/physis-mcp
call() { # tools/call name json-args
  (printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"demo","version":"0"}}}' \
    "{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/call\",\"params\":{\"name\":\"$1\",\"arguments\":$2}}"; sleep "${3:-3}") \
  | out/physis-mcp -kubeconfig $D/cluster.kubeconfig -namespace mcp-demo2 2>/dev/null | grep '"id":2' \
  | python3 -c '
import json,sys
r=json.loads(sys.stdin.read())["result"]; s=r.get("structuredContent") or {"text": r["content"][0]["text"]}
keep=sys.argv[1].split(",")
out={k:s[k] for k in keep if k in s}
if "plan" in keep and isinstance(s.get("plan"),dict):
    out["plan"]={k:s["plan"][k] for k in ("substrate","physics","algorithm","effort","joules") if k in s["plan"]}
print(json.dumps(out,ensure_ascii=False,indent=1))' "${4:-phase}"
}
say "The agent asks for a plan, nothing runs: what would classify 1797 digits at 99% quality cost?"
show "call plan '{\"class\":\"classify\",\"instance\":{\"digits\":{\"size\":1797,\"offset\":0}},\"quality\":0.99,\"confidence\":0.9,\"deadlineSeconds\":120,\"policy\":\"min-energy\"}' 15 phase,plan"
say "It submits with a request ID; a retry after a timeout returns the same problem instead of a second run"
show "call submit '{\"requestID\":\"demo-43\",\"class\":\"maxcut\",\"instance\":{\"gset\":\"G11\"},\"quality\":0.97,\"confidence\":0.9,\"deadlineSeconds\":300,\"policy\":\"min-carbon\"}' 3 name,phase,existing"
show "call submit '{\"requestID\":\"demo-43\",\"class\":\"maxcut\",\"instance\":{\"gset\":\"G11\"},\"quality\":0.97,\"confidence\":0.9,\"deadlineSeconds\":300,\"policy\":\"min-carbon\"}' 3 name,phase,existing"
say "An impossible request is refused with a code it can branch on"
show "call plan '{\"class\":\"maxcut\",\"instance\":{\"gset\":\"G22\"},\"quality\":1.0,\"confidence\":0.999,\"deadlineSeconds\":1,\"policy\":\"fastest\"}' 12 phase,message,reasonCodes"
