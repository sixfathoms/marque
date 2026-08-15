---
title: "Eight design records: the API, connections, a local proxy, keys, the console, tenancy and engines"
tags: [docs, cli, console, security, ops]
order: 2
---

The second design batch fills in the surfaces the first one deferred. Two of these reverse or
sharpen earlier positions, and both say so.

### Added

- **[One schema generates every client](/edrs/0020-one-schema-generates-every-client/)** — a single
  protobuf definition, Connect transport so the console calls the API directly from a browser with no
  proxy, and every method annotated `safe` / `keyed` / `unsafe`. Generated clients read those
  annotations, so an `unsafe` method is never auto-retried by a client whose author had not read
  [EDR-0011](/edrs/0011-execution-is-idempotent-and-fenced/).
- **[Connections, identity and read routing](/edrs/0021-connections-identity-and-read-routing/)** —
  pooled, dynamically-credentialled connections; RDS/Aurora and Cloud SQL IAM authentication; and
  **per-operator database identity**, where the auth token is minted for a database user derived from
  the human, so the target's own audit names them *independently of Marque's logbook*. Also: a
  failover-aware driver wrapper with **transparent retry disabled on writes** — a driver that
  silently replays a write after failover applies a statement outside the execution fence's
  accounting, so a failover must surface as `indeterminate` rather than as a quiet retry.
- **[The local proxy brokers every statement](/edrs/0022-local-proxy-brokers-every-statement/)** —
  `marque sql` and a loopback proxy emulating the PostgreSQL wire protocol, so psql and existing
  tools work unchanged. It **forwards no bytes**: every statement is parsed, scoped, fenced, executed
  through a Pilot and logged.
- **[Approver keys, enrolment and recovery](/edrs/0023-approver-keys-enrolment-and-recovery/)** —
  WebAuthn in the browser, platform key store in the CLI, and the rule that closes the obvious
  attack: **enrolling an additional approver key requires a second, already-enrolled approver**.
  Otherwise the shortest path to approving anything is stealing a session and enrolling your own key.
- **[The console is for deciding](/edrs/0024-the-console-is-for-deciding/)** — and has **no bulk
  approve**, no saved approvals and no risk badge. Bulk *refuse* is offered; the asymmetry is the
  point.
- **[Tenants are partitioned from day one](/edrs/0025-tenants-are-partitioned-from-day-one/)** — the
  tenant comes from the authenticated principal and never a request field, and each tenant gets its
  own logbook hash chain and its own control-plane signing key, so a cross-tenant bug fails signature
  verification instead of returning someone else's data.
- **[Be psql first, then be better than psql](/edrs/0027-be-psql-then-be-better-than-psql/)** —
  `marque psql` emulates psql's flags, meta-commands and output formats so it can be aliased in
  place. It forces a decision nobody had made: `\dt` is a catalog query, so **catalog introspection
  becomes a named statement class** — read-only, restricted to an allowlist of catalog relations
  (relations, not schemas, because an extension can add to a schema), run under the role's own
  privileges with no approval, and logged in aggregate.
- **[A second engine is a capability matrix](/edrs/0026-a-second-engine-is-a-capability-matrix/)** —
  what each engine can actually enforce is declared and published, and a control an engine cannot
  support is marked unavailable rather than silently weakened.

### Changed

- **"Not a SQL client" is no longer a non-goal.** It was reasoning about *exploration* wrongly
  applied to the *interface*. The interface and the control are separable: a statement arriving over
  a socket gets the same parse, scope decision, fence and logbook entry as one arriving over gRPC.
  The non-goal is now "not a pass-through tunnel", which is the thing that actually matters.
- **PostgreSQL is named as the first engine everywhere**, with others following behind a published
  capability matrix rather than a flag.

### The uncomfortable finding

MySQL does not port cleanly, and [EDR-0026](/edrs/0026-a-second-engine-is-a-capability-matrix/) says
so rather than discovering it later. It has **no `RETURNING`**, so the fence post-assert that catches
an `UPDATE` moving a row *out* of scope needs a locking pre-select of primary keys — which means a
row fence on MySQL requires the table to have one. Its statement-timeout variable applies to
read-only `SELECT`s only, so bounding a write needs a lock timeout plus an external watchdog. And its
DDL implicitly commits, so DDL cannot be rehearsed at all.

None of that is fatal. All of it changes what a MySQL target can be trusted to enforce, and an
operator granting a delegation on one is told which controls they actually have.
