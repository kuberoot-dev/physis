# Limitations

What the prototype does not show, and where its numbers could mislead. This
list is part of the results, not an appendix to them.

## Physics

- **Fifteen of the seventeen physics are emulated.** The CPU and, on the laptop
  stand, the Apple M4 GPU (through MLX) are real hardware; the GPU's time is
  measured, its power is an assumption because Apple does not publish it.
  The Ising machine, SFQ and adiabatic annealers, p-bits, coherent Ising
  machine, QPU, analog crossbar, photonic core, neuromorphic chip and living
  culture run as algorithms on a CPU; their time and energy come from device
  models whose parameters are either published figures (with references) or
  assumptions (with ranges). Conclusions that depend on a single assumed value
  are not conclusions; the sensitivity analysis shows which ones survive the
  ranges.
- **Same algorithm, different physics, same quality.** The CPU, digital
  annealer, SFQ and adiabatic machines run the same Metropolis annealing, so
  their quality distributions are identical by construction. Real hardware
  differs in precision (an annealer with 16-bit couplings is not a float64 CPU)
  and in noise. The prototype does not model those differences for annealers.
- **QPU emulation is crude.** QAOA uses fixed linear-ramp angles (no classical
  outer loop), a state-vector simulator up to 20 qubits and a global
  depolarising approximation of gate errors. It shows the shape of the
  trade-off (cryogenic power dwarfs computation on small instances), not the
  performance of any particular quantum computer.
- **The living culture is an echo-state network.** Its noise level is an
  assumption. Real cultures are less stable, live for months, and raise
  questions this prototype does not touch.
- **CPU energy is a model, not a measurement.** CPU time is measured
  (`getrusage`); energy is that time multiplied by the per-core share of the
  EPYC 9454P's TDP. RAPL counters are not exposed inside the pods of the
  cluster we had, nor on the laptop's ARM CPU in a form the agent can read.
- **Calibration ran on a laptop CPU** (Apple M4). The CPU rate the simulator
  uses is the M4's; the CPU power is the EPYC's. The mismatch makes the CPU
  look somewhat faster per joule than an EPYC core would be.

## Grid data

- **Carbon intensity for DE, FR and PL is computed by us** from Energy-Charts
  generation data with IPCC AR5 life-cycle medians. It is production-based
  (imports and exports ignored) and treats lignite as hard coal, which
  understates German intensity. GB intensity is the system operator's own.
- **The forecast for DE, FR and PL is persistence** (the same quarter-hour a
  day earlier). Real operators publish better forecasts; GB's official
  forecast is used for GB. The oracle experiment bounds what a perfect
  forecast would add.
- **GB has no open day-ahead price**, so money totals exclude GB runs and
  min-cost never prefers GB for being "free".
- **Generation data is fetched, not redistributed**: Energy-Charts does not
  state an open licence for it. `cmd/physis-data` reproduces the files; their
  SHA-256 sums are recorded.

## Scheduling and simulation

- **Workloads are small.** A MaxCut on G22 or a batch of 1797 digits costs
  joules, not kilowatt-hours. Absolute totals are tiny; the comparisons are
  relative. The scheduler's choices scale with the ratios, not the totals.
- **The workload mix is synthetic**: arrivals, deadlines, zone restrictions and
  quality targets are drawn from stated distributions.
- **Slots of CPU pools are not queued in the live scheduler**; the simulator
  queues every substrate.
- **Negative prices** make min-cost consume energy deliberately. That is the
  correct response to the price signal and also a reason not to use money
  alone as the objective; the article discusses it.

## System

- **Four live stands, all small.** A control plane with no nodes (envtest
  etcd and kube-apiserver 1.37) with physis as local processes; a single
  kuberoot `edge` node (arm64) in QEMU on a laptop; a two-machine kuberoot
  cluster in QEMU on the same laptop; and a kuberoot cluster (amd64, built in
  the cluster) in KubeVirt VMs on the Cozystack workshop cluster, first of
  three nodes on two physical servers, then of four on three. These are VMs on shared servers, not
  dedicated racks, and kuberoot's own high availability was not exercised.
- **The scheduler polls** every two seconds rather than using informers.
- **Learning in the live scheduler is in memory** and is lost on restart.
- **The network redesign is partly implemented.** Priced links and path
  jurisdictions work (TC-17); identity addressing with mutual authentication
  and object transfer between substrates work in tests and on the stand but
  are off by default (docs/network.md). The API-object relay for substrates
  behind NAT, data movement as a plan step, content addressing and transport
  choice remain design; a substrate's jurisdiction is declared, not proven.
- **The partition test hook** simulates a cut-off agent by an annotation in
  the conformance suite; a network-level partition (the worker's cluster link
  taken down) was tested separately on the two-node kuberoot cluster on a laptop and on the workshop cluster (three and four nodes).

## Architectures added later

- **The five architectures that change how numbers are represented or where
  they are computed** (systolic tensor array, DRAM processing, dataflow,
  stochastic, hyperdimensional) run in the live stands and in conformance, but
  not in the published quarterly simulations, which predate them.
- **Their accuracy is measured; their energy is modelled.** Peak rates are
  used, so small batches look better than they would at real utilisation;
  the dataflow card power and the energy per bit-op of stochastic and
  hyperdimensional circuits are assumptions with ranges.

## Added with the hierarchy, DRA bridge, device core and agent layer

- **The planning levels share one API server and store**; a site keeps
  planning without the global level, but not without the store.
- **The local level raises effort from the agent's own results** only when it
  has at least eight of them; it is unit-tested, and on the stands the
  emulated devices did not drift enough to trigger it.
- **The DRA bridge was tested with one hand-written ResourceSlice**, not with
  a real DRA driver.
- **The WASI device core ran under wasmtime on the laptop**, not on a
  microcontroller; the line protocol is the same, the hardware is not.
- **The container-free kuberoot distribution** was built and booted as one
  node (a VM on server ws2) and passes conformance; agents restart twice at
  boot while the API server comes up, and the agents use the admin
  kubeconfig rather than identities of their own.
- **The agent layer** (reason codes, idempotency, rate limit, MCP) is covered by
  unit tests and a run on the stand, not by conformance tests.
