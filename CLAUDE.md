# CLAUDE.md

Guidance for AI assistants working in this repository.

## What this is

**Marque** brokers statements run against production data stores. An operator submits a statement; it
is analysed and rehearsed; a human with authority signs a **marque** — a scoped, time-bounded,
revocable grant; the statement executes under a named role inside that window; everything is appended
to a hash-chained logbook.

**The repository is public.** It is a generic tool and must stay one. Do not write any organisation's
schema, instance identifiers, hostnames, internal service names or customer data into examples, tests
or fixtures. Worked examples use a neutral fictional schema (`accounts`, `settings`, `tier`).

**Status: design; scaffolding underway.** `docs/edrs/` remains the source of truth for the design.
Implementation has begun at M0 of `docs/content/overview/implementation-plan.md` — the toolchain, the
schema and the build. Nothing runs against a database yet.

## Layout

```
docs/edrs/       Decision records — the source of truth for the design
docs/content/    Documentation pages, one directory per sidebar category
docs/changelog/  One file per changelog entry
website/         Static site generator (build.mjs), templates, styles
proto/           The API description; one schema generates every client (EDR-0020)
gen/             Generated Go and Connect stubs; committed on purpose
cmd/             The three binaries — marque, harbourmaster, pilot
internal/        Implementation packages
testdata/        Normative conformance vectors, language-neutral
tools/           A separate module pinning buf and golangci-lint, so their
                 dependency graphs stay out of this one's
```

## Commands

```sh
cd website && pnpm install
cd website && pnpm run build     # the validator; run before pushing
cd website && pnpm run serve     # http://localhost:8080/marque/
```

The build **is** the test suite. It fails on: a record with a missing or over-240-character
`summary`; a record with a missing or unknown `implementation`, or a `partial` or `in-flight` one
with no `implementation_note`; a duplicate id or an id/filename mismatch; a `proposed` record past
its `proposed_until`; a changelog entry with an unknown tag, a malformed filename, or a `## `
heading; a doc page in a directory that is not a sidebar category; a changelog page missing its
`<!-- @entries -->` marker.

## Rules

1. **A decision that changes goes in a NEW record.** Never edit the decision text of an existing one
   — set `status: superseded` and `superseded_by` on the old, `supersedes` on the new. An amended
   record gets a dated Changelog line, not a rewritten Decision section. See
   `docs/edrs/README.md`.
2. **A changelog entry is a NEW FILE** at `docs/changelog/YYYY-MM-DD-slug.md`. Never edit
   `docs/content/changelog.md` to add one; that single-file pattern is what conflicted on every
   same-day change. Do not add an index or manifest of entries — the build globs them, and a registry
   would just move the conflicting line.
3. **The changelog tag vocabulary is closed**, defined once in `CHANGELOG_TAGS` in
   `website/build.mjs` and documented in `docs/changelog/README.md`. Adding a tag means editing both.
4. **The site is deliberately not indexable or archivable.** Marque is at design stage, so
   `website/templates/layout.html` carries `noindex, nofollow, noarchive, nosnippet, noimageindex`
   plus per-crawler and archiver variants, and the build **fails** if any emitted page lacks them. A
   `robots.txt` cannot do this job here: robots.txt is honoured at a domain ROOT and this is a
   project site at a subpath, so one in this repository would land at `/marque/robots.txt` and be
   ignored. Do not remove the tags or the guard until the project is deliberately made public.
5. **Render mermaid diagrams before committing.** Nothing validates them; a broken one builds fine and
   renders as an error box.
6. **Cite pull requests as links**, never a bare `#12`. Cite Field Notes as
   `[ZFN-N](https://zrz.io/zfn/N-slug/)`.
7. **British spelling in prose.**
8. **Be honest in Consequences.** A record with no "harder" section has not been thought through, and
   writing one that reads like marketing copy is worse than not writing it.
9. **Discharge every cross-record obligation in the same commit.** The corpus's dominant failure mode
   is a new record stating an obligation on an older one — "EDR-0010's report gains `write_set`",
   "this joins the list in EDR-0004", "EDR-0026's capability table gains…" — and the obligation never
   being carried out. Three of those shipped and were caught only by a third adversarial read. If you
   write that another record gains a field, a row or a rule, **edit that record in the same change**
   and give it a dated changelog line. Grep your own diff for `gains`, `joins the list`, `is added
   to`, `now carries` before pushing.

## The design's invariants

Load-bearing, and easy to undo by accident. A change that weakens one needs a superseding record, not
a commit message.

- **A marque carries two signatures** — an approver limb and the control plane's. Neither alone is
  valid. Reducing this to one signature collapses the entire security argument, because a compromised
  control plane could then manufacture authority (EDR-0004). On a **fast path** (standing order,
  delegation match, Surveyor `conforms`) no human is present at mint time, so the approver limb is
  satisfied by the **human-signed artefact** that authorised the shape — it travels with the marque
  and the Pilot verifies it offline (EDR-0029). The accurate form of the compromise boundary is
  therefore "cannot cause a statement to execute **whose shape no human signed**", not the
  unqualified version; say it the accurate way.
- **A marque states its own approval requirement inside the signed payload**, as **per-stage**
  thresholds (`approvals.stages[]`, `chain`, `roster_epoch`) mirroring the escalation chain. JWS
  signature entries are independent, so "at least one approver signature" would let a two-approver
  marque be stripped to one and still verify; and a *flat* `required`/`eligible` collapses a
  conjunction of stages into a disjunction, so a chain requiring Sam then data-oncall was satisfiable
  by two of data-oncall (EDR-0030). The Pilot additionally **recomputes** the requirement from the
  anchored policy artefact and refuses on mismatch — the payload's copy is authored by the adversary
  it defends against (EDR-0036).
- **The control plane holds no target credential**, and has no target database driver linked in. Any
  feature that needs a connection is a Pilot API, not a Harbourmaster one (EDR-0005).
- **A delegated row scope is a fence that aborts, never a rewrite.** Conjoining the predicate into the
  operator's `WHERE` is sound and silently narrows the statement, which produces a partially-applied
  change nobody reviewed. The pre-check, the post-assert and the row-count assert are three separate
  checks and all are needed — the post-assert catches an `UPDATE` moving a row *out* of scope, and a
  fourth, the write-set assertion, catches everything the engine writes on the statement's behalf
  (EDR-0007, EDR-0033). Both comparisons are **TRUE-only** and the transaction is REPEATABLE READ.
- **The analyser has no authority, and no setting grants it any.** No risk score, no recommendation,
  no auto-approval — those are the shapes that get automated against. Pre-authorised automation is
  standing orders and delegation, where a human granted it in advance (EDR-0009).
- **A rehearsal has no code path that can commit.** Not a flag that defaults to rollback — no commit
  in the package (EDR-0010).
- **The execution nonce is claimed before the statement runs**, and the budget is consumed by the
  claim rather than by success. A crash must lose the attempt, not the count (EDR-0011).
- **Marque's own database role holds `INSERT` and `SELECT` on the logbook and nothing else, and does
  not OWN the table.** An owner can grant itself anything, which would make the withheld grant
  decorative. The immutability is a withheld grant plus non-ownership, not a convention (EDR-0012).
- **Standing-order parameters bind values and never contribute syntax** — not a table name, not a
  column, not a fragment of a predicate (EDR-0008).
- **The relay is a dumb pipe.** It never parses the Pilot API. Every feature request aimed at it
  ("could it cache…", "could it retry…") should be refused on principle (EDR-0014).
- **A model can never create authority a human did not sign.** This is the invariant that spans
  EDR-0009, EDR-0016 and EDR-0017 and it is the one to defend hardest. Concretely: a written
  delegation is compiled and **the human signs the compilation, not the sentence** (EDR-0016); the
  Surveyor has exactly two outcomes, `conforms` and `refer`, runs only *inside* a compiled bound a
  human signed, requires a unanimous **separately-framed** panel (not "independent" — three calls to
  one model are correlated), and resolves every error, timeout and ambiguity to `refer` (EDR-0017). Adding a third outcome, dropping unanimity, defaulting to `conforms`, or
  letting a Tier-B delegation exist without a compiled outer bound each changes the risk *category*.
  Tier B ships off and its sampled audit is what makes it correctable — an audit queue nobody reads
  silently removes the mitigation.
- **An agent's effective scope is the intersection of three grants**, including one the agent
  declares for its own task, and the declaration attenuates only and cannot be widened mid-task
  (EDR-0018). An agent can never approve: approval needs a fresh interactive authentication no
  workload principal can satisfy, and that mechanical impossibility is asserted by a test.
- **Escalation stage 1 for an agent is always its own principal**, every stage is a human, and **a
  timeout never satisfies a stage** (EDR-0019).
- **Every method declares its behaviour** — `safe`, *or* one of `natural` / `keyed` / `unsafe`;
  either alone is a declaration — in a single `MethodBehaviour` extension, and a method declaring
  neither fails the build, because the default must be a decision (EDR-0020). **The declaration may only ever strengthen**: `safe` to not-safe,
  `natural` to `keyed` or `unsafe`, `keyed` to `unsafe`, or moving the key's field, each breaks a
  client that already compiled the old policy, so each needs a new method rather than an edit.
  `buf breaking` does *not* see custom options, so a separate check enforces this (EDR-0040). A
  `safe` method also carries the standard `idempotency_level = NO_SIDE_EFFECTS`, because that is what
  a generated Connect client actually reads — beyond `safe`, honouring the declaration is an
  interceptor that arrives with the first real client, and saying so is better than implying the loop
  is closed.
- **Transparent driver retry is OFF for writes.** A wrapper that replays a write after failover
  applies a statement outside the nonce's accounting; failover surfaces as `indeterminate` (EDR-0021).
- **The proxy forwards no bytes.** It terminates the wire protocol and constructs its own requests.
  "Just pass reads through" turns it into the tunnel the design refuses (EDR-0022).
- **Enrolling an approver key needs k enrolled approvers** (k is defined in EDR-0031 and nowhere else),
  and the roster the Pilot trusts is
  **co-signed and anchored outside the control plane** — a Pilot must never learn who may approve from
  the Harbourmaster (EDR-0023, EDR-0031).
- **The console has no bulk approve**, and every mutating action is a signed act (EDR-0024).
- **The tenant comes from the authenticated principal, never a request field**, and each tenant has
  its own chain and signing key (EDR-0025).
- **An engine declares what it can enforce**; a control it cannot support is marked unavailable, never
  silently weakened (EDR-0026).
- **Catalog introspection is bounded by the reviewed allowlist, not by the role** — most of
  `pg_catalog` is world-readable, so the role does no work there (EDR-0027).
- **A pipeline provider may narrow or veto, never widen, permit or replace a check** — but be precise
  about what that buys: the mechanisms enforce **containment within the submitter's authority, not
  narrowing**. A transform can rewrite `id = 42` to `id = 43` inside the scope and pass everything, so
  a `transform` provider is trusted for statement content. The digest is taken after transformation,
  `parse` re-runs on the result, both `req_submitted` and `req` travel, and transforms do not run on a
  standing-order fast path (EDR-0028).
- **The fence is TRUE-only, runs at REPEATABLE READ, pins `search_path`, and forces
  `SET CONSTRAINTS ALL IMMEDIATE` before the write-set check.** `NOT (fence)` lets a NULL-fenced row
  through every check; READ COMMITTED lets the pre-check and the statement see different snapshots; an
  unqualified fence can be redefined via `search_path`; and a deferred constraint trigger otherwise
  fires at COMMIT, after the write set was read (EDR-0007, EDR-0033).
- **The write-set assertion bounds everything the engine writes on the statement's behalf.**
  `max_rows` bounds the named relation only; a cascade is invisible to `RETURNING` (EDR-0033).
- **Every Pilot method verifies a submitter signature.** The control plane relays, it does not
  authorise (EDR-0034).

- **Break-glass is a pre-granted dormant scope, never a mode the software can enter.** It mints an
  ordinary fast-path marque against a signed grant (EDR-0037), so there is **no code path that skips a
  check** — what changed is that a human signed the shape earlier. It requires an explicit act, a
  justification bound into the signed payload, and a user-verification assertion; no configuration
  suppresses its notification; and an agent can never use it.
- **Urgency reroutes, it does not widen.** It changes who is asked and how loudly. It may reduce a
  chain to one stage only where a target explicitly enables `urgency_may_collapse_stages`, default
  off — otherwise `--urgent` becomes the universal bypass within a month (EDR-0037).
- **A request reference is an identifier, not a capability.** People paste them into shared channels;
  resolving one must still require entitlement, and must 404 rather than 403 so the reference does not
  confirm its own existence (EDR-0038).
- **One parser, and it is PostgreSQL's own.** `libpg_query` in every component that parses a
  statement, never a re-implementation, and never a second grammar for the client (EDR-0039).
  The consequence that will be undone by accident: **a `pg_query_go` upgrade is a reviewed change on
  the order of a schema migration, not a dependency bump.** A newer grammar parses statements the
  previous one refused, which silently widens the checkable subset — and therefore widens what an
  already-signed delegation permits, which EDR-0007 forbids. An upgrade re-runs the conformance
  corpus, and any changed verdict bumps the subset version so old delegations stay pinned to the
  subset they were signed against. Dependabot must not auto-merge it. Relatedly, a target whose
  `server_version_num` is outside the declared supported range gets **no fast path** — its statements
  take the human route rather than being proved against a grammar that may not match its server.

## Naming

Components are archetypes in one register — Harbourmaster (control), Pilot (data plane), Leadsman
(advisory), Surveyor (conformance), Tender (relay) — per [ZFN-43](https://zrz.io/zfn/43-name-systems-as-archetypes/). The
cast list at `docs/content/concepts/cast.md` is the authority and is part of the architecture. Do
not add an archetype for plumbing; plumbing gets plain descriptive names.

If a proposed feature feels wrong for a character, treat that as an architectural signal: either it
belongs elsewhere, or the component is becoming two components.
