---
title: "Expert review found two holes in the signature scheme, and both are now closed"
tags: [security, docs]
---

An expert panel reviewed the design — eight independent lenses, each finding checked twice, once by
someone trying to refute it and once by someone searching the whole corpus for whether it was already
addressed. Two critical findings survived both, and both were real.

They shared a root cause: the **fast paths** — standing-order invocation, a delegation match, a
Surveyor `conforms` — mint a marque with no human present, and no record ever said what fills the
human half of a two-signature artefact on those paths.

### Added

- **[The fast-path authority chain](/edrs/0029-the-fast-path-authority-chain/)** — the marque's
  payload gains an `auth` block naming the artefact that authorised it, and **that artefact travels
  with the marque**. A standing order and a compiled delegation are each already human-signed, so on a
  fast path **the human signed the shape rather than the instance**, and the Pilot verifies that
  artefact offline: its signatures, its digest, the template rebound with the supplied parameters
  against `req`, each parameter against its constraint, and that the marque's limits are within the
  artefact's.
- **[A marque states its own approval requirement](/edrs/0030-a-marque-states-its-own-approval-requirement/)**
  — JWS signature entries are independent, so any holder could **delete an approver signature and the
  rest would still verify**. "At least one approver signature" could not tell a stripped two-approver
  marque from a legitimate single-approver one, which meant a two-person rule was unenforceable
  offline — in exactly the control-plane-down case the design values most. `approvals.required`,
  `eligible` and the escalation-chain digest now live inside the payload every signature covers.

### Changed

- **[EDR-0003](/edrs/0003-federated-identity-and-sender-constrained-tokens/) and
  [EDR-0008](/edrs/0008-standing-orders/) contradicted each other** and neither acknowledged it: one
  required a fresh interactive authentication to sign a marque and said no workload principal could
  ever satisfy it, the other minted marques with no human in the loop. Freshness is now scoped
  explicitly to *producing a human approver signature* — assembling a reference to a signature that
  already exists is not signing.
- **The compromise boundary was overstated in four places.** SECURITY.md, the architecture page, the
  playbook and CLAUDE.md all claimed a compromised control plane "cannot cause any statement to
  execute". The accurate form is **"cannot cause a statement to execute whose shape no human
  signed"**, and all four now say that.
- **A residual is named rather than hidden.** `invokers` may resolve through identity-provider
  groups, which an offline Pilot cannot check — so a compromised control plane could invoke a genuine
  standing order as a principal of its choosing, bounded to that order's approved shape, parameter
  constraints, budget and rate limits. Standing orders on `critical` targets must now name principals
  directly, and the compromise tables list the residual.

### Why this is in the changelog

Because the interesting part is not that the gaps existed — it is that the design *claimed* a
property it did not yet deliver, in four documents, and only an adversarial read found it. The
records are the source of truth precisely so that a claim and its mechanism can be checked against
each other.
