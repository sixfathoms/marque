---
id: 10
title: "Rehearse the statement in a transaction that never commits"
summary: "Before approval, the Pilot runs the request inside a transaction it always rolls back, capturing affected rows, duration and plan. The approver sees measured numbers rather than a guess."
status: accepted
implementation: none
date: 2026-08-15
authors:
  - "Theo Zourzouvillys <theo@sixfathoms.dev>"
tags: [execution, product]
supersedes: null
superseded_by: null
aliases: []
---

## TL;DR

A request is **rehearsed** before it reaches an approver: the Pilot opens a transaction, runs the
statements, records what happened, and rolls back. The approver is shown *"this affected 3 rows,
took 41 ms, and used an index scan"* rather than being asked to imagine it.

The rehearsal is defence-in-depth, not a control:

- It runs **under the request's own role** — not a separate "rehearsal identity" — because a rehearsal
  under a read-mostly grant could not measure a write, which is the entire point. What makes it safe
  is the connection discipline below, not a narrower database user. Identity modes are
  [EDR-0021](./0021-connections-identity-and-read-routing.md)'s; who may ask for a rehearsal at all is
  [EDR-0034](./0034-the-pilot-api-has-one-authorisation-model.md)'s.
- Its results are **advice** ([EDR-0009](./0009-the-leadsman-is-advisory.md)). The binding limits are
  the assertions inside the real transaction ([EDR-0007](./0007-delegation-by-containment-proof.md)).
- **A rehearsal is not a promise.** Data moves between rehearsal and execution, and the real
  transaction re-checks everything.

Statements that cannot be rehearsed safely — DDL, anything the parser cannot bound, anything that
would hold a lock beyond the timeout — are marked `not rehearsed`, with the reason, and reach the
approver with strictly less information. That is a signal to read the SQL more carefully, and the
console says so.

## Context

An approver's hardest question is "how big is this?" — and it is a question they usually answer
wrongly, because the statement in front of them looks reasonable and the data does not. The classic
production incident is a `DELETE` whose `WHERE` clause matches far more than its author believed,
approved by someone who read the clause and agreed with the author's belief.

The number is knowable. The database will tell you exactly, if you ask it in a transaction you then
throw away. `EXPLAIN` alone will not: its row estimates are statistics, and on skewed data they are
routinely wrong by orders of magnitude — which is worse than no number, because a wrong number
presented confidently is what gets approved.

The cost is that rehearsing means *actually running* an unapproved statement. That is the thing to
design carefully, and it is why the rehearsal has its own connection discipline — a transaction that
cannot commit — rather than reusing the execution path with a flag. It does **not** get a narrower
database identity: under a read-mostly grant it could not measure a write at all.

## Decision

**Where it runs.** On the Pilot, because the Pilot is the only component with a credential
([EDR-0005](./0005-control-plane-holds-no-credentials.md)). The Harbourmaster requests a rehearsal
for a request digest; the Pilot returns a report.

**How it is contained.**

```sql
BEGIN READ WRITE;
SET LOCAL statement_timeout   = '<rehearsal timeout>';
SET LOCAL lock_timeout        = '<short>';
SET LOCAL idle_in_transaction_session_timeout = '<short>';
-- capture write-set baseline
-- statements, with RETURNING, counts captured
SET CONSTRAINTS ALL IMMEDIATE;   -- so deferred triggers are measured, not deferred past capture
-- capture write-set delta
ROLLBACK;
```

- **The rollback is structural.** The rehearsal code path contains no commit; the connection is
  returned to the pool only after a rollback is confirmed, and a connection whose state is uncertain
  is destroyed rather than reused. There is no flag whose wrong value commits a rehearsal.
- **`lock_timeout` is short on purpose** — but it bounds how long the rehearsal **waits** for a lock,
  not how long it **holds** one. A rehearsal that acquires a row lock and then runs a slow second
  statement blocks production writers for the difference. So the transaction also carries a **total
  budget**, enforced two ways: `statement_timeout` per statement and
  `idle_in_transaction_session_timeout` for the gaps, plus an **out-of-band watchdog that terminates
  the backend** if the whole rehearsal transaction exceeds its budget. A rehearsal that blocks
  production is a production incident caused by the tool that exists to prevent them, and one timeout
  does not bound it.
- **Rehearsal is refused on replicas that would be promoted**, and preferred against a reader
  endpoint where the engine and the workload allow it.

**What it reports.**

| Field | Meaning |
|---|---|
| `rows_affected` | per statement, measured |
| `duration_ms` | measured, inside the timeout |
| `plan` | `EXPLAIN` output for context, clearly labelled as an estimate |
| `fence_violations` | rows the delegation fence would have excluded, if a delegation applies |
| `sample` | a bounded sample of affected rows, subject to redaction (below) |
| `write_set` | **per-relation** rows inserted, updated and deleted for the whole rehearsal transaction, captured as a delta ([EDR-0033](./0033-assert-the-whole-write-set-not-just-the-named-relation.md)). This is where an approver sees `account_events −4,182,905` before signing |
| `warnings` | unbounded write, no `WHERE`, sequential scan on a large table, unindexed fence |
| `not_rehearsed` | with a reason, when it could not run |

**Samples are redacted by default.** Showing an approver the rows that will change is enormously
useful and is also a data-disclosure surface: it puts customer data in front of whoever can approve.
Columns are shown only if the target's configuration marks them displayable; everything else appears
as a type and a null/not-null indicator. The default for an unclassified column is redacted.

**Reconciliation compares write sets, not top-level counts.** The measured rehearsal figures are
carried into the marque's analysis digest, and at execution the Pilot compares the **per-relation
write set** to the rehearsed one and records the delta. Comparing only top-level affected rows cannot
fire on the case that matters — a cascade measures identically on both sides
([EDR-0033](./0033-assert-the-whole-write-set-not-just-the-named-relation.md)). A large divergence is a logbook finding, and — for `critical` roles — a configurable reason to
abort rather than commit.

**Not rehearsed is a first-class state**, rendered prominently. A request that could not be rehearsed
is the one that most needs a careful human, and the failure mode to avoid is an empty field that
reads like a zero.

## Consequences

**Easier.**

- The single most common catastrophic mistake — a statement that matches far more than intended — is
  visible before anyone signs anything.
- Approval gets faster, not slower: most requests are small, and a measured "3 rows" ends the
  conversation.
- The reconciliation number is a genuinely novel signal. "Rehearsed 3, affected 40 000" is a finding
  no other part of the system produces.

**Harder.**

- **An unapproved statement really does run.** Contained, rolled back, timed out and identity-scoped
  — but it executes. A rehearsal can still fire triggers, consume sequence values, write WAL, and
  cause bloat. Sequence gaps in particular are permanent and surprise people.
- **Long-running statements cannot be rehearsed**, so the requests that carry the most risk are
  precisely the ones with the least information attached. There is no fix for this; there is only
  labelling it clearly.
- Rehearsal doubles the work done for every request, and on a busy target that is real load.
- Redaction defaults will hide columns approvers want to see, and the classification work to unhide
  them is per-target and ongoing.

**New obligations.**

- The rollback discipline is tested directly: a test asserts that no code path in the rehearsal
  package can reach a commit, and it should fail if someone adds one.
- Rehearsal load is monitored against the target's capacity, and the timeout is tuned per target
  rather than globally.

## References

- [ZFN-2](https://zrz.io/zfn/2-engineering-priority-ordering/) — a rehearsal must never risk the
  thing it protects.
- [EDR-0007](./0007-delegation-by-containment-proof.md) — the assertions that actually bind.
- [EDR-0009](./0009-the-leadsman-is-advisory.md) — where these numbers appear, and why the model does
  not produce them.

## Changelog

- **2026-08-15**: Accepted.
- **2026-08-16**: Amended after the expert panel's should-fix pass: removed the undefined "rehearsal identity" — a rehearsal runs under the request's own role, since a read-mostly grant could not measure a write — and bounded how long it *holds* a lock, not only how long it waits for one.
- **2026-08-16**: Amended after the second panel's synthesis: added `write_set` to the report and made reconciliation a per-relation comparison — [EDR-0033](./0033-assert-the-whole-write-set-not-just-the-named-relation.md) had stated both obligations on this record and neither had landed.
