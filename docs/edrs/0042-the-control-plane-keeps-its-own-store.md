---
id: 42
title: "Give the control plane's store a schema, a migrator, and a driver rule that survives PostgreSQL"
summary: "The store was already fixed to PostgreSQL. What M1 owed was the schema, a forward-only digest-checked migrator, and a replacement for EDR-0005's no-driver-linked rule, which PostgreSQL for Marque's own state makes unachievable."
status: accepted
implementation: none
implementation_note: "Nothing stores anything. This is the decision M1 owed before it could write a line of storage code; the import rule it describes lands in the same change as the first package that opens a connection."
date: 2026-08-20
authors:
  - "Theo Zourzouvillys <theo@sixfathoms.dev>"
tags: [architecture, ops, foundational]
supersedes: null
superseded_by: null
aliases: []
---

## TL;DR

The [implementation plan](../content/overview/implementation-plan.md) lists *"the control-plane
storage schema and its migration tooling"* as decision debt owed before M1 closes. Read as *"which
database"*, that debt does not exist: [EDR-0013](./0013-async-work-rides-the-wal.md) fixed Marque's
own state on **PostgreSQL** and the architecture page says so in its own section. What was genuinely
undecided is the rest of the sentence, plus a problem the corpus had not noticed.

- **The schema is tenant-partitioned from migration one**, per
  [EDR-0025](./0025-tenants-are-partitioned-from-day-one.md), even though M1 has no identity to
  derive a tenant from.
- **Migrations are embedded, numbered, forward-only and digest-checked**, applied by an explicit
  command. Startup **verifies and refuses**; it does not migrate.
- **[EDR-0005](./0005-control-plane-holds-no-credentials.md)'s driver rule cannot survive as
  written.** It says the Harbourmaster *"has no database driver for target engines linked in"* — and
  its own store is PostgreSQL, and PostgreSQL is a target engine. That sentence has been
  unachievable since EDR-0013 was accepted; nobody noticed because the Harbourmaster had no storage
  code. This record replaces the mechanism and keeps the property.

## Context

### The debt was half-discharged already, and I read it wrong first

The plan's phrasing invites the question "which database", and the first draft of this record answered
it — with SQLite, on the argument that an embedded store keeps EDR-0005's driver sentence checkable.
Review found the premise false twice over:
[EDR-0013](./0013-async-work-rides-the-wal.md) states *"this ties Marque to PostgreSQL for its own
state. That is an accepted constraint, not a regrettable one"*, and
[the architecture page](../content/overview/architecture.md) opens a **Storage** section with
*"Marque's own state is PostgreSQL, and that is fixed"*.

Worse, SQLite would have removed the mechanism EDR-0013 *is*: there is no logical WAL for a
replication listener to consume and no `pg_logical_emit_message` to emit into. The record is written
down this way because the mistake is instructive — a decision that looks unmade is worth grepping
for twice, and "the corpus does not say" is a claim like any other.

### The problem nobody had noticed

EDR-0005's mechanism is a sentence about the binary: *"It has no database driver for target engines
linked in."* That is unusually checkable — a dependency graph is a fact — and it has been **false in
principle since EDR-0013 was accepted**. Marque's own store is PostgreSQL. Marque's targets are
PostgreSQL. One driver serves both, and the control plane must link it.

It survived unchallenged only because the Harbourmaster had no storage code and therefore no drivers
at all. M1 is where that ends, so M1 is where the rule has to be re-expressed or abandoned.

## Decision

### The store is PostgreSQL, which this record does not decide

[EDR-0013](./0013-async-work-rides-the-wal.md) decided it. This record depends on it and settles what
sits on top.

### EDR-0005's driver rule becomes containment, not absence

**The property EDR-0005 protects is unchanged**: an attacker owning the entire Harbourmaster obtains
no target credential and cannot reach a target. What changes is the mechanism, because the old one is
not available:

1. **The driver is confined to one package.** `internal/store` may import a driver for a target
   engine. Nothing else in the control plane may, enforced by a `depguard` rule in `.golangci.yml` —
   a file this repository already maintains, with a linter already in the enabled set.

   **Target engines, plural, not PostgreSQL.** EDR-0005's sentence is engine-agnostic and
   [EDR-0026](./0026-a-second-engine-is-a-capability-matrix.md) plans MySQL, so a rule naming only
   PostgreSQL would stop covering the sentence on the day a second engine arrives, and would do it
   silently. The rule denies a list of driver paths and that list grows with the engine list — a
   denylist, which is the weaker shape, and the one `depguard` offers.
2. **The Harbourmaster holds no target connection parameters and no target credential**, which is
   what EDR-0005's own Decision section already says at greater length. That half is untouched and
   remains the load-bearing half.

**Say plainly what is lost.** Absence is a coarser and stronger property than containment: it needs
no allowlist, and no package can quietly acquire the capability by being added to one. Containment is
defeated by a one-line edit to a lint configuration, and that edit will look innocuous in a diff.
Trading down was not a choice — EDR-0013 removed the stronger option before this record existed — but
it is a trade and the successor of this rule should know it was made under duress rather than
preference.

EDR-0005 gains a pointer here and a dated changelog line; its `implementation_note`, which currently
promises the binaries *"link no database driver"*, is corrected in this change.

### Migrations: embedded, forward-only, digest-checked, and never implicit

- **Embedded** with `embed.FS`, so the binary is self-contained and a missing migration file is a
  *build* failure rather than a deployment surprise.
- **Forward only.** No down migrations: a rollback plan nobody has run, against the only copy of the
  state, is not a plan.
- **Each migration is recorded with its content digest**, and the applied set must be an exact
  prefix of the embedded set — no gaps, no divergence. An edited migration that has already been
  applied is therefore a refusal rather than a silent difference between two deployments.
- **The record of a migration commits in the same transaction as its DDL**, which PostgreSQL supports
  and which is the reason a half-applied migration is not a state this design has.
- **Applying is an explicit command.** Startup verifies the schema and **refuses to serve** on any
  mismatch — ahead, behind, or divergent — naming both versions. Migrating implicitly at startup
  turns every deploy into a schema change nobody chose to run, and it is the same lazy-initialisation
  failure EDR-0005 warns about pointed at the schema instead of the connection.

### The M1 schema

Tenant-partitioned from the first migration, per EDR-0025: `tenant_id NOT NULL` on every domain
table, leading every index that matters and present in every unique constraint. M1 has no identity,
so it uses one configured development tenant — **never a request field**, which is the rule EDR-0025
exists to protect and which M4 makes real.

| Table | Holds |
|---|---|
| `schema_migrations` | number, content digest, applied-at |
| `requests` | `req_…` reference ([EDR-0038](./0038-a-request-is-a-shareable-watchable-object.md)), tenant, statement, target, role, submitter as a bare string, state |
| `approvals` | tenant, request, **stage**, approver, at |
| `executions` | tenant, request, at, outcome, rows affected |

Two vocabularies are taken from records rather than invented, because a forward-only schema makes
widening one an unnecessary migration: `requests.state` is EDR-0038's `pending` / `approved` /
`executed`, and `executions.outcome` is EDR-0011's, which includes **`indeterminate`** and
**`aborted_not_applied`** — neither is `failed`, and a two-valued column would have to be widened
later for nothing saved now.

There is no `targets` or `roles` table: those are reviewed configuration
([EDR-0015](./0015-policy-is-reviewed-configuration.md)), not rows. There is no `delegations`, no
`roster`, no `nonces`, and **no logbook** — each arrives with the milestone that decides it.

### Three things M1 gets wrong on purpose

Named individually, because a reader who sees only one will assume the rest is right.

1. **Approval is a row, not a signature**, and the table is flat.
   [EDR-0030](./0030-a-marque-states-its-own-approval-requirement.md) exists because a flat
   `required`/`eligible` model made a chain requiring Sam *then* data-oncall satisfiable by two of
   data-oncall. M1 carries a `stage` column so the shape is not actively wrong, and M3 replaces the
   **table**, not merely the signature.
2. **`requests.state` is authoritative**, where [EDR-0012](./0012-the-logbook-is-append-only.md)
   makes current state a disposable projection of the journal. M1 has no journal, so it has no
   projection; M6 inverts this.
3. **`executions` is a control-plane report, and is not
   [EDR-0011](./0011-execution-is-idempotent-and-fenced.md)'s ledger.** That ledger is Pilot-local,
   durable, claim-first, and carries an incarnation — it is the fence. Nothing here should grow a
   nonce column; where the Pilot keeps its own state is undecided and is
   [issue #34](https://github.com/sixfathoms/marque/issues/34), due before M5.

## Consequences

**Easier.**

- EDR-0013's mechanism stays available: the WAL is there when async work arrives, rather than needing
  a store migration first.
- EDR-0012's logbook is implementable in the same database with the permission model it requires — a
  withheld `UPDATE` grant and non-ownership — so M6 has no second transactional store and no dual
  write, which is the thing [ZFN-24](https://zrz.io/zfn/24-one-transactional-store-per-write/)
  forbids and EDR-0013 was designed around.
- Two vocabularies and a tenant column arrive at migration one, where they cost a column each.

**Harder.**

- **Running the control plane now requires a PostgreSQL**, including for a developer trying M1 on a
  laptop. `make test` stays offline because unit tests do not touch the store, but the walking
  skeleton is no longer a single binary and a file.
- **EDR-0005's guarantee is weaker than it was on paper**, and the paper version was never
  achievable. Containment is defeated by an allowlist edit; absence could not be. Anyone reviewing a
  change to that `depguard` rule is reviewing a security control.
- **A forward-only schema with no backup story is an irreversible deploy.** The migrator is explicit
  rather than automatic, which is most of the answer, and the rest belongs with the deployment story
  Phase 1 does not have.

**New obligations.**

- **M1** lands the `depguard` rule in the same change as the first package that opens a connection.
  A mechanism replaced and then not implemented is worse than the sentence it replaced.
- **M5** needs the Pilot's own durable store decided — [issue #34](https://github.com/sixfathoms/marque/issues/34).
- **M6** builds the logbook in this database, appends from the Harbourmaster
  ([EDR-0011](./0011-execution-is-idempotent-and-fenced.md) has the Pilot report and the
  Harbourmaster append), and inverts obligation 2 above.

## References

- [EDR-0013](./0013-async-work-rides-the-wal.md) — fixed the store on PostgreSQL. This record depends
  on it and does not revisit it.
- [EDR-0005](./0005-control-plane-holds-no-credentials.md) — the driver rule this re-expresses.
- [EDR-0025](./0025-tenants-are-partitioned-from-day-one.md) — tenancy from the first migration.
- [EDR-0038](./0038-a-request-is-a-shareable-watchable-object.md) and
  [EDR-0011](./0011-execution-is-idempotent-and-fenced.md) — the two vocabularies borrowed rather
  than invented.
- [The implementation plan](../content/overview/implementation-plan.md) — M1, and the decision debt
  this discharges.

## Changelog

- **2026-08-20**: Accepted.
