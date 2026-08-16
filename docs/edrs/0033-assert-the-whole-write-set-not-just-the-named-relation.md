---
id: 33
title: "Assert the transaction's whole write set, not just the named relation"
summary: "A cascading delete returns one row and destroys millions in a table no delegation names. A fourth fence check reads the transaction's per-relation write counts before commit and aborts if anything outside the declared scope was touched."
status: accepted
date: 2026-08-15
authors:
  - "Theo Zourzouvillys <theo@sixfathoms.dev>"
tags: [execution, policy, security, foundational]
supersedes: null
superseded_by: null
aliases: []
---

## TL;DR

[EDR-0007](./0007-delegation-by-containment-proof.md)'s three checks all reason about the **named
relation**: the pre-check counts it, and the post-assert and `max_rows` are computed from
`RETURNING`, which reports only rows of the statement's target. So they are all blind to writes the
engine performs on the statement's behalf:

```sql
DELETE FROM public.accounts WHERE id = 42;     -- RETURNING: 1 row.  max_rows = 1 satisfied.
-- account_events.account_id REFERENCES accounts(id) ON DELETE CASCADE
--   → millions of rows deleted in a table no delegation names, inside the committed transaction.
```

[EDR-0006](./0006-every-statement-names-a-role.md)'s role bound does not contain it, because
referential actions are performed on the constraint's behalf and are not gated by the invoking role's
privileges on the referencing table. And
[EDR-0010](./0010-rehearse-before-you-sign.md) measures the same top-level count, so the approver was
shown "1 row" and the rehearsal-versus-execution divergence signal cannot fire — both sides measure
the same wrong number.

**The fix is a fourth check, and it generalises.** PostgreSQL exposes per-relation write counts for
the *current transaction* (`pg_stat_xact_all_tables`), readable before `COMMIT`. So:

> Before committing, assert that **every relation with a non-zero write delta is inside the
> delegation's declared object scope.** Anything else aborts the transaction.

That is strictly better than enumerating the machinery that could cause a surprise, because it
measures the **effect** rather than predicting it — it catches cascades, triggers, rewrite rules, and
whatever the next surprise turns out to be. The rehearsal reports the same per-relation numbers, so
the approver sees the cascade *before* signing.

## Context

Found by the expert panel, and it is the finding that most changes what the fence means.

Three mechanisms write outside the named relation, none mentioned anywhere in the corpus:

- **Referential actions.** `ON DELETE CASCADE`, `ON UPDATE CASCADE`, `ON DELETE SET NULL`. Unbounded
  by the delegation, unbounded by `max_rows`, invisible to `RETURNING`, and not stopped by the role.
- **Triggers.** A row-level trigger can do anything the trigger function's privileges allow, and
  [EDR-0010](./0010-rehearse-before-you-sign.md) mentions triggers only as a rehearsal side effect,
  never as an authorisation concern.
- **Rewrite rules.** A `DO INSTEAD` rule makes a statement against relation X write to relation Y —
  so the *object scope proof itself* is false, not merely incomplete.

No principal gains authority they did not already hold: everything here is reachable by the role. But
magnitude and object scope were **stated over the named relation while being presented to approvers
as statements about the transaction's effect**, and that gap is what an approval is supposed to close.
"This affects 1 row" is the sentence a human relies on.

The panel proposed refusing statements whose target relation carries such machinery. That is sound
and it is a denylist — it needs a complete enumeration of every way an engine can write on your
behalf, maintained forever, per engine. Measuring the write set needs none of that.

## Decision

### The fourth check

The execution transaction ([EDR-0007](./0007-delegation-by-containment-proof.md) §2) gains a final
assertion:

```sql
BEGIN ISOLATION LEVEL REPEATABLE READ;
-- (a) fence pre-check
-- (b) the operator's statement, with RETURNING
-- (c) fence post-assert on affected rows
-- (d) max_rows assert
-- (e) WRITE-SET assert:  every relation with a non-zero write delta in this
--     transaction is inside the delegation's declared object scope
COMMIT;
```

- The write set is read from the engine's transaction-scoped per-relation statistics. On PostgreSQL
  that is `pg_stat_xact_all_tables`, whose counters cover the current transaction and are readable
  before commit.
- **Statistics collection is a prerequisite, not an optimisation.** If the engine cannot report a
  transaction's write set, the check cannot run and the execution is **refused** — it does not
  proceed unchecked ([ZFN-2](https://zrz.io/zfn/2-engineering-priority-ordering/)).
- Relations written outside scope are reported by name and count, exactly as the fence reports its
  own violations. **Nothing is partially applied**, because the abort precedes the commit.

`max_rows` continues to bound rows of the **named relation** only — that is now stated in
[EDR-0007](./0007-delegation-by-containment-proof.md) rather than assumed — and the write-set
assertion is what bounds everything else. A delegation that legitimately expects a cascade declares
the child relation in its object scope, which makes the expectation explicit and reviewable instead
of silent.

### The rehearsal reports it too

[EDR-0010](./0010-rehearse-before-you-sign.md)'s report gains **`write_set`**: per-relation rows
inserted, updated and deleted for the whole rehearsal transaction. This is the visibility half, and
it is the more valuable half day to day:

> `accounts` −1 · **`account_events` −4,182,905** ⚠ outside declared scope

An approver seeing that does not need to know what a referential action is. The divergence signal in
[EDR-0010](./0010-rehearse-before-you-sign.md) now compares write sets rather than top-level counts,
so it can actually fire.

### Machinery is fingerprinted, not enumerated

The write-set assertion is the enforcement. A **fingerprint** of the target relation's attached
machinery — rewrite rules, row-level triggers, and referential actions on constraints referencing it
— is introspected at analysis time and recorded in the analysis digest
([EDR-0004](./0004-marques-are-signed-leases.md) binds that digest into the marque). At execution the
Pilot re-introspects, and a changed fingerprint invalidates the marque.

That closes the window in which machinery is attached between approval and execution — a cascade
added after the approver looked. It is a cheap check and it does not require the fingerprint to be
*complete*, because the write-set assertion is what actually contains the statement.

**Rewrite rules are the one case that is still refused outright.** A `DO INSTEAD` rule falsifies the
object-scope proof itself rather than merely widening the effect, so a target relation carrying a
non-default rewrite rule leaves the checkable subset
([EDR-0007](./0007-delegation-by-containment-proof.md)).

### Engines declare it

[EDR-0026](./0026-a-second-engine-is-a-capability-matrix.md)'s capability table gains
**`fence.write_set`**. An engine that cannot report a transaction's write set declares it, and on that
engine a delegated write is refused rather than being weakly checked — the same rule as every other
capability there.

## Consequences

**Easier.**

- The fence finally means what approvers already believed it meant: *this transaction touches these
  relations and nothing else.*
- The mechanism is general. It catches surprises nobody has thought of yet, which a denylist of
  referential actions and trigger names cannot.
- The cascade becomes a number in front of the approver rather than a discovery afterwards.

**Harder.**

- **A legitimate cascade now needs declaring.** Delegations that quietly relied on one will start
  aborting, and the fix — naming the child relation in the object scope — is correct but is work,
  and it will read as the tool being obstructive.
- **It depends on engine statistics being enabled and accurate.** Where counters are approximate or
  disabled, the check refuses rather than degrades, and an operator with `track_counts` off gets a
  hard failure whose cause is not obvious from the error alone.
- The write set is per relation, not per row, so it bounds *what* was touched and only counts *how
  much*. A trigger that modifies one in-scope row in a way nobody intended is not caught here.
- Two more round trips inside the write transaction, one of them a scan of a statistics view.

**New obligations.**

- The conformance suite in [EDR-0026](./0026-a-second-engine-is-a-capability-matrix.md) gains a
  cascade-escape case: a delegated `DELETE` on a parent with a cascading child must abort, and a test
  asserts the child is untouched afterwards.
- The playbook gains "a write-set abort is a finding about the delegation, not about the operator" —
  the first response is to look at what the relation is attached to, not to widen the scope.

## References

- [ZFN-2](https://zrz.io/zfn/2-engineering-priority-ordering/) — refuse when the check cannot run.
- [EDR-0007](./0007-delegation-by-containment-proof.md) — the three checks this completes.
- [EDR-0010](./0010-rehearse-before-you-sign.md) — where the write set is shown before signing.
- [EDR-0026](./0026-a-second-engine-is-a-capability-matrix.md) — the capability an engine declares or
  declines.

## Changelog

- **2026-08-15**: Accepted, following an expert-panel finding that all three fence checks are blind to
  writes outside the named relation.
