---
id: 30
title: "A marque states how many approvers it needs, inside what every signature covers"
summary: "JWS signature entries are independent, so a two-approver marque could be stripped to one and still verify. The required count and eligible approvers move into the signed payload, making a stripped marque invalid rather than downgraded."
status: accepted
date: 2026-08-15
authors:
  - "Theo Zourzouvillys <theo@sixfathoms.dev>"
tags: [security, foundational]
supersedes: null
superseded_by: null
aliases: []
---

## TL;DR

A marque is a JWS in general JSON serialisation, and **its signature entries are mutually
independent** — each covers the payload, none covers the others. So any holder of a marque can
delete an approver entry and what remains still verifies.

[EDR-0004](./0004-marques-are-signed-leases.md)'s rule was "at least one `approver` and one
`authority` signature", which cannot tell a two-approver marque with an entry removed from a
legitimate single-approver one. That makes `min_approvals > 1`
([EDR-0015](./0015-policy-is-reviewed-configuration.md)) and multi-stage escalation
([EDR-0019](./0019-escalation-is-a-chain.md)) **unverifiable offline** — precisely in the
control-plane-down case the design values most.

The fix is to put the requirement inside the thing every signature covers:

```jsonc
"approvals": {
  "stages": [
    { "n": 1, "required": 1, "eligible": ["sam@acme.example"] },
    { "n": 2, "required": 1, "eligible": ["group:data-oncall"] }
  ],
  "chain": "sha256:…",          // the escalation chain computed at submission
  "roster_epoch": 47            // the epoch signatures are resolved against (EDR-0031)
}
```

**The structure mirrors the chain, and that is load-bearing.** A flat `required: 2` over a flat
`eligible: [sam, group:data-oncall]` is *not* an order- or stage-preserving encoding of
[EDR-0019](./0019-escalation-is-a-chain.md)'s chain: it is satisfied offline by **two members of
data-oncall with no signature from Sam at all** — collapsing a conjunction of stages into a
disjunction. So the Pilot requires **each stage's threshold to be met by distinct principals drawn
from that stage's own eligible set**, and the chain preimage travels with the marque so `chain` can
be checked rather than merely carried.

Removing a signature makes the marque invalid rather than weaker, and the payload cannot be edited to
lower a threshold, because every remaining signature covers it.

**`roster_epoch` fixes the temporal rule.** A signature is resolved against the named epoch: the
Pilot accepts it only if that epoch is one it has verified, and if the key was **live in that epoch**
([EDR-0031](./0031-approver-keys-are-anchored-outside-the-control-plane.md)). Without naming the
epoch there is no stated answer to "may a key retired yesterday satisfy a marque signed the day
before", and three consumers would each have picked one.

## Context

This came from review, and the reviewer's framing overstated it while the underlying defect was real.

The overstatement: the Harbourmaster releases only a completed marque, so stripping a signature
destroys evidence of an approval that already happened rather than removing a requirement that had
not been met. The logbook records `marque.signed` per approval and remains the ground truth, and the
`authority` countersignature already attests eligibility, `self_approval` and `max_marque_ttl`, none
of which are in the payload either.

The real defect: **an offline Pilot cannot verify a two-person rule.** The design's most-prized
property is that an issued marque executes while the control plane is unreachable
([EDR-0004](./0004-marques-are-signed-leases.md),
[ZFN-4](https://zrz.io/zfn/4-incident-tooling-independence/)). In exactly that state the Pilot cannot
consult the logbook or ask the Harbourmaster how many approvals were required — so a rule the
organisation believes it has is, at that moment, unenforced. A control that silently stops applying
during an incident is worse than one that was never claimed.

There is also a sharper variant worth closing: a compromised Harbourmaster holding one stage's
genuine signature over the final payload could countersign and release the marque without waiting for
the remaining stages. Nothing in the artefact would show that a second stage was ever required.

This is a standard cryptographic lesson arriving in a new place: **bind the policy into the signed
content, not into the verifier's ambient knowledge.** It is the same reason a JWS binds its algorithm
rather than letting the verifier infer one.

## Decision

**The payload gains an `approvals` block**, present on every marque including single-approver ones.
Because the escalation chain is computed at submission and recorded
([EDR-0019](./0019-escalation-is-a-chain.md)), `required` and `eligible` are known before anybody
signs — so all signatures cover the same payload, as JWS requires.

> [!NOTE]
> **Binding the requirement is necessary and not sufficient.** The payload is composed by the
> Harbourmaster before any signature exists, so the adversary this record defends against also
> *authors* `approvals.required`. [EDR-0036](./0036-what-is-signed-must-be-what-was-seen.md) closes
> that: policy becomes an anchored artefact and the Pilot **recomputes** the requirement, refusing a
> marque whose payload disagrees. The block below is still what the signatures cover — that is what
> stops stripping — it is simply no longer believed on its own.

**Verification becomes:**

1. exactly one valid `authority` signature;
2. **every stage satisfied**: for each entry in `approvals.stages`, at least `required` valid
   signatures from **distinct principals drawn from that stage's own `eligible` set**. Distinct means
   different enrolled principals — not merely different keys, since one person may hold several
   ([EDR-0023](./0023-approver-keys-enrolment-and-recovery.md));
3. **no principal counted in two stages** — a chain exists to collect independent judgements, and one
   person satisfying two stages defeats it. `sum(stages[].required)` replaces the old flat count;
4. every signature resolved against the epoch named by `roster_epoch`, with the key live in it
   ([EDR-0031](./0031-approver-keys-are-anchored-outside-the-control-plane.md));
5. everything else as before.

**The chain preimage travels with the marque**, so `chain` can be recomputed and checked rather than
merely carried — otherwise the digest attests a structure nobody can inspect.

**Group entries in `eligible` carry the same residual as `invokers`** in
[EDR-0029](./0029-the-fast-path-authority-chain.md): an offline Pilot cannot resolve identity-provider
group membership. The rule is identical — a `critical` target requires `eligible` to name principals
directly; elsewhere the Pilot verifies the signature and the count but takes group membership on
trust, and the console says so.

**The countersignature is applied last, once.** The Harbourmaster countersigns only when the required
approver signatures are present, and a marque is released only complete. This is stated rather than
assumed, because an implementation that countersigned per stage, or released mid-chain, would
reintroduce the downgrade this record closes.

**`chain` binds the escalation stages by digest**, so the marque attests not just how many approvals
were required but which sequence of stages produced them. It is what makes an after-the-fact question
— "was the data owner actually asked?" — answerable from the artefact alone.

## Consequences

**Easier.**

- A two-person rule is enforced by the artefact, everywhere, including offline. It stops being a
  control that quietly lapses during an incident.
- A marque is self-describing: given one and the enrolled public keys, its full approval requirement
  and whether it was met are decidable with no other input.
- Stripping is now a detectable tamper rather than a silent downgrade.

**Harder.**

- **The payload must be fixed before the first signature**, which means the escalation chain cannot
  change once signing has begun. [EDR-0019](./0019-escalation-is-a-chain.md) already froze the chain
  at submission, so this is a constraint that record must keep rather than a new one — but the two
  are now coupled, and loosening either breaks the other.
- **Adding an approver mid-flight is impossible** without re-issuing. If a stage's approvers are all
  unavailable, the answer is a new request, not an amended marque. That is more rigid, and correct.
- Distinctness by principal rather than by key needs the Pilot to map keys to principals offline,
  which means enrolment data must be distributed to Pilots — a modest new synchronisation obligation
  alongside the revocation list.
- Marque payloads grow slightly, and every one now carries a block that is trivial in the common
  single-approver case.

**New obligations.**

- A test constructs a valid two-approver marque, removes one signature, and asserts the Pilot refuses
  it. This is the specific regression this record exists to prevent, and it is the kind that would
  otherwise be reintroduced by a well-meaning simplification of the verifier.
- A test asserts the Harbourmaster will not countersign a marque whose required approver signatures
  are not all present.

## References

- [RFC 7515](https://www.rfc-editor.org/rfc/rfc7515) — general JSON serialisation; signature entries
  are independent, which is the property this record works around.
- [EDR-0004](./0004-marques-are-signed-leases.md) — the verification rule this replaces.
- [EDR-0019](./0019-escalation-is-a-chain.md) — the chain this binds, and whose freeze it now depends
  on.
- [EDR-0029](./0029-the-fast-path-authority-chain.md) — the companion defect, found in the same
  review.

## Changelog

- **2026-08-15**: Accepted, following an expert-panel finding that a marque's signature set is
  malleable and its approval requirement was not covered by any signature.
- **2026-08-16**: Amended after a second expert panel: noted that binding the requirement is necessary and not sufficient, since the Harbourmaster authors it; the Pilot now recomputes it from anchored policy ([EDR-0036](./0036-what-is-signed-must-be-what-was-seen.md)).
- **2026-08-16**: Amended after the second panel's should-fix pass: restructured `approvals` to mirror the escalation chain's stages. A flat `required: 2` over a flat `eligible` list collapsed a conjunction of stages into a disjunction — a chain requiring Sam then data-oncall was satisfiable by two members of data-oncall with no signature from Sam. Also added `roster_epoch` to fix the temporal acceptance rule.
- **2026-08-16**: Amended after the second panel's synthesis: rewrote the Decision's verification steps over `stages`; they still specified the flat encoding the TL;DR had replaced, so an implementer building from the Decision built exactly the defect the restructure fixed.
