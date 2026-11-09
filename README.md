# physis

[Русская версия](README.ru.md)

physis is a research prototype of an infrastructure layer for the work of
tomorrow. A workload asks for an **outcome** — a quality target, a deadline, a
budget in joules, money and grams of CO2e, the places and jurisdictions its data
may go — and physis decides **which algorithm, on which physics, where and
when** will deliver it at the least carbon, energy, money or time, using live
forecasts of the grids that would power it.

It runs on the Kubernetes API (the contract stays; what is under it changes)
and needs **no nodes**: substrates are agents that claim work, not machines
that receive pods. Every result carries a signed, hash-chained receipt that
anyone can verify offline. People delegate budgets with signed mandates; agents
act within them.

The prototype knows seventeen physics: a real CPU, a real GPU (the Apple M4 of
the laptop stand, through MLX, `tools/gpu-worker.py`), a systolic tensor array
(FP8, MXFP4, int8), DRAM processing, a dataflow processor, stochastic and
hyperdimensional computing, and emulated digital and SFQ
annealers, adiabatic logic, p-bits, a coherent optical Ising machine, a gate
quantum processor (QAOA), analog in-memory computing, a photonic core, a
neuromorphic chip and a living neuronal culture used as a reservoir. Emulated
substrates are charged at published figures or explicit assumptions, every one
with its provenance (`docs/references.md`), and are meant to be recalibrated
from receipts once real hardware is attached.

> Status: research prototype. See `docs/limitations.md` before quoting a number.

## Documents

- `docs/design.ru.md` — from the forces shaping computing to the design (Russian).
- `docs/principles.md` — the fourteen principles.
- `docs/conformance.md` — the tomorrow conformance suite, version 0.1.
- `docs/agents.md` — what serves AI agents as callers, and what is open.
- `docs/limitations.md` — what the results do not show.
- `docs/verification.md` — how every claim and link of the article was checked.
- `docs/references.md` — sources of every figure.
- `docs/article.ru.md` — the full article (Russian).

## Layout

| Path | What |
|---|---|
| `pkg/solve` | MaxCut and classification solvers for every physics, with exact operation counts |
| `pkg/device` | Device models of the seventeen physics, every parameter with provenance |
| `pkg/plan` | The scheduler: algorithm × physics × zone × start time, constraints, policies, Pareto front |
| `pkg/learn` | Quality distributions and model corrections learned from runs |
| `pkg/grid` | Grid carbon intensity and prices: recorded and live (Energy-Charts, UK Carbon Intensity) |
| `pkg/receipt`, `pkg/mandate` | Signed receipt chains; signed human mandates |
| `pkg/sim` | The digital twin used for the experiments |
| `cmd/physis-scheduler`, `cmd/physis-agent` | Control plane and substrate agent |
| `pkg/reason` | Stable machine-readable reason codes for refusals |
| `cmd/physis-mcp`, `pkg/mcp` | MCP server over stdio: the API as tools for AI agents |
| `cmd/physis-conformance` | The conformance suite |
| `cmd/physis-calibrate`, `cmd/physis-sim`, `cmd/physis-data`, `cmd/physis-load` | Experiments |
| `deploy/` | CRDs, RBAC, manifests, Helm chart and kubepkg recipe for kuberoot |

## Try it on a control plane with no nodes

```bash
go run ./cmd/physis-data -from 2026-07-01 -to 2026-10-01 -out data/grid/2026q3   # grid data
sh deploy/fetch-data.sh   # with /data replaced by ./data, or see hack/
go run ./cmd/physis-calibrate -seeds 12 -out out/calibration
hack/run-local.sh up      # etcd + kube-apiserver, scheduler, 14 agents
kubectl --kubeconfig out/stand/kubeconfig get substrates
go run ./cmd/physis-conformance -kubeconfig out/stand/kubeconfig
hack/run-local.sh down
```

## Run it on kuberoot

```bash
git clone https://github.com/kuberoot-dev/kuberoot .stand/kuberoot
hack/kuberoot-local-build.sh        # kernel, DRBD, binaries, rootfs, media (arm64, in containers)
hack/kuberoot-local-boot.sh fetch   # media and node credentials out of the build volume
hack/kuberoot-local-boot.sh up      # boot the node in QEMU (HVF on macOS)
hack/kuberoot-local-deploy.sh       # registry on the node, physis image, Helm chart
go run ./cmd/physis-conformance -kubeconfig out/kuberoot/cluster.kubeconfig
```

On any other cluster: `hack/deploy.sh <kubeconfig> <image>`, or install the
kubepkg package in `deploy/kubepkg/physis`.

## License

Apache License 2.0. Grid data and benchmark inputs are fetched from their
providers and stay under their terms.
