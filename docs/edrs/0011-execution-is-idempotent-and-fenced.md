---
id: 11
title: "Execution is idempotent, fenced, and budgeted"
summary: "Every execution carries a caller-supplied nonce recorded before the statement runs. A repeat returns the first outcome instead of applying the change twice, and the marque's budget is consumed by the nonce, not by success."
status: accepted
implementation: none
implementation_note: "The nonce appears in proto/marque/v1/common.proto only as the example of what an IDEMPOTENCY_KEYED method's key would be. Nothing claims a nonce, executes anything, or accounts for a budget."
date: 2026-08-15
authors:
  - "Theo Zourzouvillys <theo@sixfathoms.dev>"
tags: [execution, security]
supersedes: null
superseded_by: null
aliases: []
---

## TL;DR

`Execute` takes an **execution nonce** chosen by the caller. The Pilot records the nonce *before* it
runs anything and refuses to run the same nonce twice. A retry — from a dropped connection, an
impatient operator, a CLI that reconnects — returns the recorded outcome of the first attempt rather
than applying the statement again.

The marque's `budget.executions` is consumed by **claiming a nonce**, not by succeeding. An execution
that times out or whose result is never delivered has still spent its budget, because from the
Pilot's side an indeterminate attempt and a successful one are indistinguishable, and the safe
reading of "I do not know whether that applied" is *assume it did*.

`budget.max_rows` is asserted inside the transaction. Exceeding it rolls back.

## Context

Marque runs statements that are frequently not idempotent. `UPDATE accounts SET balance = balance -
10` applied twice is a different bug from applied once, and a retried gRPC call is the ordinary way
that happens. [ZFN-19](https://zrz.io/zfn/19-annotate-readonly-idempotent-endpoints/) is blunt about
it: a non-idempotent retry double-applies, so every mutation gets an idempotency key.

The subtlety specific to this system is **what a budget of one should mean**. The naive reading is
"one successful execution", which sounds right and is wrong: if the connection drops after the commit
but before the response, the operator sees a failure, retries, and a budget counting successes lets
the second attempt through. The approver said "you may do this once" and it happened twice.

So the budget counts *attempts that may have applied*, and the recovery path for a genuinely failed
attempt is to ask for another marque — a human decision, which is the correct place for it.

## Decision

**Nonce first, statement second.** The Pilot's execution path is:

1. Verify the marque ([EDR-0004](./0004-marques-are-signed-leases.md)) and **proof of possession of
   `cnf.jkt`** ([EDR-0032](./0032-a-marque-binds-its-executor-tenant-and-pilot.md)). `sub` is recorded,
   not checked as a credential — that dependency is what broke offline execution for the caller.
2. Insert `(marque_id, nonce)` into its local execution ledger. A unique-violation means this nonce
   is already claimed — return the recorded outcome, or `in_progress` if there is not one yet.
3. Check and decrement the budget in the same transaction as the claim.
4. Open the target transaction, apply the fence pre-check, run the statements, assert the fence and
   `max_rows`, commit or roll back ([EDR-0007](./0007-delegation-by-containment-proof.md)).
5. Record the outcome against the nonce.

Step 2 precedes step 4, so a crash between them leaves the nonce claimed and the statement unrun —
budget spent, nothing applied. That is the correct direction to fail: the alternative loses the
count.

**The ledger is local to the Pilot** and durable. It is the one piece of state the Pilot keeps, and
it must survive a restart or the fence is not a fence. It is small — bounded by the number of live
marques — and entries are reaped once the marque expires.

**The ledger carries an incarnation, so its loss is detectable.** A Pilot writes a durable incarnation
identifier on first initialisation and reports it with every result. **An emptied or replaced ledger —
a fresh volume, a node replacement, a restore — presents as a new incarnation**, which is a finding
rather than a silent reset of every budget it was holding. Monitoring ledger *size* only measures
reaping; continuity is what the fence actually depends on.

**A claim has an owner and a lease.** `(marque_id, nonce)` is claimed by a named Pilot incarnation
with a lease. If the process dies mid-execution, the claim does not sit in `in_progress` forever: on
lease expiry it transitions to **`indeterminate`**, which is the truthful terminal state and the one
a human resolves. Without an owner and a lease there is no transition out of "claimed, no outcome",
and the operator is left with a budget spent and a request that never resolves.

**A clean abort is re-runnable; an indeterminate one is not.** A server-reported error received
**before** `COMMIT` — a `40001` serialization failure, a fence assertion aborting, a statement
timeout inside the transaction — is *provably not applied*. It is recorded as
**`aborted_not_applied`** and may be re-run under the **same nonce without a second budget
decrement**, bounded by an attempt count so a `budget.executions: 1` marque cannot be retried
forever. The class is defined by the property — an error received before commit — not by a list of
SQLSTATEs. Without this state, [EDR-0007](./0007-delegation-by-containment-proof.md)'s claim that a
serialization failure is "retryable under the same nonce" had nothing in this record to land on.

**Indeterminate outcomes are recorded as such.** If the Pilot cannot establish whether a transaction
committed, the outcome is `indeterminate`, not `failed`. A retry with the same nonce returns
`indeterminate`; a retry with a new nonce is refused if the budget is exhausted. The operator's path
forward is a new marque, and the logbook shows an execution whose effect is unknown — which is the
truth, and is what a human needs in order to check.

**`max_rows` is an assertion, not a limit clause.** Marque does not append `LIMIT` to a write. A
statement that would affect more rows than approved is *wrong*, and truncating it to the approved
count would apply a change nobody designed.

**The outcome reaches the logbook by the same journal discipline as everything else.** The Pilot
records the outcome in its own ledger first — that is the fence — and then reports it to the
Harbourmaster, which appends the `execution.*` entry
([EDR-0012](./0012-the-logbook-is-append-only.md)). The report is **retried until acknowledged**,
keyed on `(marque, nonce)` so a repeat is idempotent. A Pilot holding unreported outcomes is a
monitored signal: the ledger is the truth, and the logbook lagging it is a reconciliation problem,
not a lost execution.

**Every execution is streamed as it happens.** The CLI shows the claim, the fence pre-check, the
statement, the assertions, and the commit as separate events. An operator watching a slow statement
needs to know it is running, and a silent client is one an operator interrupts.

**Reads are still fenced.** A `SELECT` under a marque consumes budget the same way. It is the cheapest
thing to make an exception for and the exception would mean a read marque never expires in practice.

## Consequences

**Easier.**

- Retries are safe by construction, so the CLI and the console can retry aggressively on transport
  errors without anyone thinking hard about it.
- "Did that apply?" is answerable from the ledger, including the honest answer.
- The budget means what an approver thinks it means.

**Harder.**

- **A failed attempt spends budget**, and operators will find that surprising and occasionally
  infuriating — a network blip costing a re-approval at 3am is a bad experience. Budgets on
  `routine` roles should default above one for exactly this reason.
- The Pilot now has durable state, which it would otherwise not need: something to back up, restore
  and reason about during a failover.
- Two Pilots serving the same target must not share a marque, or the ledger is not authoritative. A
  marque is pinned to a Pilot identity at issue time by the `pilot` claim
  ([EDR-0032](./0032-a-marque-binds-its-executor-tenant-and-pilot.md)), and a Pilot that has permanently lost its
  ledger cannot honour outstanding marques.

**New obligations.**

- Ledger durability is tested by killing a Pilot mid-execution and asserting the nonce is still
  claimed on restart.
- Reaping is bounded by marque lifetime, and the ledger's size is monitored — unbounded growth means
  reaping has stopped, and the first symptom would otherwise be a slow execution path.

## References

- [ZFN-19](https://zrz.io/zfn/19-annotate-readonly-idempotent-endpoints/) — make every mutation
  idempotent.
- [ZFN-37](https://zrz.io/zfn/37-every-lock-is-a-lease/) — fence the side effects so a stale holder
  cannot re-apply them.
- [EDR-0004](./0004-marques-are-signed-leases.md) — where the budget comes from.

## Changelog

- **2026-08-15**: Accepted.
- **2026-08-16**: Amended after the expert panel's should-fix pass: added a ledger incarnation so its loss is detectable, an owner and lease so a claim can leave `in_progress` after a Pilot dies, and the path by which an outcome reaches the logbook.
- **2026-08-16**: Amended after the second panel's should-fix pass: added `aborted_not_applied`, a terminal-but-clean outcome re-runnable under the same nonce without a second budget decrement, which [EDR-0007](./0007-delegation-by-containment-proof.md)'s "a 40001 is retryable" claim previously had nothing to land on.
- **2026-08-16**: Amended after the second panel's synthesis: step 1 verifies proof of possession of `cnf.jkt` rather than the caller against `sub`, which is what [EDR-0032](./0032-a-marque-binds-its-executor-tenant-and-pilot.md) replaced.
- **2026-08-16**: Amended in the second panel's should-fix pass: pointed the Pilot-pinning consequence at EDR-0032's `pilot` claim.
