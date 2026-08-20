---
id: 25
title: "Partition every tenant from day one, including its logbook chain and its signing key"
summary: "Tenancy is in the model from the first migration: the tenant comes from the authenticated principal, never a request field, and each tenant gets its own hash chain and its own control-plane signing key."
status: accepted
implementation: partial
implementation_note: "The schema half exists from migration one: a tenants table, tenant_id NOT NULL on every domain table, composite foreign keys so a row cannot reference another tenant's parent, and tenant_id leading both indexes (EDR-0042). The source does not: M1 has one configured development tenant and no identity, so nothing derives tenant_id from an authenticated principal and no query is scoped by one. Nothing structurally stops a query that forgets it — issue #43."
date: 2026-08-15
authors:
  - "Theo Zourzouvillys <theo@sixfathoms.dev>"
tags: [architecture, security]
supersedes: null
superseded_by: null
aliases: []
---

## TL;DR

Marque is tenant-partitioned from the first migration, even though the first deployments will have
exactly one tenant. Retrofitting isolation onto a shared schema is brutal, and the cost of doing it
up front is one column and one discipline
([ZFN-15](https://zrz.io/zfn/15-partition-customer-data-by-tenant/)).

Four rules, three of which are specific to this system:

1. **The tenant comes from the authenticated principal, never from a request field, path segment or
   header.** There is no code path in which a caller states which tenant they are.
2. **Each tenant has its own logbook hash chain.** One global chain would let any tenant's entries
   reveal the existence and rate of every other tenant's activity, and would make a tenant's record
   inseparable for export or deletion.
3. **Each tenant has its own control-plane signing key.** A marque for tenant A is signed by A's
   `authority` key, so a bug that confuses tenants cannot produce a *valid* marque — the signature
   fails rather than the check being skipped. The enforcement rule that makes this true — a Pilot
   trusts only its own tenant's keys and roster, and the payload carries a `tenant` claim — is in
   [EDR-0032](./0032-a-marque-binds-its-executor-tenant-and-pilot.md).
4. **A Pilot serves exactly one tenant.** It holds credentials
   ([EDR-0005](./0005-control-plane-holds-no-credentials.md)); a shared Pilot would be a place where
   two tenants' database credentials sit in one process.

## Context

The first deployment of Marque will be single-tenant, and the tempting decision is to leave tenancy
out until someone asks. That decision is very hard to reverse: every query, every index, every foreign
key and every cached lookup written without a tenant dimension has to be revisited, at a point when
there is real data and real traffic. [ZFN-15](https://zrz.io/zfn/15-partition-customer-data-by-tenant/)
argues for the model up front and one physical database to start with, and that applies cleanly here.

Two things are less standard and are what this record is really for.

**The chain.** [EDR-0012](./0012-the-logbook-is-append-only.md) hash-chains the logbook so tampering
is detectable. A single chain across tenants means each entry's hash depends on entries belonging to
other tenants — so publishing a chain head, exporting a tenant's record for an audit, or deleting a
departing tenant's data all become impossible without breaking verification for everyone else. It
also leaks: sequence numbers advance whether or not *you* did anything, which is a side channel for
how busy your neighbours are.

**The key.** Tenant isolation is usually enforced by a `WHERE tenant_id = ?` that someone can forget.
Giving each tenant its own signing key converts a class of isolation bug from "silently authorised"
into "signature does not verify" — the failure becomes loud and safe rather than quiet and
permissive. This is the same reasoning as [EDR-0006](./0006-every-statement-names-a-role.md): put the
last line of defence somewhere other than the code that might have the bug.

## Decision

**A tenant** is the organisation that owns targets, roles, policy, delegations, standing orders,
agents and a logbook. Principals belong to exactly one tenant. A person who works with two tenants
has two principals; there is no cross-tenant identity.

**Derivation.** Tenant is resolved from the authenticated principal at the edge and carried in a
request context that handlers cannot override. Repository methods take it as an explicit argument
rather than reading it from ambient state, so a query that forgot it does not compile.

**Never join across tenants.** Not for analytics, not for the analyser's "similar past requests"
lookup, not for a support tool. Cross-tenant aggregate reporting, if it ever exists, reads from a
separately-derived store — it does not get a query that spans the primary tables.

**Storage.** One physical database initially, with `tenant_id` leading every index that matters and
present in every unique constraint. The model stays shardable: no query assumes tenants share a
database, and a tenant→location directory exists from the start even while it returns one answer for
everybody.

**Deployment-operator principals are a separate class.** The people running Marque itself need
cross-tenant visibility for health, capacity and incident work. They are a distinct principal type
whose actions are logged in a deployment-level journal, and — importantly — **they cannot approve
anything for a tenant**. Operating the system is not authority within it. This is the boundary that
is easiest to blur and worst to blur.

**Quotas are per tenant** ([ZFN-18](https://zrz.io/zfn/18-enforce-quotas-at-ingress/)): submissions,
rehearsals, executions, model calls and relay connections. One tenant's runaway agent must not consume
another's capacity.

**Bootstrap and discovery.** The bootstrap document
([EDR-0002](./0002-bootstrap-discovery-document.md)) describes a *deployment*; a client resolves its
tenant from the identity it authenticates with. A deployment serving several tenants publishes one
document, and it names no tenant.

**Export and erasure operate on one tenant.** Because the chain is per tenant, a tenant's record is a
self-contained, independently verifiable artefact. Erasure remains constrained by the immutability in
[EDR-0012](./0012-the-logbook-is-append-only.md) — this record makes a tenant's data *separable*, not
individually deletable.

## Consequences

**Easier.**

- Multi-tenancy later is a deployment decision rather than a rewrite.
- A cross-tenant isolation bug fails closed at signature verification instead of returning someone
  else's data.
- A tenant's audit record can be handed over, verified independently, and removed as a unit.

**Harder.**

- **Discipline tax on every query, forever**, in a system that today has one tenant and no visible
  benefit from paying it. This will feel like ceremony for a long time before it pays.
- **Per-tenant keys multiply key management**: more keys to rotate, more public keys to retain, and a
  key-management service bill that scales with tenants rather than staying flat.
- **Per-tenant chains multiply anchoring.** Each chain needs its own external anchor, so the monthly
  verification is per tenant rather than one operation.
- Cross-tenant reporting genuinely becomes harder, and someone will eventually want it badly enough to
  propose a shortcut.

**New obligations.**

- A test asserts that no repository method can be called without a tenant, and that a query returning
  rows from two tenants fails rather than returning them.
- Chain verification runs per tenant, and a tenant with no anchor is a reported finding rather than a
  quiet gap.

## References

- [ZFN-15](https://zrz.io/zfn/15-partition-customer-data-by-tenant/) — partition from day one.
- [ZFN-18](https://zrz.io/zfn/18-enforce-quotas-at-ingress/) — quota per tenant.
- [EDR-0012](./0012-the-logbook-is-append-only.md) — the chain this partitions.
- [EDR-0005](./0005-control-plane-holds-no-credentials.md) — why a Pilot cannot be shared.

## Changelog

- **2026-08-15**: Accepted.
- **2026-08-16**: Amended in the second panel's should-fix pass: pointed rule 3 at EDR-0032, which carries the enforcement rule that makes its cryptographic-failure claim true.
