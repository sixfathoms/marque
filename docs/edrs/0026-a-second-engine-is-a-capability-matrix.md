---
id: 26
title: "Publish what each engine can actually enforce, and disable what it cannot"
summary: "Adding an engine is not a driver swap: the fence, the rehearsal and the timeout are all engine-specific. Where an engine cannot support a control, that control is marked unavailable rather than silently weakened."
status: accepted
implementation: none
date: 2026-08-15
authors:
  - "Theo Zourzouvillys <theo@sixfathoms.dev>"
tags: [execution, policy, architecture]
supersedes: null
superseded_by: null
aliases: []
---

## TL;DR

PostgreSQL first, properly. Other engines follow, and each is a **substantial piece of work rather
than a configuration flag** — the parser, the purity allowlist, the fence mechanism, the rehearsal
containment, the timeout story and the identity integration are all engine-specific.

The governing rule: **an engine that cannot support a control does not get a quietly weaker version
of it.** The control is marked unavailable for that engine, features depending on it are refused on
those targets, and the deployment can see exactly what containment it has. A capability matrix is
published per engine and is visible in the console and the CLI.

MySQL is the worked example, and it is instructive because three of Marque's mechanisms do not port
cleanly:

| Mechanism | PostgreSQL | MySQL |
|---|---|---|
| Fence post-assert (catching an `UPDATE` that moves a row *out* of scope) | `RETURNING` | **no `RETURNING`** — needs a locking pre-select of primary keys, so it requires a primary key and costs a lock |
| Statement timeout on a write | `statement_timeout` | **`max_execution_time` applies to read-only `SELECT` only** — writes need a lock timeout plus an external watchdog that kills the query |
| Rehearsal rollback of DDL | transactional DDL | **DDL implicitly commits** — DDL cannot be rehearsed at all, and must be refused rather than attempted |

None of these is fatal. All of them change what a MySQL target can be trusted to enforce, and an
operator must be told which.

## Context

"Support MySQL" sounds like adding a driver. It is not, because almost every control in this system
is implemented *in the target's own semantics* rather than in Marque:

- The fence is **four** checks inside a transaction, two of which depend on being able to see the rows
  a statement affected, and one on per-relation write counts for the transaction
  ([EDR-0007](./0007-delegation-by-containment-proof.md),
  [EDR-0033](./0033-assert-the-whole-write-set-not-just-the-named-relation.md)).
- The rehearsal depends on a transaction that can always be rolled back, and on timeouts that bound
  a statement and a lock wait ([EDR-0010](./0010-rehearse-before-you-sign.md)).
- The purity allowlist depends on knowing which functions can write, which is a per-engine catalogue.
- Identity depends on the engine's own auth integration
  ([EDR-0021](./0021-connections-identity-and-read-routing.md)).

The dangerous failure is not "MySQL is unsupported". It is **MySQL support that looks the same in the
UI and enforces less**, so an operator grants a delegation believing they have a row fence that is
not actually being post-asserted. A control you believe you have and do not is worse than one you
know you lack ([ZFN-2](https://zrz.io/zfn/2-engineering-priority-ordering/)).

## Decision

### The engine interface

An engine implementation supplies, at minimum:

| Capability | What it must provide |
|---|---|
| `parse` | statement → operations, relations, columns, extractable predicate; and whether it is inside the checkable subset |
| `purity` | the allowlist of functions known not to write |
| `fence.pre` | "would this touch rows outside the fence?" as a countable query |
| `fence.post` | "did any affected row end up outside the fence?" |
| `magnitude` | affected-row count, asserted inside the transaction |
| `fence.write_set` | per-relation write counts for the current transaction, readable before commit ([EDR-0033](./0033-assert-the-whole-write-set-not-just-the-named-relation.md)). MySQL's `performance_schema` counters are session- and instance-cumulative rather than transaction-scoped, so a MySQL engine must establish this some other way or **decline it** |
| `rehearse` | a transaction that structurally cannot commit, with statement and lock timeouts |
| `identity` | `shared`, `per_operator`, `pooled_with_role` — which are available |
| `read_routing` | replica endpoints and a comparable position for read-your-writes |
| `annotate` | how a marque id is attached to a session for the engine's own audit |
| `wire` | the protocol the local proxy emulates for this engine, if any |

**Every capability is declared, including as unavailable.** There is no default implementation that
silently no-ops; an engine that does not implement `fence.post` declares it, and Marque refuses to
sign a delegation whose enforcement would depend on it.

### The capability matrix is a product surface

Published per engine, shown when creating a target, shown on every delegation that depends on a
capability, and included in the bootstrap document
([EDR-0002](./0002-bootstrap-discovery-document.md)). An operator granting a row-fenced delegation on
an engine without `fence.post` sees, at the moment of granting, that the fence is enforced on
selection but not re-checked afterwards — and either accepts it explicitly or does not grant it.

### MySQL, specifically

When it is built, these are the decisions already made:

- **`fence.post` via a locking pre-select.** Select the primary keys the statement will affect `FOR
  UPDATE`, execute, then re-check those keys against the fence. Consequence: **a row fence on MySQL
  requires the table to have a primary key**, and costs a lock held for the transaction. A table
  without one cannot carry a row-fenced delegation, and Marque says so rather than approximating.
- **Writes are bounded by `innodb_lock_wait_timeout` plus an external watchdog** that issues a kill
  after a deadline, since the statement-timeout variable does not apply to writes. This is genuinely
  weaker than PostgreSQL's `statement_timeout` — the watchdog is a second moving part that can itself
  fail — and it is declared as such.
- **DDL is refused outright**, not merely unrehearsed. It implicitly commits, so a "rehearsal" of DDL
  would be an application, which is the one thing a rehearsal must never be
  ([EDR-0010](./0010-rehearse-before-you-sign.md)).
- **Session annotation is per connection, not per statement**, via connection attributes, so the
  marque identifier is attached at connect time and `per_operator` identity is what carries
  attribution.

### What stays shared

The parts that are genuinely engine-independent stay in one place and are not reimplemented: the
marque format and its verification, the logbook, escalation, delegation *semantics* (as opposed to
enforcement), the policy model, quotas, the scope-compilation and conformance machinery
([EDR-0016](./0016-natural-language-delegations-are-compiled.md),
[EDR-0017](./0017-conformance-matching-may-route-never-widen.md)), and the API. An engine supplies
enforcement primitives, never policy.

### Adding an engine

An engine ships when it has: a parser with a defined checkable subset, a purity allowlist reviewed
against that engine's extension ecosystem, all **four** fence checks or a declared absence, a rehearsal
whose rollback is structural, a timeout story for reads *and* writes, and a conformance suite
including near-miss statements, fence-escape attempts, a **NULL-fence** case, and a **cascade escape** —
a delegated `DELETE` on a parent with a cascading child must abort with the child named
([EDR-0033](./0033-assert-the-whole-write-set-not-just-the-named-relation.md)). **A partially-implemented engine does not
ship behind a flag**, because a flag is how a weaker control reaches production believing itself to
be the stronger one.

## Consequences

**Easier.**

- Adding an engine is a bounded, reviewable piece of work with an explicit checklist, rather than an
  open-ended port.
- Operators can see what they actually get per target, and grant accordingly.
- PostgreSQL support is not compromised to accommodate a lowest common denominator — the interface is
  "declare what you can do", not "do what everyone can do".

**Harder.**

- **Engines are expensive**, so there will be fewer of them and they will arrive slowly. Someone will
  reasonably ask why MySQL support is months rather than days; the answer is this record.
- **The capability matrix is a surface that must stay honest.** A capability declared and not truly
  implemented is worse than not declaring it, so each one needs a test that proves it on a live
  instance of that engine.
- Operators now face a decision they did not previously know existed — whether a fence they cannot
  fully enforce is acceptable — which is more correct and less comfortable.
- Per-engine test infrastructure (a real instance of each engine, in CI) is real cost.

**New obligations.**

- Every declared capability has a test executing against that engine, and the matrix is generated
  from those tests rather than hand-maintained — a hand-written matrix drifts from reality, and this
  is a matrix whose drift is a security bug.
- Engine capability differences appear in the changelog, because they change what a delegation means.

## References

- [ZFN-2](https://zrz.io/zfn/2-engineering-priority-ordering/) — never trade a stronger guarantee for
  a wider compatibility claim.
- [ZFN-22](https://zrz.io/zfn/22-extract-complexity-at-the-seam/) — the engine interface is the seam.
- [EDR-0007](./0007-delegation-by-containment-proof.md) — the four fence checks an engine must
  supply or decline.
- [EDR-0010](./0010-rehearse-before-you-sign.md) — the rehearsal contract.
- [EDR-0021](./0021-connections-identity-and-read-routing.md) — the identity modes an engine declares.

## Changelog

- **2026-08-15**: Accepted.
- **2026-08-16**: Amended after the second panel's should-fix pass: added `fence.write_set` to the capability table and corrected "three fence checks" to four; [EDR-0033](./0033-assert-the-whole-write-set-not-just-the-named-relation.md) had claimed this record already carried it, and it did not.
- **2026-08-16**: Amended in the second panel's should-fix pass: corrected the two remaining "three fence checks" references and added NULL-fence and cascade-escape cases to the required conformance suite.
