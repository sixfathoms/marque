---
id: 42
title: "Give the control plane's store a schema, a migrator, and a driver rule that survives PostgreSQL"
summary: "The store was already fixed to PostgreSQL. What M1 owed was the schema, a forward-only digest-checked migrator, and a replacement for EDR-0005's no-driver-linked rule, which PostgreSQL for Marque's own state makes unachievable."
status: accepted
implementation: partial
implementation_note: "The schema, the migrator, the confinement test and the depguard block exist, in the same change as store.Open — the obligation this record set. CI runs the loader offline and the migrator against a real PostgreSQL behind a build tag. Nothing serves yet: no binary links the store."
date: 2026-08-20
authors:
  - "Theo Zourzouvillys <theo@sixfathoms.dev>"
tags: [architecture, ops, foundational]
supersedes: null
superseded_by: null
aliases: []
---

## TL;DR

The [implementation plan](../content/overview/implementation-plan.md) lists *"The control-plane
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
[EDR-0013](./0013-async-work-rides-the-wal.md) states: *"This ties Marque to PostgreSQL for its own
state. That is an accepted constraint, not a regrettable one."*, and
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
2. **Enforced by a test that parses every file, with a `depguard` block beside it.** The test walks
   the repository and reads each `.go` file's imports with `go/parser`. `depguard` reports the same
   rule to whoever is editing, sooner and with a better message.

   **Three justifications have been given for that split. All three were capability claims about
   tooling, and all three were false.** The sequence is kept rather than tidied away, because this
   record is otherwise about exactly that failure:

   - *"`depguard` cannot report blank imports."* A reviewer who had read `depguard`'s source said
     blank and named imports are indistinguishable to it, and was overruled by a measurement. The
     measurement was confounded: `revive`'s `blank-imports` rule reports at the *same file and line*,
     and `golangci-lint`'s `--uniq-by-line` — on by default — discards the second issue at a line, so
     `depguard`'s diagnostic never printed. The control on a standard-library package tripped the
     same `revive` rule and so reproduced the artefact instead of isolating anything. **The reviewer
     was right and the measurement was wrong**, which is a worse failure than the one it was
     diagnosing. What unmasks `depguard` is the *justifying comment* this repository's driver import
     carries, which silences the `revive` rule that was covering it — not the parenthesised block, as
     an earlier draft of this paragraph said; a block without a comment still hides it.
   - *"`golangci-lint` cannot read a file behind a build tag."* It reads one under `--build-tags`, and
     under `run.build-tags` in the config with no flag at all. What is true is narrower and is about
     this repository's *invocation*: `make lint` passed no tags, so M1's integration tests were
     invisible to it. That was a setting, and `make lint` now runs a second time with the tag.
   - *"`gen/` is excluded, and is compiled into every binary."* `gen/` is skipped by an anchored
     `- ^gen/` in `.golangci.yml`'s `exclusions.paths` — a line one may delete — and it is in no
     **shipped** binary's dependency graph at all: `go list -deps` over the three commands names
     nothing under `marque/gen`. The test binaries link it, and so does `schemacheck`, which is a
     build-time `package main`.

   Each time, a defensible choice was made and then decorated with a capability claim nobody ran.
   **The claim was the defect, not the choice**, and inventing a fourth would be the same mistake with
   a different subject.

   So the honest reason, which claims nothing about capability: **both mechanisms' coverage is
   configuration, and neither's is a property of the tool.** The test's coverage is its walk and its
   skip rule; the linter's is its invocation flags, its path exclusions, its generated-file heuristic
   and `--uniq-by-line`. Either can be narrowed by a commit that does not look like it touches a
   security control — and the test's was, twice, in review: once by skipping directories by basename
   anywhere in the tree, and once by skipping `bin/` and `dist/`, which `go build` compiles from
   perfectly happily. A third time it did not descend a symlinked directory, and a Harbourmaster
   package behind one linked a driver into the binary with the test green.

   That is not a reason to prefer one mechanism. It is a reason to **probe both rather than assert
   either**. The walk now skips only what the Go toolchain itself never compiles — a directory whose
   name begins with `.` or `_`, and `testdata` — follows symlinks rather than skipping them, and
   standing tests plant a driver import in each place that has escaped a check so far.

3. **The Harbourmaster holds no target connection parameters and no target credential.** EDR-0005
   already decides the credential half; where a target's *connection parameters* live it does not
   say, and this record does not settle it either — a role names a target, and what resolves that
   name to a host is undecided. Flagged rather than assumed:
   [issue #36](https://github.com/sixfathoms/marque/issues/36).

**Say plainly what this is not.** The test reads **imports**, not capability. `database/sql`
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
  unapplied one produces a binary whose embedded and applied sets still agree — and **nothing here
  catches that**, because no record of it having existed survives. Contiguous numbering catches a
  gap, not a truncation. The honest boundary is that these rules protect *applied* history.
- **Numbered contiguously from 1**, with a gap refused. A gap means a migration was removed from the
  middle, which the digest check below would otherwise notice only once it had been applied
  somewhere.
- **What `Verify` checks is the applied HISTORY, not the schema.** Drop a table by hand and `Verify`
  still returns nil: it compares recorded migrations against embedded ones and never looks at
  `pg_catalog`. That is inherent to a digest migrator and it is worth saying, because "refuses on any
  mismatch" reads as though it inspects the database's shape. It does not.
- **Each migration records the SHA-256 of its raw bytes**, and the applied set must be an exact
  prefix of the embedded set with matching digests. So an edit, a reordering or a removal **within
  applied history** is a refusal rather than a silent difference between two deployments. A migration
  that has never been applied anywhere is outside this: nothing has recorded it, so nothing misses
  it.
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
| `requests` | `req_…` reference, tenant, statement, target, role, submitter as a bare string, the operator's **reason**, state, the caller's `idempotency_key`, `created_at` |
| `approvals` | tenant, request, **stage**, approver, at |
| `executions` | tenant, request, **nonce** (a report key, not EDR-0011's claim — see below), at, outcome, rows affected — **rows affected is M1's "result"**, and the plan's *"the result and the statement land in a table"* means exactly that and nothing richer |

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
[EDR-0012](./0012-the-logbook-is-append-only.md) illustrates — and that record calls its own list
*illustrative* rather than closed — while `aborted_not_applied` comes from EDR-0011.
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
   nonce column *for EDR-0011's purpose*. M1's `executions` does carry one, with a unique
   constraint, which is what makes a Pilot's retry of its report idempotent — a report key, not a
   claim. The claim-before-run protocol, the incarnation and the budget are the ledger, and where the
   Pilot keeps that is undecided and is
   [issue #34](https://github.com/sixfathoms/marque/issues/34), due before M5.

## Consequences

**Easier.**

- EDR-0013's mechanism stays available: the WAL is there when async work arrives, rather than needing
  a store migration first.
- EDR-0012's logbook is implementable in the same database with the permission model it requires — a
  withheld `UPDATE`/`DELETE` grant and non-ownership — so M6 has no second transactional store and no dual
  write, which is the thing [ZFN-24](https://zrz.io/zfn/24-one-transactional-store-per-write/)
  forbids and EDR-0013 was designed around.
- Both vocabularies arrive at migration one — the request states borrowed whole from EDR-0038, the
  execution outcomes fixed here — so neither is a later migration to widen a constrained column. The
  tenant column arrives with them, which avoids a different migration: adding a column, and then
  rebuilding every unique constraint and foreign key around it.

**Harder.**

- **Tenancy is not one column.** Every foreign key is composite and carries `tenant_id` — a
  constraint on every relationship in the schema from here on, not a one-off cost. EDR-0025's "one
  column and one discipline" is honest about the column and quiet about the discipline.
- **The control plane now needs a database of its own**, migrated before it will serve. M1 already
  required a PostgreSQL for the *target* — the plan's exit is an integration test against a real
  one — so the marginal cost is a database of its own and a migration step, not a first server. Not a
  role in the target's: the whole point of the boundary above is that the control plane never opens a
  connection to a target at all.
  `make test` stays offline because unit tests do not touch the store.
- **EDR-0005's guarantee is weaker than it was on paper**, and the paper version was never
  achievable. Import discipline is defeated **four** ways where absence was defeated by none: an edit
  to the permitted list; a `sql.Open` by driver name from a package importing no driver at all, since
  `database/sql` registration is process-wide; a transitive dependency, which a first-party check does
  not look at; and **a driver that is on neither list**, since both mechanisms name the drivers they
  know — a reviewer imported `go-mssqldb`, `go-ora`, `modernc.org/sqlite` and `clickhouse-go` and
  watched both stay silent. It buys "the capability arrives by a reviewed edit rather than by
  accident", and no more. Anyone touching the confinement test's permitted list is touching a security
  control.

  One defeat an earlier draft listed is **gone**: a file behind a build tag, which the parser does not
  care about. Another was never real — the claim that a lint rule cannot see a blank import, which a
  confounded measurement produced and this record retracts above. The fourth was found by a reviewer
  looking for what the list does not name, which is the question a denylist always owes.

  Two mechanisms failed on the import shape they existed to police, both after being specified and
  reviewed. That is the part worth remembering: neither failure was visible in review, and both were
  visible on the first attempt to run them.
- **A forward-only schema with no backup story is an irreversible deploy.** An explicit migrator
  means nobody applies one by accident, which is a smaller part of the answer than it sounds: once
  applied, a bad migration is repaired only by writing another, and a destructive one is not
  repairable at all. Backup, restore and roll-forward are genuinely missing, and they belong with the
  deployment story Phase 1 does not have — named here so the gap is chosen rather than discovered.

**New obligations.**

- **M1** landed the confinement test in the same change as `store.Open`, the first package that
  opens a connection — discharged 2026-08-20. A mechanism replaced and then not implemented is worse
  than the sentence it replaced.
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
- **2026-08-20**: Amended when M1 built it. The rule was specified as a `depguard` block, replaced with a `go list -deps` graph test, and replaced again with a test that parses every `.go` file. **Each replacement was justified by a claim about what a tool cannot do, and all three claims were false** — `depguard` does report blank imports (`revive` reports at the same line and `--uniq-by-line` hid the second issue; a reviewer said so from the source and was overruled), `golangci-lint` does read a file behind a build tag under `--build-tags`, and `gen/` is skipped by a deletable exclusion and is in no shipped binary anyway. The parser is kept for a reason that claims nothing: both mechanisms' coverage is configuration rather than capability, so both are probed instead of asserted. The allowlist is now per driver and per package rather than per directory tree; a second check refuses a Harbourmaster package — `cmd/harbourmaster` included, which it had omitted — importing a Pilot adapter; the walk skips only what the Go toolchain never compiles and follows symlinks, both after a reviewer defeated an earlier skip list. Defeats are four: an edit to the permitted list, a `sql.Open` by driver name, a transitive dependency, and a driver on neither list. Also clarified that `executions` carrying a nonce is a report key rather than the beginning of EDR-0011's ledger, and that `rows_affected` is absent exactly when the outcome is `indeterminate`. The decision is unchanged.
- **2026-08-20**: The migrator's refusal of statements PostgreSQL will not run inside a transaction, promised here and implemented nowhere, is implemented — at load time, so CI catches it rather than SQLSTATE 25001 part-way through a production migration. `Verify`'s scope is stated: it checks applied history, not schema shape.
