#!/usr/bin/env bash
# Runs a substrate whose computing and signing happen in a WebAssembly (WASI)
# sandbox: physis-device built for wasip1 under wasmtime, holding the key in
# its own state directory, with physis-agent around it doing the networking.
# Needs the local stand (hack/run-local.sh up) and wasmtime.
set -euo pipefail
cd "$(dirname "$0")/.."
OUT=out/stand; KC=$OUT/kubeconfig
GOOS=wasip1 GOARCH=wasm go build -o out/physis-device.wasm ./cmd/physis-device
go build -o $OUT/bin/physis-agent ./cmd/physis-agent
mkdir -p $OUT/state/wasi-device $OUT/state/wasi-cpu-ch
nohup $OUT/bin/physis-agent -kubeconfig $KC -name wasi-cpu-ch -physics cpu -zone DE -operator sandbox-ch -jurisdiction CH \
  -slots 1 -data data -state $OUT/state/wasi-cpu-ch -grid-cache $OUT/grid-cache \
  -device "wasmtime run --dir $PWD/$OUT/state/wasi-device::/state --dir $PWD/data::/data $PWD/out/physis-device.wasm -physics cpu" \
  > $OUT/logs/agent-wasi.log 2>&1 &
echo $! > $OUT/agent-wasi.pid
echo "agent with a WASI device core started; problems with jurisdictions: [CH] go to it"
