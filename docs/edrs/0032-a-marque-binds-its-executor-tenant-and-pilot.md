---
id: 32
title: "Bind the executor, the tenant and the Pilot into the marque"
summary: "Three bindings other records already assume had no payload field: the caller's key, the tenant, and the Pilot. Adding them makes offline execution work for the caller, makes tenant confusion fail closed, and makes the budget fence real."
status: accepted
implementation: none
date: 2026-08-15
authors:
  - "Theo Zourzouvillys <theo@sixfathoms.dev>"
tags: [security, execution, foundational]
supersedes: null
superseded_by: null
aliases: []
---

## TL;DR

Three records assert bindings that [EDR-0004](./0004-marques-are-signed-leases.md)'s payload does not
carry and no verification rule enforces. Each gets a field:

| Field | Fixes |
|---|---|
| **`cnf.jkt`** — the submitter's DPoP key thumbprint | The Pilot authenticates the caller by **proof of possession against the marque**, so offline execution works for the *caller* and not only for the marque |
| **`tenant`** | A Pilot's trust anchor for the `authority` limb is **its own tenant's key alone**, never a deployment-wide key set, so cross-tenant confusion fails signature verification as [EDR-0025](./0025-tenants-are-partitioned-from-day-one.md) claims |
| **`pilot`** | A Pilot refuses a marque naming another, so a budget of one cannot be spent once on each of two Pilots |

The first is the important one. [EDR-0004](./0004-marques-are-signed-leases.md)'s flagship property —
*an issued marque works while the control plane is down* — was constructed entirely for the artefact
and silently not for the person holding it.

## Context

Found by the expert panel. All three have the same shape: a record states a property in prose, and
the artefact has nothing in it that would let anyone check the property.

**The executor.** [EDR-0004](./0004-marques-are-signed-leases.md) and
[EDR-0011](./0011-execution-is-idempotent-and-fenced.md) both require the Pilot to check the caller
against `sub`. But `sub` is a bare identifier, and no record said what credential that check consumes.
In practice it would be an access token — minted by the Harbourmaster
([EDR-0003](./0003-federated-identity-and-sender-constrained-tokens.md)). So during a control-plane
outage the operator's token expires one lifetime in, there is no path to a new one, and the marque
that was carefully designed to remain verifiable becomes unusable by the only person allowed to use
it. The property held for the paper and not for the hand holding it.

**The tenant.** [EDR-0025](./0025-tenants-are-partitioned-from-day-one.md) claims a cross-tenant bug
"cannot produce a valid marque — the signature fails". It would not: verification is against the
deployment's key set, the payload names a `deployment` and no tenant, so a marque signed for tenant B
verifies exactly as well as one for tenant A. The loud failure that record advertises does not occur.

**The Pilot.** [EDR-0011](./0011-execution-is-idempotent-and-fenced.md) says "a marque is pinned to a
Pilot identity at issue time" and rests the whole nonce fence on it — but there is no field, so two
Pilot instances serving one target, or a target repointed to a second Pilot, each honour the budget
independently, and neither has anything in the artefact to refuse on.

## Decision

### `cnf.jkt` — the caller proves possession to the Pilot

The payload carries the submitter's DPoP key thumbprint, exactly as an access token does under
[RFC 9449](https://www.rfc-editor.org/rfc/rfc9449). To execute, the caller presents a DPoP proof over
the request, signed by the key the marque names.

- The Pilot's check becomes **cryptographic and local**: does this caller hold the key this marque was
  issued to? It needs no token endpoint and no identity provider.
- **The key→principal mapping is resolved against the anchored principal roster**
  ([EDR-0036](./0036-what-is-signed-must-be-what-was-seen.md)). Without that this field proves only
  that *some* key was used, and a compromised control plane would simply write its own thumbprint
  into it — which is what a second review found.
- An access token becomes an **optimisation and a second factor**, not a dependency. When the control
  plane is up, the Pilot may additionally require one; when it is down, proof of possession stands
  alone.
- `sub` remains, as the human-readable identity for the logbook and for policy. `cnf.jkt` is what is
  *verified*.

This is the same move [EDR-0004](./0004-marques-are-signed-leases.md) made for authority — replace a
lookup with a computation ([ZFN-49](https://zrz.io/zfn/49-verify-by-computation-not-lookup/)) —
applied to the half that was left out.

### `tenant` — and a per-tenant anchor

The payload names its tenant. More importantly, the **verification rule** changes:

> A Pilot serves exactly one tenant ([EDR-0025](./0025-tenants-are-partitioned-from-day-one.md)) and
> trusts **only that tenant's** `authority` key and **only that tenant's** approver roster
> ([EDR-0031](./0031-approver-keys-are-anchored-outside-the-control-plane.md)). It never verifies
> against a deployment-wide key set.

A marque from another tenant then fails signature verification rather than passing a string
comparison someone might forget to write — which is what
[EDR-0025](./0025-tenants-are-partitioned-from-day-one.md) always claimed. A mismatched `tenant`
claim is rejected as well, so the failure is loud even where key sets overlap during a migration.

### `pilot` — the marque names where it may run

The payload names the Pilot (or Pilot group) permitted to execute it. A Pilot refuses a marque naming
another.

This converts [EDR-0011](./0011-execution-is-idempotent-and-fenced.md)'s ledger rule from an operator
obligation into something the artefact can carry: a budget lives in exactly one ledger, and a second
Pilot has a signed reason to refuse rather than an assumption to honour. It also makes the
ledger-loss rule enforceable — a Pilot that has lost its ledger refuses marques naming it, instead of
relying on nobody sending any.

Where a target is served by a redundant pair, the field names the **pair** and the two share a
ledger; that is a deployment decision with its own consistency requirement, and it is stated as such
rather than left implicit.

## Consequences

**Easier.**

- The offline-execution property is now true end to end. That is the design's most-cited advantage and
  it was, until this record, half-built.
- Tenant isolation gains the cryptographic failure mode
  [EDR-0025](./0025-tenants-are-partitioned-from-day-one.md) advertised.
- The budget fence becomes a property of the artefact rather than of the topology.

**Harder.**

- **Losing the submitter's device key strands their outstanding marques.** They cannot execute a
  marque bound to a key they no longer hold, and the answer is a new request — during an outage,
  possibly no answer at all. That is the correct trade and it will hurt someone.
- **Per-tenant key sets multiply what a Pilot must hold and rotate**, on top of the roster from
  [EDR-0031](./0031-approver-keys-are-anchored-outside-the-control-plane.md).
- **Naming a Pilot reduces flexibility**: a marque cannot be executed against a target that has been
  moved to a different Pilot, and a redundant pair needs a shared ledger, which is real
  distributed-systems work rather than a configuration line.
- The payload keeps growing. Every field here is load-bearing, and the set should be treated as
  closed unless a record argues otherwise.

**New obligations.**

- A test executes a marque with a DPoP proof from the wrong key and asserts refusal; another executes
  a tenant-A marque against a tenant-B Pilot and asserts a *signature* failure rather than a
  comparison failure.
- A test sends one marque to two Pilots and asserts the second refuses on the `pilot` claim rather
  than on ledger state.

## References

- [RFC 9449](https://www.rfc-editor.org/rfc/rfc9449) — proof of possession; `cnf.jkt`.
- [ZFN-49](https://zrz.io/zfn/49-verify-by-computation-not-lookup/) — replace the lookup with a
  computation, on both halves.
- [EDR-0004](./0004-marques-are-signed-leases.md), [EDR-0011](./0011-execution-is-idempotent-and-fenced.md),
  [EDR-0025](./0025-tenants-are-partitioned-from-day-one.md) — the three records whose claims this
  makes true.
- [EDR-0031](./0031-approver-keys-are-anchored-outside-the-control-plane.md) — the per-tenant roster
  this anchors against.

## Changelog

- **2026-08-15**: Accepted, following an expert-panel finding that three asserted bindings had no
  payload field and no verification rule.
- **2026-08-16**: Amended after a second expert panel: stated that `cnf.jkt` resolves against the anchored principal roster; without that it proved only that *some* key was used ([EDR-0036](./0036-what-is-signed-must-be-what-was-seen.md)).
