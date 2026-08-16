---
id: 31
title: "Anchor the approver key set outside the control plane, or the second signature is theatre"
summary: "A Pilot must not learn which keys are approvers from the Harbourmaster. The enrolled set is a co-signed, epoch-chained roster verified back to a root configured out of band at Pilot deployment."
status: accepted
date: 2026-08-15
authors:
  - "Theo Zourzouvillys <theo@sixfathoms.dev>"
tags: [security, identity, foundational]
supersedes: null
superseded_by: null
aliases: []
---

## TL;DR

[EDR-0004](./0004-marques-are-signed-leases.md) says the Pilot verifies signatures "given the
deployment's JWKS". [EDR-0002](./0002-bootstrap-discovery-document.md) and
[EDR-0023](./0023-approver-keys-enrolment-and-recovery.md) put that key set at a `jwks_uri` the
**control plane serves**. So the Pilot asks the Harbourmaster which keys are approvers.

That makes the two-signature design a **detective control, not a preventive one**, against exactly
the adversary it was built for. A compromised Harbourmaster generates a keypair, appends a
well-formed enrolment, publishes the key, and signs both limbs. Every check in
[EDR-0029](./0029-the-fast-path-authority-chain.md) and
[EDR-0030](./0030-a-marque-states-its-own-approval-requirement.md) then verifies happily against a
key set the attacker chose.

The fix:

- **The enrolled approver set is a signed artefact — a roster — not an endpoint.** It is signed by
  **k of the already-enrolled approver device keys** (default 2), never by the control plane.
- **Rosters are epoch-chained.** Each names its predecessor's digest, and must be signed by k keys
  valid in that predecessor. Authority to change who can approve therefore descends only from people
  who could already approve.
- **Each Pilot pins a genesis root out of band, at deployment.** Infrastructure configuration, not
  something the Harbourmaster serves. The Harbourmaster may *distribute* rosters; it cannot author
  one.
- **Epochs are monotonic**, so a compromised control plane cannot serve an old roster to reinstate a
  retired key.
- **Roster digests go to the logbook's external anchor** ([EDR-0012](./0012-the-logbook-is-append-only.md)),
  so an unanchored roster is a finding even if it verifies.

This also supplies the key→principal mapping
[EDR-0030](./0030-a-marque-states-its-own-approval-requirement.md) requires and never sourced — and,
per [EDR-0036](./0036-what-is-signed-must-be-what-was-seen.md), it is the **principal** roster rather
than only the approver set: operator and agent keys ride the same artefact, so `cnf.jkt` resolves
against something anchored too.

## Context

Found by the expert panel, and it is the most serious finding in the review — because it silently
undoes two records written to close earlier findings.

The whole argument for two signatures is: *the control plane cannot manufacture authority, because it
cannot produce a human's signature.* That holds only if the definition of "a human's key" is beyond
the control plane's reach. It was not. Three records combined to make the Harbourmaster the trust
anchor for the very thing it must not control:

- [EDR-0004](./0004-marques-are-signed-leases.md) — verification is "given the deployment's JWKS".
- [EDR-0002](./0002-bootstrap-discovery-document.md) — `jwks_uri` lives in the control-plane-served
  bootstrap document.
- [EDR-0023](./0023-approver-keys-enrolment-and-recovery.md) — enrolment countersignatures are
  checked by the Harbourmaster, which then records the result.

[EDR-0002](./0002-bootstrap-discovery-document.md)'s pin-on-first-use does not rescue it: that text
is about *clients* pinning a deployment, and an approver key set legitimately changes on every
joiner, rotation and re-enrolment, so a Pilot cannot pin it. And the property this falsifies is not
only the old unqualified claim — it falsifies the **new, bounded** one from
[EDR-0029](./0029-the-fast-path-authority-chain.md) too, because an attacker who can enrol a key can
also produce the "human-signed" artefact whose shape the Pilot checks.

The lesson generalises: **it is not enough for a signature to be required; the verifier's notion of
who may sign has to be anchored somewhere the adversary does not control.** A key distribution
channel is part of the trust boundary, not plumbing around it.

## Decision

### The roster

```jsonc
{
  "tenant": "acme",
  "epoch": 47,
  "prev": "sha256:…",                       // the previous roster's digest; null at genesis
  "entries": [
    { "principal": "sam@acme.example", "jkt": "…", "envelope": "webauthn",
      "capabilities": ["approver"],        // also "operator", "agent" — EDR-0036
      "backing": "hardware",               // hardware | software — what require_key_backing checks
      "backup_eligible": false,            // a provider-synced passkey is not a hardware-bound key
      "enrolled_at": "…", "retired_at": null },
    …
  ],
  "issued_at": "…"
}
```

`backing` is established **at enrolment** — from WebAuthn attestation, or the platform key store's
own attestation — and is what [EDR-0015](./0015-policy-is-reviewed-configuration.md)'s
`require_key_backing` checks; without it that setting, which replaced the `require_envelope` default
on `critical` targets, would enforce nothing. `backup_eligible` is recorded because a provider-synced
passkey is not a hardware-bound key however it was created, and a deployment that means "hardware"
usually means "not synced to a cloud account".

Signed by **k currently-enrolled approver device keys**, where **k is defined here and nowhere else**
— deployment configuration with a minimum of 2. The control plane's key does not appear on it and adds nothing if it does.

Per [EDR-0025](./0025-tenants-are-partitioned-from-day-one.md), **the roster is per tenant**, as are
its epochs and its anchor.

### What a Pilot does

1. Holds a **genesis root** — the genesis roster's digest, or its key set — supplied at deployment as
   infrastructure configuration, out of band from the Harbourmaster.
2. Accepts a roster only if it chains back to that root: each epoch names its predecessor and carries
   k valid signatures from keys **live in that predecessor**.
3. Refuses any roster whose `epoch` is not greater than the highest it has accepted. Rollback is the
   obvious attack once forward forgery is closed.
4. Resolves signatures against the **right epoch for the kind of check**, which is not always the
   latest:
   - **A marque's approver signatures, and a fast-path artefact's**, resolve against the epoch the
     payload names (`roster_epoch`,
     [EDR-0030](./0030-a-marque-states-its-own-approval-requirement.md)) — accepted only if the Pilot
     has verified that epoch, the key was **live in it**, and `nbf` falls inside its validity window.
     That is what settles whether a key retired yesterday may satisfy a marque signed the day before:
     it may, if it was live when signed.
   - **Live authentication** — a submitter signature on a Pilot method
     ([EDR-0034](./0034-the-pilot-api-has-one-authorisation-model.md)), proof of possession of
     `cnf.jkt` — resolves against the **latest verified epoch**, because there the question is "is
     this principal live *now*".
5. Offline, uses the last roster it verified — under the **same freshness contract as the revocation
   list** ([EDR-0004](./0004-marques-are-signed-leases.md)). The roster carries signed `issued_at`
   **and `next_update`**, checked against monotonic elapsed time since fetch, and a per-target policy
   bound past which the Pilot **refuses approver signatures** rather than continuing on a stale set.
   That matters because the failure it converts is real: without it, withholding a roster silently
   *extends* the validity of keys that were retired in an epoch the Pilot never saw. With it,
   withholding degrades to denial of service, which is visible.
6. Holds its epoch high-water mark **durably, bound to its incarnation**
   ([EDR-0011](./0011-execution-is-idempotent-and-fenced.md)). Durability across a *restart* is not
   enough: a fresh container, a replaced node or a restored volume starts at genesis, the genesis
   roster chains validly to the pinned root, and a compromised control plane simply walks the rebuilt
   Pilot forward from genesis — reinstating every retired key, which is exactly the rollback the
   May-not table says is impossible. So a **`min_epoch` floor is pinned alongside the genesis root at
   deployment**, a Pilot refuses any roster below it, and a Pilot presenting a new incarnation reports
   that fact rather than starting quietly from scratch.

### What the control plane may and may not do

| May | May not |
|---|---|
| distribute rosters, and serve `jwks_uri` for its **own** `authority` key | author a roster, or add an entry to one |
| collect and relay enrolment countersignatures | be the thing that makes a countersignature true |
| refuse to distribute (a denial-of-service, and a visible one) | roll a Pilot back to an older epoch |

A compromised control plane retains two powers, and they are stated rather than wished away:
**withholding** a new roster, so a newly-enrolled approver appears unrecognised and a retired key
stays live until the Pilot sees a newer epoch; and serving a **stale but validly-chained** roster up
to that bound. Both are detectable — roster age is a monitored signal — and neither creates
authority.

### Enrolment, restated

[EDR-0023](./0023-approver-keys-enrolment-and-recovery.md) requires an existing approver to
countersign a new enrolment. That rule is right and was enforced in the wrong place. The
countersignature's **effect** is now to produce a new roster epoch signed by k approvers; the
Harbourmaster's involvement is transport. An enrolment the control plane records but that never
appears in a roster grants nothing.

### Anchoring

Every roster digest is written to the logbook's external anchor
([EDR-0012](./0012-the-logbook-is-append-only.md)). A roster that verifies but is not anchored is a
**finding**, not a fallback — it is the signature of a key set built somewhere other than the normal
process. Operators audit the roster against the logbook and the anchor on the schedule in the
playbook.

### Closing EDR-0023's bootstrap residual

The panel also found that
[EDR-0023](./0023-approver-keys-enrolment-and-recovery.md)'s bootstrap and recovery ceremonies turn
deployment-infrastructure access into approval authority — contradicting
[EDR-0025](./0025-tenants-are-partitioned-from-day-one.md)'s rule that operating the system is not
authority within it. That contradiction is real and is not fully removable: **someone who can deploy
Pilots can set their genesis root**, and therefore can define who approves. What is removable is
doing it silently. So:

- The genesis root is per tenant and recorded in the tenant's logbook at first use.
- **Changing a Pilot's genesis root is a re-deployment**, visible in infrastructure change control,
  and the Pilot reports the root it is using so a change is observable from the outside.
- The bootstrap-window state lives with the control plane, and the **derivation key that opens the
  window is held by the deployment's key-management service under a separate grant** — the process
  that can open the window must not be the process that holds the key. Two parties, as
  [EDR-0023](./0023-approver-keys-enrolment-and-recovery.md) already requires for recovery.
- The honest statement, which now appears in `SECURITY.md`: **infrastructure control is a trust root.
  Marque bounds what it can do silently; it does not pretend it is not authority.**

## Consequences

**Easier.**

- The two-signature property becomes preventive rather than detective against a control-plane
  compromise — which is what every other record already assumed and none delivered.
- Pilots gain the key→principal mapping [EDR-0030](./0030-a-marque-states-its-own-approval-requirement.md)
  needed, from a source that is not the adversary.
- "Who could approve on 3 March?" is answerable from an artefact, at any later date.

**Harder.**

- **k-of-n roster signing is a real ceremony.** Every joiner, rotation and departure needs two
  approvers to sign a new epoch. That is friction on ordinary personnel changes, and it is the price
  of the property.
- **Losing k approver keys at once freezes the roster** — nobody can enrol, including a replacement.
  This is the recovery ceremony in [EDR-0023](./0023-approver-keys-enrolment-and-recovery.md), and it
  now genuinely must be rehearsed rather than nominally.
- **Genesis root distribution is new deployment surface**, and getting it wrong is silent: a Pilot
  pinned to the wrong root rejects every legitimate approver, which presents as a total outage rather
  than as a misconfiguration.
- Offline Pilots work from a possibly-stale roster, so revocation of an *approver* has the same
  bounded propagation delay as revocation of a marque.

**New obligations.**

- Roster age, epoch and pinned-root digest are reported by every Pilot and monitored. A Pilot on an
  old epoch is a finding, because it is what withholding looks like.
- The playbook gains: audit the enrolled key set against the logbook and the external anchor; and, on
  a suspected Harbourmaster compromise, verify the current roster's chain independently before
  trusting any approval made during the window.
- A test enrols a key by control-plane action alone and asserts every Pilot refuses signatures made
  with it.

## References

- [ZFN-5](https://zrz.io/zfn/5-platform-workload-identity-service/) — key distribution is platform
  infrastructure, and it is part of the trust boundary.
- [ZFN-49](https://zrz.io/zfn/49-verify-by-computation-not-lookup/) — verification by computation is
  only as good as the anchor the computation starts from.
- [EDR-0023](./0023-approver-keys-enrolment-and-recovery.md) — the enrolment rule this relocates.
- [EDR-0029](./0029-the-fast-path-authority-chain.md),
  [EDR-0030](./0030-a-marque-states-its-own-approval-requirement.md) — the two records whose
  verification chains this makes sound.

## Changelog

- **2026-08-15**: Accepted, following the expert panel's most serious finding: the Pilot's trust
  anchor for approver public keys was the Harbourmaster itself.
- **2026-08-16**: Amended after a second expert panel: extended to the **principal** roster — entries carry a capability, so operator and agent keys ride the same anchored artefact ([EDR-0036](./0036-what-is-signed-must-be-what-was-seen.md)).
- **2026-08-16**: Amended after the second panel's should-fix pass: gave the roster the same freshness contract as the revocation list, with a bound past which a Pilot refuses approver signatures — without it, withholding a roster silently *extends* the validity of keys retired in an epoch the Pilot never saw — and required the epoch high-water mark to be durable, since a memory-only one resets on every deploy.
- **2026-08-16**: Amended after the second panel's synthesis: resolved signatures against the epoch a payload names rather than "the current roster"; bound the high-water mark to the Pilot incarnation with a `min_epoch` floor, since a rebuilt Pilot starts at genesis and can be walked forward; and added the `backing` field `require_key_backing` checks.
