---
id: 23
title: "Enrol approver keys in hardware, and require an existing approver to enrol the next"
summary: "An approver signs with a non-extractable hardware key — WebAuthn in the browser, the platform key store in the CLI. Enrolling an additional key needs a second enrolled approver, so a stolen session cannot mint its own authority."
status: accepted
date: 2026-08-15
authors:
  - "Theo Zourzouvillys <theo@sixfathoms.dev>"
tags: [identity, security, ops]
supersedes: null
superseded_by: null
aliases: []
---

## TL;DR

[EDR-0004](./0004-marques-are-signed-leases.md) requires a marque to carry the approver's own
signature. This is where that key comes from, how it rotates, and what happens when it is lost.

- **The key is non-extractable and hardware-backed** wherever the platform offers it: WebAuthn in the
  browser, Secure Enclave or TPM from the CLI, a file with restrictive permissions only as a
  documented fallback.
- **WebAuthn's user verification *is* the freshness requirement.** Each assertion demands a touch or
  a biometric, which is what makes approval an act rather than a session property
  ([EDR-0003](./0003-federated-identity-and-sender-constrained-tokens.md)) — enforced by the
  authenticator rather than by a timestamp Marque checks.
- **Enrolling an additional approver key requires a second, already-enrolled approver to
  countersign**, and every enrolment is announced. Otherwise the shortest path to approving anything
  is stealing a session and enrolling your own key, which would make the two-signature design
  decorative.
- **Everyone enrols at least two keys.** A single key is an outage waiting for a lost laptop.
- **Public keys are retained long after the private half retires**, because the logbook must stay
  verifiable for as long as it is kept.

## Context

The two-signature marque is the design's central security property, and it rests entirely on the
approver's key actually being the approver's. Three questions were left open by
[EDR-0004](./0004-marques-are-signed-leases.md), and each has a bad default:

**How does a key get enrolled?** The obvious answer — "you are logged in, so register a key" — makes
session compromise equivalent to approval authority. An attacker with a stolen token enrols a key
they control and then satisfies the human half of every marque. The whole point of requiring a
signature the server cannot produce is lost to a flow nobody thought of as security-critical.

**What signs, in a browser?** The console must be able to approve, and a browser has no Secure
Enclave API. Storing a private key in IndexedDB is extractable by any script that achieves execution
on the origin. WebAuthn is the standard built for exactly this
([ZFN-30](https://zrz.io/zfn/30-use-standards-dont-reinvent/)), and it brings user verification with
it — but its signature is not the same shape as a raw signature over a JWS input, which is a real
complication that must be declared rather than papered over.

**What happens when keys are lost?** If every approver loses their key simultaneously — a laptop
refresh, a lost hardware token, one person who was the only approver — nobody can approve anything,
including the policy change that would fix it. An unrehearsed recovery path is not a recovery path
([ZFN-36](https://zrz.io/zfn/36-test-backups-by-restoring/)).

## Decision

### Two signature envelopes, declared explicitly

The `approver` signature on a marque carries a header naming how it was made:

| Envelope | Where | Signs |
|---|---|---|
| `es256` | CLI, platform key store | the JWS signing input directly |
| `webauthn` | browser | a WebAuthn assertion whose challenge is the digest of the JWS signing input; the signature covers `authenticatorData ‖ SHA-256(clientDataJSON)` |

For `webauthn`, the assertion's `authenticatorData` and `clientDataJSON` travel in the signature's
unprotected header so a verifier can reconstruct exactly what was signed and confirm the challenge
equals **the digest of the JWS signing input** — the same quantity named in the table above, stated
once rather than twice in two different ways. **Verifiers implement both**, and the envelope is part of the signed
header so it cannot be swapped after the fact.

This is genuinely more complexity than one envelope would be. The alternative — a software key in the
browser — trades a hardware guarantee for implementation convenience in the one component most
exposed to script execution, and that is the wrong trade
([ZFN-2](https://zrz.io/zfn/2-engineering-priority-ordering/)).

**Policy may require an envelope — and, separately, a surface.** `require_envelope`
([EDR-0015](./0015-policy-is-reviewed-configuration.md)) names which envelope a target accepts, and a
`critical` target's defaults are **`require_key_backing: hardware` plus `signing_surface: local`**
([EDR-0015](./0015-policy-is-reviewed-configuration.md)) — *not* `require_envelope: webauthn`, which
an earlier version of this paragraph asserted and which
[EDR-0036](./0036-what-is-signed-must-be-what-was-seen.md) removed as the default. The envelope was
the wrong proxy for "not a file on a laptop", since `es256` covers both a Secure Enclave key and the
file fallback.

**That is a statement about the key, not about where signing happens**, and the two were originally
conflated. An envelope requirement alone pushed `critical` approvals into the browser, whose assets
the control plane serves; `signing_surface: local`
([EDR-0036](./0036-what-is-signed-must-be-what-was-seen.md)) is the setting that keeps the rendering
out of the adversary's hands. A platform authenticator works from the CLI, so the two compose.

For `webauthn`, the verifier performs the **whole** of the W3C §7.2 procedure, not only the challenge
binding — an assertion is otherwise replayable from another origin or another ceremony:

| Check | Value |
|---|---|
| `clientDataJSON.challenge` | equals the digest of the JWS signing input, **domain-separated** by a ceremony label so an approval assertion cannot be replayed as an enrolment one |
| `clientDataJSON.type` | `webauthn.get` |
| `clientDataJSON.origin` | in the deployment's configured allowlist |
| `authenticatorData` rpIdHash | equals SHA-256 of the configured RP ID |
| UV flag | **set** — user presence alone is a tap, and approving a production change should cost a deliberate act |
| signature counter | handled per the deployment's cloning policy |

The RP ID and origin allowlist are **deployment configuration a Pilot holds**, alongside its genesis
root — not values read from the assertion or from the control plane, which would defeat the point.

### Enrolment

| Situation | Requires |
|---|---|
| First key, for a person already in an approver group | a fresh interactive authentication **and** countersignatures from **k already-enrolled approvers** — k is defined in [EDR-0031](./0031-approver-keys-are-anchored-outside-the-control-plane.md) and nowhere else, since a countersignature's effect is to produce a roster epoch |
| Additional key for yourself | a fresh authentication **and** a signature from one of your existing enrolled keys |
| First key in a brand-new deployment | the bootstrap ceremony below |

Every enrolment, retirement and countersignature is a logbook entry naming both parties, and is
announced to the deployment's notification channel immediately. **An enrolment nobody was told about
is the event this design exists to make impossible to hide.**

An operator who has lost every key uses the first row: someone else countersigns. That is a
deliberate, ordinary, two-person operation rather than an emergency.

### Bootstrap

A new deployment has no enrolled approvers, so the first one is chained to infrastructure control
rather than to an existing signature: enrolment during the bootstrap window requires a token derived
from the control plane's own key material, obtainable only by someone with deployment access. The
window closes on the first successful enrolment and cannot be reopened without an infrastructure
change, which is itself logged.

This makes "who bootstrapped this deployment" answerable, and makes reopening the window a visible
act rather than an available one.

### Rotation and retirement

Rotation is enrol-then-retire, never replace: the new key is enrolled and proven with a signature
before the old one is retired, so there is no interval in which a person cannot approve.

Retiring a key **does not invalidate marques already signed with it** — the marque was valid when
made. Where a key is retired because it may be compromised, the marques signed with it after the
suspected time are separately revoked
([EDR-0004](./0004-marques-are-signed-leases.md)), which is a different act with a different record.

**Public keys are retained indefinitely by default**, with their enrolment and retirement times. A
logbook entry from two years ago is only checkable if the key that signed it can still be found
([EDR-0012](./0012-the-logbook-is-append-only.md)).

### The control plane's own key

The `authority` signature comes from the deployment's key management service — never a key on disk,
never a key the process can export. It rotates on a schedule with overlap, and its public keys are
published at the `jwks_uri` in the bootstrap document
([EDR-0002](./0002-bootstrap-discovery-document.md)) and retained on the same terms as approver keys.

### Recovery, rehearsed

The catastrophic case is every approver key being unavailable at once. Under the roster
([EDR-0031](./0031-approver-keys-are-anchored-outside-the-control-plane.md)) this is heavier than
reopening a window, and the ceremony has to say so: with no live approver keys **no new roster epoch
can be signed**, so recovery means minting a **new genesis roster**, **re-pinning every Pilot's root
by re-deployment**, and resolving each Pilot's epoch high-water mark and `min_epoch` floor against it.
It requires **two people with deployment infrastructure access**, and it is announced. It cannot be performed by one person, and it cannot be performed silently.

It is **rehearsed on a schedule** against a non-production deployment. A recovery path nobody has
executed is a hope, and this one will be reached for on the worst day
([ZFN-4](https://zrz.io/zfn/4-incident-tooling-independence/)).

## Consequences

**Easier.**

- A stolen session cannot become approval authority, which closes the most valuable attack against
  the two-signature design.
- User verification makes approval a deliberate physical act, which is a better freshness guarantee
  than any timestamp.
- Non-repudiation is genuinely strong: the private key never existed in software.

**Harder.**

- **Two signature envelopes, forever.** Every verifier implements both, and the WebAuthn path has
  more moving parts than a raw signature. This is the single largest complexity cost in the record.
- **Countersigned enrolment is friction on a first day**, and it is friction at exactly the moment a
  new joiner is least connected. A deployment with one approver has a bootstrapping problem it must
  plan for.
- **Hardware keys get lost, and platform key stores are platform-specific** and awkward to test.
  Requiring two keys per person mitigates the first and does nothing for the second.
- Retaining public keys indefinitely is a small, permanent storage obligation and a thing to migrate
  correctly forever.

**New obligations.**

- The recovery ceremony is rehearsed, and a rehearsal that has not happened within its interval is a
  reported finding.
- Every person's enrolled key count is visible, and having only one is a warning rather than a silent
  fragility.
- Enrolment announcements go to a channel someone reads. An announcement nobody sees provides none of
  the protection this decision is built on.

## References

- [W3C Web Authentication](https://www.w3.org/TR/webauthn-2/) — the assertion structure and user
  verification flag.
- [ZFN-30](https://zrz.io/zfn/30-use-standards-dont-reinvent/) — use the standard built for this.
- [ZFN-36](https://zrz.io/zfn/36-test-backups-by-restoring/) — an untested recovery is not a
  recovery.
- [EDR-0004](./0004-marques-are-signed-leases.md) — what these keys sign.
- [EDR-0024](./0024-the-console-is-for-deciding.md) — where the WebAuthn envelope is used.

## Changelog

- **2026-08-15**: Accepted.
- **2026-08-16**: Amended after the expert panel's should-fix pass: stated the WebAuthn challenge binding once rather than twice inconsistently, and added the `require_envelope` policy hook.
- **2026-08-16**: Amended after a second expert panel: stated that `require_envelope` governs the key and `signing_surface` governs where signing happens; the two were conflated ([EDR-0036](./0036-what-is-signed-must-be-what-was-seen.md)).
- **2026-08-16**: Amended after the second panel's should-fix pass: specified the **whole** WebAuthn verification set (challenge with ceremony domain separation, `type`, `origin` allowlist, rpIdHash, UV flag, counter policy) rather than only the challenge binding; an assertion checked on the challenge alone is replayable from another origin or another ceremony.
- **2026-08-16**: Amended after the second panel's synthesis: corrected the `critical` default (it is `require_key_backing: hardware` plus `signing_surface: local`, not `require_envelope: webauthn`), deferred k to [EDR-0031](./0031-approver-keys-are-anchored-outside-the-control-plane.md), and rewrote recovery as the post-roster ceremony — with no live approver keys no epoch can be signed, so recovery means a new genesis roster and re-pinning every Pilot.
