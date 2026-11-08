# Design principles

physis is a research prototype of an infrastructure layer aimed at the work of
tomorrow rather than yesterday. This document comes first: every package, API
and test in the repository is meant to follow from one of the principles
below, and the conformance suite (`docs/conformance.md`) checks them as
observable behaviour. Where the prototype falls short of a principle, the gap
is written down in `docs/limitations.md` rather than left implicit.

## P1. Keep the contract, change what is under it

The Kubernetes API -- resources, watch, RBAC, admission, the tooling around
them -- is the language infrastructure speaks today. physis adds resources to
that API and replaces what sits beneath it. A layer that asks everyone to
learn a new language loses on ecosystem, however good its design; the history
of Plan 9 and the Lisp machines says so.

## P2. Ask for an outcome, not for resources

A workload states what it needs -- a quality target, a deadline, a budget in
joules, money and grams of CO2e, the places its data may go -- and not how many
cores and gigabytes it wants. Choosing the algorithm, the physics, the place
and the time is the layer's job. Resources are an implementation detail of an
outcome.

## P3. Describe the physics, do not hide it

A substrate is described by what it physically is: its elementary operation,
rate, energy per operation or active power, setup cost, precision, noise,
calibration windows, capacity. "1 GPU" says nothing a scheduler can reason
about when the alternatives are an Ising machine, a probabilistic computer
and an analog crossbar.

## P4. Cost is a first-class field

Every decision carries predicted time, energy (computation, data movement,
facility overhead), carbon at the forecast intensity of the grid that will
supply it, and money at the price of that hour. The decision also exposes the
Pareto front of the alternatives, so whoever asked can see what was traded.

## P5. Be honest about uncertainty

Quality is a probability, reported with a lower confidence bound from
observed runs; physics that has never been observed to meet a target is not
offered for it. Every model parameter carries its provenance -- measured,
published (with a reference) or assumed (with a range) -- and conclusions are
tested against the assumed ranges. The scheduler sees forecasts; experiments
are scored on actuals.

## P6. Close the loop

Every run produces a receipt; receipts update the quality distributions and
the correction factors between predicted and measured time and energy. A
simulator that shares the scheduler's code serves as the digital twin on which
policies are evaluated before they meet real work.

## P7. No nodes

Substrates are not nodes that receive pods; they are agents that claim work
they can do. A plan names a substrate, but if that substrate stops renewing
its lease the work returns to the pool and is planned again. Losing a machine
is not an event that a central component must notice and repair; it is work
that someone else picks up.

## P8. Local autonomy, central only where it must be

An agent that has claimed work finishes it and keeps its receipt even when it
cannot reach the control plane, and reports when it can. Central components
are needed for what must be agreed on -- money, rights, audit -- not for every
heartbeat.

## P9. Make every result verifiable

A receipt names who asked (and under which mandate), what was computed (the
instance digest, the algorithm and its version), on what (the substrate and
whether it was emulated), the result digest and its quality, and the cost. It
is signed by the agent that ran it and chained to the previous receipt, so
the record can be checked offline and cannot be rewritten without detection.

## P10. People set limits, agents act within them

A mandate, signed by a person, delegates a budget, a set of zones and a scope.
The layer refuses work outside a valid mandate. People decide values and sign
limits; agents translate them into plans. How much is delegated is a setting,
from no automation to fully agent-directed, on the same core.

## P11. Data has a place

Problems carry the zone their data lives in and the zones it may go to. Moving
data costs energy, and the scheduler charges it; leaving the allowed zones is
not an option, whatever it would save.

## P12. Flexible work follows clean and cheap energy

Work that may wait is planned into the hours in which the grid that would run
it is cleaner or cheaper, as long as the deadline holds. Work that may not
wait is not delayed to make a number look good.

## P13. An open standard, checked by tests

The resources, the agent contract and the receipt format are specified, and a
conformance suite checks any implementation against them. The code is under
the Apache License 2.0.

## P14. Agents first, people through views

The API is designed for machine callers: strict schemas, plan-before-apply,
cost estimates before execution, budgets enforced by the server. People get
views over the same state -- explanations, approvals, receipts -- rather than
a separate path that agents cannot use.
