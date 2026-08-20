---
id: 42
title: "Keep the control plane's own state in an embedded store, and migrate it forward only"
summary: "The Harbourmaster's own state lives in a file-backed SQLite database reached through a pure-Go driver, so EDR-0005's ban on a target-engine driver becomes a build guard rather than a convention. Migrations are embedded and forward-only."
status: accepted
implementation: none
implementation_note: "Nothing stores anything yet. This is the decision M1 owed before it could write a line of storage code, and the guard it describes lands with the first package that opens the database."
date: 2026-08-20
authors:
  - "Theo Zourzouvillys <theo@sixfathoms.dev>"
tags: [ops, foundational, execution]
supersedes: null
superseded_by: null
aliases: []
---

## TL;DR

The Harbourmaster has to keep state — requests, approvals, what happened — and nothing said where.

- The store is **SQLite in a file**, reached through **`modernc.org/sqlite`**, which is pure Go.
- That choice is what turns [EDR-0005](./0005-control-plane-holds-no-credentials.md)'s *"no database
  driver for target engines linked in"* from a convention into a **build guard**: the test asserts
  `cmd/harbourmaster`'s dependency graph contains no PostgreSQL driver, and it can only assert that
  if the control plane does not need one.
- **Migrations are embedded in the binary, numbered, and forward-only.** They run at startup, inside
  a transaction each, and are recorded. A binary meeting a schema newer than it knows **refuses to
  start**.
- **The logbook is not this.** [EDR-0012](./0012-the-logbook-is-append-only.md) requires a role
  holding `INSERT` and `SELECT` and *not owning* the table, and SQLite has no such permission model.
  Where the logbook lives is M6's decision and is
  [issue #32](https://github.com/sixfathoms/marque/issues/32).

## Context

M1 is the walking skeleton: submit a statement, store it, approve it, run it, record what happened.
Four of those five verbs need somewhere to put something, and the corpus does not say where. The
[implementation plan](../content/overview/implementation-plan.md) lists *"the control-plane storage
schema and its migration tooling"* as decision debt owed before M1 closes, and this is that record.

The question is not really "which database". It is what happens to an invariant.

**EDR-0005 says the Harbourmaster cannot connect to a target, and says how**: *"It has no database
driver for target engines linked in."* That sentence is unusual in this corpus because it is
mechanically checkable — a dependency graph is a fact, not an intention — and nothing had ever
checked it, because the Harbourmaster had no storage code and so no drivers at all.

The moment the control plane gets a store, that changes. If the store is PostgreSQL, a PostgreSQL
driver is in the binary, and the sentence is no longer checkable: a guard could not tell a driver
linked for the control plane's own database from one linked to reach a target. The invariant would
survive as prose and lose its mechanism, which is the failure mode this corpus is most careful about.

A second constraint pushes the same way. `CGO_ENABLED=1` is already exported for the whole build in
anticipation of `libpg_query` ([EDR-0039](./0039-the-grammar-is-parsed-by-postgresqls-own-parser.md)),
and the Makefile records that *"today nothing in the dependency graph has a cgo file, so the flag
changes no binary yet"*. A cgo SQLite would make that false at M1 rather than M2, and would put a C
dependency in the release matrix a milestone early, for a store nobody will run in production.

## Decision

### The store is SQLite, in one file, through a pure-Go driver

`modernc.org/sqlite`. One file, named by configuration, created if absent.

This is a **Phase 1** decision and is written as one. The plan says Phase 1 deploys nowhere on
purpose; a single-writer file-backed database is exactly right for a control plane that runs as one
process on one machine, and exactly wrong for one that does not. Superseding this record is expected
work rather than a failure, and the thing to preserve when it happens is the property below.

### What that buys, and it is the point of the record

**The Harbourmaster links no PostgreSQL driver, and a test says so.** A build-time assertion walks
`cmd/harbourmaster`'s dependency graph and fails if it contains a PostgreSQL driver package. It is
the same instrument the schema check and the platform check already use: a property that would
otherwise be a habit, made into something that fails.

The guard is **narrow on purpose**. It does not assert the Harbourmaster cannot reach a network, or
that it holds no credential — neither is true of a process that talks to Pilots over gRPC. It asserts
exactly what EDR-0005 wrote down, no more, because a guard that claims more than it checks is worse
than the sentence it replaced.

### Migrations are embedded, numbered and forward-only

- Migration files are **embedded in the binary** with `embed.FS`. A binary that cannot find its
  migrations is a binary that cannot be deployed alone, and the operator finds out on a machine
  rather than in CI.
- **Forward only.** No down migrations. A down migration is a rollback plan that has never been run,
  and this project's position on guards nobody has watched fail applies with more force to one that
  would run against the only copy of the state.
- Each runs **inside its own transaction**, and the applied set is recorded in a table.
- **A binary meeting a schema version newer than it knows refuses to start**, naming both versions.
  The alternative is an old binary writing to a new schema, which is a corruption nobody notices
  until a read fails much later.
- Migrations run **at startup**, not on first use. Lazy initialisation is what EDR-0005 already warns
  about for connections: no attempt, no error, quiet logs, and the first symptom during an incident.

### The M1 schema

Deliberately thin, and named so a reader can see what M1 does *not* have:

| Table | Holds |
|---|---|
| `schema_migrations` | which migrations have been applied, and when |
| `requests` | the submitted statement, its target and role, the submitter as a bare string, and a state |
| `approvals` | who approved a request and when — **a row, not a signature** |
| `executions` | one row per execution attempt: when, the outcome, and rows affected |

`approvals` holding a row rather than a signature is the whole of what M1 gets wrong on purpose, and
[EDR-0004](./0004-marques-are-signed-leases.md) is what M3 replaces it with. The table is named
`approvals` and not `marques` for that reason: nothing here should read as an early marque.

There is no `delegations`, no `roster`, no `logbook`, and no `nonces`. Each arrives with the
milestone that decides it.

## Consequences

**Easier.**

- EDR-0005's driver sentence becomes a test. It has been prose since it was written.
- M1 needs no database to run its unit tests, and no container to run the control plane at all.
- The cgo posture is unchanged until M2, so `libpg_query` remains the first C dependency and the
  release matrix meets cgo once rather than twice.

**Harder.**

- **A file-backed single-writer store does not survive Phase 2.** Two Harbourmasters cannot share it,
  and the console arriving in Phase 2 does not change that by itself but a second instance would.
  This record will be superseded, and the successor has to keep the guard alive somehow — which is
  the genuinely hard part, because a PostgreSQL-backed control plane cannot assert the absence of a
  PostgreSQL driver and would need a different mechanism for the same invariant.
- **Forward-only migrations mean a bad migration is repaired by writing another one**, under whatever
  time pressure produced it. That is the accepted cost of not carrying untested rollback code.
- **The store is a file, so backing it up is somebody's job** and nothing in Phase 1 does it. The
  operational shape of that belongs with the deployment story, which Phase 1 does not have.

**New obligations.**

- **M1** implements the guard in the same change as the first package that opens the database, not
  afterwards — an invariant made checkable and then not checked is worse than one nobody promised.
- **M6** decides where the logbook lives, and it is not here:
  [issue #32](https://github.com/sixfathoms/marque/issues/32).
- **Phase 2** supersedes this record if a second control-plane instance is wanted, and owes the
  successor a mechanism for EDR-0005's driver rule.

## References

- [EDR-0005](./0005-control-plane-holds-no-credentials.md) — the invariant this makes checkable.
- [EDR-0012](./0012-the-logbook-is-append-only.md) — the append-only logbook, deliberately out of
  scope here.
- [EDR-0039](./0039-the-grammar-is-parsed-by-postgresqls-own-parser.md) — why `CGO_ENABLED=1` is
  already set, and why this record avoids being the thing that first uses it.
- [The implementation plan](../content/overview/implementation-plan.md) — M1, and the decision debt
  this discharges.

## Changelog

- **2026-08-20**: Accepted.
