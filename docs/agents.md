# Agents as callers

Principle P14 says the API is designed for machine callers first and people
get views over the same state. This note lists what in physis serves that
caller today, how an agent is expected to use it, and what is still open.

An agent differs from a person at the API in three ways that matter here. It
calls often, thousands of times where a person would call once. It retries:
a timeout tells it nothing about whether the work was accepted. And it
branches on the answer instead of reading it, so an answer that only a
person can interpret is no answer.

## What is there

**Outcome requests.** A `Problem` states what is wanted (quality target and
confidence, deadline, budget in joules, euros and grams of CO2e, allowed
zones and jurisdictions, where the data lives) rather than what to run. The
agent does not need to know which physics, algorithm or region fits; the
scheduler decides and says why.

**Dry runs.** `spec.dryRun: true` returns the chosen plan, the Pareto
alternatives it traded off, the physics on which the outcome is reachable and
the rejected candidates, without running or spending anything (TC-04,
TC-08). An agent can ask "what would this cost and where would it go" before
committing, and compare several formulations of the same goal.

**Server-side budgets and mandates.** Limits are enforced by the scheduler,
not trusted to the caller. A problem's budget is a hard cap (TC-06). A
mandate signed by a person's Ed25519 key delegates a namespace, zones, a daily
budget and an autonomy level; work outside a valid mandate is refused, and
above an approval threshold it waits for a person (TC-12). The agent acts;
the person sets the limits (P10).

**Receipts.** Every result carries a receipt signed by the substrate that
produced it, chained by hash per signing key, verifiable offline with the
keys the substrate publishes, current and retired (TC-10, TC-11). An agent
that pays for, reports on or audits work can check what ran without
trusting the control plane.

**Reason codes.** Every refusal, and every candidate the planner dropped,
carries a stable code in `status.reasonCodes` next to the text in
`status.reasons` and `status.message`. The codes (package `pkg/reason`) are:

| Code | Meaning |
|---|---|
| `ZoneNotAllowed` | the zone is outside the zones the problem or its mandate allows |
| `JurisdictionNotAllowed` | the operator's jurisdiction or a data path's is outside the allowed ones |
| `BudgetExceeded` | predicted joules, euros or grams of CO2e exceed the budget |
| `QualityUnreachable` | the quality target cannot be reached with the requested confidence |
| `DeadlineTooShort` | the run would finish after the deadline |
| `NoLink` | the data has no allowed path to the substrate's zone |
| `GridUnavailable` | no grid forecast for the zone |
| `NoSubstrates` | no live substrate could take the work |
| `InvalidRequest` | the problem cannot be resolved (instance, class or shape) |
| `MandateInvalid` | the mandate is missing, wrongly signed or covers another namespace |
| `MandateDenied` | the mandate is valid but does not admit this plan |
| `Duplicate` | another problem in the namespace has the same `requestID` |
| `RateLimited` | too many new problems from this namespace; retry later |

On a planned problem the codes describe why alternatives were dropped; on a
rejected one, why nothing was left. The planner keeps a sample of twenty
texts but records the code of every rejection. Codes are added, never
renamed or reused.

**Idempotency.** `spec.requestID` is an idempotency key. Of the problems in a
namespace that carry the same `requestID`, the scheduler plans only the
earliest created; the others are rejected with code `Duplicate` and
`status.duplicateOf` naming the original. Dry runs do not hold a
`requestID` (they run nothing), and neither does a problem rejected as
`Duplicate` or `RateLimited`, so a retry after a rate limit is not blocked by
its own failed attempt. The field is immutable. A client can also make the
object name a function of the `requestID`, so that a retry collides at the
API server before the scheduler sees it; `physis-mcp` does this.

**Rate limits.** The scheduler admits at most `-rate-limit` new problems per
namespace per `-rate-window` (60 a minute by default, which leaves room for
the conformance suite). The excess is rejected with code `RateLimited` and
is not planned. Work returned to the pool after a substrate went silent is
not new work and is not counted. The API server's own priority and fairness
still protects the server from raw request floods; this limit protects the
planner, which is the expensive part.

**MCP.** `cmd/physis-mcp` is a Model Context Protocol server over stdio, so
an agent's host can discover and call physis as tools. It runs with the
agent's kubeconfig and adds no authority of its own. The tools, each with a
strict JSON schema (unknown arguments are refused, not dropped):

| Tool | What it does |
|---|---|
| `list_substrates` | physics, zone, jurisdiction, liveness, published keys, model parameters with provenance |
| `plan` | a dry run: plan, Pareto alternatives, feasible physics, reasons and codes; the dry-run object is deleted after reading |
| `submit` | creates a problem; `requestID` is required, a retry returns the existing problem |
| `status` | phase, message, reasons and codes, `duplicateOf`, plan, claim, result, receipt name |
| `verify_receipts` | verifies receipt chains locally against the substrates' current and retired keys |

```bash
go run ./cmd/physis-mcp -kubeconfig ~/.kube/agent -namespace agents
```

The caller's identity needs `create`, `get`, `list` and `delete` on
`problems` in its namespace, and `get` and `list` on `substrates` and
`receipts`.

## What is still open

- **Agent identity in receipts.** A receipt records the requester from the
  `physis.kuberoot.dev/requester` annotation, which the caller sets itself.
  Nothing binds it to the API identity that created the problem, or records
  which agent, in which session and on whose behalf, acted. An admission
  check that stamps the authenticated user, or an agent's own signature over
  the request, is the next step.
- **Mandates per agent.** A mandate covers a namespace, not an agent. Two
  agents sharing a namespace share a budget; splitting it requires one
  namespace each.
- **Rate limit state.** The window lives in the scheduler's memory: a restart
  forgets it, and two scheduler replicas would each admit the limit. A
  shared counter (a Lease or a status field per namespace) would fix both.
- **Quotas on objects.** Rejected and finished problems stay until someone
  deletes them. A namespace quota on `problems` (an ordinary
  `ResourceQuota` with `count/problems.physis.kuberoot.dev`) bounds that, but
  physis does not set one.
- **Watching instead of polling.** `physis-mcp` polls a dry run until it is
  planned. MCP notifications or a watch would remove the polling; the MCP
  server does not stream progress yet.
- **Payment.** Budgets are in joules, euros and grams, but nothing settles
  money between an agent and an operator. Machine payment protocols (x402,
  AP2) could pay against receipts; physis does not speak them.
- **Conformance.** The suite does not test idempotency, reason codes or rate
  limits yet; they are covered by unit tests only.
