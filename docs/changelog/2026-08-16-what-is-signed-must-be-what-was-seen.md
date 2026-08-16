---
title: "A second panel: an authentic signature over a payload nobody saw"
tags: [security, docs]
order: 2
---

A second expert panel reviewed the records the first never saw — EDR-0028 through 0035 — plus the
nineteen amendments the first review prompted. It found something that defeats the design *after* it
had already been reviewed once and hardened, and the fix is
[EDR-0036](/edrs/0036-what-is-signed-must-be-what-was-seen/).

### The finding

The console is static assets served **from the same origin as the API** — that is, by the
Harbourmaster. A WebAuthn assertion attests **user presence and consent to tap**; it attests nothing
about the payload, because the challenge is an opaque 32-byte digest.

So a compromised control plane serves modified console assets, renders one marque on screen, and
calls the authenticator with the digest of a *different* one.

The signature that comes back is genuine. The key is on the roster. `approvals.required` is satisfied.
`cnf.jkt` verifies. **Every offline check added by EDR-0029, 0030, 0031 and 0032 passes perfectly** —
on a payload no human ever saw. The approver limb is not forged; it is **induced**.

Two companion findings shared the root cause. `approvals.required` is composed by the Harbourmaster
*before any signature exists*, so the adversary authors the requirement it is bound by. And `cnf.jkt`
had no anchored source at all — [EDR-0034](/edrs/0034-the-pilot-api-has-one-authorisation-model/)
reached for "the enrolled principal set distributed with it", a phrase with no schema, no author, no
k-of-n rule and no genesis root anywhere in 35 records. That one sentence quietly reopened the
extraction oracle that record was written to close.

### The lesson

**A signature is only as good as three things: the key set, the payload the signer actually saw, and
the policy that payload claims to satisfy.**
[EDR-0031](/edrs/0031-approver-keys-are-anchored-outside-the-control-plane/) anchored the first.
Anchoring one of three is not a third of the property — it is none of it.

Galling, because the design had already got this right twice elsewhere.
[EDR-0016](/edrs/0016-natural-language-delegations-are-compiled/) makes a grantor sign **the
compilation, not the sentence**, precisely because a human must sign the artefact they can read.
[EDR-0028](/edrs/0028-statement-pipeline-and-provider-spi/) takes the digest **after** transformation
for the same reason. Neither insight was carried to the surface that renders the payload.

### Changed

- **Policy becomes an anchored artefact**, co-signed by k approvers and epoch-chained like the roster.
  A Pilot **recomputes** the approval requirement and refuses a marque whose payload disagrees.
- **The payload carries a signed `display`** — a canonical rendering shown verbatim to the signer and
  re-renderable by an auditor, so a substitution is detectable from the artefact alone.
- **The roster becomes the *principal* roster.** Entries gain a capability, so operator and agent keys
  ride the same anchored artefact and `cnf.jkt` resolves against something real.
- **`critical` signing leaves the browser.** `signing_surface: local` is now a setting distinct from
  `require_envelope`, defaulting `critical` to the installed CLI, which renders exactly what it signs.

### A regression of ours, corrected

Making `webauthn` the required envelope on `critical` targets — added a day earlier — was **worse than
what it replaced**. It forced the highest-consequence approvals into the browser, the one surface the
control plane serves, and excluded the locally-installed CLI. Envelope governs the *key*; surface
governs *who renders*. They are orthogonal and were conflated.

The cost of the fix is real and is not hidden: **`critical` approvals are no longer possible from a
phone**, which is exactly what [EDR-0024](/edrs/0024-the-console-is-for-deciding/) was optimised for.
The browser was never a sound signing surface against an adversary who serves it.

### Four more criticals from the same panel

Round two also found four execution-layer defects, all confirmed and all now closed:

- **`search_path` was never pinned.** PostgreSQL resolves unqualified relations, functions **and
  operators** through it, so a fence written as `tier = 'sandbox'` can be redefined by anyone able to
  create an object in an earlier schema. The session now pins `search_path = pg_catalog` and a fence
  containing an unqualified non-builtin reference is refused at compile time.
- **A `DEFERRABLE INITIALLY DEFERRED` constraint trigger fires at `COMMIT`** — *after* the write-set
  assertion has read a clean write set — so its writes landed inside the committed transaction
  unchecked, by a mechanism designed to defer until commit. `SET CONSTRAINTS ALL IMMEDIATE` now runs
  immediately before the check.
- **The write-set assertion could not tell "nothing was written" from "the write was not counted".**
  Both read zero, so `track_counts` being off silently degraded the strongest containment in the
  design into a no-op reporting success. It now calibrates against the statement's own `RETURNING`
  count and refuses if the counters disagree.
- **A transform provider would have broken the standing-order fast path**, because
  [EDR-0029](/edrs/0029-the-fast-path-authority-chain/) requires the Pilot to recompute
  `template + binding` offline and match `req`. Transforms no longer run there; tenant scoping on a
  standing order belongs in the signed template.

Plus two majors: a fence may now reference only columns of the target relation (REPEATABLE READ
protects rows *this* transaction writes, not a tenant row a concurrent transaction changes), and
Marque's role must not **own** the logbook table — an owner can grant itself anything, which would
have made the withheld `DELETE` grant decorative.

