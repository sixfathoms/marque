---
title: "Implementation plan"
sidebar_position: 4
---

How Phase 1 gets built. [Scope](./scope.md) says what each phase contains; this says in what order,
with what proving each step actually works.

This page is **sequencing, not decisions**. Anything load-bearing that gets decided while building it
becomes a [decision record](/edrs/), and this page is edited to match. If the two ever disagree, the
records are right.

## What shapes this plan

Three constraints, chosen deliberately, and every structural choice below follows from them.

**Phase 1 ends at a local PostgreSQL, deployed nowhere.** The exit criterion is an integration test,
not a running system. That keeps identity federation, cloud deployment, relays and the console
entirely out of Phase 1, and it means the spine can be got right without also getting a deployment
right at the same time.

**One person, alongside other work.** So: strict sequencing rather than parallel tracks; steps sized
to finish in a single sitting, because a half-finished step picked up three weeks later costs more
than it saved; and the scaffolding that prevents rework goes first, since rework is what an
intermittent schedule punishes hardest.

**The grammar comes early.** Six of the eight milestones depend on it and it is the piece with the
most unknowns, so it is built second rather than discovered late
([EDR-0039](../../edrs/0039-the-grammar-is-parsed-by-postgresqls-own-parser.md)).

## Definition of done

Every milestone, without exception:

- `make build test lint` green, in CI, on a pull request.
- A [changelog](/changelog/) entry — a new file, per the house rule.
- Any load-bearing decision made along the way written as a record **before** the milestone closes,
  not afterwards from memory.
- Its exit criterion demonstrated by a test that has been **seen to fail** for the right reason. A
  guard nobody has watched fail is a guard nobody knows works.

### What "green in CI" covers

Four platforms are supported. They are not all checked the same way, and pretending otherwise would
mean either a claim CI does not meet or a matrix bought for no reason. Three tiers, each with the
reason it stops where it does:

| Tier | Platforms | What it proves |
|---|---|---|
| **Build and smoke** | linux/amd64, linux/arm64, darwin/amd64, darwin/arm64 | The release matrix builds on a native runner and the binary it produces runs. This is the tier that must not shrink: it is the only thing standing between a broken platform and a release. |
| **Test suite** | linux/amd64, darwin/arm64 | That the suite passes on these two platforms, and nothing about the other two. One runner per operating system is a **sample**. Each runner asserts which platform it actually is, so the row cannot quietly stop being true when a runner label moves. |
| **Integration** | linux/amd64 — *arrives with M1* | Containers. macOS is excluded by the platform: GitHub-hosted runners there have no Docker daemon and service containers are Linux-only. linux/arm64 has Docker and is excluded **by choice** — the suite exercises the driver and the schema, not the architecture. |

**The test tier is a cost decision, not a proof, and it is written that way on purpose.** Two
arguments for it look like proofs and are both false, so they are written down here rather than
re-made later. *"There is no assembly, no unsafe and no cgo"* is not true of the compiled program —
every binary reaches architecture-specific assembly in `runtime` and `syscall`, and the tests
additionally compile protobuf's `uintptr` arithmetic — and it would not imply
architecture-independence if it were, since Go permits FMA contraction on arm64 and not amd64.
*"The first-party tree has no concurrency and no floating-point arithmetic"* fails the same way: the
committed generated code uses `sync.Once`, and `make test` runs `-race`, which is itself built per
architecture. Two runners are a sample, chosen because a defect that shows on one architecture and
not another is *unlikely* here today rather than impossible. Unlikely is a cost judgement, and that
is the honest name for it.

**M2 is where the judgement expires**: `pg_query_go` puts a C parser in the dependency graph, and a
grammar that classifies a statement differently on one architecture is a soundness bug the
build-and-smoke tier cannot see. That obligation is written into M2's own exit criterion rather than
left here, because whoever implements M2 will read M2.

## The milestones

| | Milestone | Proves |
|---|---|---|
| **M0** | Scaffolding | The toolchain, CI and the release skeleton work on an empty service |
| **M1** | Walking skeleton | The six steps connect end to end — with nothing secured yet |
| **M2** | The grammar | A statement can be classified and its scope extracted, provably |
| **M3** | Marques | A grant is a signed artefact that verifies offline and cannot be stripped |
| **M4** | Identity | Every principal is authenticated and every token is sender-constrained |
| **M5** | The fence | A statement outside a delegated scope aborts, in every way it could escape |
| **M6** | The logbook | The record is append-only against Marque's own database role |
| **M7** | Phase 1 close | All of it, together, as one test and one written-down getting-started path |

### M0 — Scaffolding

The rework-preventing milestone. Everything here is cheap now and expensive to retrofit.

1. Go module; `cmd/marque`, `cmd/harbourmaster`, `cmd/pilot` as three binaries that print their
   version and exit. Four components with sharply different trust means separate binaries, not one
   with a role flag ([EDR-0001](../../edrs/0001-marque-platform-architecture.md)).
2. `buf` toolchain; `proto/marque/v1/common.proto` with the `safe` and `idempotency` extensions;
   generation into a committed `gen/`. Wire up **`buf breaking` against `main`** and the build failure
   for an unannotated method now, while there is one method to annotate
   ([EDR-0020](../../edrs/0020-one-schema-generates-every-client.md)).
3. `Makefile` with the uniform verbs — `build test lint docs dev clean` — and `CGO_ENABLED=1`
   throughout.
4. CI: build, test, lint on pull requests; least-privilege permissions; actions pinned to SHAs.
5. `goreleaser` configured but **not releasing** — a native runner per platform, proven by a snapshot
   build on each. Discovering the release matrix is broken at version 0.1 is a bad day; discovering it
   now costs an afternoon. Note what a snapshot proves before the grammar exists: that each runner is
   available, that the release plumbing works there, and that the binary it produces runs. Nothing in
   the dependency graph is C until `pg_query_go` lands in M2, so this starts testing a **cgo** matrix
   then — which is the point at which it would be expensive to learn that one of these platforms
   cannot build it.
6. The conformance-vector harness: an empty vector file and the test that executes it.

**Exit:** the build-and-smoke and test tiers green — the integration tier has no job until M1, and a
criterion that cannot fail is not one; `buf breaking` rejects a deliberately-broken schema change.

### M1 — Walking skeleton — **done**

The thinnest path that touches every component, so integration is proved before anything is deep.

Its exit criterion is met: `internal/e2e` runs the six steps against a real PostgreSQL and asserts
the row changed, and the import rule has been seen to refuse a driver in each place that has ever
escaped it. What follows describes what was built; the
[changelog entry](/changelog/) has what it does not do.

Submit a statement → it is stored → approve it → run it against a local PostgreSQL → the result and
the statement land in a table. **No signing, no grammar, no identity, no fence.**

This milestone deliberately builds something insecure, so it is contained by construction:

- Every command that touches anything refuses to start without `MARQUE_INSECURE_SKELETON=1`, and
  prints a banner naming this milestone and the record-free state of what it is doing. `version` is
  the exception and is meant to be: inspecting a binary should not require acknowledging what it
  would do if you ran it.
- **M5 deletes the flag**, and a test asserts no such path exists in the binary. That test is written
  now, skipped with a reason, and un-skipped in M5.

Three things arrive with the store
([EDR-0042](../../edrs/0042-the-control-plane-keeps-its-own-store.md)): the tenant-partitioned schema
and its first migration; a migrator that is an explicit command, refuses a divergent history, and
records each migration's content digest in the transaction that applies it and runs as its own role
rather than the runtime one, which M6 needs and which costs nothing at migration one; and the rule
confining a target engine's driver to the two packages that may hold one — the Harbourmaster's store
and the Pilot's adapter — landing in the **same change** as the first package that opens a
connection. That rule asks `go list -deps` what each binary links, with a filesystem walk as a
cross-check and a `depguard` block as the edit-time report.

**Exit:** an integration test against a real PostgreSQL running the six steps and asserting
the row changed; and the import rule **seen to fail**, by adding a driver import to a Harbourmaster
package that is not the store and watching the confinement test refuse it. It asks `go list -deps`
what each binary links, with a walk that parses every first-party file as a cross-check — and not
because a linter is incapable, since several claims of that shape were made here and every one was false
(EDR-0042). A linter's reach is its flags and exclusions, a walk's is its own skip rule, and
`go list`'s is the patterns and tags it is given, so none of them may be asserted and all of them
are probed. The rule replaces a mechanism EDR-0005
lost, so a version of it that has never bitten is not a replacement. The first genuine end-to-end
signal, available in week one rather than month three.
It runs on linux/amd64 only, and behind a build tag so `make test` stays offline — see the tiers
above. `make test-integration` starts a disposable PostgreSQL in Docker on an ephemeral port,
creates the runtime role before the first migration because the schema grants unconditionally, and
removes the container on exit — or, with `MARQUE_TEST_DSN` already set, uses that server and starts
no container, because the suite needs a PostgreSQL rather than Docker specifically. No testcontainers dependency: a library that drives Docker is a
large dependency graph in the module the control plane ships from, and a Makefile target and a CI
job do the same job here. The DSN comes from the environment, and its absence is a **failure**
rather than a skip — a build-tagged suite that skips itself when unconfigured reports success
having run nothing.

### M2 — The grammar

The foundation the rest of the authority model stands on
([EDR-0039](../../edrs/0039-the-grammar-is-parsed-by-postgresqls-own-parser.md)).

1. `internal/grammar` over `pg_query_go`, behind an engine-shaped interface that names no PostgreSQL
   concept.
2. The subset walk: an **allowlist** over node types returning `in_subset` / `out_of_grammar` /
   `unsupported`. Start smaller than feels useful — single-relation `UPDATE` and `DELETE` with a
   conjunctive predicate over literals. A too-narrow subset produces a good error message; a
   too-broad one produces a soundness bug.
3. Scope extraction: relation, columns written, and the predicate the fence will be built from —
   spelled as [EDR-0041](../../edrs/0041-one-spelling-for-a-scope.md) has it, which is what the
   loader already enforces.
4. The conformance corpus, populated. Include the cases that should *fail* — a function call in a
   predicate, a CTE, a second relation appearing via `FROM`, a subquery — because a corpus of only
   happy paths tests nothing.
5. A differential harness: throw statements at both the parser and a real PostgreSQL, and assert they
   agree on what parses.
6. **A scheduled check that fails when `pg_query_go` publishes a new major.** `pg_query_go` uses
   semantic import versioning, so `/v6` and `/v7` are different module paths and
   [Dependabot will never propose that upgrade](https://github.com/sixfathoms/marque/blob/main/.github/dependabot.yml)
   — the configuration isolates its minor and patch bumps and can do nothing about the major. A
   subscription in somebody's inbox is not a mechanism this repository can check, so the watch is a
   job that compares the module path pinned in `go.mod` against the latest released major and fails
   when they diverge. It goes in `CODEOWNERS` like anything else with an owner. When a major does
   arrive it is not a dependency bump: a newer grammar parses statements the previous one refused, so
   the corpus is re-run and any changed verdict bumps the subset version
   ([EDR-0039](../../edrs/0039-the-grammar-is-parsed-by-postgresqls-own-parser.md)).

**Exit:** the corpus passes **on all four supported platforms** — this is the milestone the test
tier widens for, because from here the parser is C and a grammar that classifies a statement
differently per architecture is a soundness bug the build-and-smoke tier cannot see; the subset
version is recorded on extracted scopes; widening the allowlist by one node type shows up as a
corpus diff; and step 6's release watch **has been seen to fail**, by pointing it at a major newer
than the one pinned.

### M3 — Marques

The heart of the security argument
([EDR-0004](../../edrs/0004-marques-are-signed-leases.md),
[EDR-0030](../../edrs/0030-a-marque-states-its-own-approval-requirement.md)).

1. The payload: statement digest, target, role, submitter, not-before, expiry, budget, and the
   per-stage `approvals` block.
2. JWS general JSON serialisation with two signature limbs, ES256, `alg` restricted to a configured
   set with no "whatever the header says" path.
3. Offline verification in the Pilot: signatures, then **recomputation** of the approval requirement
   from the anchored policy rather than trusting the payload's copy of it
   ([EDR-0036](../../edrs/0036-what-is-signed-must-be-what-was-seen.md)).
4. The principal roster, anchored outside the control plane
   ([EDR-0031](../../edrs/0031-approver-keys-are-anchored-outside-the-control-plane.md)).
5. The signed marque replaces M1's `approvals` table
   ([EDR-0042](../../edrs/0042-the-control-plane-keeps-its-own-store.md)), which is a row per
   approver rather than a signature — carrying `stage`, so it is not the flat shape EDR-0030 exists
   to refuse. M3 builds the marque; M7 wires it into the M1 path and deletes the stub.

**Exit:** the negative tests, each seen to fail first — a marque with only the control plane's limb
is rejected; stripping one of two signature entries is rejected; **a two-stage marque is not
satisfied by two signatures from the same stage**, which is the exact defect
[EDR-0030](../../edrs/0030-a-marque-states-its-own-approval-requirement.md) exists to prevent; an
`alg` outside the configured set is rejected.

### M4 — Identity

1. OIDC against a local test issuer; the bootstrap discovery document
   ([EDR-0002](../../edrs/0002-bootstrap-discovery-document.md)) so a client needs one URL and no
   other configuration.
2. DPoP proof-of-possession on every call
   ([EDR-0003](../../edrs/0003-federated-identity-and-sender-constrained-tokens.md)).
3. The CLI's key store, and the `es256` signing envelope for approvers
   ([EDR-0023](../../edrs/0023-approver-keys-enrolment-and-recovery.md)). The `webauthn` envelope
   waits for the console in Phase 2 — the envelope split exists precisely so it can.
4. Freshness on **producing an approver signature**, not on execution
   ([EDR-0035](../../edrs/0035-execution-freshness-is-a-property-of-the-approval.md)).
5. `tenant_id` comes from the authenticated principal, replacing M1's single configured development
   tenant ([EDR-0025](../../edrs/0025-tenants-are-partitioned-from-day-one.md),
   [EDR-0042](../../edrs/0042-the-control-plane-keeps-its-own-store.md)). It is **never** a request
   field, and the column has been there since the first migration precisely so this is a change of
   source rather than a change of schema.

**Exit:** a replayed token fails; a token presented without its proof fails; a token bound to a
different key fails.

### M5 — The fence

The milestone with the most ways to be subtly wrong, so it is mostly adversarial tests
([EDR-0007](../../edrs/0007-delegation-by-containment-proof.md),
[EDR-0033](../../edrs/0033-assert-the-whole-write-set-not-just-the-named-relation.md)).

1. Session setup: `REPEATABLE READ`, pinned `search_path`, statement and lock timeouts — plus
   `standard_conforming_strings` and `backslash_quote`, which are read by the lexer and so must be
   settled in an earlier round trip and **verified** with `current_setting()` rather than assumed
   from the `SET`. All three pins are re-verified before every step that follows code the Pilot did
   not compose, not once at `BEGIN`: a fence conjunct may call a function, a `BEFORE` trigger may,
   and so may a deferred constraint trigger fired by `SET CONSTRAINTS ALL IMMEDIATE` — which runs
   immediately before the write-set assertion, not at setup.
2. The pre-check and the post-assert are **TRUE-only**, neither of them `NOT (…)`; the row-count
   assertion is the separate numeric one that enforces `max_rows`. A fence is a list of conjuncts, so
   each is composed `(c1) AND (c2) AND …` and the whole conjunction is wrapped again before
   `IS NOT TRUE` ([EDR-0041](../../edrs/0041-one-spelling-for-a-scope.md)). Both halves fail open if
   skipped, and both look right without them.
3. The write-set assertion over everything the engine wrote, not only the named relation.
4. The execution nonce, claimed **before** the statement runs, with the budget consumed by the claim
   ([EDR-0011](../../edrs/0011-execution-is-idempotent-and-fenced.md)).
5. Delete `MARQUE_INSECURE_SKELETON` and un-skip the test asserting it is gone.

**Exit:** each escape route proven to abort — a row whose fence predicate is NULL; a row an `UPDATE`
moves *out* of scope; a cascade the fence never named; a deferred constraint trigger that would
otherwise fire after the write set was read; a two-conjunct fence whose first conjunct carries a
top-level `OR`, composed both correctly and as `c1 AND c2`, where only the second admits a row
outside the fence; the same fence composed as `(c1) AND (c2) IS NOT TRUE` rather than
`((c1) AND (c2)) IS NOT TRUE`, where a row failing `c1` alone goes uncounted; a conjunct that closes
its own parenthesis; a conjunct carrying a `$n` parameter reference, a comment token or a control
character; an empty fence array, an empty-string conjunct and a duplicate conjunct, each refused by
the Pilot rather than assumed away at authoring; a `BEFORE` trigger on the target that calls
`set_config` to move `search_path` out from under the checks that follow the statement; a deferred
constraint trigger that calls `set_config` between `SET CONSTRAINTS ALL IMMEDIATE` and the write-set
assertion; a fence conjunct that calls `set_config` during the pre-check and leaves the pins moved,
caught before (b); and a crash between claim and commit losing the attempt rather than the count.

One escape route is deliberately **not** on that list, because it does not abort: a conjunct whose
function restores `search_path` on exit, or which simply returns a spoiled answer, defeats the
pre-check without leaving anything for a later check to see. That is the gap pinned to
[issue #25](https://github.com/sixfathoms/marque/issues/25), and M5 records it rather than implying a
control it does not have.

### M6 — The logbook

1. The append-only schema and the hash chain
   ([EDR-0012](../../edrs/0012-the-logbook-is-append-only.md)).
2. The grant setup as a migration: Marque's role holds `INSERT` and `SELECT` and **does not own the
   table**, because an owner can grant itself anything.
3. Chain verification, and the tail.
4. The journal becomes authoritative. M1 made `requests.state` the truth because there was no journal
   to project from ([EDR-0042](../../edrs/0042-the-control-plane-keeps-its-own-store.md)); from here
   current state is a rebuildable projection, which is what
   [EDR-0012](../../edrs/0012-the-logbook-is-append-only.md) decided.

**Exit:** a test connecting *as Marque's own role* proves `UPDATE` and `DELETE` are denied, and that
it cannot grant them to itself. The immutability claim is worth exactly as much as that test.

### M7 — Phase 1 close

Wire M2–M6 into the path M1 stubbed, and delete the stubs. Write the getting-started guide by
following it on a clean machine, which is the only way that document is ever correct.

**Exit — and the Phase 1 exit criterion:** one integration test in which a statement is submitted,
classified by the real grammar, approved into a doubly-signed marque by an authenticated approver,
executed under a fence that aborts when it should, and appended to a chained logbook — against a
local PostgreSQL, in CI.

## What not to build yet

Named because they are the interesting parts, and interesting parts are what an intermittent schedule
drifts toward.

| Not yet | Until |
|---|---|
| `marque psql` and the loopback proxy | M7. They are the most fun and the least load-bearing, and neither can be right before the grammar is |
| The console | Phase 2. It needs the `webauthn` envelope, which needs a signing path that exists |
| The Leadsman | Phase 2. An analyser with nothing to analyse is a demo |
| Delegation and compiled sentences | Phase 3. M5 builds the fence; *granting* through it is a separate problem |
| Agents | Phase 3b, in that order, for the same reason |
| Relays, cross-cloud, deployment | Phase 3 and beyond. Phase 1 deploys nowhere on purpose — which is also why backup, restore and roll-forward for the control plane's store are absent, and why a forward-only schema is acceptable until they exist ([EDR-0042](../../edrs/0042-the-control-plane-keeps-its-own-store.md)) |
| Slack | Phase 2. It rides the WAL stream, so it is cheap whenever it lands |

## Decision debt

Decisions that will have to be made during Phase 1 and do not have a record yet. Each becomes one
before the milestone that needs it closes.

- ~~The control-plane storage schema and its migration tooling — M1.~~ Settled by
  [EDR-0042](../../edrs/0042-the-control-plane-keeps-its-own-store.md).
- **Where the Pilot keeps its execution ledger** — M5. It is the fence
  ([EDR-0011](../../edrs/0011-execution-is-idempotent-and-fenced.md)), it must be Pilot-local and
  survive a target transaction rolling back, and the target database is the wrong place for it:
  [issue #34](https://github.com/sixfathoms/marque/issues/34).
- **Where a target's connection parameters live** — M1's Pilot, which is the change that will
  otherwise settle it by accident: [issue #36](https://github.com/sixfathoms/marque/issues/36).
- **The Go module and package layout**, specifically where the engine-shaped boundary sits so a second
  engine is an implementation rather than a fork — M2.
- **The test-issuer and local-development story**, which is also the adopting team's first experience
  — M4.
- **The console's build stack** — Phase 2, constrained already to static, same-origin and CSP-strict
  ([EDR-0024](../../edrs/0024-the-console-is-for-deciding.md)).

## After Phase 1

Deliberately low resolution: planning it now would be fiction, and [Scope](./scope.md) already fixes
what each phase contains. The two sequencing points worth stating in advance:

- **Deployment comes before usability.** Phase 2 opens by getting a Harbourmaster and a Pilot running
  somewhere real with federated identity, because the deployment story is the thing Phase 1 deferred
  and everything else in Phase 2 is easier to judge against a system that exists.
- **The proxy and `marque psql` come before the console.** The scope doc's top risk is people routing
  around it; the surface that addresses that risk is the one that makes Marque disappear into the
  tools people already use.

## The first session

```sh
git switch -c m0-scaffolding
go mod init github.com/sixfathoms/marque
mkdir -p cmd/marque cmd/harbourmaster cmd/pilot proto/marque/v1 internal/grammar
```

Then M0 step 1: three binaries that print a version and exit, a `Makefile` with the six verbs, and a
CI workflow that runs them. It is a deliberately small first step, and it is the one that makes every
step after it cheaper.
