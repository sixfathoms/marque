---
id: 21
title: "Connect as the operator where the database can, and never let a driver retry a write"
summary: "Pilots use pooled, dynamically-credentialled connections. Where the engine allows, a session authenticates as the individual operator via IAM so the database's own audit names them; reads may route to replicas."
status: accepted
implementation: partial
implementation_note: "One rule of this record is built and tested: transparent retry is OFF for writes, so a commit whose answer never arrives is reported as indeterminate rather than replayed — internal/pilot, with the classification that tells a refused commit from a lost one. Nothing else is: no connection identity, no read routing, no pooling policy, and no session settings pinned on a target."
date: 2026-08-15
authors:
  - "Theo Zourzouvillys <theo@sixfathoms.dev>"
tags: [execution, security, ops]
supersedes: null
superseded_by: null
aliases: []
---

## TL;DR

Four decisions about how a Pilot actually reaches a database.

1. **Credentials are minted per connection, never stored.** On AWS, an RDS or Aurora IAM auth token
   generated from the Pilot's task role; on GCP, Cloud SQL IAM authentication; elsewhere, a
   dereferenced secret as a documented carve-out
   ([EDR-0005](./0005-control-plane-holds-no-credentials.md)).
2. **Where the engine can, the session authenticates as the individual operator.** A role may declare
   `identity: per_operator`, in which case the database user is derived from the operator's federated
   subject and the token is minted for *that* user. `session_user` in the database is then the human,
   so **the database's own audit trail names them independently of Marque's logbook** — a second
   record that survives a Marque compromise.
3. **Transparent driver retry is disabled on every execution transaction.** A failover-aware wrapper
   is used for connection management and topology awareness, but a driver that silently replays a
   write after failover breaks the execution fence
   ([EDR-0011](./0011-execution-is-idempotent-and-fenced.md)) by applying a statement Marque did not
   count. Failover mid-execution must surface as `indeterminate`, not as a quiet retry.
4. **Reads may go to a replica, under a freshness bound.** Read-only statements route to a reader
   endpoint when the request tolerates it; anything in a task that has already written goes to the
   writer, gated on the replica having applied that write's position
   ([ZFN-25](https://zrz.io/zfn/25-read-your-writes-version-token/)).

Every session sets `application_name` to the marque identifier, so a DBA looking at a running query
can see what authorised it without asking anybody.

## Context

[EDR-0005](./0005-control-plane-holds-no-credentials.md) settled *where* credentials live and
[EDR-0006](./0006-every-statement-names-a-role.md) settled that a role's grants are the outer bound.
Neither says how a connection is actually obtained, pooled, or routed — and the answers turn out to
have security consequences rather than merely operational ones.

The strongest of those is identity. A role like `settings_writer` is a shared database identity: the
database sees `settings_writer` doing things, and *which human* is knowable only from Marque's
logbook. That is a single point of truth for attribution, held by the system whose compromise is
exactly the scenario in which you would most want a second opinion. Cloud IAM database
authentication makes a better answer available: mint the token for a database user that corresponds
to the person, and the target's own logs — `pg_stat_activity`, `pgaudit`, the engine's audit
extension — attribute independently. Two records that must agree is meaningfully stronger than one.

The retry point is the non-obvious one. Modern failover-aware driver wrappers exist precisely to hide
a failover from the application, and hiding it is usually correct. Here it is dangerous: Marque's
entire double-apply defence assumes that **Marque decides when something is retried**
([EDR-0011](./0011-execution-is-idempotent-and-fenced.md)). A driver that transparently replays a
write on a new writer applies a statement outside that accounting, and the operator's budget of one
has silently become two. The convenience feature and the safety property are in direct conflict, and
the safety property wins ([ZFN-2](https://zrz.io/zfn/2-engineering-priority-ordering/)).

## Decision

### Credentials and pooling

Connections are pooled per `(target, role, identity)`. A pool is bounded and the sum of a Pilot's
pools is bounded below the target's connection limit — a tool that exists to protect production must
not be able to exhaust its connections
([ZFN-18](https://zrz.io/zfn/18-enforce-quotas-at-ingress/)).

Auth tokens are short-lived, so a pool refreshes credentials on its own schedule and **re-mints on
an authentication failure** rather than failing the request
([ZFN-35](https://zrz.io/zfn/35-dereference-secrets-not-store-in-config/)). On AWS the connection is
managed by the failover-aware advanced driver wrapper, configured for IAM authentication and topology
awareness, with the retry behaviour below.

### Operator identity at the database

A role declares one of:

| `identity` | Database user | Attribution | Cost |
|---|---|---|---|
| `shared` | the role's own user | Marque's logbook only | one pool per role; the default, and the only option where IAM auth is unavailable |
| `per_operator` | derived from the operator's federated subject | **the database independently names the human** | a pool per operator; user provisioning per operator |
| `pooled_with_role` | the role's user, then `SET ROLE` to an operator role | `current_user` names the human; `session_user` does not | one pool; **weaker — see below** |

`per_operator` is preferred wherever the engine and provisioning allow, and is the recommended
default for `sensitive` and `critical` roles.

**`pooled_with_role` is a documented compromise, not an equivalent.** `SET ROLE` is reversible by the
session, so it narrows privilege and attributes *only* as long as nothing in the session resets it.
The checkable-statement grammar forbids `SET ROLE` and unrecognised function calls
([EDR-0007](./0007-delegation-by-containment-proof.md)), which is what makes it defensible at all —
but it is a defence in Marque's parser rather than in the database, and that is the wrong place for
one ([EDR-0006](./0006-every-statement-names-a-role.md) exists precisely to avoid relying on it).
Use it for high-volume standing-order traffic where per-operator pools are impractical, and record
the choice. Note also that a **human-approved out-of-grammar statement** — one that reached a person
precisely because the parser could not bound it — may reset the role, degrading the session to the
`shared` baseline for its duration. Attribution, not containment, is what is lost; the role's grants
still bound it.

**A `per_operator` user holds no privilege beyond the role it maps to** — ideally by being a member of
that role and nothing else. Otherwise per-operator identity quietly becomes per-operator *authority*,
and the role stops being the outer bound ([EDR-0006](./0006-every-statement-names-a-role.md), whose
role introspection extends to these derived users).

**Provisioning is not Marque's job.** Marque never issues `GRANT` or creates database users. A
`per_operator` role names the mapping from federated subject to database user; if the user does not
exist or lacks `rds-db:connect`, the connection fails with that reason. Creating and revoking those
users belongs to whatever provisions database access — and an operator removed from the identity
provider should also lose the database user, which is a real integration someone has to build.

**Verify positively.** On connect the Pilot asserts the session's actual `session_user` (and
`current_user` under `pooled_with_role`) matches what the role declares, and refuses on mismatch. A
lazily-initialised pool otherwise hides broken database authentication indefinitely: no connection
attempt, no error, quiet logs, and the first symptom arrives during an incident.

### Retry, failover and the fence

- **Transparent driver retry is off for execution.** The wrapper may reconnect and refresh topology;
  it may not replay a statement or a transaction. A failover during an execution transaction
  produces an aborted transaction and an `indeterminate` outcome recorded against the nonce
  ([EDR-0011](./0011-execution-is-idempotent-and-fenced.md)) — the honest answer, which a human then
  resolves.
- **Rehearsals may be retried automatically**, because a rehearsal cannot commit
  ([EDR-0010](./0010-rehearse-before-you-sign.md)).
- **Read-only statements may be retried automatically**, per the `safe` annotation
  ([EDR-0020](./0020-one-schema-generates-every-client.md)).

### Read routing

A statement the parser establishes as read-only may be served by a reader endpoint. Two constraints:

- **Read-your-writes, carried by a client-supplied version token.** After a write, the Pilot returns
  the writer's commit position on the response; the client presents it on subsequent reads, which then
  go to the writer or wait for a replica that has applied it. The token lives in the request/response
  shape ([EDR-0020](./0020-one-schema-generates-every-client.md)), so it works identically for an
  agent task, a `marque psql` session and a one-shot execution — none of which share a server-side
  session. Silently serving a stale read after a write an operator just made is how somebody concludes
  their change did not work and applies it again
  ([ZFN-25](https://zrz.io/zfn/25-read-your-writes-version-token/)).
- **Staleness is reported, never hidden.** A result served from a replica says so, with the observed
  lag. An operator checking whether their fix worked must be able to tell "not yet replicated" from
  "did not happen".

**Rehearsal of a write always runs on the writer** — a replica is read-only, so a write rehearsal
there fails rather than measuring anything. Rehearsal of a read may use a replica.

### Observability at the target

Every session sets `application_name` to `marque:<mrq_id>` (or `marque:rehearse:<req_digest>`), so
the marque is visible in `pg_stat_activity`, slow-query logs and the engine's audit output. This is
close to free and is the difference between a DBA seeing an unexplained `UPDATE` and seeing one they
can trace to an approval in one query.

## Consequences

**Easier.**

- Attribution stops depending solely on Marque. Under `per_operator`, the database's own audit is a
  witness **independent of the control plane** — the thing you want when the question is whether the
  Harbourmaster was tampered with. It is **not** independent of the Pilot: the Pilot chooses which
  operator's token to mint, so a compromised Pilot can attribute its own activity to any operator it
  can mint for. The cross-check is the logbook, and a reconciliation job that diffs the target's audit
  against it.
- Least privilege can be genuinely per-person where it matters, enforced by the database rather than
  by policy.
- No stored database passwords in the common deployment, and rotation is invisible.
- A DBA can trace any running statement back to its approval without leaving their usual tools.

**Harder.**

- **`per_operator` multiplies pools.** Fifty operators against one target is fifty pools, each with
  idle connections, against a connection limit that is often low on managed engines. This is the
  reason `pooled_with_role` exists, and the reason it will be tempting.
- **Provisioning database users per operator is real integration work** that Marque deliberately does
  not do, and a half-built version of it produces confusing "role does not exist" failures for exactly
  the new joiner who is least able to diagnose them.
- **Disabling transparent retry makes failovers visible to operators** as failed executions that need
  a new marque. That is correct and it will be perceived as flakiness. The error text has to explain
  that Marque refuses to retry a write it cannot prove did not apply.
- Read routing adds a decision to every read and a way to be subtly wrong. Reporting staleness rather
  than hiding it is the mitigation, and it means operators see a detail they did not ask for.
- `application_name` puts a marque identifier in the target's logs, which is a small disclosure to
  whoever can read those logs. It names no statement and no data.

**New obligations.**

- Pool counts, idle connections and the sum against the target's limit are monitored. A Pilot
  approaching the limit must shed rather than exhaust ([ZFN-13](https://zrz.io/zfn/13-load-shedding-and-flow-control/)).
- The `session_user` assertion is tested against a deliberately misconfigured role, since the failure
  it guards is silent by nature.
- Any use of `pooled_with_role` is recorded with an owner and reviewed, like the credential carve-out
  in [EDR-0005](./0005-control-plane-holds-no-credentials.md).

## References

- [ZFN-2](https://zrz.io/zfn/2-engineering-priority-ordering/) — correctness beats the driver's
  convenience feature.
- [ZFN-25](https://zrz.io/zfn/25-read-your-writes-version-token/) — track the position a client has
  seen.
- [ZFN-35](https://zrz.io/zfn/35-dereference-secrets-not-store-in-config/) — refresh and re-fetch on
  auth failure.
- [ZFN-9](https://zrz.io/zfn/9-no-long-lived-cloud-keys/) — federated identity rather than stored
  credentials.
- [EDR-0011](./0011-execution-is-idempotent-and-fenced.md) — the fence a transparent retry would
  break.

## Changelog

- **2026-08-15**: Accepted.
- **2026-08-16**: Amended after the expert panel's should-fix pass: bounded the "independent witness" claim to independence *of the control plane*, required a per-operator user to hold no privilege beyond its role, and named the read-your-writes carrier as a client-supplied version token.
