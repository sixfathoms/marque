---
id: 7
title: "Prove object scope, fence row scope, and escalate anything unprovable"
summary: "A delegation is checked in three ways: object scope by static proof over a restricted statement grammar, row scope by a transactional fence that aborts loudly rather than narrowing silently, and magnitude by an affected-row assertion."
status: accepted
date: 2026-08-15
authors:
  - "Theo Zourzouvillys <theo@sixfathoms.dev>"
tags: [policy, execution, foundational]
supersedes: null
superseded_by: null
aliases: []
---

## TL;DR

An operator wants to say: *"Sam may update `settings` on `accounts`, for rows where `tier =
'sandbox'`, up to 100 rows at a time."* Marque enforces that sentence in three separate ways, because
its three clauses have three different truth conditions:

| Clause | Mechanism | Where |
|---|---|---|
| `UPDATE`, table `accounts`, column `settings` | **static proof** over a restricted statement grammar | Harbourmaster, at submission |
| rows where `tier = 'sandbox'` | **transactional fence** — a pre-check that aborts, plus an assertion on affected rows | Pilot, at execution |
| at most 100 rows | **affected-row assertion** inside the transaction | Pilot, at execution |

Anything the grammar cannot parse into a provable shape is not "probably fine" — **no delegation
applies to it**, and it goes to a full human approval. Refusing to decide is the design, not a gap
in it.

The fence never silently narrows a statement. If your statement would touch a row outside your
scope, the transaction aborts and tells you how many rows it was. Silently applying to the subset is
worse than refusing, because it produces a partially-applied change nobody reviewed.

## Context

"Delegate the ability to change this column on these rows" is the feature that makes an approval
system usable at more than one team's scale. It is also where such systems quietly become unsound.

The tempting implementation is to parse the SQL and check the `WHERE` clause *implies* the delegated
predicate. General predicate entailment over SQL is undecidable, and the decidable fragments are
small enough that any real statement falls outside them. Every implementation that tries ends up
approximating — and an approximation in this direction is a silent authorisation bug: the checker
believes `WHERE id = 42` is contained by `tier = 'sandbox'` because it cannot see that row 42 is a
production account.

The second tempting implementation is to rewrite the statement, conjoining the delegated predicate
into the `WHERE`. That is sound — but it silently changes what the operator asked for. They wrote a
statement they believe updates 40 rows, 12 of them out of scope; it updates 28; nobody is told. A
half-applied change is often worse than no change, and it is always worse than a refusal.

So the decision is to stop trying to prove the part that cannot be proved, and instead *check it at
the only place where the answer is knowable* — inside the transaction, against the actual data, with
an abort.

[ZFN-2](https://zrz.io/zfn/2-engineering-priority-ordering/) governs the residual: when the analyser
is unsure, correctness beats availability, and the answer is a human.

## Decision

### The delegation

```jsonc
{
  "id": "dlg_01JB…",
  "to": "sam@acme.example",
  "grants": ["submit", "self_approve"],   // or ["approve"] to delegate reviewing
  "target": "prod-primary",
  "role": "settings_writer",
  "operations": ["UPDATE"],
  "objects": [ { "table": "public.accounts", "columns": ["settings", "settings_updated_at"] } ],
  "fence": "tier = 'sandbox'",
  "max_rows": 100,
  "not_after": "2026-11-30T00:00:00Z",
  "granted_by": "theo@acme.example"
}
```

Two structural rules:

- **Attenuation only.** A delegation can never grant more than the delegator holds — narrower
  operations, a subset of objects, a fence at least as tight, a `max_rows` no larger, and a
  `not_after` no later. Chains attenuate at every hop, and depth is bounded by deployment
  configuration.
- **Delegations expire.** `not_after` is required. There is no perpetual delegation
  ([ZFN-37](https://zrz.io/zfn/37-every-lock-is-a-lease/)).

### 1. Object scope: static proof

Checked at submission against a deliberately small grammar. A statement is **in the checkable
subset** only if all of the following hold:

- It is a single top-level statement, one of `SELECT`, `INSERT`, `UPDATE`, `DELETE`.
- Its target relation is a base table named directly. Not a view, not a table-returning function, not
  resolved through a search path that could bind elsewhere.
- No data-modifying common table expressions.
- No DDL, no `DO`, no `CALL`, no `COPY … PROGRAM`, no `SET ROLE`, no transaction control.
- Every function it calls is on an allowlist of known-pure built-ins. **This is not pedantry**: a
  function can execute arbitrary SQL, so `SELECT tidy_up()` is a write statement wearing a `SELECT`.
  A statement calling anything unrecognised leaves the subset.
- Assigned columns (`SET`, `INSERT` column list) are a subset of the delegation's `columns`.
- Read-only subqueries are permitted; their tables must be readable by the role but need not be in
  the delegation's object list.

A statement outside the subset is not rejected — it is simply **unmatched by any delegation**, so it
follows the normal path to a human approver. The analyser says why in one sentence
("`tidy_up()` is not a recognised pure function, so scope could not be established"), because an
operator who does not know why they are queueing will assume the tool is broken.

### 2. Row scope: the fence

The fence never rewrites the operator's statement. The Pilot executes, in one transaction:

```sql
BEGIN;
SET LOCAL statement_timeout = …;

-- (a) pre-check: would this touch anything outside the fence?
SELECT count(*) FROM public.accounts
 WHERE (<the statement's own predicate>) AND NOT (tier = 'sandbox');
--   > 0  →  ROLLBACK, and report the count

-- (b) the operator's statement, unmodified, with RETURNING added
UPDATE public.accounts SET settings = … WHERE … RETURNING id, tier;

-- (c) post-assert: did any affected row end up outside the fence?
--     catches an update that moves a row out of scope
-- (d) affected rows <= max_rows

COMMIT;
```

- **(a)** is possible precisely because the grammar in step 1 guarantees the predicate is extractable.
- **(c)** exists because an `UPDATE` can satisfy the fence before and violate it after — setting
  `tier = 'production'` on a sandbox row is exactly the escape a naive check misses.
- Any of (a), (c) or (d) failing rolls the transaction back. **Nothing is partially applied**, and the
  operator is told which check failed and by how much.

### 3. Magnitude

`max_rows` is asserted inside the transaction, not estimated beforehand. A rehearsal
([EDR-0010](./0010-rehearse-before-you-sign.md)) gives the operator the number in advance, but the
rehearsal is advice; the assertion is the control.

### Multi-statement requests

Every statement in a request is checked independently, and **all** must be in scope for a delegation
to apply. They execute in one transaction, so the whole request commits or none of it does.

## Consequences

**Easier.**

- The sentence an operator wants to say is expressible, and the enforcement of each clause is
  auditable in isolation. "Why was this allowed?" has three separate, checkable answers.
- Delegation can be granted safely to people who are not trusted with the role itself, which is the
  entire reason to have it.
- The unprovable cases fail towards a human rather than towards permission.

**Harder.**

- **The checkable subset will feel arbitrary and restrictive**, especially the function allowlist.
  People will hit it with statements that are obviously fine to a human. That friction is the cost of
  not having a soundness bug, and the mitigation is a good error message, not a wider grammar.
- **Every supported engine needs its own parser and its own notion of purity.** PostgreSQL first;
  MySQL is a second implementation, not a configuration flag.
- **The pre-check costs a scan.** On a large table with an unindexed fence predicate this is slow,
  and it runs inside the transaction holding the write. Fences should be over indexed columns, and
  the rehearsal warns when one is not.
- The fence is evaluated at execution time, so a row that moves into scope between approval and
  execution becomes eligible. Short marque lifetimes bound this;
  [EDR-0010](./0010-rehearse-before-you-sign.md) reports when the rehearsed row set differs from the
  executed one.

**New obligations.**

- The checkable-subset rules are versioned, and a delegation records which version it was evaluated
  under. Widening the grammar must not retroactively change what an old delegation permits.
- The function allowlist is reviewed when the target's schema changes; a new extension can add
  volatile functions with innocent names.

## References

- [ZFN-2](https://zrz.io/zfn/2-engineering-priority-ordering/) — when unsure, refuse.
- [ZFN-37](https://zrz.io/zfn/37-every-lock-is-a-lease/) — delegations expire.
- [ZFN-38](https://zrz.io/zfn/38-agents-are-principals/) — delegation is explicit, scoped,
  time-bounded, revocable, and recorded on both sides.
- [EDR-0006](./0006-every-statement-names-a-role.md) — the role is the outer bound this narrows.
- [EDR-0010](./0010-rehearse-before-you-sign.md) — how the numbers reach the approver in advance.

## Changelog

- **2026-08-15**: Accepted.
