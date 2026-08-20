---
title: "Where the control plane keeps its own state, and what that buys"
tags: [ops, security, docs]
order: 2
---

M1 could not write a line of storage code without answering a question the corpus had never asked:
where does the Harbourmaster keep its own state? The [implementation
plan](/overview/implementation-plan/) listed it as decision debt owed before the milestone closes.
[EDR-0042](/edrs/0042-the-control-plane-keeps-its-own-store/) answers it, and the answer turns out to
be less about databases than about an invariant.

### Added

- **The control plane's store is SQLite in a file, through a pure-Go driver.** One process, one file,
  created if absent. Written explicitly as a **Phase 1** decision: a single-writer file-backed store
  is exactly right for a control plane that runs as one process and exactly wrong for one that does
  not, and the record says superseding it is expected work rather than failure.
- **Migrations are embedded in the binary, numbered, forward-only, and run at startup.** No down
  migrations — a rollback plan nobody has run, against the only copy of the state, is not a plan. A
  binary meeting a schema newer than it knows refuses to start and names both versions.
- **A thin M1 schema**: `requests`, `approvals`, `executions`, `schema_migrations`. The `approvals`
  table holds a row and not a signature, which is the whole of what M1 gets wrong on purpose. It is
  named `approvals` rather than `marques` so nothing reads as an early
  [marque](/edrs/0004-marques-are-signed-leases/).

### Changed

- **[EDR-0005](/edrs/0005-control-plane-holds-no-credentials/)'s driver rule becomes a build guard.**
  That record says the Harbourmaster *"has no database driver for target engines linked in"* — an
  unusually checkable sentence, because a dependency graph is a fact rather than an intention, and
  one nothing had ever checked because the Harbourmaster had no storage code and so no drivers at
  all.

  The moment the control plane gets a store, that stops being free. A PostgreSQL-backed control plane
  puts a PostgreSQL driver in the binary, and no guard can then distinguish a driver linked for the
  control plane's own database from one linked to reach a target. The invariant would survive as
  prose and lose its mechanism — which is the failure this corpus spends most of its effort on.

  Choosing an embedded store keeps the sentence checkable, and M1 implements the check in the same
  change as the first package that opens the database.

The record is honest about what this costs. A file-backed store does not survive a second control
plane instance, and the successor inherits a genuinely hard problem: a PostgreSQL-backed control
plane cannot assert the absence of a PostgreSQL driver, so the same invariant would need a different
mechanism. That is named in Consequences rather than discovered in Phase 2.

One question is scoped out and tracked rather than answered:
[EDR-0012](/edrs/0012-the-logbook-is-append-only/) makes the logbook immutable through a withheld
`UPDATE` grant and non-ownership of the table, and SQLite has no role system, no `GRANT` and no table
ownership. Where the logbook lives is M6's decision —
[issue #32](https://github.com/sixfathoms/marque/issues/32) — and one of its candidate answers, that
the Pilot writes it, would change what M1's execution recording grows into.
