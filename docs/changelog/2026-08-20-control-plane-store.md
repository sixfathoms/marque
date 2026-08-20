---
title: "A schema, a migrator, and a rule that had quietly stopped being true"
tags: [ops, security, docs]
order: 2
---

The [implementation plan](/overview/implementation-plan/) listed *"the control-plane storage schema
and its migration tooling"* as decision debt owed before M1 closes.
[EDR-0042](/edrs/0042-the-control-plane-keeps-its-own-store/) discharges it — and found, on the way,
that one of the corpus's mechanisms had been unachievable for five days without anyone noticing.

### Fixed

- **[EDR-0005](/edrs/0005-control-plane-holds-no-credentials/)'s driver rule could not be
  implemented.** It says the Harbourmaster *"has no database driver for target engines linked in"*.
  But [EDR-0013](/edrs/0013-async-work-rides-the-wal/) fixes Marque's own state on **PostgreSQL**,
  and PostgreSQL is a target engine — one driver serves both, and the control plane must link it.

  The sentence survived because it was never tested against reality: the Harbourmaster had no storage
  code, so it linked no drivers of any kind and the rule was true by vacancy. M1 is the milestone
  that ends that, which is why M1 is where the rule had to be re-expressed.

  EDR-0042 replaces **absence** with **import discipline** — a driver confined by `depguard` to the
  one package that needs it, with **no exception at all** for an engine Marque does not store its own
  state in, so MySQL stays wholly absent when
  [EDR-0026](/edrs/0026-a-second-engine-is-a-capability-matrix/) arrives. PostgreSQL is the single
  weakened case, weakened only because EDR-0013 made it the control plane's own store.

  The record is blunt about what that is not. A linter reads **imports, not capability**:
  `database/sql` registration is process-wide, so once the store package registers a driver any
  package can `sql.Open` it without importing anything the rule can see. It buys "the capability
  arrives by a reviewed edit rather than by accident", and nothing stronger. EDR-0005's sentence is
  amended in place rather than left standing, because two accepted records contradicting each other
  is worse than either being wrong, and `CLAUDE.md`'s invariant list is corrected for the same reason.

### Added

- **A tenant-partitioned schema from migration one**, per
  [EDR-0025](/edrs/0025-tenants-are-partitioned-from-day-one/) — `tenant_id` leading every index that
  matters and present in every unique constraint, filled from one configured development tenant while
  M1 has no identity to derive it from, and **never a request field**.
- **A migrator that refuses more than it applies.** Embedded, numbered, forward-only, each migration
  recorded with a content digest in the transaction that applies it, and the applied set required to
  be an exact prefix of the embedded set. It runs as an explicit command; startup **verifies and
  refuses to serve** on any mismatch. Migrating implicitly at startup turns every deploy into a
  schema change nobody chose to run.
- **Two vocabularies borrowed rather than invented**, because a forward-only schema makes widening a
  column an unnecessary migration: request states from
  [EDR-0038](/edrs/0038-a-request-is-a-shareable-watchable-object/), and execution outcomes from
  [EDR-0011](/edrs/0011-execution-is-idempotent-and-fenced/) — which means `indeterminate` and
  `aborted_not_applied` exist from the first migration, and neither is `failed`.

**Three things M1 gets wrong on purpose, named individually** so a reader who sees one does not assume
the rest is right: approval is a row rather than a signature — carrying `stage`, so it is not the
flat shape
([EDR-0030](/edrs/0030-a-marque-states-its-own-approval-requirement/) exists because flat was a
defect); `requests.state` is authoritative where [EDR-0012](/edrs/0012-the-logbook-is-append-only/)
makes current state a disposable projection; and `executions` is a control-plane *report* which must
never grow a nonce column, because [EDR-0011](/edrs/0011-execution-is-idempotent-and-fenced/)'s
ledger is Pilot-local and is the fence itself.

Where the Pilot keeps that ledger is undecided, and no record says —
[issue #34](https://github.com/sixfathoms/marque/issues/34), due before M5. The target database is
the wrong answer for a reason worth knowing: a role that can write a Marque-owned table is a role
with more grants than the operator's statement needs, which is the opposite of what EDR-0005 is for.

This is the first record in the corpus forced by implementation rather than by review, and it shows.
A design read end to end looks consistent; a design you try to build tells you which of its sentences
were never load-bearing.
