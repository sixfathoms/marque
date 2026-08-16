---
id: 29
title: "On a fast path the human signed the shape, and the Pilot verifies that artefact"
summary: "A marque minted without a human present carries the standing order or compiled delegation that authorised it, with its own approver signature, so the Pilot verifies offline that some human signed the shape of what it is about to run."
status: accepted
date: 2026-08-15
authors:
  - "Theo Zourzouvillys <theo@sixfathoms.dev>"
tags: [security, execution, foundational]
supersedes: null
superseded_by: null
aliases: []
---

## TL;DR

[EDR-0004](./0004-marques-are-signed-leases.md) requires two signatures on a marque, one of them a
human's device key. Three paths mint a marque with **no human present at that moment**: standing-order
invocation ([EDR-0008](./0008-standing-orders.md)), a delegation match
([EDR-0007](./0007-delegation-by-containment-proof.md),
[EDR-0016](./0016-natural-language-delegations-are-compiled.md)), and a Surveyor `conforms`
([EDR-0017](./0017-conformance-matching-may-route-never-widen.md)). No record said what fills the
approver limb on those paths. This one does.

**The human signed the shape rather than the instance.** A standing order, a compiled delegation and
a Tier-B outer bound are each already human-signed artefacts. On a fast path:

- the marque's payload names the authorising artefact and its digest;
- the artefact travels **with** the marque, carrying its own signatures;
- the Pilot verifies the artefact offline, then checks that the statement in hand is what that
  artefact authorises, and that the marque's limits are within the artefact's.

The property from [EDR-0004](./0004-marques-are-signed-leases.md) therefore survives in a precise
form: **a compromised control plane cannot cause a statement to execute whose shape no human
signed.** That is weaker than the unqualified claim four documents previously made, and those are
corrected.

## Context

The gap was found by review, and it was real. Three separate symptoms pointed at one missing
decision:

1. **A missing field.** [EDR-0008](./0008-standing-orders.md) says invocation mints "an ordinary
   marque … referencing the standing order's identifier as its authority", and
   [EDR-0017](./0017-conformance-matching-may-route-never-widen.md) says a fast-path marque "names
   the judgment in its payload". [EDR-0004](./0004-marques-are-signed-leases.md)'s payload has no
   such field. Two records assumed a schema a third did not define.
2. **A contradiction.** [EDR-0003](./0003-federated-identity-and-sender-constrained-tokens.md)
   requires a fresh interactive authentication to sign a marque, and states no workload principal can
   satisfy it. [EDR-0008](./0008-standing-orders.md) mints one with no human in the loop. Both
   records are silent about the other.
3. **An overstated claim.** `SECURITY.md`, `architecture.md`, the playbook and `CLAUDE.md` all
   asserted that a compromised control plane "cannot cause any statement to execute". If the
   Harbourmaster assembles a fast-path marque by itself — holding its own signing key and needing no
   approver key — that claim is false for the majority of traffic, which fast paths are explicitly
   designed to be.

The intent was always that authority traces back to a human signature made in advance. What was
missing was the *mechanism* by which an offline Pilot can check that, given it holds no policy, no
standing-order catalogue and no delegation list, and looks up only revocations.

## Decision

### The payload gains an authority reference

```jsonc
{
  "mrq": "mrq_01JB…", "target": "…", "role": "…", "sub": "…", "req": "sha256:…",
  "nbf": …, "exp": …, "budget": { … }, "fence": [ … ],
  "auth": {
    "kind": "interactive" | "standing_order" | "delegation" | "surveyed",
    "artefact": "sto_01JB…",              // absent when kind = interactive
    "artefact_digest": "sha256:…",         // binds the exact version signed
    "binding": { "account_id": "…" },      // parameters, for a standing order
    "judgment": "sha256:…"                 // the Surveyor's recorded decision, when kind = surveyed
  }
}
```

`kind: interactive` is the ordinary path and behaves exactly as
[EDR-0004](./0004-marques-are-signed-leases.md) already describes: a human signs the payload at
approval time.

### The authority bundle travels with the marque

For every non-interactive kind, the marque is delivered together with the **authorising artefact** —
the signed standing order, or the signed compiled delegation and its outer bound. The artefact is not
secret, is small, and carries its own two signatures
([EDR-0008](./0008-standing-orders.md) signs the template, parameter declarations, role, limits and
invoker list; [EDR-0016](./0016-natural-language-delegations-are-compiled.md) has the grantor sign
the compilation).

### What the Pilot checks, offline

1. Both signatures on the **marque** verify, exactly as before — except that on a non-interactive
   kind the approver limb is satisfied by the artefact rather than by a signature over this payload.
2. Both signatures on the **artefact** verify, against enrolled keys
   ([EDR-0023](./0023-approver-keys-enrolment-and-recovery.md)).
3. `artefact_digest` matches the artefact presented, so a marque cannot be moved to a different or
   later version of the order.
4. The artefact has not expired on its own terms (`expires`, `not_after`).
5. **The statement is what the artefact authorises.** For a standing order: recompute the template
   bound with `binding`, and require its digest to equal `req`; check each parameter against its
   declared constraint. For a delegation: check the statement against the compiled object scope, as
   the Harbourmaster did — the check is deterministic, so the Pilot repeating it costs little and
   removes a trust assumption.
6. `sub` is permitted by the artefact — see the residual below.
7. The marque's `exp`, `budget` and `fence` are **within** the artefact's limits. A marque may never
   be more permissive than the artefact it claims authority from.

Any failure is a refusal. The fence, the magnitude assertion and the nonce all still run afterwards
([EDR-0007](./0007-delegation-by-containment-proof.md),
[EDR-0011](./0011-execution-is-idempotent-and-fenced.md)).

### Freshness is a property of signing, not of minting

[EDR-0003](./0003-federated-identity-and-sender-constrained-tokens.md)'s freshness requirement
applies to **producing a human approver signature**. It was satisfied when the standing order or the
delegation was signed, interactively, by a present human. Minting a fast-path marque is not signing —
it is assembling a reference to a signature that already exists. The contradiction was in the wording,
and both records now say so.

### The Surveyor case

`kind: surveyed` references the human-signed outer bound plus the recorded judgment. The Pilot
verifies the **bound**, not the judgment — it cannot confirm the Surveyor ran, and it does not need
to. A compromised Harbourmaster skipping the Surveyor entirely is equivalent to a Surveyor that
answered `conforms`, and both are bounded by the outer bound a human signed. That is exactly the
containment [EDR-0017](./0017-conformance-matching-may-route-never-widen.md) claims, now stated as a
verification property rather than an intention.

### The residual, stated plainly

**Step 6 is only fully verifiable offline when the artefact names principals directly.**
`invokers` may name identity-provider groups
([EDR-0015](./0015-policy-is-reviewed-configuration.md)), and group membership is control-plane
state that an offline Pilot cannot resolve. So:

- A standing order whose `invokers` are **principals** is fully offline-verifiable.
- A standing order whose `invokers` are **groups** is not: a compromised Harbourmaster could invoke a
  genuine standing order naming a principal of its choosing. It remains bounded to that order's
  approved statement shape, its parameter constraints, its budget and its rate limits — it cannot
  reach an arbitrary statement — but it is a real residual and it is not zero.
- Therefore: standing orders on `critical` targets **must** name principals directly. Elsewhere the
  choice is the grantor's, and the console shows which form an order uses and what it implies.

## Consequences

**Easier.**

- The two-signature property becomes checkable on every path rather than only the interactive one,
  which is what the design always claimed and did not previously deliver.
- An offline Pilot can now justify a fast-path execution entirely from artefacts in hand — the
  control-plane-down case the design prizes most.
- Fast-path traffic gains the same non-repudiation as interactive traffic: the record shows which
  human signed which shape, and when.

**Harder.**

- **A marque is no longer self-contained.** Delivering it means delivering a bundle, and every
  client, cache and log that handles marques must handle the artefact too.
- **The Pilot repeats work the Harbourmaster already did** — parameter binding and object-scope
  checking. That is deliberate duplication to remove a trust assumption, and it is more code in the
  component that should stay smallest ([EDR-0005](./0005-control-plane-holds-no-credentials.md)).
- **Group-named invokers are weaker than principal-named ones**, and that distinction is now
  something operators have to understand. Forcing principals on critical targets limits the blast
  radius of getting it wrong.
- Artefact versioning becomes load-bearing: `artefact_digest` means editing a standing order
  invalidates outstanding marques that referenced the old version. That is correct and it will
  surprise someone.

**New obligations.**

- The claim wording in `SECURITY.md`, `architecture.md`, the playbook and `CLAUDE.md` is corrected to
  the bounded form: *a compromised control plane cannot cause a statement to execute whose shape no
  human signed*, with the group-invoker residual named.
- A test mints a fast-path marque with a tampered artefact, a mismatched digest, an out-of-constraint
  parameter and a widened limit, and asserts the Pilot refuses each.

## References

- [EDR-0004](./0004-marques-are-signed-leases.md) — the two-signature rule this completes.
- [EDR-0003](./0003-federated-identity-and-sender-constrained-tokens.md) — the freshness rule this
  scopes.
- [EDR-0008](./0008-standing-orders.md), [EDR-0016](./0016-natural-language-delegations-are-compiled.md),
  [EDR-0017](./0017-conformance-matching-may-route-never-widen.md) — the artefacts that carry the
  human signature.
- [EDR-0030](./0030-a-marque-states-its-own-approval-requirement.md) — the related defect: a payload
  that does not state how many approvers it needs.

## Changelog

- **2026-08-15**: Accepted, following an expert-panel finding that no record specified what fills the
  approver limb on a fast path.
