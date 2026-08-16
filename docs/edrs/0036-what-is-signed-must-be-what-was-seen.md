---
id: 36
title: "Anchor what is signed and what it claims, not only who may sign"
summary: "A compromised control plane can render one payload and obtain a signature over another. Policy becomes an anchored artefact, the payload carries a signed rendering, and critical signing leaves the browser."
status: accepted
date: 2026-08-16
authors:
  - "Theo Zourzouvillys <theo@sixfathoms.dev>"
tags: [security, identity, foundational]
supersedes: null
superseded_by: null
aliases: []
---

## TL;DR

[EDR-0031](./0031-approver-keys-are-anchored-outside-the-control-plane.md) anchored **who may sign**
outside the control plane. A second panel found that three things stayed control-plane-authored, and
each of them makes an authentic signature attest something other than what everybody believes:

| Still the adversary's to author | Consequence |
|---|---|
| **What is rendered to the signer.** The console is served by the Harbourmaster, and a WebAuthn challenge is an opaque 32-byte digest | Display payload A, challenge over payload B. The approver limb is not forged — it is **induced**, over content of the adversary's choosing |
| **The approval requirement.** `approvals.required` / `eligible` / `chain` are composed by the Harbourmaster before any signature exists | It sets `required: 1` where policy demanded 2, and [EDR-0030](./0030-a-marque-states-its-own-approval-requirement.md)'s binding faithfully protects the wrong number |
| **The executor key mapping.** `cnf.jkt` had no anchored source — [EDR-0034](./0034-the-pilot-api-has-one-authorisation-model.md) reached for "the enrolled principal set distributed with it", which has no schema, author or anchor anywhere | Write your own thumbprint into `cnf.jkt` and every offline check passes |

Three fixes, all reusing the roster's mechanism rather than inventing new ones:

1. **Policy becomes an anchored artefact.** The applied policy version is co-signed by k approvers and
   epoch-chained, exactly as the roster is. A Pilot **recomputes** `approvals.required` and `eligible`
   from it and refuses a marque whose payload disagrees. The requirement is derived, not asserted.
2. **The payload carries a `display`** — a canonical human-readable rendering, covered by every
   signature. A signing client must show it verbatim, and an auditor can re-render it from the
   artefact alone, so a substitution is detectable after the fact.
3. **The roster becomes the *principal* roster.** Entries gain a capability (`approver`, `operator`,
   `agent`), so `cnf.jkt` resolves against the same k-of-n, epoch-chained, genesis-anchored artefact
   as an approver key. EDR-0034's undefined "enrolled principal set" is deleted.

And one correction to a decision made a day earlier: **`require_envelope` and the signing *surface*
are orthogonal, and conflating them made things worse.** Requiring `webauthn` on `critical` targets
pushed the highest-consequence approvals into the browser — the one surface the control plane serves
— and excluded the locally-installed CLI, which displays exactly the bytes it signs. `critical` now
requires a **local signing surface**; the envelope requirement is a separate dial.

## Context

The finding that produced this record is the one worth reading twice, because it defeats a design
that had already been reviewed once and hardened:

> The console is static assets served **from the same origin as the API** — that is, by the
> Harbourmaster ([EDR-0024](./0024-the-console-is-for-deciding.md)). A WebAuthn assertion attests
> **user presence and consent to tap**; it attests nothing about the payload, because the challenge
> is an opaque digest. So a compromised control plane serves modified console assets, renders one
> marque on screen, and calls the authenticator with the digest of a different one.

The signature that comes back is genuine. The key is on the roster. `approvals.required` is satisfied.
`cnf.jkt` verifies. Every offline check added by
[EDR-0029](./0029-the-fast-path-authority-chain.md),
[EDR-0030](./0030-a-marque-states-its-own-approval-requirement.md),
[EDR-0031](./0031-approver-keys-are-anchored-outside-the-control-plane.md) and
[EDR-0032](./0032-a-marque-binds-its-executor-tenant-and-pilot.md) passes **perfectly**, on a payload
no human ever saw.

`SECURITY.md`'s existing limitation — "an approver can approve something they did not read" — does
not cover this, and it would be dishonest to let it seem to. That limitation is about human
diligence. Here diligence does not help: what is read and what is signed are **different objects**.
Nor does [EDR-0024](./0024-the-console-is-for-deciding.md)'s "a stolen console session cannot approve
anything" — true, and irrelevant, because a compromised *server* is not a stolen session.

The design has already reasoned about this exact class and got it right twice elsewhere.
[EDR-0016](./0016-natural-language-delegations-are-compiled.md) makes the grantor sign **the
compilation, not the sentence**, precisely because a human must sign the artefact they can read.
[EDR-0028](./0028-statement-pipeline-and-provider-spi.md) takes the digest **after** transformation
for the same reason. Neither insight was carried to the surface that renders the payload.

The general lesson, which is this record's title: **a signature is only as good as three things — the
key set, the payload the signer actually saw, and the policy that payload claims to satisfy.**
Anchoring one of three is not a third of the property; it is none of it.

## Decision

### 1. Policy is an anchored artefact, and the requirement is derived

[EDR-0015](./0015-policy-is-reviewed-configuration.md) already makes policy reviewed configuration
applied by a signed act. That act now produces an artefact of the same family as the roster:

```jsonc
{ "tenant": "acme", "epoch": 31, "prev": "sha256:…", "policy": { … }, "issued_at": "…" }
```

co-signed by **k approver device keys**, epoch-chained, monotonic, and distributed to Pilots. The
control plane transports it and cannot author it.

A Pilot then **recomputes** the approval requirement for `(target, role, request shape)` from the
policy artefact and **compares it to the payload's `approvals` block**. A mismatch is a refusal, not
a warning. `approvals` stays in the payload — it is what the signatures cover, and
[EDR-0030](./0030-a-marque-states-its-own-approval-requirement.md)'s anti-stripping property depends
on it — but it is now **checked against an independent source** rather than believed.

### 2. The payload carries a signed `display`

```jsonc
"display": "UPDATE 1 row in public.accounts (settings) on prod-primary as settings_writer; fence tier='sandbox'; expires 10:14 UTC; budget 1; approvals 2 of 2"
```

- **Canonical.** Generated by a deterministic renderer from the payload, so it can be re-derived and
  compared. Its rules are versioned; the version is in the payload.
- **Covered by every signature**, since it is part of the payload.
- **A signing client must show it verbatim** and must refuse to sign if it cannot render it.
- **An auditor re-renders it** from the artefact and compares. That is what makes substitution
  detectable after the fact, from the logbook alone, with no cooperation from the control plane.

This does not prevent a compromised renderer from showing something else on screen. It makes the lie
**durable and discoverable**, which is the most a payload-level mechanism can do — the prevention is
mechanism 3.

### 3. Critical signing leaves the browser

`require_envelope` ([EDR-0015](./0015-policy-is-reviewed-configuration.md),
[EDR-0023](./0023-approver-keys-enrolment-and-recovery.md)) governs the **key**: hardware-backed,
user-verified. A new, separate setting governs the **surface**:

| Setting | Meaning |
|---|---|
| `require_envelope` | which signature envelope — `webauthn` or `es256` — the key must use |
| **`signing_surface`** | `any`, or **`local`**: the payload must be rendered and signed by locally-installed code the control plane does not serve |

**`critical` targets default to `signing_surface: local`.** The normative path becomes
`marque approve` in the installed CLI, which renders the canonical `display` it is about to sign. The
console is demoted for those targets to **review-and-hand-off**: it shows the queue, the analysis and
the rehearsal, and hands the approver a request id to sign locally.

A platform authenticator is still available from the CLI, so `signing_surface: local` and
`require_envelope: webauthn` compose — which is the point of separating them. **The mistake being
corrected is that making `webauthn` the `critical` default silently forced the browser**, and the
browser is the surface the adversary serves.

Where a browser path must remain for `critical`, the escape hatch is an explicit acknowledgement plus
a **digest-pinned console bundle whose hash is co-signed into the roster's artefact family** and
verified by installed code before an authenticator prompt may be raised — the console then inherits
the same anchor as everything else, instead of being trusted because it is same-origin.

### 4. The roster is the principal roster

[EDR-0031](./0031-approver-keys-are-anchored-outside-the-control-plane.md)'s entries gain a
capability:

```jsonc
{ "principal": "sam@acme.example", "jkt": "…", "envelope": "webauthn",
  "capabilities": ["approver"], "enrolled_at": "…", "retired_at": null }
```

`operator` and `agent` keys ride the same co-signed, epoch-chained, genesis-anchored artefact. A Pilot
resolves `cnf.jkt` ([EDR-0032](./0032-a-marque-binds-its-executor-tenant-and-pilot.md)) and every
`Rehearse`/`Introspect` submitter signature
([EDR-0034](./0034-the-pilot-api-has-one-authorisation-model.md)) against it.
**EDR-0034's "enrolled principal set distributed with it" is deleted** — it had no schema, no author,
no k-of-n rule and no anchor, and it was the sentence that quietly reopened the oracle that record was
written to close.

This also repairs a claim: [EDR-0029](./0029-the-fast-path-authority-chain.md) said a standing order
with **principal-named** `invokers` is "fully offline-verifiable". It was not — `sub` is a string the
Harbourmaster wrote, and the only cryptographic check was `cnf.jkt`, also unanchored. With the
principal roster it becomes true, and the residual really does narrow to the group case as the
compromise tables claim.

## Consequences

**Easier.**

- The two-signature property finally means what every record has claimed: an authentic approver
  signature now attests **a payload a human saw**, satisfying **a requirement derived from anchored
  policy**, executed by **a principal from an anchored roster**.
- One artefact family — roster, policy, and console bundle if used — with one anchor and one ceremony.
  Nothing new to reason about.
- Post-hoc detection improves independently of prevention: `display` makes a substituted approval
  visible in the logbook to anyone who re-renders it.

**Harder.**

- **`critical` approvals stop being possible from a phone**, which is exactly what
  [EDR-0024](./0024-the-console-is-for-deciding.md) optimised for, and it will lengthen time-in-stage
  on the targets where escalation latency hurts most. The honest framing: the browser was never a
  sound signing surface against an adversary who serves it, and the phone-approval story survives for
  everything below `critical`.
- **Policy becomes a k-of-n ceremony.** Every approval-policy change now needs two approvers to sign
  an epoch, on top of the pull request. That is heavy, and it is the same weight the roster already
  carries for personnel changes.
- **Canonical rendering is a specification with teeth.** Two renderers that disagree produce a
  refusal-to-sign or a false audit mismatch, so the rules and their version need the same care as
  statement canonicalisation.
- The payload grows again, and `display` is the largest field in it.

**New obligations.**

- A test renders payload A, signs a challenge over payload B, and asserts the verifier refuses —
  the specific regression this record exists to prevent.
- A test mints a marque whose `approvals.required` is lower than the anchored policy demands and
  asserts the Pilot refuses.
- The playbook gains: on a suspected control-plane compromise, **re-render `display` for every marque
  signed during the window and compare it to what the logbook says was shown**.

## References

- [EDR-0031](./0031-approver-keys-are-anchored-outside-the-control-plane.md) — anchored *who may
  sign*; this record anchors the other two thirds.
- [EDR-0016](./0016-natural-language-delegations-are-compiled.md) — sign the compilation, not the
  sentence; the same insight, applied earlier and not carried forward.
- [EDR-0030](./0030-a-marque-states-its-own-approval-requirement.md) — the requirement this makes
  derivable rather than asserted.
- [EDR-0024](./0024-the-console-is-for-deciding.md) — the surface this demotes for `critical`.
- [W3C WebAuthn](https://www.w3.org/TR/webauthn-2/) — user verification proves presence, **not**
  agreement to a payload.

## Changelog

- **2026-08-16**: Accepted, following a second expert panel's finding that an authentic approver
  signature could be induced over a payload the approver never saw.
