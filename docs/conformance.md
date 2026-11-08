# Tomorrow conformance, version 0.1

A conformance suite for infrastructure that aims at tomorrow's work. Kubernetes
conformance asks "does this cluster implement the Kubernetes API the way the
reference does?" This suite asks a different question: "does this system
behave the way the principles in `principles.md` require?" It is black-box: it
talks to the API server, creates problems in its own namespace and judges what
the system does. Any implementation of the `physis.kuberoot.dev/v1alpha1`
resources can run it; the reference implementation is `cmd/physis-conformance`.

## Levels

- **Core** — required. A system that fails a Core test does not conform.
- **Extended** — expected of production systems; may be skipped with a stated
  reason (for instance, a system with no flexible work cannot show deferral).

A test may also report **SKIP** when its scenario could not be set up (for
example, no substrate slow enough to be cut off between claim and report). A
skip is never a pass.

## Tests

| ID | Level | Principle | Requirement | How it is checked |
|---|---|---|---|---|
| TC-01 | Core | P3 | Every live substrate states its physics, zone, operator jurisdiction and a model whose every parameter has provenance (`measured`, `ref:<key>` or `assumption`). | Lists live substrates and inspects their specs. |
| TC-02 | Core | P7 | Work completes without Node objects, and the system's roles grant no access to nodes or pods. | Counts Node objects; inspects the system's ClusterRoles; runs a problem to completion. |
| TC-03 | Core | P2, P4 | An outcome request is planned with predicted time, energy and carbon intensity, and completes before its deadline. | Submits a MaxCut problem with a quality target and deadline; compares the receipt's finish time with the deadline. |
| TC-04 | Core | P4 | The plan exposes the alternatives it traded off, and none of them is dominated by another (on carbon, energy, finish time, and money where the price is known). | Dry-run plan; pairwise dominance check. |
| TC-05 | Core | P5 | An unattainable outcome is refused with reasons and never runs. | Asks for the proven optimum of G22 with 99.9% confidence in one second. |
| TC-06 | Core | P4 | A budget is a hard limit. | Plans a problem, then asks again with a thousandth of its cheapest energy as the budget; the second plan must be refused or stay within budget. |
| TC-07 | Core | P11 | Allowed zones and allowed operator jurisdictions are hard constraints. | For every zone and jurisdiction present, a dry-run restricted to it must plan inside it or be refused. |
| TC-08 | Core | P14 | A dry run returns a plan and runs nothing. | Waits after the plan and checks nothing claimed it. |
| TC-09 | Extended | P12 | Flexible work with a long deadline is placed at the lowest forecast carbon among its alternatives. | Deferrable min-carbon dry run; no alternative may have lower predicted emissions. |
| TC-10 | Core | P9 | Every result has a receipt signed by the substrate that produced it; each substrate's receipts form a hash chain that verifies offline with the substrate's published key; a tampered receipt does not verify. | Runs problems, fetches all receipts, verifies chains, tampers with a copy. |
| TC-11 | Core | P9 | Receipts cannot be modified once written. | Attempts an update; the API must refuse it. |
| TC-12 | Core | P10 | Work under a mandate whose signature does not match the principal's registered key is refused; under a valid mandate with an approval threshold, a plan above the threshold waits for a person's approval and runs after it. | Registers a person's key; submits under a forged and a valid mandate; approves. |
| TC-13 | Core | P7 | Work planned on a substrate that falls silent before claiming returns to the pool and completes elsewhere. | Finds where a problem would go, silences that substrate, submits it, waits for completion elsewhere. |
| TC-14 | Extended | P8 | A substrate cut off from the API server after claiming finishes the work and delivers the report when the connection returns. | Claims slow work, cuts the agent off, checks the report arrives after the cut. |
| TC-15 | Core | P2, P3 | The same problem class can be met on more than one physics. | Dry runs of a MaxCut and a classification problem; counts physics with feasible plans. |
| TC-16 | Core | P5 | Results report the achieved quality and plans report a lower confidence bound and the number of observations behind it. | Runs a problem and inspects plan and result. |

## Test hooks

TC-13 and TC-14 need to make a substrate unreachable. A real deployment can do
this with a network policy; the reference suite uses a portable hook instead:
the annotation `physis.kuberoot.dev/simulate-partition-until: <RFC 3339 time>`
on a Substrate asks its agent to stop all API traffic (heartbeats, claims and
reports) until that time while continuing any work in progress. Conforming
agents must honour the hook or the suite must be run with a network-level
partition.

## Running

```bash
go run ./cmd/physis-conformance -kubeconfig <path> -report report.json
```

The exit code is 0 when every Core test passes.

## What the suite does not check

It does not check that device models are accurate (that is a matter of
calibration and is reported, not judged), that a scheduler's choices are
optimal, or how a system performs at scale. It checks behaviour that the
principles make into promises.
