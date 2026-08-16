---
id: 6
title: "Every statement names a role, and the role is the real limit"
summary: "A request must name a target role, and the database's own grants on that role bound what any marque can do. Marque's policy narrows what the role could do; it never widens it."
status: accepted
date: 2026-08-15
authors:
  - "Theo Zourzouvillys <theo@sixfathoms.dev>"
tags: [policy, security]
supersedes: null
superseded_by: null
aliases: []
---

## TL;DR

Every request names a **role**, and every marque carries it. The role is a real database identity
with real grants, and those grants are the outer bound on what any approval can authorise. Marque's
policy, scopes and fences only ever **narrow** that bound.

There is no ambient role, no default, and no superuser role available through Marque. A deployment
that routes an administrative account through Marque has a workflow, not a control.

The practical rule for operators: **if you would not be comfortable with the role existing without
Marque in front of it, the role is too broad.**

## Context

It is tempting to treat Marque's policy engine as the security boundary and connect everything as one
privileged account: policy decides what may run, so the connection may as well be able to do
anything. That inverts the trust. It makes correctness of the SQL parser, the scope checker and the
policy evaluator into the only thing standing between an approved statement and the whole database —
and SQL parsers are not a component anyone should want as their last line of defence.

[ZFN-2](https://zrz.io/zfn/2-engineering-priority-ordering/) applies: when the convenience of one
connection conflicts with containment, containment wins. The database has spent decades building a
privilege system that is enforced *after* every clever thing anyone can write in a statement. Marque
should be the second lock, not the only one.

The related failure is quieter: a service configured to authenticate as its intended identity, but
actually connecting as the instance's master user because a credential reference pointed somewhere
broader than anyone read. Nothing errors. The privilege is simply larger than the design says. This
is why the check below is on the actual session user rather than on configuration.

## Decision

**Roles are declared, not derived.** A deployment declares its roles ahead of time
([EDR-0005](./0005-control-plane-holds-no-credentials.md) covers how each obtains a credential).
Every request selects one; a request without a role is rejected at submission, not at execution.

**The database's grants are authoritative.** Marque never assumes a role can do something because
policy allows it. If policy permits an `UPDATE` the role lacks the grant for, the statement fails at
the target, which is the correct outcome.

**Roles are least-privilege by convention, and the convention is checked.** Marque periodically
introspects each declared role and records what it can actually reach. Two findings are reported:

- a role holding `SUPERUSER`, `rds_superuser`, `cloudsqlsuperuser`, ownership of the schema, or
  membership that transitively grants any of those;
- a role whose effective privileges have *widened* since the last introspection.

Both are surfaced in the console and in the operator playbook's review. Neither blocks execution —
Marque does not manage the target's grants and must not pretend to — but a deployment cannot be
unaware of them.

**Session identity is verified, not assumed.** On connect, the Pilot asserts that the session's
actual database user matches the role's declared `db_user`, and refuses the connection on mismatch.
This is the check that catches a credential reference resolving to a broader identity than intended.

**Roles carry a criticality.** `routine`, `sensitive`, `critical`. Criticality is an input to policy
(who may approve), to signing requirements (`require_key_backing`, `signing_surface`), and to
defaults (budget size, marque lifetime). It is **not** an input to execution-time freshness — that
clause was removed by
[EDR-0035](./0035-execution-freshness-is-a-property-of-the-approval.md), which also settles that
target and role criticality compose as `max(target, role)`. It is a property of the role rather than of the statement, because the
statement is what everyone is looking at and the role is what everyone forgets.

**Read-only roles are first-class.** A large fraction of production access is a `SELECT` someone is
nervous about. A read-only role with a `routine` criticality and a generous standing order
([EDR-0008](./0008-standing-orders.md)) should be the path of least resistance, or people will ask
for the writable role out of habit.

## Consequences

**Easier.**

- A bug in Marque's scope checker is contained by the role. That is the whole point, and it is worth
  the setup cost on its own.
- Reviewing "what can Marque do to this database" is reading a short list of roles and their grants,
  in the database's own vocabulary, using the database's own tools.
- Criticality gives one dial that tunes approval, freshness and budget defaults together, instead of
  three settings that drift apart.

**Harder.**

- **Somebody has to define the roles**, and defining good ones needs schema knowledge Marque does not
  have. The initial set will be too broad, and narrowing it is ongoing work rather than a task that
  completes.
- **Requests get rejected at the target** for missing grants, which reads to the submitter as a
  Marque failure. The error has to explain that the role lacks the privilege and name which one.
- Per-role connection pooling means more connections to the target than a single shared account
  would use.

**New obligations.**

- Role introspection runs on a schedule and its findings are reviewed; a widened role is a finding,
  not a fact of life.
- Adding a role is a reviewed change, in the same repository as the policy that references it
  ([EDR-0015](./0015-policy-is-reviewed-configuration.md)).

## References

- [ZFN-2](https://zrz.io/zfn/2-engineering-priority-ordering/) — security over convenience.
- [ZFN-15](https://zrz.io/zfn/15-partition-customer-data-by-tenant/) — the adjacent discipline for
  multi-tenant targets.
- [EDR-0005](./0005-control-plane-holds-no-credentials.md) — how a role obtains its credential.
- [EDR-0007](./0007-delegation-by-containment-proof.md) — how policy narrows a role further.

## Changelog

- **2026-08-15**: Accepted.
- **2026-08-16**: Amended after the second panel's synthesis: criticality no longer feeds execution-time freshness ([EDR-0035](./0035-execution-freshness-is-a-property-of-the-approval.md)), and composes as `max(target, role)`.
