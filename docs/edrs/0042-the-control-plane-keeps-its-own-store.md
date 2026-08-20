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

### EDR-0005's driver rule becomes import discipline, not absence

**The property EDR-0005 protects is unchanged**: an attacker owning the entire Harbourmaster obtains
no target credential and cannot mint authority to reach one. Not *"cannot reach a target"* — EDR-0005
was amended on 2026-08-16 to say a compromised control plane retains a bounded, quota'd, target-visible
read channel by relaying operator-signed reads, and overstating that is the error that amendment
exists to correct. What changes is the mechanism, because the old one is not available.

1. **Two packages, and only they import a driver.** `internal/harbourmaster/store` imports a
   PostgreSQL driver for the control plane's own database. `internal/pilot/postgres` imports one to
   reach a target — the Pilot must, and any rule confining the driver repository-wide would be wrong
   about it. No other package imports either, and **no Harbourmaster package imports the Pilot
   adapter**, which is the boundary that carries the weight.
2. **Enforced by `depguard`.** It is **not** in `.golangci.yml` today, and this record does not add
   it — the rule lands with M1's first storage package, which is the change that first makes it
   possible to violate. A
   driver for a target engine Marque does not yet store its own state in — MySQL, when
   [EDR-0026](./0026-a-second-engine-is-a-capability-matrix.md) arrives — stays wholly absent from
   the control plane, with no exception at all. PostgreSQL is the single weakened case and it is
   weakened only because EDR-0013 made it the control plane's own store.
3. **The Harbourmaster holds no target connection parameters and no target credential.** EDR-0005
   already decides the credential half; where a target's *connection parameters* live it does not
   say, and this record does not settle it either — a role names a target, and what resolves that
   name to a host is undecided. Flagged rather than assumed:
   [issue #36](https://github.com/sixfathoms/marque/issues/36).

**Say plainly what this is not.** `depguard` reads **imports**, not capability. `database/sql`
registration is process-wide, so once `internal/harbourmaster/store` registers a driver, any package
in the binary can call `sql.Open("pgx", …)` without importing anything the linter would see. This is
**import discipline**, which makes the capability arrive by a reviewed edit rather than by accident —
it is not a sandbox, and calling it containment would claim more than it checks.

Absence was strictly stronger: it needed no allowlist, no exceptions, and could not be widened by a
diff that looks like configuration. Trading down was not a choice — EDR-0013 removed the stronger
option before this record existed — but it is a trade, and the next person to touch that `depguard`
block is editing a security control.

EDR-0005's driver sentence is **amended in place** in this change, because leaving two accepted
records contradicting each other is worse than either of them being wrong. Its decision — the control
plane holds no target credential — is untouched.

### Migrations: forward-only, digest-checked, serialised, and never implicit

- **Embedded** with `embed.FS`, so the binary carries its migrations and a deployment cannot arrive
  missing half of itself. This is **not** a guard against a deleted migration file: deleting the last
  unapplied one produces a binary whose embedded and applied sets still agree. What catches that is
  contiguous numbering.
- **Numbered contiguously from 1**, with a gap refused. Combined with the digest below, an edited,
  reordered or removed migration is a refusal rather than a silent difference between two
  deployments.
- **Each migration records the SHA-256 of its raw bytes**, and the applied set must be an exact
  prefix of the embedded set with matching digests.
- **Forward only.** No down migrations: a rollback plan nobody has run, against the only copy of the
  state, is not a plan.
- **The record of a migration commits in the same transaction as its DDL.** PostgreSQL supports
  transactional DDL, which is why a half-applied migration is not a state this design has — and a
  migration containing an operation PostgreSQL forbids inside a transaction (`CREATE INDEX
  CONCURRENTLY`, `VACUUM`, `ALTER TYPE … ADD VALUE` on older servers) is **rejected by the migrator**
  rather than run outside one.
- **Serialised by a session-scoped advisory lock** — `pg_advisory_lock`, taken before verification
  and released explicitly when the run ends. Not a transaction-scoped one: that releases at commit,
  so with each migration in its own transaction the second and later ones would race. Two migrators
  starting together is an ordinary deployment event, not an exotic one.
- **Migrations run as their own role**, distinct from the runtime role and holding the privileges
  the runtime role must not have. [EDR-0012](./0012-the-logbook-is-append-only.md) already requires
  the journal to be owned by a role used only by migrations, because Marque's runtime role must not
  own the table it appends to — establishing that separation at migration one costs nothing and
  M6 otherwise inherits an ownership problem.
- **Applying is an explicit command.** Startup verifies and **refuses to serve** on any mismatch —
  ahead, behind, or divergent — naming both versions. Migrating implicitly at startup turns every
  deploy into a schema change nobody chose to run, and it is the same lazy-initialisation failure
  EDR-0005 warns about, pointed at the schema instead of the connection.

### The M1 schema

Tenant-partitioned from the first migration, per EDR-0025: `tenant_id NOT NULL` on every domain
table, leading every index that matters and present in every unique constraint. **A tenant column is
not partitioning on its own** — every foreign key is composite and carries `tenant_id`, so a row
cannot reference a parent belonging to another tenant. M1 has no identity, so `tenant_id` comes from
one configured development tenant, **never a request field**, which is the rule EDR-0025 exists to
protect and which M4 makes real.

| Table | Holds |
|---|---|
| `schema_migrations` | number, SHA-256 digest, applied-at |
| `requests` | `req_…` reference, tenant, statement, target, role, submitter as a bare string, the operator's **reason**, state, `created_at` |
| `approvals` | tenant, request, **stage**, approver, at |
| `executions` | tenant, request, at, outcome, rows affected — **rows affected is M1's "result"**, and the plan's *"the result and the statement land in a table"* means exactly that and nothing richer |

**`requests.state` carries all seven of [EDR-0038](./0038-a-request-is-a-shareable-watchable-object.md)'s
values** — `pending`, `verifying`, `approved`, `refused`, `expired`, `executed`, `indeterminate` —
even though M1 can only produce three of them. A forward-only schema makes widening a constrained
column an unnecessary migration, and a vocabulary that already exists in an accepted record is not
this record's to shorten.

**`executions.outcome` is decided here, not borrowed.**
[EDR-0011](./0011-execution-is-idempotent-and-fenced.md) names `in_progress`, `aborted_not_applied`
and `indeterminate`, closes no set and settles no successful token, so a first migration cannot be
written from it. This record fixes the column as `committed`, `rolled_back`, `aborted_not_applied`
and `indeterminate`. Three of those match `execution.*` kinds
[EDR-0012](./0012-the-logbook-is-append-only.md) illustrates — and *illustrates* is that record's own
word, since its list is explicitly not closed — while `aborted_not_applied` comes from EDR-0011.
`in_progress` is deliberately absent: a control-plane report is written when an attempt ends, and
in-flight state belongs to the Pilot's own ledger.

There is no `targets` or `roles` table: those are reviewed configuration
([EDR-0015](./0015-policy-is-reviewed-configuration.md)), not rows. There is no `delegations`, no
`roster`, no `nonces`, and **no logbook** — each arrives with the milestone that decides it.

### Three things M1 gets wrong on purpose

Named individually, because a reader who sees only one will assume the rest is right.

1. **Approval is a row, not a signature.** A row asserts that somebody approved; a signature is
   checkable by a Pilot that need call nobody back except for revocation — the asterisk
   [EDR-0004](./0004-marques-are-signed-leases.md) says must be stated wherever its offline property
   is claimed. The table is **not** flat — it carries `stage`,
   because [EDR-0030](./0030-a-marque-states-its-own-approval-requirement.md) exists on the strength
   of a flat `required`/`eligible` model letting a chain requiring Sam *then* data-oncall be
   satisfied by two of data-oncall, and that shape costs one column to avoid now and a rebuild to
   avoid later. **M3 builds the
   signed marque that replaces the table**, and M7 wires it into this path and deletes the stub —
   the plan is explicit that M2–M6 build and M7 activates.
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
- The request-state vocabulary and the tenant column arrive at migration one, where the column
  costs a column.

**Harder.**

- **Tenancy is not one column.** Every foreign key is composite and carries `tenant_id` — a
  constraint on every relationship in the schema from here on, not a one-off cost. EDR-0025's "one
  column and one discipline" is honest about the column and quiet about the discipline.
- **Running the control plane now requires a PostgreSQL**, including for a developer trying M1 on a
  laptop. `make test` stays offline because unit tests do not touch the store, but the walking
  skeleton is no longer a single binary and a file.
- **EDR-0005's guarantee is weaker than it was on paper**, and the paper version was never
  achievable. Import discipline is defeated four ways where absence was defeated by none: an
  allowlist edit; a `sql.Open` by driver name in a package that imports nothing the linter sees; a
  transitive dependency linking a driver the first-party rule never looks at; and **a file the
  linter never parses** — `make lint` passes no build tags, so anything behind one is invisible,
  which includes M1's own integration test, the file most certain to import a driver. It buys "the
  capability arrives by a reviewed edit rather than by accident", and no more. Anyone touching that
  `depguard` block is touching a security control.
- **A forward-only schema with no backup story is an irreversible deploy.** An explicit migrator
  means nobody applies one by accident, which is a smaller part of the answer than it sounds: once
  applied, a bad migration is repaired only by writing another, and a destructive one is not
  repairable at all. Backup, restore and roll-forward are genuinely missing, and they belong with the
  deployment story Phase 1 does not have — named here so the gap is chosen rather than discovered.

**New obligations.**

- **M1** lands the `depguard` rule in the same change as the first package that opens a connection.
  A mechanism replaced and then not implemented is worse than the sentence it replaced.
- **M5** needs the Pilot's own durable store decided — [issue #34](https://github.com/sixfathoms/marque/issues/34).
- **M6** builds the logbook in this database and appends from the Harbourmaster
  ([EDR-0011](./0011-execution-is-idempotent-and-fenced.md) has the Pilot report and the Harbourmaster
  append), after which the journal is authoritative and `requests.state` is a projection.
- **M4** takes `tenant_id` from the authenticated principal instead of the configured development
  tenant. The column exists from migration one so that is a change of source, not of schema.

## References

- [EDR-0013](./0013-async-work-rides-the-wal.md) — fixed the store on PostgreSQL. This record depends
  on it and does not revisit it.
- [EDR-0005](./0005-control-plane-holds-no-credentials.md) — the driver rule this re-expresses.
- [EDR-0025](./0025-tenants-are-partitioned-from-day-one.md) — tenancy from the first migration.
- [EDR-0038](./0038-a-request-is-a-shareable-watchable-object.md) — the request states, borrowed in
  full. [EDR-0011](./0011-execution-is-idempotent-and-fenced.md) — which names three execution
  outcomes and closes no set, so this record decides that column rather than borrowing it.
- [The implementation plan](../content/overview/implementation-plan.md) — M1, and the decision debt
  this discharges.

## Changelog

- **2026-08-20**: Accepted.
